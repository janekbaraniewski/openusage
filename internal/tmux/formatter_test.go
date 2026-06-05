package tmux

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// newTestContext returns a Context populated with a Claude Code snapshot
// resembling what BuildContext produces after a typical render path. It is
// the canonical fixture for table-driven tests below.
func newTestContext() Context {
	used := func(v float64) *float64 { return &v }
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "default",
		Metrics: map[string]core.Metric{
			"today_api_cost":  {Used: used(4.21), Unit: "USD"},
			"5h_block_cost":   {Used: used(3.40), Unit: "USD"},
			"usage_five_hour": {Used: used(47.0), Unit: "%"},
			"requests_today":  {Used: used(42.0), Unit: "requests"},
		},
		Attributes: map[string]string{
			"model": "Opus 4.7 Sonnet long-name",
		},
	}
	return Context{
		Provider:     "claude_code",
		Account:      "default",
		Snapshot:     snap,
		AllSnapshots: []core.UsageSnapshot{snap},
		Synthetic: map[string]string{
			"_block_burn_rate":  "$1.20",
			"_block_remaining":  "2h17m",
			"_block_projection": "$9.40",
			"_context_pct":      "42",
			"_context_tokens":   "84000",
		},
		ThemeRefs: map[string]string{
			"accent": "#FF6600",
			"base":   "#050B24",
			"green":  "#59D4A0",
			"red":    "#F06A7A",
			"peach":  "#F09860",
			"yellow": "#F0C75E",
		},
		Theme: ThemeColors{
			Accent: "#FF6600", Base: "#050B24",
			Green: "#59D4A0", Red: "#F06A7A", Peach: "#F09860", Yellow: "#F0C75E",
		},
		Now:       time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC),
		ColorMode: ColorModeTruecolor,
		Glyphs:    GlyphTierUnicode,
	}
}

func renderOrFail(t *testing.T, ctx Context, tmpl string) string {
	t.Helper()
	out, err := Render(tmpl, ctx)
	if err != nil {
		t.Fatalf("Render(%q): %v", tmpl, err)
	}
	return out
}

func TestRender_LiteralPassesThrough(t *testing.T) {
	ctx := newTestContext()
	if got := renderOrFail(t, ctx, "hello world"); got != "hello world" {
		t.Errorf("got %q", got)
	}
}

