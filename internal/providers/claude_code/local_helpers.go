package claude_code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/samber/lo"
)

func parseJSONLTimestamp(raw string) (time.Time, bool) {
	t, err := shared.ParseTimestampString(raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func isMutatingTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	return strings.Contains(n, "edit") ||
		strings.Contains(n, "write") ||
		strings.Contains(n, "create") ||
		strings.Contains(n, "delete") ||
		strings.Contains(n, "rename") ||
		strings.Contains(n, "move")
}

func extractToolCommand(input any) string {
	var command string
	var walk func(value any)
	walk = func(value any) {
		if command != "" || value == nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				if k == "command" || k == "cmd" || k == "script" || k == "shell_command" {
					if s, ok := child.(string); ok {
						command = strings.TrimSpace(s)
						return
					}
				}
			}
			for _, child := range v {
				walk(child)
				if command != "" {
					return
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
				if command != "" {
					return
				}
			}
		}
	}
	walk(input)
	return command
}

func estimateToolLineDelta(toolName string, input any) (added int, removed int) {
	lineCount := func(text string) int {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		return strings.Count(text, "\n") + 1
	}
	lowerTool := strings.ToLower(strings.TrimSpace(toolName))
	var walk func(value any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			oldKeys := []string{"old_string", "old_text", "from", "replace"}
			newKeys := []string{"new_string", "new_text", "to", "with"}
			var oldText string
			var newText string
			for _, key := range oldKeys {
				if raw, ok := v[key]; ok {
					if s, ok := raw.(string); ok {
						oldText = s
						break
					}
				}
			}
			for _, key := range newKeys {
				if raw, ok := v[key]; ok {
					if s, ok := raw.(string); ok {
						newText = s
						break
					}
				}
			}
			if oldText != "" || newText != "" {
				removed += lineCount(oldText)
				added += lineCount(newText)
			}
			if strings.Contains(lowerTool, "write") || strings.Contains(lowerTool, "create") {
				if raw, ok := v["content"]; ok {
					if s, ok := raw.(string); ok {
						added += lineCount(s)
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
	return added, removed
}

func extractToolPathCandidates(input any) []string {
	pathKeyHints := map[string]bool{
		"path": true, "paths": true, "file": true, "files": true, "filepath": true, "file_path": true,
		"cwd": true, "directory": true, "dir": true, "glob": true, "pattern": true, "target": true,
		"from": true, "to": true, "include": true, "exclude": true,
	}

	candidates := make(map[string]bool)
	var walk func(value any, hinted bool)
	walk = func(value any, hinted bool) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				childHinted := hinted || pathKeyHints[k] || strings.Contains(k, "path") || strings.Contains(k, "file")
				walk(child, childHinted)
			}
		case []any:
			for _, child := range v {
				walk(child, hinted)
			}
		case string:
			if !hinted {
				return
			}
			for _, token := range extractPathTokens(v) {
				candidates[token] = true
			}
		}
	}
	walk(input, false)

	return core.SortedStringKeys(candidates)
}

func extractPathTokens(raw string) []string {
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
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return lo.Uniq(out)
}

func inferLanguageFromPath(path string) string {
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
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
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

func summarizeCountMap(values map[string]int, limit int) string {
	type entry struct {
		name  string
		value int
	}
	entries := make([]entry, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		entries = append(entries, entry{name: name, value: value})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value == entries[j].value {
			return entries[i].name < entries[j].name
		}
		return entries[i].value > entries[j].value
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		parts = append(parts, fmt.Sprintf("%s %d", name, entries[i].value))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func summarizeFloatMap(values map[string]float64, unit string, limit int) string {
	type entry struct {
		name  string
		value float64
	}
	entries := make([]entry, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		entries = append(entries, entry{name: name, value: value})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value == entries[j].value {
			return entries[i].name < entries[j].name
		}
		return entries[i].value > entries[j].value
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		value := shortTokenCount(entries[i].value)
		if unit != "" {
			value += " " + unit
		}
		parts = append(parts, fmt.Sprintf("%s %s", name, value))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func summarizeTotalsMap(values map[string]*modelUsageTotals, preferCost bool, limit int) string {
	type entry struct {
		name   string
		tokens float64
		cost   float64
	}
	entries := make([]entry, 0, len(values))
	totalCost := 0.0
	for name, totals := range values {
		if totals == nil {
			continue
		}
		tokens := totals.input + totals.output + totals.cached + totals.cacheCreate + totals.reasoning
		cost := totals.cost
		if tokens <= 0 && cost <= 0 {
			continue
		}
		totalCost += cost
		entries = append(entries, entry{name: name, tokens: tokens, cost: cost})
	}
	if len(entries) == 0 {
		return ""
	}
	useCost := preferCost && totalCost > 0
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].tokens
		right := entries[j].tokens
		if useCost {
			left = entries[i].cost
			right = entries[j].cost
		}
		if left == right {
			return entries[i].name < entries[j].name
		}
		return left > right
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		if useCost {
			parts = append(parts, fmt.Sprintf("%s %s %s tok", name, formatUSDSummary(entries[i].cost), shortTokenCount(entries[i].tokens)))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s tok", name, shortTokenCount(entries[i].tokens)))
		}
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func collectJSONLFiles(dir string) []string {
	var files []string
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return files
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	return files
}

// collectJSONLFilesWithStat walks the directory like collectJSONLFiles but also returns
// the os.FileInfo for each file, enabling cache invalidation by mtime+size.
func collectJSONLFilesWithStat(dir string) map[string]os.FileInfo {
	result := make(map[string]os.FileInfo)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return result
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			result[path] = info
		}
		return nil
	})

	return result
}

func parseJSONLFile(path string) []jsonlEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []jsonlEntry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

func sanitizeModelName(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "unknown"
	}

	result := make([]byte, 0, len(model))
	for _, c := range model {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}

	out := strings.Trim(string(result), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func setMetricMax(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	if existing, ok := snap.Metrics[key]; ok && existing.Used != nil && *existing.Used >= value {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{Used: &v, Unit: unit, Window: window}
}

func normalizeModelUsage(snap *core.UsageSnapshot) {
	modelTotals := make(map[string]*modelUsageTotals)
	legacyMetricKeys := make([]string, 0, 16)

	ensureModel := func(name string) *modelUsageTotals {
		if _, ok := modelTotals[name]; !ok {
			modelTotals[name] = &modelUsageTotals{}
		}
		return modelTotals[name]
	}

	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}

		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens")
			ensureModel(model).input += *metric.Used
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens")
			ensureModel(model).output += *metric.Used
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost_usd"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			ensureModel(model).cost += *metric.Used
		case strings.HasPrefix(key, "input_tokens_"):
			model := sanitizeModelName(strings.TrimPrefix(key, "input_tokens_"))
			ensureModel(model).input += *metric.Used
			legacyMetricKeys = append(legacyMetricKeys, key)
		case strings.HasPrefix(key, "output_tokens_"):
			model := sanitizeModelName(strings.TrimPrefix(key, "output_tokens_"))
			ensureModel(model).output += *metric.Used
			legacyMetricKeys = append(legacyMetricKeys, key)
		}
	}

	for key, value := range snap.Raw {
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_read"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_read")
			if parsed, ok := parseMetricNumber(value); ok {
				setMetricMax(snap, "model_"+model+"_cached_tokens", parsed, "tokens", "all-time")
			}
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_create"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_create")
			if parsed, ok := parseMetricNumber(value); ok {
				setMetricMax(snap, "model_"+model+"_cache_creation_tokens", parsed, "tokens", "all-time")
			}
		}
	}

	for _, key := range legacyMetricKeys {
		delete(snap.Metrics, key)
	}

	for model, totals := range modelTotals {
		modelPrefix := "model_" + sanitizeModelName(model)
		setMetricMax(snap, modelPrefix+"_input_tokens", totals.input, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_output_tokens", totals.output, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_cost_usd", totals.cost, "USD", "all-time")
	}

	buildModelUsageSummaryRaw(snap)
}

