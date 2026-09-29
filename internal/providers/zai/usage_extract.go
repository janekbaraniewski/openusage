package zai

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func extractUsageSamples(raw json.RawMessage, kind string) []usageSample {
	if isJSONEmpty(raw) {
		return nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	rows := extractUsageRows(payload)
	rows = dropMirroredUsageRows(rows)
	if len(rows) == 0 {
		return nil
	}

	samples := make([]usageSample, 0, len(rows))
	for _, row := range rows {
		sample := usageSample{
			Date: normalizeDate(firstAnyByPaths(row,
				[]string{"date"},
				[]string{"day"},
				[]string{"time"},
				[]string{"timestamp"},
				[]string{"created_at"},
				[]string{"createdAt"},
				[]string{"ts"},
				[]string{"meta", "date"},
				[]string{"meta", "timestamp"},
			)),
		}

		if kind == "model" {
			sample.Name = firstStringByPaths(row,
				[]string{"model"},
				[]string{"model_id"},
				[]string{"modelId"},
				[]string{"model_name"},
				[]string{"modelName"},
				[]string{"name"},
				[]string{"model", "id"},
				[]string{"model", "name"},
				[]string{"model", "modelId"},
				[]string{"meta", "model"},
			)
		} else {
			sample.Name = firstStringByPaths(row,
				[]string{"tool"},
				[]string{"tool_name"},
				[]string{"toolName"},
				[]string{"name"},
				[]string{"tool_id"},
				[]string{"toolId"},
				[]string{"tool", "name"},
				[]string{"tool", "id"},
				[]string{"meta", "tool"},
			)
		}
		sample.Client = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"client"},
			[]string{"client_name"},
			[]string{"clientName"},
			[]string{"application"},
			[]string{"app"},
			[]string{"sdk"},
			[]string{"meta", "client"},
			[]string{"client", "name"},
			[]string{"context", "client"},
		))
		sample.Source = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"source"},
			[]string{"source_name"},
			[]string{"sourceName"},
			[]string{"origin"},
			[]string{"channel"},
			[]string{"meta", "source"},
			[]string{"meta", "origin"},
		))
		sample.Provider = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"provider"},
			[]string{"provider_name"},
			[]string{"providerName"},
			[]string{"upstream_provider"},
			[]string{"upstreamProvider"},
			[]string{"model", "provider"},
			[]string{"model", "provider_name"},
			[]string{"route", "provider_name"},
		))
		sample.Interface = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"interface"},
			[]string{"interface_name"},
			[]string{"interfaceName"},
			[]string{"mode"},
			[]string{"client_type"},
			[]string{"entrypoint"},
			[]string{"meta", "interface"},
		))
		sample.Endpoint = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"endpoint"},
			[]string{"endpoint_name"},
			[]string{"endpointName"},
			[]string{"route"},
			[]string{"path"},
			[]string{"meta", "endpoint"},
		))
		sample.Language = normalizeUsageDimension(firstStringByPaths(row,
			[]string{"language"},
			[]string{"language_name"},
			[]string{"languageName"},
			[]string{"lang"},
			[]string{"programming_language"},
			[]string{"programmingLanguage"},
			[]string{"code_language"},
			[]string{"codeLanguage"},
			[]string{"input_language"},
			[]string{"inputLanguage"},
			[]string{"file_language"},
			[]string{"meta", "language"},
		))
		bucket := strings.ToLower(strings.TrimSpace(firstStringByPaths(row, []string{"__usage_bucket"})))
		usageKey := normalizeUsageDimension(firstStringByPaths(row, []string{"__usage_key"}))
		// Whole-window rollups describe the payload as a whole, not an
		// entity, so they are never treated as a named model/tool row (the
		// name fallbacks below are skipped) and the projections treat them as
		// window totals instead of per-entity values. Both monitor payloads use
		// the same shape: model-usage carries totalModelCallCount/
		// totalTokensUsage, tool-usage carries totalNetworkSearchCount and
		// friends.
		sample.Aggregate = isUsageRollupRow(row)

		if sample.Language == "" && usageKey != "" && strings.Contains(bucket, "language") {
			sample.Language = usageKey
		}
		if sample.Client == "" && usageKey != "" && strings.Contains(bucket, "client") {
			sample.Client = usageKey
		}
		if sample.Source == "" && usageKey != "" && strings.Contains(bucket, "source") {
			sample.Source = usageKey
		}
		if sample.Provider == "" && usageKey != "" && strings.Contains(bucket, "provider") {
			sample.Provider = usageKey
		}
		if sample.Interface == "" && usageKey != "" && strings.Contains(bucket, "interface") {
			sample.Interface = usageKey
		}
		if sample.Endpoint == "" && usageKey != "" && strings.Contains(bucket, "endpoint") {
			sample.Endpoint = usageKey
		}
		if kind == "model" && sample.Name == "" && !sample.Aggregate && usageKey != "" && (strings.Contains(bucket, "model") || bucket == "") {
			sample.Name = usageKey
		}
		if kind == "tool" && sample.Name == "" && !sample.Aggregate && usageKey != "" && (strings.Contains(bucket, "tool") || bucket == "") {
			sample.Name = usageKey
		}

		if sample.Source == "" && sample.Client != "" {
			sample.Source = sample.Client
		}
		if sample.Client == "" && sample.Source != "" {
			sample.Client = sample.Source
		}

		if sample.Provider == "" {
			modelProviderHint := normalizeUsageDimension(firstStringByPaths(row,
				[]string{"model", "provider"},
				[]string{"model", "provider_name"},
				[]string{"model", "vendor"},
			))
			if modelProviderHint != "" {
				sample.Provider = modelProviderHint
			}
		}

		sample.Requests, _ = firstNumberByPaths(row,
			[]string{"requests"},
			[]string{"request_count"},
			[]string{"requestCount"},
			[]string{"request_num"},
			[]string{"requestNum"},
			[]string{"calls"},
			[]string{"callCount"},
			[]string{"count"},
			[]string{"usageCount"},
			[]string{"usage", "requests"},
			[]string{"stats", "requests"},
			// Rollup-style names, mirroring totalTokens on the token side, so a
			// per-entity row carrying only a total* count still reports calls.
			[]string{"totalCalls"},
			[]string{"totalCallCount"},
			[]string{"totalCount"},
			[]string{"totalRequestCount"},
			[]string{"totalRequests"},
		)
		sample.Input, _ = firstNumberByPaths(row,
			[]string{"input_tokens"},
			[]string{"inputTokens"},
			[]string{"input_token_count"},
			[]string{"prompt_tokens"},
			[]string{"promptTokens"},
			[]string{"usage", "input_tokens"},
			[]string{"usage", "inputTokens"},
		)
		sample.Output, _ = firstNumberByPaths(row,
			[]string{"output_tokens"},
			[]string{"outputTokens"},
			[]string{"completion_tokens"},
			[]string{"completionTokens"},
			[]string{"usage", "output_tokens"},
			[]string{"usage", "outputTokens"},
		)
		sample.Reasoning, _ = firstNumberByPaths(row,
			[]string{"reasoning_tokens"},
			[]string{"reasoningTokens"},
			[]string{"thinking_tokens"},
			[]string{"thinkingTokens"},
			[]string{"usage", "reasoning_tokens"},
		)
		sample.Total, _ = firstNumberByPaths(row,
			[]string{"total_tokens"},
			[]string{"totalTokens"},
			[]string{"tokens"},
			[]string{"token_count"},
			[]string{"tokenCount"},
			[]string{"usage", "total_tokens"},
			[]string{"usage", "totalTokens"},
		)
		if sample.Total == 0 {
			sample.Total = sample.Input + sample.Output + sample.Reasoning
		}
		sample.CostUSD = parseCostUSD(row)
		if kind == "model" && sample.Language == "" {
			sample.Language = inferModelUsageLanguage(sample.Name)
		}

		if sample.Requests > 0 || sample.Total > 0 || sample.CostUSD > 0 || sample.Name != "" {
			samples = append(samples, sample)
		}
	}

	return samples
}