func TestRender_EmptyTemplate(t *testing.T) {
	if got := renderOrFail(t, newTestContext(), ""); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

func TestRender_VariableExpansion(t *testing.T) {
	ctx := newTestContext()
	cases := []struct {
		tmpl string
		want string
	}{
		{"{tool}", "claude_code"},
		{"{account}", "default"},
		{"{model}", "Opus 4.7 Sonnet long-name"},
		{"{today_cost}", "4.21"},
		{"{block_cost}", "3.4"},
		{"{block_pct}", "47"},
		{"{burn_rate}", "$1.20"},
		{"{block_remaining}", "2h17m"},
		{"{context_pct}", "42"},
		{"{unknown_var}", ""},
	}
	for _, c := range cases {
		t.Run(c.tmpl, func(t *testing.T) {
			if got := renderOrFail(t, ctx, c.tmpl); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestModifier_Short(t *testing.T) {
	ctx := newTestContext()
	if got := renderOrFail(t, ctx, "{today_cost:short}"); got != "$4.21" {
		t.Errorf("got %q", got)
	}
}

func TestModifier_Long(t *testing.T) {
	ctx := newTestContext()
	if got := renderOrFail(t, ctx, "{today_cost:long}"); got != "$4.21 today" {
		t.Errorf("got %q", got)
	}
	if got := renderOrFail(t, ctx, "{burn_rate:long}"); got != "$1.20/hr" {
		t.Errorf("burn_rate: got %q", got)
	}
}

func TestModifier_Money(t *testing.T) {
	ctx := newTestContext()
	if got := renderOrFail(t, ctx, "{today_cost:money}"); got != "$4.21" {
		t.Errorf("default precision: got %q", got)
	}
	if got := renderOrFail(t, ctx, "{today_cost:money:1}"); got != "$4.2" {
		t.Errorf("precision 1: got %q", got)
	}
	if got := renderOrFail(t, ctx, "{today_cost:money:0}"); got != "$4" {
		t.Errorf("precision 0: got %q", got)
	}
}

func TestModifier_Pct(t *testing.T) {
	ctx := newTestContext()
	if got := renderOrFail(t, ctx, "{block_pct:pct}"); got != "47%" {
		t.Errorf("default precision: got %q", got)
	}
	if got := renderOrFail(t, ctx, "{block_pct:pct:1}"); got != "47.0%" {
		t.Errorf("precision 1: got %q", got)
	}
}

func TestModifier_Bar(t *testing.T) {
	ctx := newTestContext()
	// 50% of width 10 = 5 filled, 5 empty.
	ctx.Synthetic["_block_burn_rate"] = "" // not used
	pct50 := func() Context {
		c := newTestContext()
		used := 50.0
		c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &used, Unit: "%"}
		return c
	}()
	out := renderOrFail(t, pct50, "{block_pct:bar:10}")
	if len([]rune(out)) != 10 {
		t.Errorf("bar width: got %d, want 10 runes (%q)", len([]rune(out)), out)
	}
	if strings.Count(out, "▓") != 5 {
		t.Errorf("expected 5 filled glyphs, got %q", out)
	}
}

func TestModifier_BarClamps(t *testing.T) {
	c := newTestContext()
	over := 250.0
	c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &over, Unit: "%"}
	out := renderOrFail(t, c, "{block_pct:bar:5}")
	// >100 clamps to fully filled bar (5 cells).
	if strings.Count(out, "▓") != 5 {
		t.Errorf("bar clamp: %q", out)
	}
}

func TestModifier_BarASCIITier(t *testing.T) {
	c := newTestContext()
	c.Glyphs = GlyphTierASCII
	half := 50.0
	c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &half, Unit: "%"}
	out := renderOrFail(t, c, "{block_pct:bar:4}")
	if out != "##.." {
		t.Errorf("ascii bar: got %q want %q", out, "##..")
	}
}

func TestModifier_Color_Threshold(t *testing.T) {
	c := newTestContext()
	// 95% (>= HighAt=90) should pick red.
	high := 95.0
	c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &high, Unit: "%"}
	out := renderOrFail(t, c, "{block_pct:color}")
	if !strings.Contains(out, "fg=#F06A7A") {
		t.Errorf("expected red threshold, got %q", out)
	}
	// 30% should pick green.
	low := 30.0
	c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &low, Unit: "%"}
	out = renderOrFail(t, c, "{block_pct:color}")
	if !strings.Contains(out, "fg=#59D4A0") {
		t.Errorf("expected green threshold, got %q", out)
	}
}

func TestModifier_Color_NoneMode(t *testing.T) {
	c := newTestContext()
	c.ColorMode = ColorModeNone
	out := renderOrFail(t, c, "{block_pct:color}")
	if strings.Contains(out, "#[") {
		t.Errorf("none mode should not emit directives: %q", out)
	}
}

func TestModifier_Icon(t *testing.T) {
	c := newTestContext()
	c.Glyphs = GlyphTierASCII
	out := renderOrFail(t, c, "{tool:icon}")
	if out != "[claude]" {
		t.Errorf("ascii icon: got %q", out)
	}
}

func TestModifier_Tokens(t *testing.T) {
	c := newTestContext()
	out := renderOrFail(t, c, "{context_tokens:tokens}")
	if out != "84k" {
		t.Errorf("got %q want %q", out, "84k")
	}
	big := 1_500_000.0
	c.Snapshot.Metrics["custom_big"] = core.Metric{Used: &big}
	if got := renderOrFail(t, c, "{custom_big:tokens}"); got != "1.5M" {
		t.Errorf("M scale: got %q", got)
	}
	small := 250.0
	c.Snapshot.Metrics["custom_small"] = core.Metric{Used: &small}
	if got := renderOrFail(t, c, "{custom_small:tokens}"); got != "250" {
		t.Errorf("small: got %q", got)
	}
}

func TestModifier_Duration(t *testing.T) {
	c := newTestContext()
	// Already-formatted passes through.
	if got := renderOrFail(t, c, "{block_remaining:duration}"); got != "2h17m" {
		t.Errorf("preformatted: got %q", got)
	}
	// Numeric seconds case via custom variable.
	c.Variables = map[string]string{"secs": "7320"}
	if got := renderOrFail(t, c, "{secs:duration}"); got != "2h02m" {
		t.Errorf("seconds: got %q want 2h02m", got)
	}
}

func TestModifier_UpperLower(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{tool:upper}"); got != "CLAUDE_CODE" {
		t.Errorf("upper: got %q", got)
	}
	if got := renderOrFail(t, c, "{tool:lower}"); got != "claude_code" {
		t.Errorf("lower: got %q", got)
	}
}

