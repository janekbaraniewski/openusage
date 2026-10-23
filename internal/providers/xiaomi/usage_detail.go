package xiaomi

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	// usageDetailDays is the span the per-model figures cover.
	usageDetailDays   = 30
	usageDetailWindow = "30d"
	dayLayout         = "2006-01-02"
)

// usageDetailRow is one (day, model, API key) line of the console's usage
// statistics. Input tokens are split into cache hits and misses.
type usageDetailRow struct {
	Date           string     `json:"date"`
	Model          string     `json:"model"`
	ConsumedAmount flexNumber `json:"consumedAmount"`
	TotalToken     flexNumber `json:"totalToken"`
	InputHitToken  flexNumber `json:"inputHitToken"`
	InputMissToken flexNumber `json:"inputMissToken"`
	OutputToken    flexNumber `json:"outputToken"`
	RequestCount   flexNumber `json:"requestCount"`
}

// fetchUsageDetail returns the daily rows for the months the detail span
// touches. The console serves one calendar month per request.
func fetchUsageDetail(ctx context.Context, client *consoleClient, now time.Time) ([]usageDetailRow, error) {
	start := now.AddDate(0, 0, -(usageDetailDays - 1))
	var rows []usageDetailRow
	for month := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !month.After(now); month = month.AddDate(0, 1, 0) {
		var page []usageDetailRow
		body := map[string]int{"year": month.Year(), "month": int(month.Month())}
		if err := client.post(ctx, "/usage/detail/list", body, &page); err != nil {
			return nil, err
		}
		// Keep only the requested month so a row can never be counted twice.
		prefix := month.Format("2006-01")
		for _, row := range page {
			if strings.HasPrefix(row.Date, prefix) {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// applyUsageDetail projects the rows from the last usageDetailDays days into
// per-model metrics and daily cost, request and token series.
func applyUsageDetail(snap *core.UsageSnapshot, rows []usageDetailRow, currency string, now time.Time) {
	type modelTotals struct {
		input, cacheRead, output, cost, requests, total float64
	}
	cutoff := now.AddDate(0, 0, -(usageDetailDays - 1)).Format(dayLayout)
	models := map[string]*modelTotals{}
	dailyCost := map[string]float64{}
	dailyRequests := map[string]float64{}
	dailyTokens := map[string]float64{}

	for _, row := range rows {
		if row.Model == "" || len(row.Date) != len(dayLayout) || row.Date < cutoff {
			continue
		}
		m := models[row.Model]
		if m == nil {
			m = &modelTotals{}
			models[row.Model] = m
		}
		m.input += row.InputMissToken.value
		m.cacheRead += row.InputHitToken.value
		m.output += row.OutputToken.value
		m.cost += row.ConsumedAmount.value
		m.requests += row.RequestCount.value
		m.total += row.TotalToken.value
		dailyCost[row.Date] += row.ConsumedAmount.value
		dailyRequests[row.Date] += row.RequestCount.value
		dailyTokens[row.Date] += row.TotalToken.value
	}
	if len(models) == 0 {
		return
	}

	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)

	var cost, requests float64
	for _, name := range names {
		m := models[name]
		key := "model_" + shared.SanitizeMetricName(name)
		tokens := func(suffix string, value float64) {
			snap.Metrics[key+suffix] = core.Metric{Used: core.Float64Ptr(value), Unit: "tokens", Window: usageDetailWindow}
		}
		tokens("_input_tokens", m.input)
		tokens("_cache_read_tokens", m.cacheRead)
		tokens("_output_tokens", m.output)
		snap.Metrics[key+"_cost_usd"] = core.Metric{Used: core.Float64Ptr(m.cost), Unit: currency, Window: usageDetailWindow}
		snap.Metrics[key+"_requests"] = core.Metric{Used: core.Float64Ptr(m.requests), Unit: "requests", Window: usageDetailWindow}

		snap.AppendModelUsage(core.ModelUsageRecord{
			RawModelID:   name,
			RawSource:    "api",
			Window:       usageDetailWindow,
			InputTokens:  core.Float64Ptr(m.input),
			OutputTokens: core.Float64Ptr(m.output),
			CachedTokens: core.Float64Ptr(m.cacheRead),
			TotalTokens:  core.Float64Ptr(m.total),
			CostUSD:      core.Float64Ptr(m.cost),
			Requests:     core.Float64Ptr(m.requests),
		})
		cost += m.cost
		requests += m.requests
	}

	// Spend over the standard dashboard windows, summed from the daily rows.
	today := now.Format(dayLayout)
	weekStart := now.AddDate(0, 0, -6).Format(dayLayout)
	var costToday, cost7d float64
	for day, value := range dailyCost {
		if day == today {
			costToday += value
		}
		if day >= weekStart {
			cost7d += value
		}
	}
	snap.Metrics["today_api_cost"] = core.Metric{Used: core.Float64Ptr(costToday), Unit: currency, Window: "1d"}
	snap.Metrics["7d_api_cost"] = core.Metric{Used: core.Float64Ptr(cost7d), Unit: currency, Window: "7d"}
	snap.Metrics["30d_api_cost"] = core.Metric{Used: core.Float64Ptr(cost), Unit: currency, Window: usageDetailWindow}
	snap.Metrics["30d_requests"] = core.Metric{Used: core.Float64Ptr(requests), Unit: "requests", Window: usageDetailWindow}
	snap.Metrics["window_cost"] = core.Metric{Used: core.Float64Ptr(cost), Unit: currency, Window: usageDetailWindow}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	snap.DailySeries["cost"] = core.SortedTimePoints(dailyCost)
	snap.DailySeries["requests"] = core.SortedTimePoints(dailyRequests)
	snap.DailySeries["tokens"] = core.SortedTimePoints(dailyTokens)
}
