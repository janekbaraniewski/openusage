package copilot

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/samber/lo"
)

func parseCompactionLine(line string) logTokenEntry {
	var entry logTokenEntry

	if len(line) >= 24 {
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", line[:24]); err == nil {
			entry.Timestamp = t
		}
	}

	parenStart := strings.Index(line, "(")
	parenEnd := strings.Index(line, " tokens)")
	if parenStart >= 0 && parenEnd > parenStart {
		inner := line[parenStart+1 : parenEnd]
		parts := strings.Split(inner, "/")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &entry.Used)
			fmt.Sscanf(parts[1], "%d", &entry.Total)
		}
	}

	return entry
}

func sortCompactionEntries(entries []logTokenEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ti := entries[i].Timestamp
		tj := entries[j].Timestamp
		switch {
		case ti.IsZero() && tj.IsZero():
			return entries[i].Used < entries[j].Used
		case ti.IsZero():
			return false
		case tj.IsZero():
			return true
		default:
			return ti.Before(tj)
		}
	})
}

func newestCompactionEntry(entries []logTokenEntry) (logTokenEntry, bool) {
	if len(entries) == 0 {
		return logTokenEntry{}, false
	}
	best := entries[0]
	for _, te := range entries[1:] {
		if best.Timestamp.IsZero() && !te.Timestamp.IsZero() {
			best = te
			continue
		}
		if !best.Timestamp.IsZero() && te.Timestamp.IsZero() {
			continue
		}
		if !te.Timestamp.IsZero() && te.Timestamp.After(best.Timestamp) {
			best = te
			continue
		}
		if best.Timestamp.Equal(te.Timestamp) && te.Used > best.Used {
			best = te
		}
	}
	return best, true
}

func parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		result[key] = val
	}
	return result
}

func storeSeries(snap *core.UsageSnapshot, key string, m map[string]float64) {
	if len(m) > 0 {
		snap.DailySeries[key] = core.SortedTimePoints(m)
	}
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func dayForSession(createdAt, updatedAt time.Time) string {
	if !updatedAt.IsZero() {
		return updatedAt.Format("2006-01-02")
	}
	if !createdAt.IsZero() {
		return createdAt.Format("2006-01-02")
	}
	return ""
}

func latestSeriesValue(m map[string]float64) (string, float64) {
	if len(m) == 0 {
		return "", 0
	}
	dates := core.SortedStringKeys(m)
	last := dates[len(dates)-1]
	return last, m[last]
}

func sumLastNDays(m map[string]float64, days int) float64 {
	if len(m) == 0 || days <= 0 {
		return 0
	}
	date, _ := latestSeriesValue(m)
	if date == "" {
		return 0
	}
	end, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	start := end.AddDate(0, 0, -(days - 1))
	sum := 0.0
	for d, v := range m {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if !t.Before(start) && !t.After(end) {
			sum += v
		}
	}
	return sum
}

func topModelNames(tokenMap map[string]float64, messageMap map[string]int, limit int) []string {
	type row struct {
		model    string
		tokens   float64
		messages int
	}

	seen := make(map[string]bool)
	var rows []row
	for model, tokens := range tokenMap {
		seen[model] = true
		rows = append(rows, row{model: model, tokens: tokens, messages: messageMap[model]})
	}
	for model, messages := range messageMap {
		if seen[model] {
			continue
		}
		rows = append(rows, row{model: model, messages: messages})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tokens == rows[j].tokens {
			if rows[i].messages == rows[j].messages {
				return rows[i].model < rows[j].model
			}
			return rows[i].messages > rows[j].messages
		}
		return rows[i].tokens > rows[j].tokens
	})

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return lo.Map(rows, func(r row, _ int) string { return r.model })
}