func TestModifier_Trunc(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{model:trunc:12}"); got != "Opus 4.7 So…" {
		t.Errorf("trunc 12: got %q", got)
	}
	// Shorter than length: unchanged.
	if got := renderOrFail(t, c, "{tool:trunc:99}"); got != "claude_code" {
		t.Errorf("no-op trunc: got %q", got)
	}
}

func TestModifier_Pad(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "ab"}
	if got := renderOrFail(t, c, "{x:pad:5}"); got != "ab   " {
		t.Errorf("right pad: got %q", got)
	}
	if got := renderOrFail(t, c, "{x:pad:5:l}"); got != "   ab" {
		t.Errorf("left pad: got %q", got)
	}
	// Already long enough.
	if got := renderOrFail(t, c, "{tool:pad:3}"); got != "claude_code" {
		t.Errorf("no-op pad: got %q", got)
	}
}

func TestModifier_Default(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{unknown_var:default:n/a}"); got != "n/a" {
		t.Errorf("default empty: got %q", got)
	}
	if got := renderOrFail(t, c, "{tool:default:n/a}"); got != "claude_code" {
		t.Errorf("default non-empty: got %q", got)
	}
}

func TestModifier_Chain(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{tool:upper:trunc:4}"); got != "CLA…" {
		t.Errorf("chained: got %q", got)
	}
}

func TestConditional_Truthy(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{?today_cost:has cost}"); got != "has cost" {
		t.Errorf("got %q", got)
	}
}

func TestConditional_Falsy(t *testing.T) {
	c := newTestContext()
	// missing variable
	if got := renderOrFail(t, c, "{?missing:then:else}"); got != "else" {
		t.Errorf("else: got %q", got)
	}
	if got := renderOrFail(t, c, "{?missing:then}"); got != "" {
		t.Errorf("no else: got %q", got)
	}
}

func TestConditional_ZeroIsFalsy(t *testing.T) {
	c := newTestContext()
	zero := 0.0
	c.Snapshot.Metrics["today_api_cost"] = core.Metric{Used: &zero, Unit: "USD"}
	if got := renderOrFail(t, c, "{?today_cost:y:n}"); got != "n" {
		t.Errorf("zero falsy: got %q", got)
	}
}

func TestConditional_NestedBraces(t *testing.T) {
	c := newTestContext()
	got := renderOrFail(t, c, "{?today_cost:{today_cost:money}}")
	if got != "$4.21" {
		t.Errorf("nested: got %q", got)
	}
}

func TestPassthrough_TmuxFormat(t *testing.T) {
	c := newTestContext()
	// `#[fg=$accent]` resolves $accent to the theme hex.
	got := renderOrFail(t, c, "#[fg=$accent]x#[default]")
	if !strings.Contains(got, "#[fg=#FF6600]") {
		t.Errorf("theme ref: got %q", got)
	}
	if !strings.Contains(got, "#[default]") {
		t.Errorf("default reset: got %q", got)
	}
}

func TestPassthrough_TmuxFormat_UnknownThemeRef(t *testing.T) {
	c := newTestContext()
	got := renderOrFail(t, c, "#[fg=$notathing,bold]x")
	// Unknown $name is dropped; we keep the rest of the directive.
	if !strings.Contains(got, "#[fg=,bold]") {
		t.Errorf("unknown $name: got %q", got)
	}
}

func TestPassthrough_TmuxNative(t *testing.T) {
	c := newTestContext()
	// `#(...)` and `#{...}` pass through untouched.
	got := renderOrFail(t, c, "#(date +%H:%M) #{pane_current_command}")
	if got != "#(date +%H:%M) #{pane_current_command}" {
		t.Errorf("native passthrough: got %q", got)
	}
}

func TestEscape_Braces(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, `\{tool\}`); got != "{tool}" {
		t.Errorf("escaped braces: got %q", got)
	}
}

func TestEscape_Hash(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, `\#literal`); got != "#literal" {
		t.Errorf("escaped hash: got %q", got)
	}
}

func TestEscape_Newline(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, `a\nb`); got != "a\nb" {
		t.Errorf("newline: got %q", got)
	}
}

func TestEscape_Backslash(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, `\\`); got != `\` {
		t.Errorf("escaped backslash: got %q", got)
	}
}

func TestSanitize_HashInUserValue(t *testing.T) {
	c := newTestContext()
	c.Snapshot.Attributes["model"] = "model#2"
	out := renderOrFail(t, c, "{model}")
	// Every `#` doubled to defang tmux format substitution.
	if out != "model##2" {
		t.Errorf("sanitize: got %q want model##2", out)
	}
}

