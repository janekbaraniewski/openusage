package zen

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleSapphire
	cfg.APIKeyEnv = "ZEN_API_KEY"
	cfg.DefaultAccountID = "zen"
	cfg.GaugePriority = []string{
		"models_total", "models_free", "models_paid",
		"free_probe_total_tokens", "billing_probe_total_tokens",
		"free_probe_cost_usd", "billing_probe_cost_usd",
		"pricing_input_min_paid_per_1m", "pricing_output_min_paid_per_1m",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Catalog", Keys: []string{"models_total", "models_free", "models_paid", "models_unknown"}, MaxSegments: 4},
		{Label: "Patterns", Keys: []string{"endpoint_chat_models", "endpoint_responses_models", "endpoint_messages_models", "endpoint_google_models"}, MaxSegments: 4},
		{Label: "Probe", Keys: []string{"free_probe_total_tokens", "free_probe_cost_usd", "billing_probe_total_tokens", "billing_probe_cost_usd"}, MaxSegments: 4},
		{Label: "Pricing", Keys: []string{"pricing_input_min_paid_per_1m", "pricing_output_min_paid_per_1m", "pricing_input_max_per_1m", "pricing_output_max_per_1m"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides["models_total"] = "Models"
	cfg.MetricLabelOverrides["models_free"] = "Free Models"
	cfg.MetricLabelOverrides["models_paid"] = "Paid Models"
	cfg.MetricLabelOverrides["models_unknown"] = "Unknown Models"
	cfg.MetricLabelOverrides["endpoint_chat_models"] = "Chat Endpoint Models"
	cfg.MetricLabelOverrides["endpoint_responses_models"] = "Responses Endpoint Models"
	cfg.MetricLabelOverrides["endpoint_messages_models"] = "Messages Endpoint Models"
	cfg.MetricLabelOverrides["endpoint_google_models"] = "Google Endpoint Models"
	cfg.MetricLabelOverrides["free_probe_input_tokens"] = "Free Probe Input"
	cfg.MetricLabelOverrides["free_probe_output_tokens"] = "Free Probe Output"
	cfg.MetricLabelOverrides["free_probe_total_tokens"] = "Free Probe Tokens"
	cfg.MetricLabelOverrides["free_probe_cached_tokens"] = "Free Probe Cached"
	cfg.MetricLabelOverrides["free_probe_cost_usd"] = "Free Probe Cost"
	cfg.MetricLabelOverrides["billing_probe_input_tokens"] = "Paid Probe Input"
	cfg.MetricLabelOverrides["billing_probe_output_tokens"] = "Paid Probe Output"
	cfg.MetricLabelOverrides["billing_probe_total_tokens"] = "Paid Probe Tokens"
	cfg.MetricLabelOverrides["billing_probe_cost_usd"] = "Paid Probe Cost"
	cfg.MetricLabelOverrides["pricing_input_min_paid_per_1m"] = "Input Min (Paid)"
	cfg.MetricLabelOverrides["pricing_input_max_per_1m"] = "Input Max"
	cfg.MetricLabelOverrides["pricing_output_min_paid_per_1m"] = "Output Min (Paid)"
	cfg.MetricLabelOverrides["pricing_output_max_per_1m"] = "Output Max"
	cfg.MetricLabelOverrides["free_probe_price_input_per_1m"] = "Free Probe Input Price"
	cfg.MetricLabelOverrides["free_probe_price_output_per_1m"] = "Free Probe Output Price"
	cfg.MetricLabelOverrides["billing_probe_price_input_per_1m"] = "Paid Probe Input Price"
	cfg.MetricLabelOverrides["billing_probe_price_output_per_1m"] = "Paid Probe Output Price"
	cfg.MetricLabelOverrides["subscription_active"] = "Subscription Active"
	cfg.MetricLabelOverrides["billing_payment_method_missing"] = "Payment Method Missing"
	cfg.MetricLabelOverrides["billing_out_of_credits"] = "Out of Credits"

	cfg.MetricGroupOverrides["models_total"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Models", Order: 1}
	cfg.MetricGroupOverrides["models_free"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Free Models", Order: 1}
	cfg.MetricGroupOverrides["models_paid"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Paid Models", Order: 1}
	cfg.MetricGroupOverrides["models_unknown"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Unknown Models", Order: 1}
	cfg.MetricGroupOverrides["endpoint_chat_models"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Chat Endpoint", Order: 1}
	cfg.MetricGroupOverrides["endpoint_responses_models"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Responses Endpoint", Order: 1}
	cfg.MetricGroupOverrides["endpoint_messages_models"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Messages Endpoint", Order: 1}
	cfg.MetricGroupOverrides["endpoint_google_models"] = core.DashboardMetricGroupOverride{Group: "Catalog", Label: "Google Endpoint", Order: 1}
	cfg.MetricGroupOverrides["free_probe_total_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Free Probe Tokens", Order: 2}
	cfg.MetricGroupOverrides["free_probe_input_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Free Probe Input", Order: 2}
	cfg.MetricGroupOverrides["free_probe_output_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Free Probe Output", Order: 2}
	cfg.MetricGroupOverrides["free_probe_cached_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Free Probe Cached", Order: 2}
	cfg.MetricGroupOverrides["billing_probe_total_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Paid Probe Tokens", Order: 2}
	cfg.MetricGroupOverrides["billing_probe_input_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Paid Probe Input", Order: 2}
	cfg.MetricGroupOverrides["billing_probe_output_tokens"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Paid Probe Output", Order: 2}
	cfg.MetricGroupOverrides["free_probe_cost_usd"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Free Probe Cost", Order: 3}
	cfg.MetricGroupOverrides["billing_probe_cost_usd"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Paid Probe Cost", Order: 3}
	cfg.MetricGroupOverrides["pricing_input_min_paid_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Input Min (Paid)", Order: 4}
	cfg.MetricGroupOverrides["pricing_output_min_paid_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Output Min (Paid)", Order: 4}
	cfg.MetricGroupOverrides["pricing_input_max_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Input Max", Order: 4}
	cfg.MetricGroupOverrides["pricing_output_max_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Output Max", Order: 4}
	cfg.MetricGroupOverrides["free_probe_price_input_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Free Probe Input Price", Order: 4}
	cfg.MetricGroupOverrides["free_probe_price_output_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Free Probe Output Price", Order: 4}
	cfg.MetricGroupOverrides["billing_probe_price_input_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Paid Probe Input Price", Order: 4}
	cfg.MetricGroupOverrides["billing_probe_price_output_per_1m"] = core.DashboardMetricGroupOverride{Group: "Pricing", Label: "Paid Probe Output Price", Order: 4}
	cfg.MetricGroupOverrides["subscription_active"] = core.DashboardMetricGroupOverride{Group: "Billing", Label: "Subscription Active", Order: 5}
	cfg.MetricGroupOverrides["billing_payment_method_missing"] = core.DashboardMetricGroupOverride{Group: "Billing", Label: "Payment Method Missing", Order: 5}
	cfg.MetricGroupOverrides["billing_out_of_credits"] = core.DashboardMetricGroupOverride{Group: "Billing", Label: "Out of Credits", Order: 5}

	cfg.CompactMetricLabelOverrides["models_total"] = "models"
	cfg.CompactMetricLabelOverrides["models_free"] = "free"
	cfg.CompactMetricLabelOverrides["models_paid"] = "paid"
	cfg.CompactMetricLabelOverrides["models_unknown"] = "unknown"
	cfg.CompactMetricLabelOverrides["endpoint_chat_models"] = "chat"
	cfg.CompactMetricLabelOverrides["endpoint_responses_models"] = "resp"
	cfg.CompactMetricLabelOverrides["endpoint_messages_models"] = "msg"
	cfg.CompactMetricLabelOverrides["endpoint_google_models"] = "google"
	cfg.CompactMetricLabelOverrides["free_probe_total_tokens"] = "free tok"
	cfg.CompactMetricLabelOverrides["billing_probe_total_tokens"] = "paid tok"
	cfg.CompactMetricLabelOverrides["free_probe_cost_usd"] = "free $"
	cfg.CompactMetricLabelOverrides["billing_probe_cost_usd"] = "paid $"
	cfg.CompactMetricLabelOverrides["pricing_input_min_paid_per_1m"] = "in min"
	cfg.CompactMetricLabelOverrides["pricing_output_min_paid_per_1m"] = "out min"
	cfg.CompactMetricLabelOverrides["pricing_input_max_per_1m"] = "in max"
	cfg.CompactMetricLabelOverrides["pricing_output_max_per_1m"] = "out max"

	cfg.RawGroups = []core.DashboardRawGroup{
		{
			Label: "Account",
			Keys: []string{
				"workspace_id", "subscription_status", "billing_status", "payment_required",
				"billing_url", "team_billing_policy", "team_model_access",
			},
		},
		{
			Label: "Catalog",
			Keys: []string{
				"models_count", "models_preview", "models_free_count", "models_paid_count", "models_unknown_count",
				"endpoint_unknown_models",
			},
		},
		{
			Label: "Probes",
			Keys: []string{
				"free_probe_status", "free_probe_endpoint", "free_probe_model", "free_probe_request_id", "free_probe_error_type", "free_probe_error",
				"billing_probe_status", "billing_probe_endpoint", "billing_probe_model", "billing_probe_request_id", "billing_probe_error_type", "billing_probe_error", "billing_probe_skipped",
			},
		},
		{
			Label: "Policies",
			Keys: []string{
				"provider_docs", "pricing_docs", "pricing_last_verified",
				"billing_model", "billing_fee_policy", "monthly_limits_scope", "subscription_mutability",
				"monthly_limits_supported", "auto_reload_supported", "team_roles_supported", "byok_supported",
				"api_base_url",
			},
		},
	}

	cfg.SuppressZeroMetricKeys = []string{
		"free_probe_cost_usd", "billing_probe_cost_usd",
		"billing_payment_method_missing", "billing_out_of_credits",
	}
	cfg.HideMetricPrefixes = nil
	return cfg
}

func (p *Provider) DetailWidget() core.DetailWidget {
	cfg := core.DefaultDetailWidget()
	cfg.Sections = append(cfg.Sections,
		core.DetailSection{Name: "Catalog", Order: 1, Style: core.DetailSectionStyleList},
		core.DetailSection{Name: "Pricing", Order: 4, Style: core.DetailSectionStyleList},
		core.DetailSection{Name: "Billing", Order: 5, Style: core.DetailSectionStyleList},
	)
	return cfg
}