func extractUsageRows(v any) []map[string]any {
	return extractUsageRowsAt(v, "")
}

// extractUsageRowsAt walks a generic usage payload and returns the row maps it
// holds. path is the "."-joined payload location of v, recorded on every list
// of rows as __usage_path so a mirrored list can be told apart from another
// list that merely shares its key.
func extractUsageRowsAt(v any, path string) []map[string]any {
	switch value := v.(type) {
	case []any:
		rows := mapsFromArray(value)
		if len(rows) > 0 {
			return tagUsageRowPath(rows, path)
		}
		var nested []map[string]any
		for _, item := range value {
			nested = append(nested, extractUsageRowsAt(item, path)...)
		}
		return nested
	case map[string]any:
		if looksLikeUsageRow(value) {
			return tagUsageRowPath([]map[string]any{value}, path)
		}

		keys := []string{
			"data", "items", "list", "rows", "records", "usage",
			"model_usage", "modelUsage",
			"tool_usage", "toolUsage",
			"language_usage", "languageUsage",
			"client_usage", "clientUsage",
			"source_usage", "sourceUsage",
			"provider_usage", "providerUsage",
			"endpoint_usage", "endpointUsage",
			"result",
		}
		var combined []map[string]any
		for _, key := range keys {
			if nested, ok := mapValue(value, key); ok {
				rows := extractUsageRowsAt(nested, joinUsagePath(path, key))
				if len(rows) > 0 {
					for _, row := range rows {
						tagged := row
						if firstStringFromMap(row, "__usage_bucket") == "" {
							tagged = cloneStringAnyMap(row)
							tagged["__usage_bucket"] = key
						}
						combined = append(combined, tagged)
					}
				}
			}
		}
		if len(combined) > 0 {
			return combined
		}

		mapKeys := core.SortedStringKeys(value)

		var all []map[string]any
		for _, key := range mapKeys {
			nested := value[key]
			rows := extractUsageRowsAt(nested, joinUsagePath(path, key))
			if len(rows) > 0 {
				for _, row := range rows {
					tagged := row
					if firstStringFromMap(row, "__usage_key") == "" {
						tagged = cloneStringAnyMap(row)
						tagged["__usage_key"] = key
					}
					all = append(all, tagged)
				}
				continue
			}
			if numeric, ok := parseFloat(nested); ok {
				field, rollup := scalarUsageField(key)
				row := map[string]any{
					field:         numeric,
					"__usage_key": key,
				}
				if rollup {
					row["__usage_rollup"] = true
				}
				all = append(all, row)
			}
		}
		return all
	default:
		return nil
	}
}