func TestUserVariable_Substitution(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{
		"greeting": "hi {tool}",
	}
	if got := renderOrFail(t, c, "{greeting}"); got != "hi claude_code" {
		t.Errorf("got %q", got)
	}
}

func TestUserVariable_RecursionDepthCap(t *testing.T) {
	c := newTestContext()
	// Create a self-referencing variable; depth cap (4) prevents infinite
	// loop and ultimately resolves to empty.
	c.Variables = map[string]string{"loop": "{loop}"}
	// Should not hang and should return without error.
	out, err := Render("{loop}", c)
	if err != nil {
		t.Fatalf("recursion cap: %v", err)
	}
	if out != "" {
		t.Errorf("loop should resolve to empty after depth cap, got %q", out)
	}
}

func TestResolutionOrder_UserVarBeatsBuiltin(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"cost": "OVERRIDE"}
	if got := renderOrFail(t, c, "{cost}"); got != "OVERRIDE" {
		t.Errorf("user var should win over builtin segment: got %q", got)
	}
}

func TestResolutionOrder_SegmentBeatsMetric(t *testing.T) {
	c := newTestContext()
	// `cost` is a built-in segment that returns modShort(today_cost). The
	// snapshot has no bare `cost` metric, so the segment should resolve.
	if got := renderOrFail(t, c, "{cost}"); got != "$4.21" {
		t.Errorf("got %q", got)
	}
}

func TestResolutionOrder_AliasFallback(t *testing.T) {
	c := newTestContext()
	// `today_cost` alias maps to claude_code's `today_api_cost`.
	if got := renderOrFail(t, c, "{today_cost}"); got != "4.21" {
		t.Errorf("got %q", got)
	}
}

func TestUnterminatedBrace_Errors(t *testing.T) {
	_, err := Render("{tool", newTestContext())
	if err == nil {
		t.Errorf("expected error for unterminated brace")
	}
}

func TestUnterminatedTmuxFormat_Errors(t *testing.T) {
	_, err := Render("#[fg=red", newTestContext())
	if err == nil {
		t.Errorf("expected error for unterminated #[")
	}
}

func TestUnknownModifier_Errors(t *testing.T) {
	_, err := Render("{tool:no_such_mod}", newTestContext())
	if err == nil {
		t.Errorf("expected error for unknown modifier")
	}
}

func TestCompactPreset_EndToEnd(t *testing.T) {
	c := newTestContext()
	tmpl := "{tool:icon} {block_pct:pct:color} {today_cost:money}"
	out := renderOrFail(t, c, tmpl)
	if !strings.Contains(out, "47%") || !strings.Contains(out, "$4.21") {
		t.Errorf("compact preset: %q", out)
	}
	// 47% sits in MediumAt=70 band (between LowAt=0 and MediumAt=70), so it
	// should pick low/green.
	if !strings.Contains(out, "fg=#59D4A0") {
		t.Errorf("expected green threshold in: %q", out)
	}
}

func TestColorMode256_DowngradesHexInsideTmuxFormat(t *testing.T) {
	c := newTestContext()
	c.ColorMode = ColorMode256
	c.ThemeRefs = ThemeRefs(c.Theme, ColorMode256)
	out := renderOrFail(t, c, "#[fg=$accent]x#[default]")
	if !strings.Contains(out, "fg=colour") {
		t.Errorf("256 mode should emit colour###: %q", out)
	}
}

func TestColorModeNone_StripsAllDirectives(t *testing.T) {
	c := newTestContext()
	c.ColorMode = ColorModeNone
	out := renderOrFail(t, c, "#[fg=$accent]x#[default]")
	if strings.Contains(out, "#[") {
		t.Errorf("none: should strip directives, got %q", out)
	}
	if out != "x" {
		t.Errorf("none: text leaked, got %q", out)
	}
}

func TestSegment_Cost(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{cost}"); got != "$4.21" {
		t.Errorf("cost: %q", got)
	}
}

func TestSegment_Block(t *testing.T) {
	c := newTestContext()
	out := renderOrFail(t, c, "{block}")
	if !strings.Contains(out, "$3.40 block") || !strings.Contains(out, "2h17m") {
		t.Errorf("block: %q", out)
	}
}

func TestSegment_BlockWithoutRemaining(t *testing.T) {
	c := newTestContext()
	delete(c.Synthetic, "_block_remaining")
	if got := renderOrFail(t, c, "{block}"); got != "$3.40 block" {
		t.Errorf("block-no-remaining: %q", got)
	}
}

