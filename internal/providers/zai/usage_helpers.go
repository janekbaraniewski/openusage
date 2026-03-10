package zai

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func captureEndpointPayload(snap *core.UsageSnapshot, endpoint string, body []byte) {
	if snap == nil {
		return
	}
	endpointSlug := sanitizeMetricSlug(endpoint)
	if endpointSlug == "" {
		endpointSlug = "unknown"
	}
	prefix := "api_" + endpointSlug

	if len(body) == 0 {
		return
	}
	setUsedMetric(snap, prefix+"_payload_bytes", float64(len(body)), "bytes", "current")

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		snap.Raw[prefix+"_parse"] = "non_json"
		return
	}
	snap.Raw[prefix+"_parse"] = "json"

	numericByPath := make(map[string]*payloadNumericStat)
	leafCount := 0
	objectCount := 0
	arrayCount := 0
	walkPayloadStats("", payload, numericByPath, &leafCount, &objectCount, &arrayCount)

	setUsedMetric(snap, prefix+"_field_count", float64(leafCount), "fields", "current")
	setUsedMetric(snap, prefix+"_object_nodes", float64(objectCount), "objects", "current")
	setUsedMetric(snap, prefix+"_array_nodes", float64(arrayCount), "arrays", "current")
	setUsedMetric(snap, prefix+"_numeric_count", float64(len(numericByPath)), "fields", "current")

	type numericEntry struct {
		path string
		stat *payloadNumericStat
	}
	entries := make([]numericEntry, 0, len(numericByPath))
	for path, stat := range numericByPath {
		if stat == nil {
			continue
		}
		entries = append(entries, numericEntry{path: path, stat: stat})
	}
	sort.Slice(entries, func(i, j int) bool {
		left := math.Abs(entries[i].stat.Sum)
		right := math.Abs(entries[j].stat.Sum)
		if left != right {
			return left > right
		}
		return entries[i].path < entries[j].path
	})

	if len(entries) > 0 {
		top := entries
		if len(top) > 8 {
			top = top[:8]
		}
		parts := make([]string, 0, len(top))
		for _, entry := range top {
			value := entry.stat.Last
			if entry.stat.Count > 1 {
				value = entry.stat.Sum
			}
			path := strings.TrimSpace(entry.path)
			if path == "" {
				path = "root"
			}
			parts = append(parts, fmt.Sprintf("%s=%s", path, formatPayloadValue(value)))
		}
		snap.Raw[prefix+"_numeric_top"] = strings.Join(parts, ", ")
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})
	emitted := 0
	maxDynamicMetrics := 96
	for _, entry := range entries {
		if emitted >= maxDynamicMetrics {
			break
		}
		pathSlug := sanitizeMetricSlug(strings.Trim(entry.path, "._"))
		if pathSlug == "" {
			pathSlug = "root"
		}
		metricKey := prefix + "_" + pathSlug
		if _, exists := snap.Metrics[metricKey]; exists {
			continue
		}
		value := entry.stat.Last
		if entry.stat.Count > 1 {
			value = entry.stat.Sum
		}
		setUsedMetric(snap, metricKey, value, "value", "current")
		emitted++
	}
	if len(entries) > emitted {
		snap.Raw[prefix+"_numeric_omitted"] = strconv.Itoa(len(entries) - emitted)
	}
}

func walkPayloadStats(path string, v any, numericByPath map[string]*payloadNumericStat, leafCount, objectCount, arrayCount *int) {
	switch value := v.(type) {
	case map[string]any:
		if objectCount != nil {
			*objectCount = *objectCount + 1
		}
		keys := core.SortedStringKeys(value)
		for _, key := range keys {
			next := appendPayloadPath(path, key)
			walkPayloadStats(next, value[key], numericByPath, leafCount, objectCount, arrayCount)
		}
	case []any:
		if arrayCount != nil {
			*arrayCount = *arrayCount + 1
		}
		next := appendPayloadPath(path, "items")
		for _, item := range value {
			walkPayloadStats(next, item, numericByPath, leafCount, objectCount, arrayCount)
		}
	default:
		if leafCount != nil {
			*leafCount = *leafCount + 1
		}
		if numericByPath == nil {
			return
		}
		numeric, ok := parseFloat(v)
		if !ok {
			return
		}
		key := strings.TrimSpace(path)
		if key == "" {
			key = "root"
		}
		stat := numericByPath[key]
		if stat == nil {
			stat = &payloadNumericStat{Min: numeric, Max: numeric}
			numericByPath[key] = stat
		}
		stat.Count++
		stat.Sum += numeric
		stat.Last = numeric
		if numeric < stat.Min {
			stat.Min = numeric
		}
		if numeric > stat.Max {
			stat.Max = numeric
		}
	}
}

