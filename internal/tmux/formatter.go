package tmux

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Render evaluates a template against ctx and returns the rendered string.
// The grammar is documented in the design doc; the public entry is pure and
// callable from tests without any I/O.
func Render(template string, ctx Context) (string, error) {
	r := &renderer{ctx: ctx, depth: 0}
	out, err := r.renderTemplate(template)
	if err != nil {
		return "", fmt.Errorf("tmux: rendering format: %w", err)
	}
	return out, nil
}

// renderer carries the per-call state for template evaluation. It is not safe
// for concurrent use; one renderer per Render call.
type renderer struct {
	ctx   Context
	depth int
}

const maxVariableRecursion = 4

// renderTemplate walks the template string and replaces the three substitution
// kinds: `#[...]` passthrough, `{?cond:then:else}` conditionals, and
// `{name[:mod...]}` variable expansions. Literal text passes through unchanged.
// Escapes: \{ \} \# \$ \\ \n.
func (r *renderer) renderTemplate(template string) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(template) {
		ch := template[i]
		switch ch {
		case '\\':
			// Escape sequence: emit the next byte literally.
			if i+1 < len(template) {
				next := template[i+1]
				switch next {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(next)
				}
				i += 2
				continue
			}
			b.WriteByte(ch)
			i++
		case '#':
			// `#[...]` is tmux-format passthrough; `#(...)` and `#{...}` are
			// passed through verbatim so users can compose with native tmux
			// syntax. A bare `#` is also passthrough since tmux uses it as
			// the format-string escape character itself.
			if i+1 < len(template) && template[i+1] == '[' {
				end := indexOfMatchingBracket(template, i+1, '[', ']')
				if end < 0 {
					return "", fmt.Errorf("unterminated #[ at offset %d", i)
				}
				inner := template[i+2 : end]
				b.WriteString(r.renderTmuxAttrs(inner))
				i = end + 1
				continue
			}
			if i+1 < len(template) && (template[i+1] == '(' || template[i+1] == '{') {
				open := template[i+1]
				closer := byte(')')
				if open == '{' {
					closer = '}'
				}
				end := indexOfMatchingBracket(template, i+1, rune(open), rune(closer))
				if end < 0 {
					return "", fmt.Errorf("unterminated #%c at offset %d", open, i)
				}
				b.WriteString(template[i : end+1])
				i = end + 1
				continue
			}
			b.WriteByte(ch)
			i++
		case '{':
			end := indexOfMatchingBracket(template, i, '{', '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated { at offset %d", i)
			}
			expr := template[i+1 : end]
			rendered, err := r.renderExpr(expr)
			if err != nil {
				return "", err
			}
			b.WriteString(rendered)
			i = end + 1
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return b.String(), nil
}

// renderExpr handles the body of a `{...}` block: either `?cond:then:else` or
// `name[:mod[:arg]...]`. Nested `{}` inside then/else is supported via the
// bracket-aware splitter.
func (r *renderer) renderExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	if strings.HasPrefix(expr, "?") {
		return r.renderConditional(expr[1:])
	}
	parts := splitTopLevel(expr, ':')
	name := strings.TrimSpace(parts[0])
	value, ok := r.resolve(name)
	if !ok {
		// Run modifiers even on empty values so :default can substitute.
		value = ""
	}
	value = sanitizeUserValue(value)
	mods, err := parseModifiers(parts[1:])
	if err != nil {
		return "", err
	}
	for _, m := range mods {
		value, err = r.applyModifier(name, value, m.op, m.args)
		if err != nil {
			return "", err
		}
	}
	return value, nil
}

// modifierCall captures one parsed modifier and its consumed argument tokens.
type modifierCall struct {
	op   string
	args []string
}