func TestSegment_BlockEmpty(t *testing.T) {
	c := newTestContext()
	delete(c.Snapshot.Metrics, "5h_block_cost")
	if got := renderOrFail(t, c, "{block}"); got != "" {
		t.Errorf("block-empty: %q", got)
	}
}

func TestSegment_Burn(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{burn}"); got != "$1.20/hr" {
		t.Errorf("burn: %q", got)
	}
}

func TestSegment_BurnEmpty(t *testing.T) {
	c := newTestContext()
	c.Synthetic["_block_burn_rate"] = ""
	if got := renderOrFail(t, c, "{burn}"); got != "" {
		t.Errorf("burn-empty: %q", got)
	}
}

func TestSegment_Tokens(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{tokens}"); got != "84k" {
		t.Errorf("tokens segment: %q", got)
	}
}

func TestSegment_TokensEmpty(t *testing.T) {
	c := newTestContext()
	c.Synthetic["_context_tokens"] = ""
	if got := renderOrFail(t, c, "{tokens}"); got != "" {
		t.Errorf("tokens-empty: %q", got)
	}
}

func TestSegment_Context(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{context}"); got != "84k (42%)" {
		t.Errorf("context: %q", got)
	}
}

func TestSegment_ContextPctOnly(t *testing.T) {
	c := newTestContext()
	c.Synthetic["_context_tokens"] = ""
	if got := renderOrFail(t, c, "{context}"); got != "42%" {
		t.Errorf("context pct-only: %q", got)
	}
}

func TestSegment_ContextTokensOnly(t *testing.T) {
	c := newTestContext()
	c.Synthetic["_context_pct"] = ""
	if got := renderOrFail(t, c, "{context}"); got != "84k" {
		t.Errorf("context tokens-only: %q", got)
	}
}

func TestSegment_ContextEmpty(t *testing.T) {
	c := newTestContext()
	c.Synthetic["_context_pct"] = ""
	c.Synthetic["_context_tokens"] = ""
	if got := renderOrFail(t, c, "{context}"); got != "" {
		t.Errorf("context empty: %q", got)
	}
}

func TestSegment_Daily(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{daily}"); got != "$4.21 today" {
		t.Errorf("daily: %q", got)
	}
}

func TestSegment_DailyEmpty(t *testing.T) {
	c := newTestContext()
	delete(c.Snapshot.Metrics, "today_api_cost")
	if got := renderOrFail(t, c, "{daily}"); got != "" {
		t.Errorf("daily empty: %q", got)
	}
}

func TestSegment_ActiveTools(t *testing.T) {
	c := newTestContext()
	c.AllSnapshots = []core.UsageSnapshot{
		{ProviderID: "claude_code"},
		{ProviderID: "cursor"},
		{ProviderID: ""}, // skipped
		{ProviderID: "codex"},
	}
	if got := renderOrFail(t, c, "{active_tools}"); got != "claude_code | cursor | codex" {
		t.Errorf("active_tools: %q", got)
	}
}

func TestSegment_ActiveToolsEmpty(t *testing.T) {
	c := newTestContext()
	c.AllSnapshots = nil
	if got := renderOrFail(t, c, "{active_tools}"); got != "" {
		t.Errorf("active_tools empty: %q", got)
	}
}

func TestSegment_NamesAccessor(t *testing.T) {
	names := SegmentNames()
	if len(names) < 9 {
		t.Errorf("expected at least 9 built-in segments, got %d", len(names))
	}
	// Must include the canonical names.
	want := map[string]bool{"cost": false, "block": false, "burn": false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("SegmentNames missing %q", k)
		}
	}
}

func TestSegment_UserOverride(t *testing.T) {
	c := newTestContext()
	c.Segments = map[string]string{"cost": "OVERRIDDEN"}
	if got := renderOrFail(t, c, "{cost}"); got != "OVERRIDDEN" {
		t.Errorf("user segment override: %q", got)
	}
}

func TestSegment_UserOverrideWithNesting(t *testing.T) {
	c := newTestContext()
	c.Segments = map[string]string{"tag": "[{tool}]"}
	if got := renderOrFail(t, c, "{tag}"); got != "[claude_code]" {
		t.Errorf("nested user segment: %q", got)
	}
}

func TestColorRule_UserOverride(t *testing.T) {
	c := newTestContext()
	c.ColorRules = map[string]ColorRule{
		"block_pct": {
			HighAt:    50,
			HighColor: "#FF0000",
		},
	}
	// 47% is below the user's HighAt=50, so it should fall to the merged
	// medium/low default. We assert the result does NOT contain the override
	// red, since 47 is below 50.
	out := renderOrFail(t, c, "{block_pct:color}")
	if strings.Contains(out, "fg=#FF0000") {
		t.Errorf("47 should not pick HighAt=50 color: %q", out)
	}
}

