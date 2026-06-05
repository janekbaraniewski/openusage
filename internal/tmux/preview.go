package tmux

import (
	"regexp"
	"strconv"
	"strings"
)

// Preview translates a rendered tmux-format string into ANSI escapes for
// outside-tmux terminals. It accepts the output of Render (which contains
// `#[fg=#RRGGBB,...]` directives) and replaces each directive with the
// equivalent SGR sequence, leaving plain text intact.
//
// Unknown attributes are silently dropped: the goal is a visual preview, not
// strict tmux emulation. `#[default]` resets via `\x1b[0m`.
func Preview(rendered string) string {
	if !strings.Contains(rendered, "#[") {
		return rendered
	}
	var out strings.Builder
	i := 0
	for i < len(rendered) {
		if rendered[i] == '#' && i+1 < len(rendered) && rendered[i+1] == '[' {
			end := strings.Index(rendered[i+2:], "]")
			if end < 0 {
				// Malformed directive: emit as-is and bail out.
				out.WriteString(rendered[i:])
				return out.String()
			}
			body := rendered[i+2 : i+2+end]
			out.WriteString(directiveToANSI(body))
			i = i + 2 + end + 1
			continue
		}
		out.WriteByte(rendered[i])
		i++
	}
	return out.String()
}

// directiveToANSI returns the SGR sequence for one `#[...]` body. The
// supported subset covers fg=#hex, bg=#hex, fg=colourNNN, bg=colourNNN, fg=
// <name>, bg=<name>, bold, dim, underline, reverse, and the reset keyword
// `default`. Other tokens are ignored.
func directiveToANSI(body string) string {
	parts := strings.Split(body, ",")
	codes := make([]string, 0, len(parts))
	for _, raw := range parts {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		switch strings.ToLower(tok) {
		case "default", "none":
			return "\x1b[0m"
		case "bold":
			codes = append(codes, "1")
			continue
		case "dim":
			codes = append(codes, "2")
			continue
		case "underline":
			codes = append(codes, "4")
			continue
		case "reverse":
			codes = append(codes, "7")
			continue
		}
		if code, ok := colorTokenToSGR(tok); ok {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

var hexColorRE = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)
var paletteRE = regexp.MustCompile(`^colou?r(\d+)$`)

// colorTokenToSGR parses one `fg=...` or `bg=...` token. Returns the SGR
// numeric body (no escapes, no terminator) and ok=true on a successful
// match.
func colorTokenToSGR(tok string) (string, bool) {
	eq := strings.Index(tok, "=")
	if eq <= 0 {
		return "", false
	}
	side := strings.ToLower(strings.TrimSpace(tok[:eq]))
	value := strings.TrimSpace(tok[eq+1:])
	var base int
	switch side {
	case "fg":
		base = 38
	case "bg":
		base = 48
	default:
		return "", false
	}

	if m := hexColorRE.FindStringSubmatch(value); m != nil {
		val, err := strconv.ParseUint(m[1], 16, 32)
		if err != nil {
			return "", false
		}
		r := (val >> 16) & 0xff
		g := (val >> 8) & 0xff
		b := val & 0xff
		return formatColor(base, 2, int(r), int(g), int(b)), true
	}
	if m := paletteRE.FindStringSubmatch(strings.ToLower(value)); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", false
		}
		return formatColor(base, 5, n), true
	}
	// Named ANSI colors (black/red/green/...).
	if n, ok := namedANSI(value, base == 48); ok {
		return strconv.Itoa(n), true
	}
	return "", false
}

func formatColor(base, mode int, parts ...int) string {
	b := make([]string, 0, len(parts)+2)
	b = append(b, strconv.Itoa(base), strconv.Itoa(mode))
	for _, p := range parts {
		b = append(b, strconv.Itoa(p))
	}
	return strings.Join(b, ";")
}

var namedANSITable = map[string]int{
	"black":   30,
	"red":     31,
	"green":   32,
	"yellow":  33,
	"blue":    34,
	"magenta": 35,
	"cyan":    36,
	"white":   37,
}

func namedANSI(name string, bg bool) (int, bool) {
	code, ok := namedANSITable[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return 0, false
	}
	if bg {
		code += 10
	}
	return code, true
}