func joinUsagePath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// tagUsageRowPath records a row's payload location, leaving rows that already
// carry one alone: an inner list keeps its own, more specific, location.
func tagUsageRowPath(rows []map[string]any, path string) []map[string]any {
	if path == "" {
		return rows
	}
	tagged := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if firstStringFromMap(row, "__usage_path") != "" {
			tagged = append(tagged, row)
			continue
		}
		clone := cloneStringAnyMap(row)
		clone["__usage_path"] = path
		tagged = append(tagged, clone)
	}
	return tagged
}

// scalarUsageField classifies a scalar numeric key found while walking a
// generic usage payload into the usageSample field it represents.
//
// Z.AI's monitor payloads mix per-entity counts with whole-window rollups
// (totalTokensUsage, totalModelCallCount) in the same object, and the walker
// previously assumed every scalar was a request count — so a token
// rollup surfaced as a request count. Token-count keys are routed to
// total_tokens; other scalars keep the historical request meaning so keyed
// breakdowns like {"go": 3} still work.
//
// rollup is true for keys that total the whole payload rather than count a
// single entity; those values are the window totals (authoritative for model
// usage; a fallback for tool usage, whose per-tool totals may overlap).
func scalarUsageField(key string) (field string, rollup bool) {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return "requests", false
	}
	rollup = strings.HasPrefix(normalized, "total") &&
		(strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "call") ||
			strings.Contains(normalized, "request") ||
			strings.Contains(normalized, "count"))
	if strings.Contains(normalized, "token") {
		return "total_tokens", rollup
	}
	return "requests", rollup
}