func TestColorRule_UserOverrideHits(t *testing.T) {
	c := newTestContext()
	c.ColorRules = map[string]ColorRule{
		"block_pct": {
			HighAt:    40,
			HighColor: "#FF0000",
		},
	}
	out := renderOrFail(t, c, "{block_pct:color}")
	// 47 >= 40 picks the override red.
	if !strings.Contains(out, "fg=#FF0000") {
		t.Errorf("47 should pick HighAt=40 override: %q", out)
	}
}

func TestColorRule_ThemeRef(t *testing.T) {
	c := newTestContext()
	c.ColorRules = map[string]ColorRule{
		"block_pct": {
			HighAt:    40,
			HighColor: "$accent",
		},
	}
	out := renderOrFail(t, c, "{block_pct:color}")
	if !strings.Contains(out, "fg=#FF6600") {
		t.Errorf("theme ref should resolve to accent hex: %q", out)
	}
}

func TestColorRule_ThemeRefUnknown(t *testing.T) {
	c := newTestContext()
	c.ColorRules = map[string]ColorRule{
		"block_pct": {
			HighAt:    40,
			HighColor: "$nonexistent",
		},
	}
	out := renderOrFail(t, c, "{block_pct:color}")
	// Unknown theme ref resolves to empty so the value passes through unwrapped.
	if strings.Contains(out, "#[") {
		t.Errorf("unknown theme ref should drop directive: %q", out)
	}
}

func TestModifier_DurationPassesPreformatted(t *testing.T) {
	// Already-formatted durations like "90m" pass through unchanged so the
	// synthetic _block_remaining values don't get reformatted on every render.
	c := newTestContext()
	c.Variables = map[string]string{"d": "90m"}
	if got := renderOrFail(t, c, "{d:duration}"); got != "90m" {
		t.Errorf("preformatted duration: %q", got)
	}
}

func TestModifier_DurationUnparseable(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"d": "not-a-duration-x"}
	if got := renderOrFail(t, c, "{d:duration}"); got != "not-a-duration-x" {
		t.Errorf("unparseable duration: %q", got)
	}
}

func TestModifier_NonNumericNoOp(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "not-a-number"}
	if got := renderOrFail(t, c, "{x:money}"); got != "not-a-number" {
		t.Errorf("money on non-numeric: %q", got)
	}
	if got := renderOrFail(t, c, "{x:pct}"); got != "not-a-number" {
		t.Errorf("pct on non-numeric: %q", got)
	}
	if got := renderOrFail(t, c, "{x:tokens}"); got != "not-a-number" {
		t.Errorf("tokens on non-numeric: %q", got)
	}
}

func TestModifier_BarInvalidWidthFallsBack(t *testing.T) {
	c := newTestContext()
	// Bare {block_pct:bar} uses default width 8.
	out := renderOrFail(t, c, "{block_pct:bar}")
	if len([]rune(out)) != 8 {
		t.Errorf("default bar width 8, got %d runes (%q)", len([]rune(out)), out)
	}
}

func TestParseFloat_StripsUnits(t *testing.T) {
	cases := map[string]float64{
		"$4.21":    4.21,
		"42%":      42,
		"$1.20/hr": 1.20,
		"  100  ":  100,
	}
	for in, want := range cases {
		got, ok := parseFloat(in)
		if !ok || got != want {
			t.Errorf("parseFloat(%q) = (%v,%v) want (%v,true)", in, got, ok, want)
		}
	}
	if _, ok := parseFloat(""); ok {
		t.Errorf("parseFloat empty should fail")
	}
	if _, ok := parseFloat("hello"); ok {
		t.Errorf("parseFloat non-numeric should fail")
	}
}

func TestIsTruthy(t *testing.T) {
	// isTruthy operates on raw resolved values (pre-modifier). The
	// formatter only invokes the truth test before any `:money` or `:pct`
	// has been applied, so the test inputs are bare numbers / strings.
	cases := map[string]bool{
		"":      false,
		" ":     false,
		"0":     false,
		"0.00":  false,
		"0.0":   false,
		"1":     true,
		"hello": true, // non-numeric non-empty is truthy
	}
	for in, want := range cases {
		if got := isTruthy(in); got != want {
			t.Errorf("isTruthy(%q) = %v want %v", in, got, want)
		}
	}
}