func topCopilotClientNames(tokenMap map[string]float64, sessionMap, messageMap map[string]int, limit int) []string {
	type row struct {
		client   string
		tokens   float64
		sessions int
		messages int
	}

	seen := make(map[string]bool)
	var rows []row
	for client, tokens := range tokenMap {
		seen[client] = true
		rows = append(rows, row{
			client:   client,
			tokens:   tokens,
			sessions: sessionMap[client],
			messages: messageMap[client],
		})
	}
	for client, sessions := range sessionMap {
		if seen[client] {
			continue
		}
		seen[client] = true
		rows = append(rows, row{
			client:   client,
			sessions: sessions,
			messages: messageMap[client],
		})
	}
	for client, messages := range messageMap {
		if seen[client] {
			continue
		}
		rows = append(rows, row{
			client:   client,
			messages: messages,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tokens == rows[j].tokens {
			if rows[i].sessions == rows[j].sessions {
				if rows[i].messages == rows[j].messages {
					return rows[i].client < rows[j].client
				}
				return rows[i].messages > rows[j].messages
			}
			return rows[i].sessions > rows[j].sessions
		}
		return rows[i].tokens > rows[j].tokens
	})

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return lo.Map(rows, func(r row, _ int) string { return r.client })
}

func normalizeCopilotClient(repo, cwd string) string {
	repo = strings.TrimSpace(repo)
	if repo != "" && repo != "." {
		return repo
	}

	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		base := strings.TrimSpace(filepath.Base(cwd))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}

	return "cli"
}

func formatCopilotClientUsage(clients []string, labels map[string]string, tokens map[string]float64, sessions map[string]int) string {
	if len(clients) == 0 {
		return ""
	}

	parts := make([]string, 0, len(clients))
	for _, client := range clients {
		label := labels[client]
		if label == "" {
			label = client
		}

		value := tokens[client]
		sessionCount := sessions[client]

		item := fmt.Sprintf("%s %s tok", label, formatCopilotTokenCount(value))
		if sessionCount > 0 {
			item += fmt.Sprintf(" · %d sess", sessionCount)
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, ", ")
}

func formatCopilotTokenCount(value float64) string { return shared.FormatTokenCountF(value) }

func parseDayFromTimestamp(ts string) string {
	t := flexParseTime(ts)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func flexParseTime(s string) time.Time {
	return shared.FlexParseTime(s)
}

func parseCopilotTime(s string) time.Time {
	return shared.FlexParseTime(s)
}

func extractModelFromInfoMsg(msg string) string {
	idx := strings.Index(msg, ": ")
	if idx < 0 {
		return ""
	}
	m := strings.TrimSpace(msg[idx+2:])
	if pIdx := strings.Index(m, " ("); pIdx >= 0 {
		m = m[:pIdx]
	}
	return m
}

func extractCopilotToolName(raw json.RawMessage) string {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ""
	}

	var tool struct {
		Name     string `json:"name"`
		ToolName string `json:"toolName"`
		Tool     string `json:"tool"`
	}
	if err := json.Unmarshal(raw, &tool); err != nil {
		return ""
	}

	candidates := []string{tool.Name, tool.ToolName, tool.Tool}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func isCopilotMutatingTool(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false
	}
	return strings.Contains(name, "edit") ||
		strings.Contains(name, "write") ||
		strings.Contains(name, "create") ||
		strings.Contains(name, "delete") ||
		strings.Contains(name, "rename") ||
		strings.Contains(name, "move") ||
		strings.Contains(name, "replace")
}

func extractCopilotToolCommand(raw json.RawMessage) string {
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

func extractCopilotToolPaths(raw json.RawMessage) []string {
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
			for _, token := range extractCopilotPathTokens(value) {
				candidates[token] = true
			}
		}
	}
	walk(payload, false)

	return core.SortedStringKeys(candidates)
}

func extractCopilotPathTokens(raw string) []string {
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

func estimateCopilotToolLineDelta(raw json.RawMessage) (added int, removed int) {
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

func inferCopilotLanguageFromPath(path string) string {
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

func formatModelMap(m map[string]int, unit string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for model, count := range m {
		parts = append(parts, fmt.Sprintf("%s: %d %s", model, count, unit))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func formatModelMapPlain(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for model, count := range m {
		parts = append(parts, fmt.Sprintf("%s: %d", model, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func setRawInt(snap *core.UsageSnapshot, key string, v int) {
	if v > 0 {
		snap.Raw[key] = fmt.Sprintf("%d", v)
	}
}

func setRawStr(snap *core.UsageSnapshot, key, v string) {
	if v != "" {
		snap.Raw[key] = v
	}
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
