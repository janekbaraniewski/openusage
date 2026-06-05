package tmux

// semanticAliases maps the friendly template variable names (used in presets
// and user formats) to provider-specific metric keys in UsageSnapshot.Metrics.
// The first matching key for a provider wins; unmapped providers fall through
// to an empty string so `{?cond:then:else}` can suppress the section.
//
// Keys prefixed with "_" are synthetic and are populated by BuildContext from
// report.Build rather than from the snapshot Metrics map. They are visible to
// the alias map but are never user-typeable as bare variable names.
var semanticAliases = map[string]map[string]string{
	"today_cost": {
		"claude_code": "today_api_cost",
		"cursor":      "today_cost",
		"codex":       "today_cost",
		"openrouter":  "today_cost",
		"copilot":     "messages_today",
	},
	"block_cost":      {"claude_code": "5h_block_cost"},
	"block_pct":       {"claude_code": "usage_five_hour"},
	"block_remaining": {"claude_code": "_block_remaining"},
	"block_projection": {
		"claude_code": "_block_projection",
	},
	"burn_rate": {
		"claude_code": "_block_burn_rate",
		"openrouter":  "burn_rate",
	},
	"plan_pct": {
		"cursor": "plan_auto_percent_used",
		"codex":  "plan_api_percent_used",
	},
	"today_input_tokens": {
		"openrouter": "today_input_tokens",
		"copilot":    "today_input_tokens",
	},
	"today_output_tokens": {
		"openrouter": "today_output_tokens",
		"copilot":    "today_output_tokens",
	},
	"requests_today": {
		"cursor": "requests_today",
		"codex":  "requests_today",
	},
	"context_pct":    {"claude_code": "_context_pct"},
	"context_tokens": {"claude_code": "_context_tokens"},
	"tool_color":     {"*": "_tool_color"},
}

// resolveAlias returns the provider-specific metric key for a semantic alias
// name, or "" if no mapping exists. The "*" entry in the inner map is used as
// a catch-all so providers without an explicit override still match.
func resolveAlias(name, provider string) string {
	tbl, ok := semanticAliases[name]
	if !ok {
		return ""
	}
	if key, ok := tbl[provider]; ok && key != "" {
		return key
	}
	if key, ok := tbl["*"]; ok && key != "" {
		return key
	}
	return ""
}

// aliasNames returns the list of all known semantic alias names. Used by the
// `openusage tmux variables` command.
func aliasNames() []string {
	out := make([]string, 0, len(semanticAliases))
	for k := range semanticAliases {
		out = append(out, k)
	}
	return out
}