// modifierArity maps known modifier names to the maximum number of `:arg`
// tokens they consume after their name. The parser uses this table to walk
// the colon-separated mod list and group args correctly, so chained
// expressions like `{x:money:1:trunc:4}` parse as money(1) then trunc(4).
var modifierArity = map[string]int{
	"short":    0,
	"long":     0,
	"color":    0,
	"icon":     0,
	"tokens":   0,
	"duration": 0,
	"upper":    0,
	"lower":    0,
	"money":    1,
	"pct":      1,
	"bar":      1,
	"trunc":    1,
	"default":  1,
	"pad":      2,
}

// parseModifiers groups the colon-delimited modifier stream into call records
// using the arity table. Unknown modifier names are treated as 0-arity so the
// renderer can return a helpful "unknown modifier" error later.
func parseModifiers(parts []string) ([]modifierCall, error) {
	out := make([]modifierCall, 0, len(parts))
	i := 0
	for i < len(parts) {
		op := strings.ToLower(strings.TrimSpace(parts[i]))
		i++
		arity, known := modifierArity[op]
		if !known {
			return nil, fmt.Errorf("unknown modifier %q", op)
		}
		call := modifierCall{op: op}
		// Greedily take up to `arity` args. Stop early if we hit another
		// known modifier name (so `{x:pad:5:upper}` parses pad(5) + upper
		// rather than pad(5, upper)).
		for j := 0; j < arity && i < len(parts); j++ {
			if _, isMod := modifierArity[strings.ToLower(strings.TrimSpace(parts[i]))]; isMod {
				break
			}
			call.args = append(call.args, parts[i])
			i++
		}
		out = append(out, call)
	}
	return out, nil
}

// renderConditional evaluates a `?cond:then:else` body. cond is a variable
// name (truthy if non-empty and not "0" or "0.00"). then and else may contain
// nested `{...}` and `#[...]` and are rendered recursively.
func (r *renderer) renderConditional(body string) (string, error) {
	parts := splitTopLevel(body, ':')
	if len(parts) < 2 {
		return "", fmt.Errorf("conditional %q needs at least ?cond:then", body)
	}
	cond := strings.TrimSpace(parts[0])
	value, _ := r.resolve(cond)
	truthy := isTruthy(value)
	branch := parts[1]
	if !truthy {
		if len(parts) >= 3 {
			branch = strings.Join(parts[2:], ":")
		} else {
			return "", nil
		}
	} else if len(parts) > 2 {
		branch = parts[1]
	}
	return r.renderTemplate(branch)
}

// resolve performs the variable lookup chain described in the design doc:
// user variables → built-in segment → snapshot metric → semantic alias →
// synthetic. ThemeRefs are *not* consulted here; they live inside `#[...]`
// only.
func (r *renderer) resolve(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	// 1. User-defined variables (recursive, depth-capped).
	if r.depth < maxVariableRecursion {
		if tmpl, ok := r.ctx.Variables[name]; ok {
			r.depth++
			out, err := r.renderTemplate(tmpl)
			r.depth--
			if err == nil {
				return out, true
			}
		}
	}
	// 2. Built-in segments (or user-defined segment overrides).
	if v, ok := r.expandSegment(name); ok {
		return v, true
	}
	// 3. Provider-native metric (Metrics[name].Used).
	if v, ok := metricUsedString(r.ctx.Snapshot, name); ok {
		return v, true
	}
	// 4. Semantic alias.
	if r.ctx.Provider != "" {
		if key := resolveAlias(name, r.ctx.Provider); key != "" {
			if strings.HasPrefix(key, "_") {
				if v, ok := r.ctx.Synthetic[key]; ok && v != "" {
					return v, true
				}
				return "", false
			}
			if v, ok := metricUsedString(r.ctx.Snapshot, key); ok {
				return v, true
			}
		}
	}
	// 5. Bare-name access to common snapshot attributes for ergonomic
	// templates (e.g. {tool}, {model}, {account}).
	switch name {
	case "tool", "provider":
		return r.ctx.Provider, r.ctx.Provider != ""
	case "account":
		return r.ctx.Account, r.ctx.Account != ""
	case "model":
		if v := r.ctx.Snapshot.Attributes["model"]; v != "" {
			return v, true
		}
	}
	return "", false
}

