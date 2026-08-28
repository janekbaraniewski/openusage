package webserve

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func f64(v float64) *float64 { return &v }

func demoSnapshots(now time.Time) []core.UsageSnapshot {
	return []core.UsageSnapshot{
		demoClaude(now),
		demoCursor(now),
		demoOpenRouter(now),
		demoCopilot(now),
		demoCodex(now),
		demoOllama(now),
	}
}

func demoSeries(now time.Time, values ...float64) []core.TimePoint {
	out := make([]core.TimePoint, 0, len(values))
	start := now.UTC().AddDate(0, 0, -(len(values) - 1))
	for i, v := range values {
		out = append(out, core.TimePoint{
			Date:  start.AddDate(0, 0, i).Format("2006-01-02"),
			Value: v,
		})
	}
	return out
}

func demoClaude(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("claude_code", "claude-code")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "~$42.18 today · $8.40/h"
	snap.Metrics = map[string]core.Metric{
		"today_api_cost":      {Used: f64(42.18), Unit: "USD", Window: "today"},
		"7d_api_cost":         {Used: f64(187.40), Unit: "USD", Window: "7d"},
		"all_time_api_cost":   {Used: f64(912.50), Unit: "USD"},
		"burn_rate":           {Used: f64(8.40), Unit: "USD/h"},
		"today_input_tokens":  {Used: f64(1_240_000), Unit: "tokens", Window: "today"},
		"today_output_tokens": {Used: f64(318_000), Unit: "tokens", Window: "today"},
		"usage_five_hour":     {Used: f64(38), Limit: f64(100), Remaining: f64(62), Unit: "%", Window: "rolling-5h"},
		"usage_seven_day":     {Used: f64(54), Limit: f64(100), Remaining: f64(46), Unit: "%", Window: "rolling-7d"},
		"sessions_today":      {Used: f64(12), Unit: "sessions", Window: "today"},
		"messages_today":      {Used: f64(184), Unit: "messages", Window: "today"},
	}
	snap.Attributes = map[string]string{"plan_type": "max_5"}
	snap.ModelUsage = []core.ModelUsageRecord{
		{RawModelID: "claude-opus-4-6", Canonical: "claude-opus-4-6", CanonicalFamily: "claude", CostUSD: f64(31.20), InputTokens: f64(820000), OutputTokens: f64(210000), Window: "today", Confidence: 1},
		{RawModelID: "claude-sonnet-4-6", Canonical: "claude-sonnet-4-6", CanonicalFamily: "claude", CostUSD: f64(8.40), InputTokens: f64(310000), OutputTokens: f64(82000), Window: "today", Confidence: 1},
		{RawModelID: "claude-haiku-4-5", Canonical: "claude-haiku-4-5", CanonicalFamily: "claude", CostUSD: f64(2.58), InputTokens: f64(110000), OutputTokens: f64(26000), Window: "today", Confidence: 1},
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"cost":     demoSeries(now, 18.2, 21.4, 16.8, 29.1, 24.6, 33.0, 42.18),
		"tokens":   demoSeries(now, 820000, 910000, 740000, 1.1e6, 980000, 1.3e6, 1.558e6),
		"requests": demoSeries(now, 92, 101, 84, 128, 114, 146, 184),
	}
	snap.Resets = map[string]time.Time{
		"usage_five_hour": now.Add(2*time.Hour + 15*time.Minute),
	}
	return snap
}