func parseMetricNumber(raw string) (float64, bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if clean == "" {
		return 0, false
	}
	fields := strings.Fields(clean)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func buildModelUsageSummaryRaw(snap *core.UsageSnapshot) {
	type entry struct {
		name   string
		input  float64
		output float64
		cost   float64
	}

	byModel := make(map[string]*entry)
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "model_") {
			continue
		}

		switch {
		case strings.HasSuffix(key, "_input_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].input += *metric.Used
		case strings.HasSuffix(key, "_output_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].output += *metric.Used
		case strings.HasSuffix(key, "_cost_usd"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].cost += *metric.Used
		}
	}

	entries := make([]entry, 0, len(byModel))
	totalTokens := float64(0)
	totalCost := float64(0)
	for _, model := range byModel {
		if model.input <= 0 && model.output <= 0 && model.cost <= 0 {
			continue
		}
		entries = append(entries, *model)
		totalTokens += model.input + model.output
		totalCost += model.cost
	}
	if len(entries) == 0 {
		delete(snap.Raw, "model_usage")
		delete(snap.Raw, "model_usage_window")
		delete(snap.Raw, "model_count")
		return
	}

	useCost := totalCost > 0
	total := totalTokens
	if useCost {
		total = totalCost
	}
	if total <= 0 {
		delete(snap.Raw, "model_usage")
		delete(snap.Raw, "model_usage_window")
		delete(snap.Raw, "model_count")
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].input + entries[i].output
		right := entries[j].input + entries[j].output
		if useCost {
			left = entries[i].cost
			right = entries[j].cost
		}
		if left == right {
			return entries[i].name < entries[j].name
		}
		return left > right
	})

	limit := maxModelUsageSummaryItems
	if limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		value := entries[i].input + entries[i].output
		if useCost {
			value = entries[i].cost
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		tokens := entries[i].input + entries[i].output
		modelName := strings.ReplaceAll(entries[i].name, "_", "-")

		if useCost {
			parts = append(parts, fmt.Sprintf("%s %s %s tok (%.0f%%)", modelName, formatUSDSummary(entries[i].cost), shortTokenCount(tokens), pct))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s tok (%.0f%%)", modelName, shortTokenCount(tokens), pct))
		}
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}

	snap.Raw["model_usage"] = strings.Join(parts, ", ")
	snap.Raw["model_usage_window"] = "all-time"
	snap.Raw["model_count"] = fmt.Sprintf("%d", len(entries))
}

func shortTokenCount(v float64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", v/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", v/1_000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func formatUSDSummary(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}