// expandSegment returns the value of a named segment. User-defined segments
// (from settings.tmux.segments) override built-ins so users can rewire any
// segment without losing access to the rest of the registry.
func (r *renderer) expandSegment(name string) (string, bool) {
	if tmpl, ok := r.ctx.Segments[name]; ok {
		if r.depth < maxVariableRecursion {
			r.depth++
			out, err := r.renderTemplate(tmpl)
			r.depth--
			if err == nil {
				return out, true
			}
		}
	}
	if fn, ok := builtinSegments()[name]; ok {
		return fn(r), true
	}
	return "", false
}

// applyModifier applies one parsed modifier call to value. op has already
// been lowercased and args holds the consumed argument tokens (0..arity).
func (r *renderer) applyModifier(varName, value, op string, args []string) (string, error) {
	arg0 := ""
	if len(args) > 0 {
		arg0 = args[0]
	}
	switch op {
	case "short":
		return modShort(value), nil
	case "long":
		return modLong(varName, value), nil
	case "money":
		return modMoney(value, arg0), nil
	case "pct":
		return modPct(value, arg0), nil
	case "bar":
		return modBar(value, arg0, r.ctx.Glyphs), nil
	case "color":
		return modColor(varName, value, r.ctx), nil
	case "icon":
		return ProviderIcon(value, r.ctx.Glyphs), nil
	case "tokens":
		return modTokens(value), nil
	case "duration":
		return modDuration(value), nil
	case "upper":
		return strings.ToUpper(value), nil
	case "lower":
		return strings.ToLower(value), nil
	case "trunc":
		return modTrunc(value, arg0), nil
	case "pad":
		return modPad(value, args), nil
	case "default":
		if strings.TrimSpace(value) == "" {
			return arg0, nil
		}
		return value, nil
	}
	return value, fmt.Errorf("unknown modifier %q", op)
}

// renderTmuxAttrs renders the body of a `#[...]` block. Inside the block,
// `$name` is resolved against ThemeRefs; everything else is passed through.
// When ColorMode is none, the entire directive is suppressed.
func (r *renderer) renderTmuxAttrs(body string) string {
	if r.ctx.ColorMode == ColorModeNone {
		return ""
	}
	expanded := expandThemeRefs(body, r.ctx.ThemeRefs)
	return "#[" + expanded + "]"
}

// expandThemeRefs replaces $name tokens with their resolved color value. An
// unresolved $name is dropped to avoid emitting an invalid tmux directive.
func expandThemeRefs(body string, refs map[string]string) string {
	if !strings.Contains(body, "$") {
		return body
	}
	var b strings.Builder
	i := 0
	for i < len(body) {
		if body[i] == '\\' && i+1 < len(body) {
			b.WriteByte(body[i+1])
			i += 2
			continue
		}
		if body[i] != '$' {
			b.WriteByte(body[i])
			i++
			continue
		}
		// Read identifier.
		j := i + 1
		for j < len(body) && (isIdentChar(body[j])) {
			j++
		}
		name := body[i+1 : j]
		if name == "" {
			b.WriteByte('$')
			i++
			continue
		}
		if v, ok := refs[strings.ToLower(name)]; ok {
			b.WriteString(v)
		}
		i = j
	}
	return b.String()
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// sanitizeUserValue defangs raw `#` characters in variable values so they
// cannot be interpreted as tmux format escapes. Each `#` becomes `##`.
func sanitizeUserValue(v string) string {
	if !strings.Contains(v, "#") {
		return v
	}
	return strings.ReplaceAll(v, "#", "##")
}

// metricUsedString returns the `Used` value for the named metric formatted as
// a numeric string (or empty when the metric is missing or has no Used value).
// Float values keep a "%g" representation so downstream modifiers can choose a
// presentation; integers come back without a decimal point.
func metricUsedString(snap core.UsageSnapshot, key string) (string, bool) {
	if snap.Metrics == nil {
		return "", false
	}
	m, ok := snap.Metrics[key]
	if !ok {
		return "", false
	}
	if m.Used == nil {
		return "", false
	}
	v := *m.Used
	if math.Trunc(v) == v && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10), true
	}
	return strconv.FormatFloat(v, 'f', -1, 64), true
}