func appendPayloadPath(path, segment string) string {
	path = strings.TrimSpace(path)
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return path
	}
	if path == "" {
		return segment
	}
	return path + "." + segment
}

func formatPayloadValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func applyUsageRange(reqURL string) (string, error) {
	parsed, err := url.Parse(reqURL)
	if err != nil {
		return "", err
	}
	start, end := usageWindow()
	q := parsed.Query()
	q.Set("startTime", start)
	q.Set("endTime", end)
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func usageWindow() (start, end string) {
	now := time.Now().UTC()
	startTime := time.Date(now.Year(), now.Month(), now.Day()-6, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	return startTime.Format("2006-01-02 15:04:05"), endTime.Format("2006-01-02 15:04:05")
}

func joinURL(base, endpoint string) string {
	trimmedBase := strings.TrimRight(base, "/")
	trimmedEndpoint := strings.TrimLeft(endpoint, "/")
	return trimmedBase + "/" + trimmedEndpoint
}

func parseAPIError(body []byte) (code, msg string) {
	var payload struct {
		Code    any       `json:"code"`
		Msg     string    `json:"msg"`
		Message string    `json:"message"`
		Error   *apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}

	if payload.Error != nil {
		if payload.Error.Message != "" {
			msg = payload.Error.Message
		}
		if payload.Error.Code != nil {
			code = anyToString(payload.Error.Code)
		}
	}
	if code == "" && payload.Code != nil {
		code = anyToString(payload.Code)
	}
	if msg == "" {
		msg = core.FirstNonEmpty(payload.Message, payload.Msg)
	}
	return code, msg
}

func parseCostUSD(row map[string]any) float64 {
	if cents, ok := firstNumberByPaths(row,
		[]string{"cost_cents"},
		[]string{"costCents"},
		[]string{"total_cost_cents"},
		[]string{"totalCostCents"},
		[]string{"usage", "cost_cents"},
	); ok {
		return cents / 100
	}

	if micros, ok := firstNumberByPaths(row,
		[]string{"cost_micros"},
		[]string{"costMicros"},
		[]string{"total_cost_micros"},
		[]string{"totalCostMicros"},
	); ok {
		return micros / 1_000_000
	}

	value, ok := firstNumberByPaths(row,
		[]string{"cost_usd"},
		[]string{"costUSD"},
		[]string{"total_cost_usd"},
		[]string{"totalCostUSD"},
		[]string{"total_cost"},
		[]string{"totalCost"},
		[]string{"api_cost"},
		[]string{"apiCost"},
		[]string{"cost"},
		[]string{"amount"},
		[]string{"total_amount"},
		[]string{"totalAmount"},
		[]string{"usage", "cost_usd"},
		[]string{"usage", "costUSD"},
		[]string{"usage", "cost"},
	)
	if ok {
		return value
	}
	return 0
}

func parseNumberFromMap(row map[string]any, keys ...string) (float64, bool) {
	value, _, ok := firstNumberWithKey(row, keys...)
	return value, ok
}

func firstNumberWithKey(row map[string]any, keys ...string) (float64, string, bool) {
	for _, key := range keys {
		raw, ok := mapValue(row, key)
		if !ok {
			continue
		}
		if parsed, ok := parseFloat(raw); ok {
			return parsed, key, true
		}
	}
	return 0, "", false
}

func parseFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case int32:
		return float64(value), true
	case int16:
		return float64(value), true
	case int8:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint64:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint8:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func firstStringFromMap(row map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := mapValue(row, key)
		if !ok || raw == nil {
			continue
		}
		str := strings.TrimSpace(anyToString(raw))
		if str != "" {
			return str
		}
	}
	return ""
}

func firstAnyFromMap(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if raw, ok := mapValue(row, key); ok {
			return raw
		}
	}
	return nil
}

func mapValue(row map[string]any, key string) (any, bool) {
	if row == nil {
		return nil, false
	}
	if raw, ok := row[key]; ok {
		return raw, true
	}
	for candidate, raw := range row {
		if strings.EqualFold(candidate, key) {
			return raw, true
		}
	}
	return nil, false
}

func valueAtPath(row map[string]any, path []string) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}

	var current any = row
	for _, segment := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := mapValue(node, segment)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func firstAnyByPaths(row map[string]any, paths ...[]string) any {
	for _, path := range paths {
		if raw, ok := valueAtPath(row, path); ok {
			return raw
		}
	}
	return nil
}

func firstStringByPaths(row map[string]any, paths ...[]string) string {
	for _, path := range paths {
		raw, ok := valueAtPath(row, path)
		if !ok || raw == nil {
			continue
		}
		text := strings.TrimSpace(anyToString(raw))
		if text != "" {
			return text
		}
	}
	return ""
}