func demoCursor(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("cursor", "cursor-ide")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "$5.23 today · 56% of billing cycle"
	snap.Metrics = map[string]core.Metric{
		"today_cost":            {Used: f64(5.23), Unit: "USD", Window: "today"},
		"plan_spend":            {Used: f64(40.93), Limit: f64(60), Remaining: f64(19.07), Unit: "USD"},
		"spend_limit":           {Used: f64(531.11), Limit: f64(3600), Remaining: f64(3068.89), Unit: "USD"},
		"requests_today":        {Used: f64(412), Unit: "requests", Window: "today"},
		"billing_input_tokens":  {Used: f64(597100), Unit: "tokens", Window: "month"},
		"billing_output_tokens": {Used: f64(320100), Unit: "tokens", Window: "month"},
	}
	snap.Attributes = map[string]string{"plan_type": "pro"}
	snap.ModelUsage = []core.ModelUsageRecord{
		{RawModelID: "claude-4.6-opus", Canonical: "claude-opus-4.6", CostUSD: f64(39.28), Window: "month", Confidence: 0.9},
		{RawModelID: "gpt-5-mini", Canonical: "gpt-5-mini", CostUSD: f64(2.12), Window: "month", Confidence: 0.9},
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"cost":     demoSeries(now, 3.1, 4.8, 2.9, 6.4, 5.0, 7.2, 5.23),
		"requests": demoSeries(now, 280, 310, 240, 390, 340, 420, 412),
	}
	return snap
}

func demoOpenRouter(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("openrouter", "openrouter")
	snap.Timestamp = now
	snap.Status = core.StatusNearLimit
	snap.Message = "$1.72 remaining"
	snap.Metrics = map[string]core.Metric{
		"credit_balance":      {Used: f64(8.28), Limit: f64(10), Remaining: f64(1.72), Unit: "USD", Window: "current"},
		"today_cost":          {Used: f64(0.18), Unit: "USD", Window: "today"},
		"7d_api_cost":         {Used: f64(6.50), Unit: "USD", Window: "7d"},
		"today_requests":      {Used: f64(21), Unit: "requests", Window: "today"},
		"today_input_tokens":  {Used: f64(746000), Unit: "tokens", Window: "today"},
		"today_output_tokens": {Used: f64(89300), Unit: "tokens", Window: "today"},
	}
	snap.ModelUsage = []core.ModelUsageRecord{
		{RawModelID: "moonshotai/kimi-k2.5", Canonical: "kimi-k2.5", CostUSD: f64(3.76), Window: "activity", Confidence: 1},
		{RawModelID: "qwen/qwen3-coder-flash", Canonical: "qwen3-coder-flash", CostUSD: f64(2.44), Window: "activity", Confidence: 1},
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"cost": demoSeries(now, 0.9, 1.1, 0.7, 1.4, 1.0, 1.2, 0.18),
	}
	return snap
}

func demoCopilot(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("copilot", "copilot")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "Premium requests 62% used"
	snap.Metrics = map[string]core.Metric{
		"premium_requests": {Used: f64(186), Limit: f64(300), Remaining: f64(114), Unit: "requests", Window: "month"},
		"chat_requests":    {Used: f64(94), Unit: "requests", Window: "today"},
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"requests": demoSeries(now, 70, 82, 64, 91, 88, 102, 94),
	}
	return snap
}

func demoCodex(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "$11.40 today"
	snap.Metrics = map[string]core.Metric{
		"today_cost":          {Used: f64(11.40), Unit: "USD", Window: "today"},
		"7d_api_cost":         {Used: f64(48.20), Unit: "USD", Window: "7d"},
		"today_input_tokens":  {Used: f64(420000), Unit: "tokens", Window: "today"},
		"today_output_tokens": {Used: f64(96000), Unit: "tokens", Window: "today"},
	}
	snap.ModelUsage = []core.ModelUsageRecord{
		{RawModelID: "gpt-5.1-codex", Canonical: "gpt-5.1-codex", CostUSD: f64(11.40), Window: "today", Confidence: 1},
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"cost": demoSeries(now, 6.2, 8.1, 5.4, 9.8, 7.6, 10.2, 11.40),
	}
	return snap
}

func demoOllama(now time.Time) core.UsageSnapshot {
	snap := core.NewUsageSnapshot("ollama", "ollama-local")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "Local runtime · 4 models"
	snap.Metrics = map[string]core.Metric{
		"requests_today": {Used: f64(38), Unit: "requests", Window: "today"},
		"models_loaded":  {Used: f64(4), Unit: "models"},
	}
	snap.Attributes = map[string]string{"base_url": "http://127.0.0.1:11434"}
	snap.DailySeries = map[string][]core.TimePoint{
		"requests": demoSeries(now, 12, 18, 9, 22, 16, 28, 38),
	}
	return snap
}
