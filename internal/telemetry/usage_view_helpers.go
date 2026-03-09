package telemetry

import (
	"sort"
	"strings"
	"unicode"
)

// Raw tool names use double underscores: mcp__server__function.
// Returns ("", "", false) for non-MCP tools.
// parseMCPToolName extracts server and function from an MCP tool name.
// Supports two formats:
//   - Canonical: "mcp__server__function" (double underscores, from Claude Code and normalized Cursor)
//   - Legacy:    "server-function (mcp)" or "user-server-function (mcp)" (old Cursor data)
func parseMCPToolName(raw string) (server, function string, ok bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))

	if strings.HasPrefix(raw, "mcp__") {
		rest := raw[5:]
		idx := strings.Index(rest, "__")
		if idx < 0 {
			return rest, "", true
		}
		return rest[:idx], rest[idx+2:], true
	}

	if strings.Contains(raw, "_mcp_server_") {
		parts := strings.SplitN(raw, "_mcp_server_", 2)
		server = sanitizeMCPToolSegment(parts[0])
		function = sanitizeMCPToolSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}

	if strings.Contains(raw, "-mcp-server-") {
		parts := strings.SplitN(raw, "-mcp-server-", 2)
		server = sanitizeMCPToolSegment(parts[0])
		function = sanitizeMCPToolSegment(parts[1])
		if server != "" && function != "" {
			return server, function, true
		}
	}

	if strings.HasSuffix(raw, " (mcp)") {
		body := strings.TrimSpace(strings.TrimSuffix(raw, " (mcp)"))
		if body == "" {
			return "", "", false
		}
		body = strings.TrimPrefix(body, "user-")
		if idx := findServerFunctionSplit(body); idx > 0 {
			return body[:idx], body[idx+1:], true
		}
		return "other", body, true
	}

	return "", "", false
}

func sanitizeMCPToolSegment(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func findServerFunctionSplit(s string) int {
	bestIdx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			rest := s[i+1:]
			if strings.Contains(rest, "_") {
				bestIdx = i
			}
		}
	}
	if bestIdx > 0 {
		return bestIdx
	}
	if idx := strings.Index(s, "-"); idx > 0 {
		return idx
	}
	return -1
}

func buildMCPAgg(tools []telemetryToolAgg) []telemetryMCPServerAgg {
	type serverData struct {
		calls   float64
		calls1d float64
		funcs   map[string]*telemetryMCPFunctionAgg
	}
	servers := make(map[string]*serverData)

	for _, tool := range tools {
		server, function, ok := parseMCPToolName(tool.Tool)
		if !ok || server == "" {
			continue
		}
		sd, exists := servers[server]
		if !exists {
			sd = &serverData{funcs: make(map[string]*telemetryMCPFunctionAgg)}
			servers[server] = sd
		}
		sd.calls += tool.Calls
		sd.calls1d += tool.Calls1d
		if function != "" {
			if f, ok := sd.funcs[function]; ok {
				f.Calls += tool.Calls
				f.Calls1d += tool.Calls1d
			} else {
				sd.funcs[function] = &telemetryMCPFunctionAgg{
					Function: function,
					Calls:    tool.Calls,
					Calls1d:  tool.Calls1d,
				}
			}
		}
	}

	result := make([]telemetryMCPServerAgg, 0, len(servers))
	for name, sd := range servers {
		var funcs []telemetryMCPFunctionAgg
		for _, fn := range sd.funcs {
			funcs = append(funcs, *fn)
		}
		sort.Slice(funcs, func(i, j int) bool {
			if funcs[i].Calls != funcs[j].Calls {
				return funcs[i].Calls > funcs[j].Calls
			}
			return funcs[i].Function < funcs[j].Function
		})
		result = append(result, telemetryMCPServerAgg{
			Server:    name,
			Calls:     sd.calls,
			Calls1d:   sd.calls1d,
			Functions: funcs,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Calls != result[j].Calls {
			return result[i].Calls > result[j].Calls
		}
		return result[i].Server < result[j].Server
	})
	return result
}

func deleteByPrefixes[V any](m map[string]V, prefixes []string) {
	for key := range m {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				delete(m, key)
				break
			}
		}
	}
}

func sanitizeMetricID(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
