package core

import (
	"sort"
	"strconv"
	"strings"
)

type modelMetricKind string

const (
	modelMetricInput     modelMetricKind = "input"
	modelMetricOutput    modelMetricKind = "output"
	modelMetricCached    modelMetricKind = "cached"
	modelMetricReasoning modelMetricKind = "reasoning"
	modelMetricCostUSD   modelMetricKind = "cost_usd"
	modelMetricRequests  modelMetricKind = "requests"
)

type modelWindowKey struct {
	model  string
	window string
}

func BuildModelUsageFromSnapshotMetrics(s UsageSnapshot) []ModelUsageRecord {
	records := make(map[modelWindowKey]*ModelUsageRecord)

	ensure := func(rawModelID, window string) *ModelUsageRecord {
		rawModelID = strings.TrimSpace(rawModelID)
		window = strings.TrimSpace(window)
		if rawModelID == "" {
			rawModelID = "unknown"
		}
		if window == "" {
			window = "unknown"
		}
		key := modelWindowKey{model: rawModelID, window: window}
		if rec, ok := records[key]; ok {
			return rec
		}
		rec := &ModelUsageRecord{
			RawModelID: rawModelID,
			RawSource:  "metrics_fallback",
			Window:     window,
			Dimensions: map[string]string{
				"provider_id": s.ProviderID,
				"account_id":  s.AccountID,
			},
		}
		records[key] = rec
		return rec
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil {
			continue
		}
		rawModel, kind, ok := parseModelMetricKey(key)
		if !ok {
			continue
		}
		rec := ensure(rawModel, metric.Window)
		applyModelMetric(rec, kind, *metric.Used)
	}

	for key, rawValue := range s.Raw {
		rawModel, kind, ok := parseModelMetricKey(key)
		if !ok {
			continue
		}
		val, ok := parseModelRawValue(rawValue)
		if !ok {
			continue
		}
		rec := ensure(rawModel, "unknown")
		applyModelMetric(rec, kind, val)
	}

	keys := make([]modelWindowKey, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		return keys[i].window < keys[j].window
	})

	out := make([]ModelUsageRecord, 0, len(keys))
	for _, key := range keys {
		rec := records[key]
		// synthesize total tokens when absent and partial token stats exist
		if rec.TotalTokens == nil {
			total := float64(0)
			hasAny := false
			if rec.InputTokens != nil {
				total += *rec.InputTokens
				hasAny = true
			}
			if rec.OutputTokens != nil {
				total += *rec.OutputTokens
				hasAny = true
			}
			if hasAny {
				rec.TotalTokens = Float64Ptr(total)
			}
		}
		if rec.InputTokens == nil && rec.OutputTokens == nil && rec.CachedTokens == nil &&
			rec.ReasoningTokens == nil && rec.CostUSD == nil && rec.Requests == nil && rec.TotalTokens == nil {
			continue
		}
		out = append(out, *rec)
	}

	return out
}

func parseModelMetricKey(key string) (rawModelID string, kind modelMetricKind, ok bool) {
	switch {
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens"), modelMetricInput, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens"), modelMetricOutput, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cached_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cached_tokens"), modelMetricCached, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_read_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_read_tokens"), modelMetricCached, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_write_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_write_tokens"), modelMetricCached, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_reasoning_tokens"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_reasoning_tokens"), modelMetricReasoning, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost_usd"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd"), modelMetricCostUSD, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost"), modelMetricCostUSD, true
	case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests"), modelMetricRequests, true
	case strings.HasPrefix(key, "input_tokens_"):
		return strings.TrimPrefix(key, "input_tokens_"), modelMetricInput, true
	case strings.HasPrefix(key, "output_tokens_"):
		return strings.TrimPrefix(key, "output_tokens_"), modelMetricOutput, true
	default:
		return "", "", false
	}
}

func parseModelRawValue(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ",", "")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func applyModelMetric(rec *ModelUsageRecord, kind modelMetricKind, value float64) {
	if rec == nil || value <= 0 {
		return
	}
	switch kind {
	case modelMetricInput:
		rec.InputTokens = addPtrValue(rec.InputTokens, value)
	case modelMetricOutput:
		rec.OutputTokens = addPtrValue(rec.OutputTokens, value)
	case modelMetricCached:
		rec.CachedTokens = addPtrValue(rec.CachedTokens, value)
	case modelMetricReasoning:
		rec.ReasoningTokens = addPtrValue(rec.ReasoningTokens, value)
	case modelMetricCostUSD:
		rec.CostUSD = addPtrValue(rec.CostUSD, value)
	case modelMetricRequests:
		rec.Requests = addPtrValue(rec.Requests, value)
	}
}

func addPtrValue(ptr *float64, add float64) *float64 {
	if ptr == nil {
		return Float64Ptr(add)
	}
	v := *ptr + add
	return Float64Ptr(v)
}