func isUsageRollupRow(row map[string]any) bool {
	raw, ok := row["__usage_rollup"]
	if !ok {
		return false
	}
	rollup, ok := raw.(bool)
	return ok && rollup
}

// mirroredUsageBreakdowns maps a summary key that repeats a richer sibling
// breakdown to the key it mirrors. Z.AI's model- and tool-usage payloads carry
// the same summary list at the top level and again inside totalUsage, next to
// the richer data list; accumulating every copy triples the model token totals
// and doubles the tool call counts.
var mirroredUsageBreakdowns = map[string]string{
	"modelSummaryList": "modelDataList",
	"toolSummaryList":  "toolDataList",
}

// dropMirroredUsageRows removes the redundant copies of a mirrored breakdown
// list. A summary list is dropped when the richer list it mirrors is present,
// so the per-entity values feed the metrics once. A repeated copy of the
// summary is dropped even when the richer list is absent, because the live
// payload nests the same summary both under totalUsage and at the top level;
// copies are compared by content so genuinely different lists are never
// collapsed into one.
func dropMirroredUsageRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}

	runs := mirroredRowRuns(rows)
	present := make(map[string]bool, len(runs))
	for _, run := range runs {
		present[run.key] = true
	}

	drop := make([]bool, len(rows))
	keptSignature := make(map[string]string, len(mirroredUsageBreakdowns))
	for _, run := range runs {
		primary, mirrored := mirroredUsageBreakdowns[run.key]
		if !mirrored {
			continue
		}
		if present[primary] {
			// The richer list this summary mirrors is present; the entities
			// are read from it, so every copy of the summary is redundant.
			for _, i := range run.indices {
				drop[i] = true
			}
			continue
		}
		signature := mirroredGroupSignature(rows, run.indices)
		if previous, ok := keptSignature[run.key]; ok && previous == signature {
			// The same summary repeated at another payload location.
			for _, i := range run.indices {
				drop[i] = true
			}
			continue
		}
		keptSignature[run.key] = signature
	}

	kept := make([]map[string]any, 0, len(rows))
	for i, row := range rows {
		if drop[i] {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// mirroredRowRun is the set of rows contributed by one breakdown list; the
// rows of a list share the key and payload path they were read from.
type mirroredRowRun struct {
	key     string
	indices []int
}

func mirroredRowRuns(rows []map[string]any) []mirroredRowRun {
	index := make(map[[2]string]int, len(rows))
	runs := make([]mirroredRowRun, 0, len(rows))
	for i, row := range rows {
		id := [2]string{
			firstStringFromMap(row, "__usage_key"),
			firstStringFromMap(row, "__usage_path"),
		}
		if at, ok := index[id]; ok {
			runs[at].indices = append(runs[at].indices, i)
			continue
		}
		index[id] = len(runs)
		runs = append(runs, mirroredRowRun{key: id[0], indices: []int{i}})
	}
	return runs
}

// mirroredGroupSignature renders a breakdown group in a comparable form so a
// repeated copy of the same list can be recognised. Internal __usage_* tags are
// excluded because they record where the copy was found, not what it contains.
func mirroredGroupSignature(rows []map[string]any, indices []int) string {
	var b strings.Builder
	for _, i := range indices {
		for _, key := range core.SortedStringKeys(rows[i]) {
			if strings.HasPrefix(key, "__") {
				continue
			}
			fmt.Fprintf(&b, "%s=%v;", key, rows[i][key])
		}
		b.WriteByte('|')
	}
	return b.String()
}

func extractLimitRows(v any) []map[string]any {
	switch value := v.(type) {
	case []any:
		return mapsFromArray(value)
	case map[string]any:
		if _, ok := value["type"]; ok {
			return []map[string]any{value}
		}
		for _, key := range []string{"limits", "items", "data"} {
			if nested, ok := value[key]; ok {
				rows := extractLimitRows(nested)
				if len(rows) > 0 {
					return rows
				}
			}
		}
		var all []map[string]any
		for _, nested := range value {
			rows := extractLimitRows(nested)
			all = append(all, rows...)
		}
		return all
	default:
		return nil
	}
}

func extractCreditGrantRows(v any) []map[string]any {
	switch value := v.(type) {
	case []any:
		var rows []map[string]any
		for _, item := range value {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if looksLikeCreditGrantRow(row) {
				rows = append(rows, row)
				continue
			}
			rows = append(rows, extractCreditGrantRows(row)...)
		}
		return rows
	case map[string]any:
		if looksLikeCreditGrantRow(value) {
			return []map[string]any{value}
		}

		var rows []map[string]any
		for _, key := range []string{"credit_grants", "creditGrants", "grants", "items", "list", "data"} {
			nested, ok := mapValue(value, key)
			if !ok {
				continue
			}
			rows = append(rows, extractCreditGrantRows(nested)...)
		}
		if len(rows) > 0 {
			return rows
		}

		keys := core.SortedStringKeys(value)
		for _, key := range keys {
			rows = append(rows, extractCreditGrantRows(value[key])...)
		}
		return rows
	default:
		return nil
	}
}

func looksLikeCreditGrantRow(row map[string]any) bool {
	if row == nil {
		return false
	}
	_, hasAmount := parseNumberFromMap(row,
		"grant_amount", "grantAmount",
		"total_granted", "totalGranted",
		"amount", "total_amount", "totalAmount")
	_, hasUsed := parseNumberFromMap(row,
		"used_amount", "usedAmount",
		"used", "usage", "spent")
	_, hasAvailable := parseNumberFromMap(row,
		"available_amount", "availableAmount",
		"remaining_amount", "remainingAmount",
		"remaining_balance", "remainingBalance",
		"available_balance", "availableBalance",
		"available", "remaining")
	return hasAmount || hasUsed || hasAvailable
}

func parseCreditGrantExpiry(row map[string]any) (time.Time, bool) {
	raw := firstAnyFromMap(row,
		"expires_at", "expiresAt", "expiry_time", "expiryTime",
		"expire_at", "expireAt", "expiration_time", "expirationTime")
	if raw == nil {
		return time.Time{}, false
	}
	return parseTimeValue(raw)
}

func mapsFromArray(values []any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for _, item := range values {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	return maps.Clone(in)
}

func looksLikeUsageRow(row map[string]any) bool {
	if row == nil {
		return false
	}
	hasName := firstStringByPaths(row,
		[]string{"model"},
		[]string{"model_id"},
		[]string{"modelName"},
		[]string{"tool"},
		[]string{"tool_name"},
		[]string{"name"},
		[]string{"model", "name"},
		[]string{"tool", "name"},
	) != ""
	if hasName {
		return true
	}
	_, hasReq := firstNumberByPaths(row, []string{"requests"}, []string{"request_count"}, []string{"calls"}, []string{"count"}, []string{"usage", "requests"})
	_, hasTokens := firstNumberByPaths(row, []string{"total_tokens"}, []string{"tokens"}, []string{"input_tokens"}, []string{"output_tokens"}, []string{"usage", "total_tokens"})
	_, hasCost := firstNumberByPaths(row, []string{"cost"}, []string{"total_cost"}, []string{"cost_usd"}, []string{"total_cost_usd"}, []string{"usage", "cost_usd"})
	return hasReq || hasTokens || hasCost
}
