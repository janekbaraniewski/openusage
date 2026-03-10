package gemini_cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func formatNamedCountMap(m map[string]int, unit string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for name, count := range m {
		if count <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %d %s", name, count, unit))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func isGeminiToolCallSuccessful(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "" || status == "success" || status == "succeeded" || status == "ok" || status == "completed"
}

func isGeminiMutatingTool(toolName string) bool {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if toolName == "" {
		return false
	}
	return strings.Contains(toolName, "edit") ||
		strings.Contains(toolName, "write") ||
		strings.Contains(toolName, "create") ||
		strings.Contains(toolName, "delete") ||
		strings.Contains(toolName, "rename") ||
		strings.Contains(toolName, "move") ||
		strings.Contains(toolName, "replace")
}

func extractGeminiToolCommand(raw json.RawMessage) string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	var command string
	var walk func(v any)
	walk = func(v any) {
		if command != "" || v == nil {
			return
		}
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				k := strings.ToLower(strings.TrimSpace(key))
				if k == "command" || k == "cmd" || k == "script" || k == "shell_command" {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range value {
				walk(child)
				if command != "" {
					return
				}
			}
		case []any:
			for _, child := range value {
				walk(child)
				if command != "" {
					return
				}
			}
		}
	}
	walk(payload)
	return command
}

func extractGeminiToolPaths(raw json.RawMessage) []string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}

	pathHints := map[string]bool{
		"path": true, "paths": true, "file": true, "files": true, "filepath": true, "file_path": true,
		"cwd": true, "dir": true, "directory": true, "target": true, "pattern": true, "glob": true,
		"from": true, "to": true, "include": true, "exclude": true,
	}

	candidates := make(map[string]bool)
	var walk func(v any, hinted bool)
	walk = func(v any, hinted bool) {
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				k := strings.ToLower(strings.TrimSpace(key))
				childHinted := hinted || pathHints[k] || strings.Contains(k, "path") || strings.Contains(k, "file")
				walk(child, childHinted)
			}
		case []any:
			for _, child := range value {
				walk(child, hinted)
			}
		case string:
			if !hinted {
				return
			}
			for _, token := range extractGeminiPathTokens(value) {
				candidates[token] = true
			}
		}
	}
	walk(payload, false)

	return core.SortedStringKeys(candidates)
}

func extractGeminiPathTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		fields = []string{raw}
	}

	var out []string
	for _, field := range fields {
		token := strings.Trim(field, "\"'`()[]{}<>,:;")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://") {
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if !strings.Contains(token, "/") && !strings.Contains(token, "\\") && !strings.Contains(token, ".") {
			continue
		}
		token = strings.TrimPrefix(token, "./")
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return lo.Uniq(out)
}

func estimateGeminiToolLineDelta(raw json.RawMessage) (added int, removed int) {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return 0, 0
	}
	lineCount := func(text string) int {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		return strings.Count(text, "\n") + 1
	}
	var walk func(v any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			var oldText, newText string
			for _, key := range []string{"old_string", "old_text", "from", "replace"} {
				if rawValue, ok := value[key]; ok {
					if s, ok := rawValue.(string); ok {
						oldText = s
						break
					}
				}
			}
			for _, key := range []string{"new_string", "new_text", "to", "with"} {
				if rawValue, ok := value[key]; ok {
					if s, ok := rawValue.(string); ok {
						newText = s
						break
					}
				}
			}
			if oldText != "" || newText != "" {
				removed += lineCount(oldText)
				added += lineCount(newText)
			}
			if rawValue, ok := value["content"]; ok {
				if s, ok := rawValue.(string); ok {
					added += lineCount(s)
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)
	return added, removed
}

func extractGeminiToolDiffStat(raw json.RawMessage) (geminiDiffStat, bool) {
	var empty geminiDiffStat
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return empty, false
	}

	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return empty, false
	}
	diffRaw, ok := root["diffStat"]
	if !ok {
		return empty, false
	}

	var stat geminiDiffStat
	if json.Unmarshal(diffRaw, &stat) != nil {
		return empty, false
	}

	stat.ModelAddedLines = max(0, stat.ModelAddedLines)
	stat.ModelRemovedLines = max(0, stat.ModelRemovedLines)
	stat.ModelAddedChars = max(0, stat.ModelAddedChars)
	stat.ModelRemovedChars = max(0, stat.ModelRemovedChars)
	stat.UserAddedLines = max(0, stat.UserAddedLines)
	stat.UserRemovedLines = max(0, stat.UserRemovedLines)
	stat.UserAddedChars = max(0, stat.UserAddedChars)
	stat.UserRemovedChars = max(0, stat.UserRemovedChars)

	if stat.ModelAddedLines == 0 &&
		stat.ModelRemovedLines == 0 &&
		stat.ModelAddedChars == 0 &&
		stat.ModelRemovedChars == 0 &&
		stat.UserAddedLines == 0 &&
		stat.UserRemovedLines == 0 &&
		stat.UserAddedChars == 0 &&
		stat.UserRemovedChars == 0 {
		return empty, false
	}

	return stat, true
}

func inferGeminiLanguageFromPath(path string) string {
	p := strings.ToLower(strings.TrimSpace(path))
	if p == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(p))
	switch base {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "make"
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".tf", ".tfvars", ".hcl":
		return "terraform"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".md", ".mdx":
		return "markdown"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	}
	return ""
}

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:       current.InputTokens - previous.InputTokens,
		CachedInputTokens: current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:      current.OutputTokens - previous.OutputTokens,
		ReasoningTokens:   current.ReasoningTokens - previous.ReasoningTokens,
		ToolTokens:        current.ToolTokens - previous.ToolTokens,
		TotalTokens:       current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningTokens >= 0 &&
		delta.ToolTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func normalizeClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Other"
	}
	return name
}

func sanitizeMetricName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func getModelContextLimit(model string) int {
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "1.5-pro"), strings.Contains(model, "1.5-flash-8b"):
		return 2_000_000
	case strings.Contains(model, "1.5-flash"):
		return 1_000_000
	case strings.Contains(model, "2.0-flash"):
		return 1_000_000
	case strings.Contains(model, "gemini-3"), strings.Contains(model, "gemini-exp"):
		return 2_000_000
	case strings.Contains(model, "pro"):
		return 32_000
	case strings.Contains(model, "flash"):
		return 32_000
	}
	return 0
}

func dayFromTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, timestamp); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if len(timestamp) >= 10 {
		candidate := timestamp[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func dayFromSession(startTime, lastUpdated string) string {
	if day := dayFromTimestamp(lastUpdated); day != "" {
		return day
	}
	return dayFromTimestamp(startTime)
}

func isQuotaLimitMessage(content json.RawMessage) bool {
	text := strings.ToLower(parseMessageContentText(content))
	if text == "" {
		return false
	}
	return strings.Contains(text, "usage limit reached") ||
		strings.Contains(text, "all pro models") ||
		strings.Contains(text, "/stats for usage details")
}

func parseMessageContentText(content json.RawMessage) string {
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return ""
	}

	var asString string
	if content[0] == '"' && json.Unmarshal(content, &asString) == nil {
		return asString
	}

	var asArray []map[string]any
	if content[0] == '[' && json.Unmarshal(content, &asArray) == nil {
		var parts []string
		for _, item := range asArray {
			if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}

	return string(content)
}