func TestConditional_EmptyCondName(t *testing.T) {
	_, err := Render("{?:then}", newTestContext())
	if err != nil {
		// Empty cond name resolves to "", which is falsy; no error expected.
		t.Errorf("empty cond produced error: %v", err)
	}
}

func TestConditional_OnlyCond_Errors(t *testing.T) {
	_, err := Render("{?cond}", newTestContext())
	if err == nil {
		t.Errorf("expected error for conditional with no then branch")
	}
}

func TestRender_TrailingBackslash(t *testing.T) {
	got := renderOrFail(t, newTestContext(), `end\`)
	if got != `end\` {
		t.Errorf("trailing backslash: %q", got)
	}
}

func TestRender_HashAtEnd(t *testing.T) {
	// A bare `#` at end is passed through.
	got := renderOrFail(t, newTestContext(), "end#")
	if got != "end#" {
		t.Errorf("trailing #: %q", got)
	}
}

func TestRender_HashOnlyDollarSign(t *testing.T) {
	// A `$` outside `#[...]` is literal.
	got := renderOrFail(t, newTestContext(), "price: $5")
	if got != "price: $5" {
		t.Errorf("literal $: %q", got)
	}
}

func TestRender_DollarSignInsideDirectiveWithNoIdent(t *testing.T) {
	got := renderOrFail(t, newTestContext(), "#[fg=$]x")
	// A bare `$` with no ident inside `#[...]` is preserved.
	if !strings.Contains(got, "$") {
		t.Errorf("bare $ in directive: %q", got)
	}
}

func TestRender_BackslashEscapeInThemeRef(t *testing.T) {
	got := renderOrFail(t, newTestContext(), `#[fg=\$accent]x`)
	// Escaped $ is literal.
	if !strings.Contains(got, "$accent") {
		t.Errorf("escaped $ in directive: %q", got)
	}
}

func TestModifier_ShortOnNonNumeric(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "not-a-number"}
	if got := renderOrFail(t, c, "{x:short}"); got != "not-a-number" {
		t.Errorf("short non-numeric: %q", got)
	}
}

func TestModifier_LongFallback(t *testing.T) {
	// :long on a variable that isn't one of the special names just returns
	// modShort behavior.
	c := newTestContext()
	c.Variables = map[string]string{"misc": "5.0"}
	if got := renderOrFail(t, c, "{misc:long}"); got != "$5.00" {
		t.Errorf("long fallback: %q", got)
	}
}

func TestModifier_TruncOnShorterStringNoOp(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "abc"}
	if got := renderOrFail(t, c, "{x:trunc:10}"); got != "abc" {
		t.Errorf("trunc on shorter: %q", got)
	}
}

func TestModifier_TruncInvalidWidthNoOp(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "long enough"}
	if got := renderOrFail(t, c, "{x:trunc:not-a-num}"); got != "long enough" {
		t.Errorf("trunc bad width: %q", got)
	}
}

func TestModifier_TruncToOne(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "abcdef"}
	if got := renderOrFail(t, c, "{x:trunc:1}"); got != "a" {
		t.Errorf("trunc to 1: %q", got)
	}
}

func TestModifier_PadNoArg(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "ab"}
	// No width arg: passes through.
	if got := renderOrFail(t, c, "{x:pad}"); got != "ab" {
		t.Errorf("pad no arg: %q", got)
	}
}

func TestModifier_PadInvalidWidth(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "ab"}
	if got := renderOrFail(t, c, "{x:pad:bogus}"); got != "ab" {
		t.Errorf("pad bad width: %q", got)
	}
}

func TestModifier_BarInvalidWidthArg(t *testing.T) {
	c := newTestContext()
	// width must be >0 and <=64; out-of-range falls to default 8.
	out := renderOrFail(t, c, "{block_pct:bar:nope}")
	if len([]rune(out)) != 8 {
		t.Errorf("bar bad width: %q (%d runes)", out, len([]rune(out)))
	}
}

func TestModifier_BarNegativePct(t *testing.T) {
	c := newTestContext()
	neg := -10.0
	c.Snapshot.Metrics["usage_five_hour"] = core.Metric{Used: &neg, Unit: "%"}
	out := renderOrFail(t, c, "{block_pct:bar:5}")
	// Negative percent clamps to 0; all cells empty.
	full, empty := barGlyphs(GlyphTierUnicode)
	want := strings.Repeat(empty, 5)
	if out != want {
		t.Errorf("negative pct bar: got %q want %q (full=%q)", out, want, full)
	}
}

