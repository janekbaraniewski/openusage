package copilot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func parseCopilotTelemetryToolRequest(raw json.RawMessage) (copilotTelemetryToolRequest, bool) {
	var reqMap map[string]any
	if json.Unmarshal(raw, &reqMap) != nil {
		return copilotTelemetryToolRequest{}, false
	}

	out := copilotTelemetryToolRequest{
		ToolCallID: strings.TrimSpace(anyToString(reqMap["toolCallId"])),
		RawName:    core.FirstNonEmpty(anyToString(reqMap["name"]), anyToString(reqMap["toolName"]), anyToString(reqMap["tool"])),
	}
	if out.RawName == "" {
		out.RawName = extractCopilotToolName(raw)
	}
	for _, key := range []string{"arguments", "args", "input"} {
		if value, ok := reqMap[key]; ok && out.Input == nil {
			out.Input = decodeCopilotTelemetryJSONAny(value)
		}
	}
	return out, true
}

func normalizeCopilotTelemetryToolName(raw string) (string, map[string]any) {
	meta := map[string]any{}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "unknown", meta
	}
	meta["tool_name_raw"] = name
	if server, function, ok := parseCopilotTelemetryMCPTool(name); ok {
		meta["tool_type"] = "mcp"
		meta["mcp_server"] = server
		meta["mcp_function"] = function
		return "mcp__" + server + "__" + function, meta
	}
	return sanitizeMetricName(name), meta
}

func parseCopilotTelemetryMCPTool(raw string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", "", false
	}
	for _, marker := range []string{"_mcp_server_", "-mcp-server-"} {
		if parts := strings.SplitN(normalized, marker, 2); len(parts) == 2 {
			server := sanitizeCopilotMCPSegment(parts[0])
			function := sanitizeCopilotMCPSegment(parts[1])
			if server != "" && function != "" {
				return server, function, true
			}
		}
	}
	if strings.HasPrefix(normalized, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(normalized, "mcp__"), "__", 2)
		if len(parts) == 2 {
			server := sanitizeCopilotMCPSegment(parts[0])
			function := sanitizeCopilotMCPSegment(parts[1])
			if server != "" && function != "" {
				return server, function, true
			}
		}
	}
	if strings.HasPrefix(normalized, "mcp-") || strings.HasPrefix(normalized, "mcp_") {
		canonical := normalizeCopilotCursorStyleMCPName(normalized)
		if strings.HasPrefix(canonical, "mcp__") {
			parts := strings.SplitN(strings.TrimPrefix(canonical, "mcp__"), "__", 2)
			if len(parts) == 2 {
				server := sanitizeCopilotMCPSegment(parts[0])
				function := sanitizeCopilotMCPSegment(parts[1])
				if server != "" && function != "" {
					return server, function, true
				}
			}
		}
	}
	if strings.HasSuffix(normalized, " (mcp)") {
		body := strings.TrimSpace(strings.TrimSuffix(normalized, " (mcp)"))
		body = strings.TrimPrefix(body, "user-")
		if body == "" {
			return "", "", false
		}
		if idx := findCopilotTelemetryServerFunctionSplit(body); idx > 0 {
			server := sanitizeCopilotMCPSegment(body[:idx])
			function := sanitizeCopilotMCPSegment(body[idx+1:])
			if server != "" && function != "" {
				return server, function, true
			}
		}
		return "other", sanitizeCopilotMCPSegment(body), true
	}
	return "", "", false
}