// isTruthy implements the `{?cond:...}` truth test: non-empty AND not "0"
// AND not "0.00" / "0.0".
func isTruthy(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if v == "0" {
		return false
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n == 0 {
		return false
	}
	return true
}

// indexOfMatchingBracket returns the index of the closing bracket that
// balances the opener at start. Returns -1 if no balanced close exists. The
// scan respects backslash escapes so `\}` does not close a `{`.
func indexOfMatchingBracket(s string, start int, open, close rune) int {
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if rune(s[i]) == open {
			depth++
		} else if rune(s[i]) == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevel splits s on sep, ignoring sep that appears inside `{}` or
// `#[...]` blocks. This is what makes `{?cond:#[fg=$red]hot#[default]:cool}`
// parse correctly.
func splitTopLevel(s string, sep byte) []string {
	parts := []string{}
	depth := 0
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '\\':
			i += 2
			continue
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case '#':
			if i+1 < len(s) && s[i+1] == '[' {
				end := indexOfMatchingBracket(s, i+1, '[', ']')
				if end > 0 {
					i = end + 1
					continue
				}
			}
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
				i++
				continue
			}
		}
		i++
	}
	parts = append(parts, s[start:])
	return parts
}

// --- modifier implementations -------------------------------------------------

func modShort(v string) string {
	f, ok := parseFloat(v)
	if !ok {
		return v
	}
	return fmt.Sprintf("$%.2f", f)
}

func modLong(varName, v string) string {
	short := modShort(v)
	switch varName {
	case "today_cost":
		return short + " today"
	case "block_cost":
		return short + " block"
	case "burn_rate":
		return short + "/hr"
	}
	return short
}

func modMoney(v, arg string) string {
	f, ok := parseFloat(v)
	if !ok {
		return v
	}
	prec := 2
	if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil && n >= 0 && n <= 8 {
		prec = n
	}
	return fmt.Sprintf("$%.*f", prec, f)
}

func modPct(v, arg string) string {
	f, ok := parseFloat(v)
	if !ok {
		return v
	}
	prec := 0
	if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil && n >= 0 && n <= 4 {
		prec = n
	}
	return fmt.Sprintf("%.*f%%", prec, f)
}

func modBar(v, arg string, tier GlyphTier) string {
	f, ok := parseFloat(v)
	if !ok {
		return v
	}
	if f < 0 {
		f = 0
	}
	if f > 100 {
		f = 100
	}
	width := 8
	if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil && n > 0 && n <= 64 {
		width = n
	}
	filled := int(math.Round(float64(width) * f / 100))
	if filled > width {
		filled = width
	}
	full, empty := barGlyphs(tier)
	return strings.Repeat(full, filled) + strings.Repeat(empty, width-filled)
}

func modColor(varName, value string, ctx Context) string {
	if ctx.ColorMode == ColorModeNone {
		return value
	}
	rule := lookupColorRule(varName, ctx)
	f, ok := parseFloat(value)
	if !ok {
		return value
	}
	var hex string
	switch {
	case f >= rule.HighAt:
		hex = resolveColorRef(rule.HighColor, ctx)
	case f >= rule.MediumAt:
		hex = resolveColorRef(rule.MediumColor, ctx)
	default:
		hex = resolveColorRef(rule.LowColor, ctx)
	}
	if hex == "" {
		return value
	}
	resolved := colorForMode(hex, ctx.ColorMode)
	return "#[fg=" + resolved + "]" + value + "#[default]"
}

// lookupColorRule returns the threshold rule for a variable. Users override
// via settings.tmux.color_rules; otherwise variable-specific defaults apply.
func lookupColorRule(varName string, ctx Context) ColorRule {
	if r, ok := ctx.ColorRules[varName]; ok {
		return r.withDefaults(ctx)
	}
	return defaultColorRule(varName, ctx)
}

