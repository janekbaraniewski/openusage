package claude_code

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type conversationUsageProjection struct {
	now                  time.Time
	inCurrentBlock       bool
	currentBlockStart    time.Time
	currentBlockEnd      time.Time
	blockCostUSD         float64
	blockInputTokens     int
	blockOutputTokens    int
	blockCacheRead       int
	blockCacheCreate     int
	blockMessages        int
	blockModels          map[string]bool
	blockStartCandidates []time.Time

	todayCostUSD       float64
	todayInputTokens   int
	todayOutputTokens  int
	todayCacheRead     int
	todayCacheCreate   int
	todayMessages      int
	todayModels        map[string]bool
	todaySessions      map[string]bool
	todayCacheCreate5m int
	todayCacheCreate1h int
	todayReasoning     int
	todayToolCalls     int
	todayWebSearch     int
	todayWebFetch      int

	weeklyCostUSD       float64
	weeklyInputTokens   int
	weeklyOutputTokens  int
	weeklyMessages      int
	weeklySessions      map[string]bool
	weeklyCacheRead     int
	weeklyCacheCreate   int
	weeklyCacheCreate5m int
	weeklyCacheCreate1h int
	weeklyReasoning     int
	weeklyToolCalls     int
	weeklyWebSearch     int
	weeklyWebFetch      int

	allTimeCostUSD       float64
	allTimeEntries       int
	allTimeInputTokens   int
	allTimeOutputTokens  int
	allTimeCacheRead     int
	allTimeCacheCreate   int
	allTimeCacheCreate5m int
	allTimeCacheCreate1h int
	allTimeReasoning     int
	allTimeToolCalls     int
	allTimeWebSearch     int
	allTimeWebFetch      int
	allTimeLinesAdded    int
	allTimeLinesRemoved  int
	allTimeCommitCount   int

	modelTotals        map[string]*modelUsageTotals
	clientTotals       map[string]*modelUsageTotals
	projectTotals      map[string]*modelUsageTotals
	agentTotals        map[string]*modelUsageTotals
	serviceTierTotals  map[string]float64
	inferenceGeoTotals map[string]float64

	toolUsageCounts     map[string]int
	languageUsageCounts map[string]int
	changedFiles        map[string]bool
	seenUsageKeys       map[string]bool

	dailyClientTokens map[string]map[string]float64
	dailyTokenTotals  map[string]int
	dailyMessages     map[string]int
	dailyCost         map[string]float64
	dailyModelTokens  map[string]map[string]int
}

