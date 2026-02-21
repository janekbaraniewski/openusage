package core

import "strings"

type MetricGroup string

const (
	MetricGroupUsage    MetricGroup = "Usage"
	MetricGroupSpending MetricGroup = "Spending"
	MetricGroupTokens   MetricGroup = "Tokens"
	MetricGroupActivity MetricGroup = "Activity"
)

func InferMetricGroup(key string, m Metric) MetricGroup {
	lk := strings.ToLower(key)

	switch {
	case key == "rpm" || key == "tpm" || key == "rpd" || key == "tpd":
		return MetricGroupUsage
	case strings.HasPrefix(key, "rate_limit_"):
		return MetricGroupUsage
	case key == "rpm_headers" || key == "tpm_headers":
		return MetricGroupUsage
	case key == "gh_api_rpm" || key == "copilot_chat":
		return MetricGroupUsage
	case key == "plan_percent_used" || key == "spend_limit" || key == "plan_spend":
		return MetricGroupUsage
	case key == "monthly_spend" && m.Limit != nil:
		return MetricGroupUsage
	case key == "monthly_budget" && m.Limit != nil:
		return MetricGroupUsage
	case (key == "credits" || key == "credit_balance") && m.Limit != nil:
		return MetricGroupUsage
	case key == "context_window":
		return MetricGroupUsage
	case key == "usage_daily" || key == "usage_weekly" || key == "usage_monthly" || key == "limit_remaining":
		return MetricGroupUsage
	case m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD":
		return MetricGroupUsage
	case m.Unit == "%" && (m.Used != nil || m.Remaining != nil):
		return MetricGroupUsage
	case m.Used != nil && m.Limit != nil && !strings.Contains(lk, "token") && m.Unit != "%" && m.Unit != "USD":
		return MetricGroupUsage

	case strings.HasPrefix(key, "model_") &&
		!strings.HasSuffix(key, "_input_tokens") &&
		!strings.HasSuffix(key, "_output_tokens"):
		return MetricGroupSpending
	case key == "plan_included" || key == "plan_bonus" || key == "plan_total_spend_usd" || key == "plan_limit_usd":
		return MetricGroupSpending
	case key == "individual_spend":
		return MetricGroupSpending
	case strings.Contains(lk, "cost") || strings.Contains(lk, "burn_rate"):
		return MetricGroupSpending
	case key == "credits" || key == "credit_balance":
		return MetricGroupSpending
	case key == "monthly_spend" || key == "monthly_budget":
		return MetricGroupSpending
	case strings.HasSuffix(key, "_balance"):
		return MetricGroupSpending

	case IsPerModelTokenMetricKey(key):
		return MetricGroupTokens
	case strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_reasoning_tokens") || strings.HasSuffix(key, "_cached_tokens") || strings.HasSuffix(key, "_image_tokens")):
		return MetricGroupTokens
	case strings.HasPrefix(key, "session_"):
		return MetricGroupTokens
	case strings.HasPrefix(key, "today_") && strings.Contains(lk, "token"):
		return MetricGroupTokens
	case strings.Contains(lk, "token"):
		return MetricGroupTokens
	}

	return MetricGroupActivity
}

func MetricUsedPercent(key string, m Metric) float64 {
	if key == "context_window" {
		return -1
	}
	if m.Unit == "%" && m.Used != nil {
		return *m.Used
	}
	if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
		return (*m.Limit - *m.Remaining) / *m.Limit * 100
	}
	if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		return *m.Used / *m.Limit * 100
	}
	return -1
}

func IsModelCostMetricKey(key string) bool {
	return strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_cost") || strings.HasSuffix(key, "_cost_usd"))
}

func IsPerModelTokenMetricKey(key string) bool {
	if strings.HasPrefix(key, "input_tokens_") || strings.HasPrefix(key, "output_tokens_") {
		return true
	}
	if strings.HasPrefix(key, "model_") &&
		(strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")) {
		return true
	}
	return false
}