func defaultColorRule(varName string, ctx Context) ColorRule {
	switch varName {
	case "block_pct", "context_pct", "plan_pct":
		return ColorRule{
			LowAt: 0, MediumAt: 70, HighAt: 90,
			LowColor:    pickColor(ctx.Theme.Green, "#59D4A0"),
			MediumColor: pickColor(ctx.Theme.Yellow, "#F0C75E"),
			HighColor:   pickColor(ctx.Theme.Red, "#F06A7A"),
		}
	}
	return ColorRule{
		LowAt: 0, MediumAt: 50, HighAt: 80,
		LowColor:    pickColor(ctx.Theme.Green, "#59D4A0"),
		MediumColor: pickColor(ctx.Theme.Yellow, "#F0C75E"),
		HighColor:   pickColor(ctx.Theme.Red, "#F06A7A"),
	}
}

func pickColor(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func resolveColorRef(ref string, ctx Context) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "$") {
		name := strings.ToLower(strings.TrimPrefix(ref, "$"))
		if v, ok := ctx.ThemeRefs[name]; ok {
			return v
		}
		return ""
	}
	return ref
}

// withDefaults fills in any zero fields from the variable-specific defaults so
// users can override only one color at a time.
func (rule ColorRule) withDefaults(ctx Context) ColorRule {
	def := defaultColorRule("", ctx)
	if rule.HighAt == 0 {
		rule.HighAt = def.HighAt
	}
	if rule.MediumAt == 0 {
		rule.MediumAt = def.MediumAt
	}
	if rule.LowColor == "" {
		rule.LowColor = def.LowColor
	}
	if rule.MediumColor == "" {
		rule.MediumColor = def.MediumColor
	}
	if rule.HighColor == "" {
		rule.HighColor = def.HighColor
	}
	return rule
}

// ColorRule is the formatter's local mirror of config.ColorRule so the
// internal/tmux package does not import internal/config (which would create a
// reverse-direction dependency for downstream readers).
type ColorRule struct {
	LowAt       float64
	MediumAt    float64
	HighAt      float64
	LowColor    string
	MediumColor string
	HighColor   string
}

func modTokens(v string) string {
	f, ok := parseFloat(v)
	if !ok {
		return v
	}
	switch {
	case f >= 1_000_000:
		return fmt.Sprintf("%.1fM", f/1e6)
	case f >= 1_000:
		return fmt.Sprintf("%.0fk", f/1e3)
	default:
		return fmt.Sprintf("%.0f", f)
	}
}

func modDuration(v string) string {
	if v == "" {
		return ""
	}
	// Already-formatted durations (the synthetic _block_remaining writes
	// "2h17m" directly) pass through unchanged.
	if strings.ContainsAny(v, "hms") {
		return v
	}
	if d, err := time.ParseDuration(v); err == nil {
		return fmtDurationDefault(d)
	}
	// Seconds-as-number fallback.
	if f, ok := parseFloat(v); ok {
		return fmtDurationDefault(time.Duration(f * float64(time.Second)))
	}
	return v
}

func modTrunc(v, arg string) string {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n <= 0 {
		return v
	}
	if len([]rune(v)) <= n {
		return v
	}
	runes := []rune(v)
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

func modPad(v string, args []string) string {
	if len(args) == 0 {
		return v
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n <= 0 {
		return v
	}
	side := "r"
	if len(args) > 1 {
		side = strings.ToLower(strings.TrimSpace(args[1]))
	}
	cur := len([]rune(v))
	if cur >= n {
		return v
	}
	pad := strings.Repeat(" ", n-cur)
	if side == "l" {
		return pad + v
	}
	return v + pad
}

// parseFloat is the formatter's input-tolerant numeric parser. It strips a
// leading "$", "%" sign, and any trailing units (e.g. "/hr") before parsing.
func parseFloat(v string) (float64, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "%")
	// Strip a trailing "/hr", "/min", etc.
	if i := strings.Index(s, "/"); i > 0 {
		s = s[:i]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