func applyConversationUsageProjection(snap *core.UsageSnapshot, p conversationUsageProjection) {
	for model, totals := range p.modelTotals {
		modelPrefix := "model_" + model
		setMetricMax(snap, modelPrefix+"_input_tokens", totals.input, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_output_tokens", totals.output, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cached_tokens", totals.cached, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_tokens", totals.cacheCreate, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_5m_tokens", totals.cache5m, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_1h_tokens", totals.cache1h, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_reasoning_tokens", totals.reasoning, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_search_requests", totals.webSearch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_fetch_requests", totals.webFetch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cost_usd", totals.cost, "USD", "all-time estimate")
	}

	for client, totals := range p.clientTotals {
		key := "client_" + client
		setMetricMax(snap, key+"_input_tokens", totals.input, "tokens", "all-time")
		setMetricMax(snap, key+"_output_tokens", totals.output, "tokens", "all-time")
		setMetricMax(snap, key+"_cached_tokens", totals.cached, "tokens", "all-time")
		setMetricMax(snap, key+"_reasoning_tokens", totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_total_tokens", totals.input+totals.output+totals.cached+totals.cacheCreate+totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_sessions", totals.sessions, "sessions", "all-time")
	}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	dates := core.SortedStringKeys(p.dailyTokenTotals)

	if len(snap.DailySeries["messages"]) == 0 && len(dates) > 0 {
		for _, d := range dates {
			snap.DailySeries["messages"] = append(snap.DailySeries["messages"], core.TimePoint{Date: d, Value: float64(p.dailyMessages[d])})
			snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"], core.TimePoint{Date: d, Value: float64(p.dailyTokenTotals[d])})
			snap.DailySeries["cost"] = append(snap.DailySeries["cost"], core.TimePoint{Date: d, Value: p.dailyCost[d]})
		}

		allModels := make(map[string]int64)
		for _, dm := range p.dailyModelTokens {
			for model, tokens := range dm {
				allModels[model] += int64(tokens)
			}
		}
		type modelVolume struct {
			name  string
			total int64
		}
		var ranked []modelVolume
		for model, total := range allModels {
			ranked = append(ranked, modelVolume{name: model, total: total})
		}
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].total > ranked[j].total })
		limit := min(5, len(ranked))
		for i := 0; i < limit; i++ {
			model := ranked[i].name
			key := fmt.Sprintf("tokens_%s", sanitizeModelName(model))
			for _, d := range dates {
				snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{
					Date:  d,
					Value: float64(p.dailyModelTokens[d][model]),
				})
			}
		}
	}

	if len(dates) > 0 {
		clientNames := make(map[string]bool)
		for _, byClient := range p.dailyClientTokens {
			for client := range byClient {
				clientNames[client] = true
			}
		}
		for client := range clientNames {
			key := "tokens_client_" + client
			for _, d := range dates {
				snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{
					Date:  d,
					Value: p.dailyClientTokens[d][client],
				})
			}
		}
	}

	if p.todayCostUSD > 0 {
		snap.Metrics["today_api_cost"] = core.Metric{Used: core.Float64Ptr(p.todayCostUSD), Unit: "USD", Window: "since midnight"}
	}
	if p.todayInputTokens > 0 {
		in := float64(p.todayInputTokens)
		snap.Metrics["today_input_tokens"] = core.Metric{Used: &in, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayOutputTokens > 0 {
		out := float64(p.todayOutputTokens)
		snap.Metrics["today_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayCacheRead > 0 {
		value := float64(p.todayCacheRead)
		snap.Metrics["today_cache_read_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayCacheCreate > 0 {
		value := float64(p.todayCacheCreate)
		snap.Metrics["today_cache_create_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayMessages > 0 {
		setMetricMax(snap, "messages_today", float64(p.todayMessages), "messages", "since midnight")
	}
	if len(p.todaySessions) > 0 {
		setMetricMax(snap, "sessions_today", float64(len(p.todaySessions)), "sessions", "since midnight")
	}
	if p.todayToolCalls > 0 {
		setMetricMax(snap, "tool_calls_today", float64(p.todayToolCalls), "calls", "since midnight")
	}
	if p.todayReasoning > 0 {
		value := float64(p.todayReasoning)
		snap.Metrics["today_reasoning_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayCacheCreate5m > 0 {
		value := float64(p.todayCacheCreate5m)
		snap.Metrics["today_cache_create_5m_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayCacheCreate1h > 0 {
		value := float64(p.todayCacheCreate1h)
		snap.Metrics["today_cache_create_1h_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "since midnight"}
	}
	if p.todayWebSearch > 0 {
		value := float64(p.todayWebSearch)
		snap.Metrics["today_web_search_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "since midnight"}
	}
	if p.todayWebFetch > 0 {
		value := float64(p.todayWebFetch)
		snap.Metrics["today_web_fetch_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "since midnight"}
	}

	if p.weeklyCostUSD > 0 {
		snap.Metrics["7d_api_cost"] = core.Metric{Used: core.Float64Ptr(p.weeklyCostUSD), Unit: "USD", Window: "rolling 7 days"}
	}
	if p.weeklyMessages > 0 {
		wm := float64(p.weeklyMessages)
		snap.Metrics["7d_messages"] = core.Metric{Used: &wm, Unit: "messages", Window: "rolling 7 days"}
		in := float64(p.weeklyInputTokens)
		out := float64(p.weeklyOutputTokens)
		snap.Metrics["7d_input_tokens"] = core.Metric{Used: &in, Unit: "tokens", Window: "rolling 7 days"}
		snap.Metrics["7d_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyCacheRead > 0 {
		value := float64(p.weeklyCacheRead)
		snap.Metrics["7d_cache_read_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyCacheCreate > 0 {
		value := float64(p.weeklyCacheCreate)
		snap.Metrics["7d_cache_create_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyCacheCreate5m > 0 {
		value := float64(p.weeklyCacheCreate5m)
		snap.Metrics["7d_cache_create_5m_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyCacheCreate1h > 0 {
		value := float64(p.weeklyCacheCreate1h)
		snap.Metrics["7d_cache_create_1h_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyReasoning > 0 {
		value := float64(p.weeklyReasoning)
		snap.Metrics["7d_reasoning_tokens"] = core.Metric{Used: &value, Unit: "tokens", Window: "rolling 7 days"}
	}
	if p.weeklyToolCalls > 0 {
		setMetricMax(snap, "7d_tool_calls", float64(p.weeklyToolCalls), "calls", "rolling 7 days")
	}
	if p.weeklyWebSearch > 0 {
		value := float64(p.weeklyWebSearch)
		snap.Metrics["7d_web_search_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "rolling 7 days"}
	}
	if p.weeklyWebFetch > 0 {
		value := float64(p.weeklyWebFetch)
		snap.Metrics["7d_web_fetch_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "rolling 7 days"}
	}
	if len(p.weeklySessions) > 0 {
		setMetricMax(snap, "7d_sessions", float64(len(p.weeklySessions)), "sessions", "rolling 7 days")
	}

	if p.todayMessages > 0 {
		today := p.now.Format("2006-01-02")
		snap.Raw["jsonl_today_date"] = today
		snap.Raw["jsonl_today_messages"] = fmt.Sprintf("%d", p.todayMessages)
		snap.Raw["jsonl_today_input_tokens"] = fmt.Sprintf("%d", p.todayInputTokens)
		snap.Raw["jsonl_today_output_tokens"] = fmt.Sprintf("%d", p.todayOutputTokens)
		snap.Raw["jsonl_today_cache_read_tokens"] = fmt.Sprintf("%d", p.todayCacheRead)
		snap.Raw["jsonl_today_cache_create_tokens"] = fmt.Sprintf("%d", p.todayCacheCreate)
		snap.Raw["jsonl_today_reasoning_tokens"] = fmt.Sprintf("%d", p.todayReasoning)
		snap.Raw["jsonl_today_web_search_requests"] = fmt.Sprintf("%d", p.todayWebSearch)
		snap.Raw["jsonl_today_web_fetch_requests"] = fmt.Sprintf("%d", p.todayWebFetch)
		snap.Raw["jsonl_today_models"] = strings.Join(core.SortedStringKeys(p.todayModels), ", ")
	}

	if p.inCurrentBlock {
		snap.Metrics["5h_block_cost"] = core.Metric{
			Used:   core.Float64Ptr(p.blockCostUSD),
			Unit:   "USD",
			Window: fmt.Sprintf("%s – %s", p.currentBlockStart.Format("15:04"), p.currentBlockEnd.Format("15:04")),
		}
		blockIn := float64(p.blockInputTokens)
		blockOut := float64(p.blockOutputTokens)
		blockMsgs := float64(p.blockMessages)
		snap.Metrics["5h_block_input"] = core.Metric{Used: &blockIn, Unit: "tokens", Window: "current 5h block"}
		snap.Metrics["5h_block_output"] = core.Metric{Used: &blockOut, Unit: "tokens", Window: "current 5h block"}
		snap.Metrics["5h_block_msgs"] = core.Metric{Used: &blockMsgs, Unit: "messages", Window: "current 5h block"}
		if p.blockCacheRead > 0 {
			setMetricMax(snap, "5h_block_cache_read_tokens", float64(p.blockCacheRead), "tokens", "current 5h block")
		}
		if p.blockCacheCreate > 0 {
			setMetricMax(snap, "5h_block_cache_create_tokens", float64(p.blockCacheCreate), "tokens", "current 5h block")
		}

		remaining := p.currentBlockEnd.Sub(p.now)
		if remaining > 0 {
			snap.Resets["billing_block"] = p.currentBlockEnd
			snap.Raw["block_time_remaining"] = fmt.Sprintf("%s", remaining.Round(time.Minute))
			elapsed := p.now.Sub(p.currentBlockStart)
			progress := math.Min(elapsed.Seconds()/billingBlockDuration.Seconds()*100, 100)
			snap.Raw["block_progress_pct"] = fmt.Sprintf("%.0f", progress)
		}

		snap.Raw["block_start"] = p.currentBlockStart.Format(time.RFC3339)
		snap.Raw["block_end"] = p.currentBlockEnd.Format(time.RFC3339)
		snap.Raw["block_models"] = strings.Join(core.SortedStringKeys(p.blockModels), ", ")

		elapsed := p.now.Sub(p.currentBlockStart)
		if elapsed > time.Minute && p.blockCostUSD > 0 {
			burnRate := p.blockCostUSD / elapsed.Hours()
			snap.Metrics["burn_rate"] = core.Metric{Used: core.Float64Ptr(burnRate), Unit: "USD/h", Window: "current 5h block"}
			snap.Raw["burn_rate"] = fmt.Sprintf("$%.2f/hour", burnRate)
		}
	}

	if p.allTimeCostUSD > 0 {
		snap.Metrics["all_time_api_cost"] = core.Metric{Used: core.Float64Ptr(p.allTimeCostUSD), Unit: "USD", Window: "all-time estimate"}
	}
	if p.allTimeInputTokens > 0 {
		setMetricMax(snap, "all_time_input_tokens", float64(p.allTimeInputTokens), "tokens", "all-time estimate")
	}
	if p.allTimeOutputTokens > 0 {
		setMetricMax(snap, "all_time_output_tokens", float64(p.allTimeOutputTokens), "tokens", "all-time estimate")
	}
	if p.allTimeCacheRead > 0 {
		setMetricMax(snap, "all_time_cache_read_tokens", float64(p.allTimeCacheRead), "tokens", "all-time estimate")
	}
	if p.allTimeCacheCreate > 0 {
		setMetricMax(snap, "all_time_cache_create_tokens", float64(p.allTimeCacheCreate), "tokens", "all-time estimate")
	}
	if p.allTimeCacheCreate5m > 0 {
		setMetricMax(snap, "all_time_cache_create_5m_tokens", float64(p.allTimeCacheCreate5m), "tokens", "all-time estimate")
	}
	if p.allTimeCacheCreate1h > 0 {
		setMetricMax(snap, "all_time_cache_create_1h_tokens", float64(p.allTimeCacheCreate1h), "tokens", "all-time estimate")
	}
	if p.allTimeReasoning > 0 {
		setMetricMax(snap, "all_time_reasoning_tokens", float64(p.allTimeReasoning), "tokens", "all-time estimate")
	}
	if p.allTimeToolCalls > 0 {
		setMetricMax(snap, "all_time_tool_calls", float64(p.allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_calls_total", float64(p.allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_completed", float64(p.allTimeToolCalls), "calls", "all-time estimate")
		setMetricMax(snap, "tool_success_rate", 100.0, "%", "all-time estimate")
	}
	if len(p.seenUsageKeys) > 0 {
		setMetricMax(snap, "total_prompts", float64(len(p.seenUsageKeys)), "prompts", "all-time estimate")
	}
	if len(p.changedFiles) > 0 {
		setMetricMax(snap, "composer_files_changed", float64(len(p.changedFiles)), "files", "all-time estimate")
	}
	if p.allTimeLinesAdded > 0 {
		setMetricMax(snap, "composer_lines_added", float64(p.allTimeLinesAdded), "lines", "all-time estimate")
	}
	if p.allTimeLinesRemoved > 0 {
		setMetricMax(snap, "composer_lines_removed", float64(p.allTimeLinesRemoved), "lines", "all-time estimate")
	}
	if p.allTimeCommitCount > 0 {
		setMetricMax(snap, "scored_commits", float64(p.allTimeCommitCount), "commits", "all-time estimate")
	}
	if p.allTimeLinesAdded > 0 || p.allTimeLinesRemoved > 0 {
		hundred := 100.0
		zero := 0.0
		snap.Metrics["ai_code_percentage"] = core.Metric{Used: &hundred, Remaining: &zero, Limit: &hundred, Unit: "%", Window: "all-time estimate"}
	}
	for lang, count := range p.languageUsageCounts {
		if count > 0 {
			setMetricMax(snap, "lang_"+sanitizeModelName(lang), float64(count), "requests", "all-time estimate")
		}
	}
	for toolName, count := range p.toolUsageCounts {
		if count > 0 {
			setMetricMax(snap, "tool_"+sanitizeModelName(toolName), float64(count), "calls", "all-time estimate")
		}
	}
	if p.allTimeWebSearch > 0 {
		setMetricMax(snap, "all_time_web_search_requests", float64(p.allTimeWebSearch), "requests", "all-time estimate")
	}
	if p.allTimeWebFetch > 0 {
		setMetricMax(snap, "all_time_web_fetch_requests", float64(p.allTimeWebFetch), "requests", "all-time estimate")
	}

	snap.Raw["tool_usage"] = summarizeCountMap(p.toolUsageCounts, 6)
	snap.Raw["language_usage"] = summarizeCountMap(p.languageUsageCounts, 8)
	snap.Raw["project_usage"] = summarizeTotalsMap(p.projectTotals, true, 6)
	snap.Raw["agent_usage"] = summarizeTotalsMap(p.agentTotals, false, 4)
	snap.Raw["service_tier_usage"] = summarizeFloatMap(p.serviceTierTotals, "tok", 4)
	snap.Raw["inference_geo_usage"] = summarizeFloatMap(p.inferenceGeoTotals, "tok", 4)
	if p.allTimeCacheRead > 0 || p.allTimeCacheCreate > 0 {
		snap.Raw["cache_usage"] = fmt.Sprintf("read %s · create %s (1h %s, 5m %s)",
			shortTokenCount(float64(p.allTimeCacheRead)),
			shortTokenCount(float64(p.allTimeCacheCreate)),
			shortTokenCount(float64(p.allTimeCacheCreate1h)),
			shortTokenCount(float64(p.allTimeCacheCreate5m)),
		)
	}
	snap.Raw["project_count"] = fmt.Sprintf("%d", len(p.projectTotals))
	snap.Raw["tool_count"] = fmt.Sprintf("%d", len(p.toolUsageCounts))
	snap.Raw["jsonl_total_entries"] = fmt.Sprintf("%d", p.allTimeEntries)
	snap.Raw["jsonl_total_blocks"] = fmt.Sprintf("%d", len(p.blockStartCandidates))
	snap.Raw["jsonl_unique_requests"] = fmt.Sprintf("%d", len(p.seenUsageKeys))
	buildModelUsageSummaryRaw(snap)
}
