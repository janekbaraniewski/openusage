package pricing

import "time"

// hardcodedTable is the last-resort fallback when both LiteLLM and
// OpenRouter are unreachable / unparseable. It intentionally covers only
// the ~20 most-used models across Anthropic, OpenAI, Google, DeepSeek and
// popular OSS lineups. Rates are USD per 1,000,000 tokens, captured from
// each vendor's public pricing pages.
//
// When upstream pricing changes, prefer the dynamic fetchers; this table
// exists so the dashboard keeps producing reasonable cost numbers when
// the user is offline or both upstreams 5xx.
//
// All entries here use Source=hardcoded; LastUpdated is filled in on
// demand from hardcodedRevision so consumers see a stable "as of" date.
var hardcodedTable = map[string]Price{
	"claude-3-5-sonnet-20241022": {
		ModelID:                  "claude-3-5-sonnet-20241022",
		Provider:                 "anthropic",
		InputCostPerMillion:      3.0,
		OutputCostPerMillion:     15.0,
		CacheReadCostPerMillion:  0.30,
		CacheWriteCostPerMillion: 3.75,
		ContextWindow:            200_000,
	},
	"claude-3-5-haiku-20241022": {
		ModelID:                  "claude-3-5-haiku-20241022",
		Provider:                 "anthropic",
		InputCostPerMillion:      0.80,
		OutputCostPerMillion:     4.0,
		CacheReadCostPerMillion:  0.08,
		CacheWriteCostPerMillion: 1.0,
		ContextWindow:            200_000,
	},
	"claude-3-opus-20240229": {
		ModelID:                  "claude-3-opus-20240229",
		Provider:                 "anthropic",
		InputCostPerMillion:      15.0,
		OutputCostPerMillion:     75.0,
		CacheReadCostPerMillion:  1.50,
		CacheWriteCostPerMillion: 18.75,
		ContextWindow:            200_000,
	},
	"claude-opus-4-20250514": {
		ModelID:                  "claude-opus-4-20250514",
		Provider:                 "anthropic",
		InputCostPerMillion:      15.0,
		OutputCostPerMillion:     75.0,
		CacheReadCostPerMillion:  1.50,
		CacheWriteCostPerMillion: 18.75,
		ContextWindow:            200_000,
	},
	"claude-sonnet-4-20250514": {
		ModelID:                  "claude-sonnet-4-20250514",
		Provider:                 "anthropic",
		InputCostPerMillion:      3.0,
		OutputCostPerMillion:     15.0,
		CacheReadCostPerMillion:  0.30,
		CacheWriteCostPerMillion: 3.75,
		ContextWindow:            200_000,
	},
	"gpt-4o": {
		ModelID:                  "gpt-4o",
		Provider:                 "openai",
		InputCostPerMillion:      2.50,
		OutputCostPerMillion:     10.0,
		CacheReadCostPerMillion:  1.25,
		CacheWriteCostPerMillion: 0,
		ContextWindow:            128_000,
	},
	"gpt-4o-mini": {
		ModelID:                  "gpt-4o-mini",
		Provider:                 "openai",
		InputCostPerMillion:      0.15,
		OutputCostPerMillion:     0.60,
		CacheReadCostPerMillion:  0.075,
		CacheWriteCostPerMillion: 0,
		ContextWindow:            128_000,
	},
	"gpt-4-turbo": {
		ModelID:              "gpt-4-turbo",
		Provider:             "openai",
		InputCostPerMillion:  10.0,
		OutputCostPerMillion: 30.0,
		ContextWindow:        128_000,
	},
	"o1": {
		ModelID:                 "o1",
		Provider:                "openai",
		InputCostPerMillion:     15.0,
		OutputCostPerMillion:    60.0,
		CacheReadCostPerMillion: 7.50,
		ContextWindow:           200_000,
	},
	"o1-mini": {
		ModelID:                 "o1-mini",
		Provider:                "openai",
		InputCostPerMillion:     3.0,
		OutputCostPerMillion:    12.0,
		CacheReadCostPerMillion: 1.50,
		ContextWindow:           128_000,
	},
	"o3-mini": {
		ModelID:                 "o3-mini",
		Provider:                "openai",
		InputCostPerMillion:     1.10,
		OutputCostPerMillion:    4.40,
		CacheReadCostPerMillion: 0.55,
		ContextWindow:           200_000,
	},
	"gemini-1-5-pro": {
		ModelID:              "gemini-1.5-pro",
		Provider:             "google",
		InputCostPerMillion:  1.25,
		OutputCostPerMillion: 5.0,
		ContextWindow:        2_000_000,
		Tiers: TierOverrides{
			Above128k: &TierRates{
				InputCostPerMillion:  ptrFloat64(2.50),
				OutputCostPerMillion: ptrFloat64(10.0),
			},
		},
	},
	"gemini-1-5-flash": {
		ModelID:              "gemini-1.5-flash",
		Provider:             "google",
		InputCostPerMillion:  0.075,
		OutputCostPerMillion: 0.30,
		ContextWindow:        1_000_000,
		Tiers: TierOverrides{
			Above128k: &TierRates{
				InputCostPerMillion:  ptrFloat64(0.15),
				OutputCostPerMillion: ptrFloat64(0.60),
			},
		},
	},
	"gemini-2-0-flash": {
		ModelID:              "gemini-2.0-flash",
		Provider:             "google",
		InputCostPerMillion:  0.10,
		OutputCostPerMillion: 0.40,
		ContextWindow:        1_000_000,
	},
	"deepseek-chat": {
		ModelID:                 "deepseek-chat",
		Provider:                "deepseek",
		InputCostPerMillion:     0.27,
		OutputCostPerMillion:    1.10,
		CacheReadCostPerMillion: 0.07,
		ContextWindow:           128_000,
	},
	"deepseek-reasoner": {
		ModelID:                 "deepseek-reasoner",
		Provider:                "deepseek",
		InputCostPerMillion:     0.55,
		OutputCostPerMillion:    2.19,
		CacheReadCostPerMillion: 0.14,
		ContextWindow:           128_000,
	},
	"llama-3-1-70b": {
		ModelID:              "llama-3.1-70b",
		Provider:             "meta",
		InputCostPerMillion:  0.59,
		OutputCostPerMillion: 0.79,
		ContextWindow:        128_000,
	},
	"llama-3-1-8b": {
		ModelID:              "llama-3.1-8b",
		Provider:             "meta",
		InputCostPerMillion:  0.05,
		OutputCostPerMillion: 0.08,
		ContextWindow:        128_000,
	},
	"mistral-large": {
		ModelID:              "mistral-large",
		Provider:             "mistral",
		InputCostPerMillion:  2.0,
		OutputCostPerMillion: 6.0,
		ContextWindow:        128_000,
	},
	"qwen-2-5-72b": {
		ModelID:              "qwen-2.5-72b",
		Provider:             "qwen",
		InputCostPerMillion:  0.90,
		OutputCostPerMillion: 0.90,
		ContextWindow:        128_000,
	},
}

// hardcodedRevision is the date the hardcoded prices above were last
// reviewed. Bump this when you refresh the table.
var hardcodedRevision = time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)

// lookupHardcoded returns a copy of the hardcoded Price for `model` if any
// of the fuzzy candidates resolve to an entry in the table.
func lookupHardcoded(model string) (Price, bool) {
	if len(hardcodedTable) == 0 {
		return Price{}, false
	}
	keys := make([]string, 0, len(hardcodedTable))
	for k := range hardcodedTable {
		keys = append(keys, k)
	}
	hit, ok := bestFuzzyMatch(model, keys)
	if !ok {
		return Price{}, false
	}
	p := hardcodedTable[hit]
	p.Source = SourceHardcoded
	p.LastUpdated = hardcodedRevision
	return p, true
}

func ptrFloat64(v float64) *float64 { return &v }