func firstNumberByPaths(row map[string]any, paths ...[]string) (float64, bool) {
	for _, path := range paths {
		raw, ok := valueAtPath(row, path)
		if !ok {
			continue
		}
		if parsed, ok := parseFloat(raw); ok {
			return parsed, true
		}
	}
	return 0, false
}

func normalizeUsageDimension(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "\"'")
	if value == "" {
		return ""
	}
	switch strings.ToLower(value) {
	case "null", "nil", "n/a", "na", "unknown":
		return ""
	default:
		return value
	}
}

func accumulateRollupValues(acc *usageRollup, sample usageSample) {
	if acc == nil {
		return
	}
	acc.Requests += sample.Requests
	acc.Input += sample.Input
	acc.Output += sample.Output
	acc.Reasoning += sample.Reasoning
	acc.Total += sample.Total
	acc.CostUSD += sample.CostUSD
}

func accumulateUsageRollup(target map[string]*usageRollup, key string, sample usageSample) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	acc, ok := target[key]
	if !ok {
		acc = &usageRollup{}
		target[key] = acc
	}
	accumulateRollupValues(acc, sample)
}

func sortedUsageRollupKeys(values map[string]*usageRollup) []string {
	return core.SortedStringKeys(values)
}

func summarizeShareUsage(values map[string]float64, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	var (
		list  []item
		total float64
	)
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
		total += value
	}
	if len(list) == 0 || total <= 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f%%", normalizeUsageLabel(entry.name), entry.value/total*100))
	}
	return strings.Join(parts, ", ")
}

func summarizeCountUsage(values map[string]float64, unit string, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	var list []item
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f %s", normalizeUsageLabel(entry.name), entry.value, unit))
	}
	return strings.Join(parts, ", ")
}

func normalizeUsageLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("_", " ", "-", " ")
	return replacer.Replace(value)
}

func inferModelUsageLanguage(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return ""
	}
	switch {
	case strings.Contains(model, "coder"), strings.Contains(model, "code"), strings.Contains(model, "codestral"), strings.Contains(model, "devstral"):
		return "code"
	case strings.Contains(model, "vision"), strings.Contains(model, "image"), strings.Contains(model, "multimodal"), strings.Contains(model, "omni"), strings.Contains(model, "vl"):
		return "multimodal"
	case strings.Contains(model, "audio"), strings.Contains(model, "speech"), strings.Contains(model, "voice"), strings.Contains(model, "whisper"), strings.Contains(model, "tts"), strings.Contains(model, "stt"):
		return "audio"
	case strings.Contains(model, "reason"), strings.Contains(model, "thinking"):
		return "reasoning"
	default:
		return "general"
	}
}

func anyToString(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		if math.Mod(value, 1) == 0 {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case bool:
		return strconv.FormatBool(value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func normalizeDate(raw any) string {
	if raw == nil {
		return ""
	}

	if ts, ok := parseTimeValue(raw); ok {
		return ts.UTC().Format("2006-01-02")
	}

	value := strings.TrimSpace(anyToString(raw))
	if value == "" {
		return ""
	}
	if len(value) >= 10 {
		candidate := value[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func parseTimeValue(raw any) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}

	if n, ok := parseFloat(raw); ok {
		if n <= 0 {
			return time.Time{}, false
		}
		sec := int64(n)
		if n > 1e12 {
			sec = int64(n / 1000)
		}
		return time.Unix(sec, 0).UTC(), true
	}

	value := strings.TrimSpace(anyToString(raw))
	if value == "" {
		return time.Time{}, false
	}

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}

	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n > 1e12 {
			return time.Unix(n/1000, 0).UTC(), true
		}
		return time.Unix(n, 0).UTC(), true
	}

	return time.Time{}, false
}

func isJSONEmpty(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null" || trimmed == "{}" || trimmed == "[]"
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if key == "" || value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   core.Float64Ptr(value),
		Unit:   unit,
		Window: window,
	}
}

func sanitizeMetricSlug(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '_':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return "unknown"
	}
	return slug
}

func clamp(value, minVal, maxVal float64) float64 {
	return lo.Clamp(value, minVal, maxVal)
}

func apiErrorMessage(err *apiError) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Message)
}

func isNoPackageCode(code, msg string) bool {
	code = strings.TrimSpace(code)
	if code == "1113" {
		return true
	}
	lowerMsg := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(lowerMsg, "insufficient balance") ||
		strings.Contains(lowerMsg, "no resource package") ||
		strings.Contains(lowerMsg, "no active coding package")
}