func TestSplitTopLevel_UnterminatedHashBracket(t *testing.T) {
	// Cover the branch where splitTopLevel sees `#[` that doesn't close
	// before end-of-string. This shouldn't crash the splitter; the outer
	// renderTemplate is what surfaces the unterminated-bracket error.
	parts := splitTopLevel("a:#[fg=red,bad", ':')
	if len(parts) < 1 {
		t.Errorf("splitTopLevel unterminated bracket: got %v", parts)
	}
}

func TestModifier_ColorOnNonNumeric(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": "not-a-number"}
	// :color on non-numeric is a no-op passthrough.
	if got := renderOrFail(t, c, "{x:color}"); got != "not-a-number" {
		t.Errorf("color non-numeric: %q", got)
	}
}

func TestModifier_MoneyZeroPrecision(t *testing.T) {
	c := newTestContext()
	if got := renderOrFail(t, c, "{today_cost:money:8}"); got != "$4.21000000" {
		t.Errorf("money precision 8: %q", got)
	}
	// Precision out-of-range falls to default 2.
	if got := renderOrFail(t, c, "{today_cost:money:99}"); got != "$4.21" {
		t.Errorf("money precision 99 (out of range): %q", got)
	}
}

func TestModifier_PctPrecisionOutOfRange(t *testing.T) {
	c := newTestContext()
	// Precision >4 falls to default 0.
	if got := renderOrFail(t, c, "{block_pct:pct:99}"); got != "47%" {
		t.Errorf("pct out of range: %q", got)
	}
}

func TestModDuration_FormatsZero(t *testing.T) {
	if got := fmtDurationDefault(0); got != "" {
		t.Errorf("zero duration: %q", got)
	}
	if got := fmtDurationDefault(-1); got != "" {
		t.Errorf("negative duration: %q", got)
	}
}

func TestModDuration_HoursOnly(t *testing.T) {
	if got := fmtDurationDefault(30 * 60 * 1_000_000_000); got != "30m" {
		t.Errorf("30m: %q", got)
	}
}

func TestColorRule_NonHexLowColor(t *testing.T) {
	c := newTestContext()
	// LowColor is a literal hex (no $), exercising the non-$ branch of
	// resolveColorRef.
	c.ColorRules = map[string]ColorRule{
		"block_pct": {
			LowColor: "#112233",
		},
	}
	out := renderOrFail(t, c, "{block_pct:color}")
	if !strings.Contains(out, "fg=#112233") {
		t.Errorf("non-$ low color: %q", out)
	}
}

func TestPickColor_FallsBack(t *testing.T) {
	if got := pickColor("", "#FALLBACK"); got != "#FALLBACK" {
		t.Errorf("pickColor empty primary: %q", got)
	}
	if got := pickColor("#PRIMARY", "#FALLBACK"); got != "#PRIMARY" {
		t.Errorf("pickColor with primary: %q", got)
	}
}

func TestModColor_NoFloatPassthrough(t *testing.T) {
	c := newTestContext()
	c.Variables = map[string]string{"x": ""}
	// Empty value passes through unwrapped.
	out := renderOrFail(t, c, "{x:color}")
	if strings.Contains(out, "#[") {
		t.Errorf("empty value should not wrap: %q", out)
	}
}

func TestMetricUsedString_FloatFormatting(t *testing.T) {
	used := func(v float64) *float64 { return &v }
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"intish": {Used: used(42)},
			"floaty": {Used: used(3.14)},
			"nil":    {},
		},
	}
	v, ok := metricUsedString(snap, "intish")
	if !ok || v != "42" {
		t.Errorf("int metric: got (%q,%v)", v, ok)
	}
	v, ok = metricUsedString(snap, "floaty")
	if !ok || v != "3.14" {
		t.Errorf("float metric: got (%q,%v)", v, ok)
	}
	if _, ok = metricUsedString(snap, "nil"); ok {
		t.Errorf("nil Used should miss")
	}
	if _, ok = metricUsedString(snap, "missing"); ok {
		t.Errorf("missing key should miss")
	}
}

func TestMetricUsedString_NilMetrics(t *testing.T) {
	if _, ok := metricUsedString(core.UsageSnapshot{}, "anything"); ok {
		t.Errorf("nil Metrics map should miss")
	}
}

func TestSegmentModel_FromAttributes(t *testing.T) {
	c := newTestContext()
	delete(c.Snapshot.Attributes, "model")
	if got := renderOrFail(t, c, "{model}"); got != "" {
		t.Errorf("model missing: %q", got)
	}
}
