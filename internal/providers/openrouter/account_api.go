package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

func (p *Provider) fetchAuthKey(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	for _, endpoint := range []string{"/key", "/auth/key"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+endpoint, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.Client().Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		snap.Raw = parsers.RedactHeaders(resp.Header)
		if resp.StatusCode == http.StatusNotFound && endpoint == "/key" {
			resp.Body.Close()
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("reading body: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
			return nil
		case http.StatusOK:
		default:
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var keyResp keyResponse
		if err := json.Unmarshal(body, &keyResp); err != nil {
			snap.Status = core.StatusError
			snap.Message = "failed to parse key response"
			return nil
		}

		applyKeyData(&keyResp.Data, snap)
		parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm_headers", "requests", "1m",
			"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
		parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm_headers", "tokens", "1m",
			"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")
		return nil
	}

	return fmt.Errorf("key endpoint not available (HTTP 404)")
}

func applyKeyData(data *keyData, snap *core.UsageSnapshot) {
	usage := data.Usage
	var remaining *float64
	if data.LimitRemaining != nil {
		remaining = data.LimitRemaining
	} else if data.Limit != nil {
		r := *data.Limit - usage
		remaining = &r
	}

	if data.Limit != nil {
		snap.Metrics["credits"] = core.Metric{
			Limit:     data.Limit,
			Used:      &usage,
			Remaining: remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}
	} else {
		snap.Metrics["credits"] = core.Metric{Used: &usage, Unit: "USD", Window: "lifetime"}
	}

	if remaining != nil {
		snap.Metrics["limit_remaining"] = core.Metric{Used: remaining, Unit: "USD", Window: "current_period"}
	}
	if data.UsageDaily != nil {
		snap.Metrics["usage_daily"] = core.Metric{Used: data.UsageDaily, Unit: "USD", Window: "1d"}
	}
	if data.UsageWeekly != nil {
		snap.Metrics["usage_weekly"] = core.Metric{Used: data.UsageWeekly, Unit: "USD", Window: "7d"}
	}
	if data.UsageMonthly != nil {
		snap.Metrics["usage_monthly"] = core.Metric{Used: data.UsageMonthly, Unit: "USD", Window: "30d"}
	}
	if data.ByokUsage != nil && *data.ByokUsage > 0 {
		snap.Metrics["byok_usage"] = core.Metric{Used: data.ByokUsage, Unit: "USD", Window: "lifetime"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageDaily != nil && *data.ByokUsageDaily > 0 {
		snap.Metrics["byok_daily"] = core.Metric{Used: data.ByokUsageDaily, Unit: "USD", Window: "1d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageWeekly != nil && *data.ByokUsageWeekly > 0 {
		snap.Metrics["byok_weekly"] = core.Metric{Used: data.ByokUsageWeekly, Unit: "USD", Window: "7d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageMonthly != nil && *data.ByokUsageMonthly > 0 {
		snap.Metrics["byok_monthly"] = core.Metric{Used: data.ByokUsageMonthly, Unit: "USD", Window: "30d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageInference != nil && *data.ByokUsageInference > 0 {
		snap.Metrics["today_byok_cost"] = core.Metric{Used: data.ByokUsageInference, Unit: "USD", Window: "1d"}
		snap.Raw["byok_in_use"] = "true"
	}

	if data.RateLimit.Requests > 0 {
		rl := float64(data.RateLimit.Requests)
		snap.Metrics["rpm"] = core.Metric{Limit: &rl, Unit: "requests", Window: data.RateLimit.Interval}
	}

	keyLabel := data.Label
	if keyLabel == "" {
		keyLabel = data.Name
	}
	if keyLabel != "" {
		snap.Raw["key_label"] = keyLabel
	}
	if data.IsFreeTier {
		snap.Raw["tier"] = "free"
	} else {
		snap.Raw["tier"] = "paid"
	}

	snap.Raw["is_free_tier"] = fmt.Sprintf("%t", data.IsFreeTier)
	snap.Raw["is_management_key"] = fmt.Sprintf("%t", data.IsManagementKey)
	snap.Raw["is_provisioning_key"] = fmt.Sprintf("%t", data.IsProvisioningKey)
	snap.Raw["include_byok_in_limit"] = fmt.Sprintf("%t", data.IncludeByokInLimit)
	if data.RateLimit.Note != "" {
		snap.Raw["rate_limit_note"] = data.RateLimit.Note
	}

	switch {
	case data.IsManagementKey:
		snap.Raw["key_type"] = "management"
	case data.IsProvisioningKey:
		snap.Raw["key_type"] = "provisioning"
	default:
		snap.Raw["key_type"] = "standard"
	}

	if data.LimitReset != "" {
		snap.Raw["limit_reset"] = data.LimitReset
		if t, err := time.Parse(time.RFC3339, data.LimitReset); err == nil {
			snap.Resets["limit_reset"] = t
		}
	}
	if data.ExpiresAt != "" {
		snap.Raw["expires_at"] = data.ExpiresAt
		if t, err := time.Parse(time.RFC3339, data.ExpiresAt); err == nil {
			snap.Resets["key_expires"] = t
		}
	}

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("$%.4f used", usage)
	if data.Limit != nil {
		snap.Message += fmt.Sprintf(" / $%.2f limit", *data.Limit)
	}
}

func (p *Provider) fetchCreditsDetail(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/credits", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var detail creditsDetailResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		return err
	}

	remaining := detail.Data.TotalCredits - detail.Data.TotalUsage
	if detail.Data.RemainingBalance != nil {
		remaining = *detail.Data.RemainingBalance
	}
	if detail.Data.TotalCredits > 0 || detail.Data.TotalUsage > 0 || remaining > 0 {
		totalCredits := detail.Data.TotalCredits
		totalUsage := detail.Data.TotalUsage
		snap.Metrics["credit_balance"] = core.Metric{
			Limit:     &totalCredits,
			Used:      &totalUsage,
			Remaining: &remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}
		snap.Message = fmt.Sprintf("$%.4f used", totalUsage)
		if totalCredits > 0 {
			snap.Message += fmt.Sprintf(" / $%.2f credits", totalCredits)
		}
	}

	return nil
}

func (p *Provider) fetchKeysMeta(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	const (
		pageSizeHint = 100
		maxPages     = 20
	)

	var allKeys []keyListEntry
	offset := 0
	for page := 0; page < maxPages; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/keys?include_disabled=true&offset=%d", baseURL, offset), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.Client().Do(req)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusForbidden {
			return nil
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var pageResp keysResponse
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return fmt.Errorf("parsing keys list: %w", err)
		}
		if len(pageResp.Data) == 0 {
			break
		}

		allKeys = append(allKeys, pageResp.Data...)
		offset += len(pageResp.Data)
		if len(pageResp.Data) < pageSizeHint {
			break
		}
	}

	snap.Raw["keys_total"] = fmt.Sprintf("%d", len(allKeys))

	active := 0
	for _, key := range allKeys {
		if !key.Disabled {
			active++
		}
	}
	snap.Raw["keys_active"] = fmt.Sprintf("%d", active)
	disabled := len(allKeys) - active
	snap.Raw["keys_disabled"] = fmt.Sprintf("%d", disabled)

	totalF := float64(len(allKeys))
	activeF := float64(active)
	disabledF := float64(disabled)
	snap.Metrics["keys_total"] = core.Metric{Used: &totalF, Unit: "keys", Window: "account"}
	snap.Metrics["keys_active"] = core.Metric{Used: &activeF, Unit: "keys", Window: "account"}
	if disabled > 0 {
		snap.Metrics["keys_disabled"] = core.Metric{Used: &disabledF, Unit: "keys", Window: "account"}
	}

	currentLabel := snap.Raw["key_label"]
	if currentLabel == "" {
		return nil
	}

	var current *keyListEntry
	for i := range allKeys {
		if allKeys[i].Label == currentLabel {
			current = &allKeys[i]
			break
		}
	}
	if current == nil {
		snap.Raw["key_lookup"] = "not_in_keys_list"
		return nil
	}

	if current.Name != "" {
		snap.Raw["key_name"] = current.Name
	}
	snap.Raw["key_disabled"] = fmt.Sprintf("%t", current.Disabled)
	if current.CreatedAt != "" {
		snap.Raw["key_created_at"] = current.CreatedAt
	}
	if current.UpdatedAt != nil && *current.UpdatedAt != "" {
		snap.Raw["key_updated_at"] = *current.UpdatedAt
	}
	if current.Hash != "" {
		hash := current.Hash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		snap.Raw["key_hash_prefix"] = hash
	}

	if snap.Raw["is_management_key"] == "true" {
		var totalUsage, daily, weekly, monthly float64
		for _, key := range allKeys {
			totalUsage += key.Usage
			daily += key.UsageDaily
			weekly += key.UsageWeekly
			monthly += key.UsageMonthly
		}
		if totalUsage > 0 {
			snap.Metrics["credits"] = core.Metric{Used: &totalUsage, Unit: "USD", Window: "lifetime"}
			if lim := snap.Metrics["credits"].Limit; lim != nil {
				snap.Metrics["credits"] = core.Metric{Limit: lim, Used: &totalUsage, Unit: "USD", Window: "lifetime"}
			}
		}
		if daily > 0 {
			snap.Metrics["usage_daily"] = core.Metric{Used: &daily, Unit: "USD", Window: "1d"}
		}
		if weekly > 0 {
			snap.Metrics["usage_weekly"] = core.Metric{Used: &weekly, Unit: "USD", Window: "7d"}
		}
		if monthly > 0 {
			snap.Metrics["usage_monthly"] = core.Metric{Used: &monthly, Unit: "USD", Window: "30d"}
		}
	}

	return nil
}