func normalizeCopilotCursorStyleMCPName(name string) string {
	if strings.HasPrefix(name, "mcp-") {
		rest := name[4:]
		parts := strings.SplitN(rest, "-user-", 2)
		if len(parts) == 2 {
			server := parts[0]
			afterUser := parts[1]
			serverDash := server + "-"
			if strings.HasPrefix(afterUser, serverDash) {
				return "mcp__" + server + "__" + afterUser[len(serverDash):]
			}
			if idx := strings.LastIndex(afterUser, "-"); idx > 0 {
				return "mcp__" + server + "__" + afterUser[idx+1:]
			}
			return "mcp__" + server + "__" + afterUser
		}
		if idx := strings.Index(rest, "-"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}
	if strings.HasPrefix(name, "mcp_") {
		rest := name[4:]
		if idx := strings.Index(rest, "_"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}
	return name
}

func findCopilotTelemetryServerFunctionSplit(s string) int {
	best := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '-' && strings.Contains(s[i+1:], "_") {
			best = i
		}
	}
	return best
}

func sanitizeCopilotMCPSegment(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func copilotTelemetryToolStatus(success *bool, statusRaw, errorCode, errorMessage string) shared.TelemetryStatus {
	if success != nil {
		if *success {
			return shared.TelemetryStatusOK
		}
		if copilotTelemetryLooksAborted(errorCode, errorMessage, statusRaw) {
			return shared.TelemetryStatusAborted
		}
		return shared.TelemetryStatusError
	}
	switch strings.ToLower(strings.TrimSpace(statusRaw)) {
	case "ok", "success", "succeeded", "completed", "complete":
		return shared.TelemetryStatusOK
	case "aborted", "cancelled", "canceled", "denied":
		return shared.TelemetryStatusAborted
	case "error", "failed", "failure":
		return shared.TelemetryStatusError
	}
	if errorCode != "" || errorMessage != "" {
		if copilotTelemetryLooksAborted(errorCode, errorMessage, statusRaw) {
			return shared.TelemetryStatusAborted
		}
		return shared.TelemetryStatusError
	}
	return shared.TelemetryStatusUnknown
}

func copilotTelemetryLooksAborted(parts ...string) bool {
	for _, part := range parts {
		lower := strings.ToLower(strings.TrimSpace(part))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "denied") || strings.Contains(lower, "cancel") || strings.Contains(lower, "abort") || strings.Contains(lower, "rejected") || strings.Contains(lower, "user initiated") {
			return true
		}
	}
	return false
}

func summarizeCopilotTelemetryResult(raw json.RawMessage) map[string]any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	decoded := decodeCopilotTelemetryJSONAny(raw)
	if decoded == nil {
		return nil
	}
	payload := map[string]any{}
	if paths := shared.ExtractFilePathsFromPayload(decoded); len(paths) > 0 {
		payload["result_file"] = paths[0]
	}
	switch value := decoded.(type) {
	case map[string]any:
		if content := anyToString(value["content"]); content != "" {
			payload["result_chars"] = len(content)
			if added, removed := countCopilotTelemetryUnifiedDiff(content); added > 0 || removed > 0 {
				payload["lines_added"] = added
				payload["lines_removed"] = removed
			}
		}
		if detailed := anyToString(value["detailedContent"]); detailed != "" {
			payload["result_detailed_chars"] = len(detailed)
			if _, ok := payload["lines_added"]; !ok {
				if added, removed := countCopilotTelemetryUnifiedDiff(detailed); added > 0 || removed > 0 {
					payload["lines_added"] = added
					payload["lines_removed"] = removed
				}
			}
		}
		if msg := anyToString(value["message"]); msg != "" {
			payload["result_message"] = truncate(msg, 240)
		}
	case string:
		if value != "" {
			payload["result_chars"] = len(value)
			if added, removed := countCopilotTelemetryUnifiedDiff(value); added > 0 || removed > 0 {
				payload["lines_added"] = added
				payload["lines_removed"] = removed
			}
		}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func countCopilotTelemetryUnifiedDiff(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" || (!strings.Contains(raw, "diff --git") && !strings.Contains(raw, "\n@@")) {
		return 0, 0
	}
	added, removed := 0, 0
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "@@"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func summarizeCopilotTelemetryError(raw json.RawMessage) (string, string) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "", ""
	}
	decoded := decodeCopilotTelemetryJSONAny(raw)
	if decoded == nil {
		return "", ""
	}
	switch value := decoded.(type) {
	case map[string]any:
		return strings.TrimSpace(anyToString(value["code"])), strings.TrimSpace(anyToString(value["message"]))
	case string:
		return "", strings.TrimSpace(value)
	default:
		return "", strings.TrimSpace(anyToString(decoded))
	}
}

func copilotTelemetryBasePayload(path string, line int, client, repo, cwd, event string) map[string]any {
	payload := map[string]any{
		"source_file":       path,
		"line":              line,
		"event":             event,
		"client":            client,
		"upstream_provider": "github",
	}
	if strings.TrimSpace(repo) != "" {
		payload["repository"] = strings.TrimSpace(repo)
	}
	if strings.TrimSpace(cwd) != "" {
		payload["cwd"] = strings.TrimSpace(cwd)
	}
	return payload
}

func copyCopilotTelemetryPayload(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func decodeCopilotTelemetryJSONAny(raw any) any {
	switch value := raw.(type) {
	case nil:
		return nil
	case map[string]any, []any:
		return value
	case json.RawMessage:
		var out any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
		return strings.TrimSpace(string(value))
	case []byte:
		var out any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
		return strings.TrimSpace(string(value))
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		var out any
		if json.Unmarshal([]byte(trimmed), &out) == nil {
			return out
		}
		return trimmed
	default:
		return value
	}
}

func extractCopilotTelemetryCommand(input any) string {
	var command string
	var walk func(any)
	walk = func(value any) {
		if command != "" || value == nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				if (k == "command" || k == "cmd" || k == "script" || k == "shell_command") && child != nil {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(input)
	return command
}

func estimateCopilotTelemetryLineDelta(input any) (int, int) {
	if input == nil {
		return 0, 0
	}
	encoded, err := json.Marshal(map[string]any{"arguments": input})
	if err != nil {
		return 0, 0
	}
	return estimateCopilotToolLineDelta(encoded)
}

func copilotUpstreamProviderForModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || model == "unknown" {
		return "github"
	}
	switch {
	case strings.Contains(model, "claude"):
		return "anthropic"
	case strings.Contains(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return "openai"
	case strings.Contains(model, "gemini"):
		return "google"
	case strings.Contains(model, "qwen"):
		return "alibaba_cloud"
	case strings.Contains(model, "deepseek"):
		return "deepseek"
	case strings.Contains(model, "llama"):
		return "meta"
	case strings.Contains(model, "mistral"):
		return "mistral"
	default:
		return "github"
	}
}

func anyToString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func truncate(input string, max int) string {
	input = strings.TrimSpace(input)
	if max <= 0 || len(input) <= max {
		return input
	}
	return input[:max]
}
