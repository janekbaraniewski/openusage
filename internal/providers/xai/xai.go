package xai

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

const defaultBaseURL = "https://api.x.ai/v1"

type apiKeyResponse struct {
	Name       string `json:"name"`
	APIKeyID   string `json:"api_key_id"`
	TeamID     string `json:"team_id"`
	CreateTime string `json:"create_time"`
	ModifyTime string `json:"modify_time"`
	ACLS       struct {
		AllowedModels []string `json:"allowed_models"`
	} `json:"acls"`
	RemainingBalance *float64 `json:"remaining_balance"`
	SpentBalance     *float64 `json:"spent_balance"`
	TotalGranted     *float64 `json:"total_granted"`
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "xai" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "xAI (Grok)",
		Capabilities: []string{"headers", "api_key_info"},
		DocURL:       "https://docs.x.ai/docs",
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusAuth,
			Message:    fmt.Sprintf("env var %s not set", acct.APIKeyEnv),
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	if err := p.fetchAPIKeyInfo(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["api_key_info_error"] = err.Error()
	}

	if err := p.fetchRateLimits(ctx, baseURL, apiKey, &snap); err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, err
	}

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}

func (p *Provider) fetchAPIKeyInfo(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/api-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
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

	var keyInfo apiKeyResponse
	if err := json.Unmarshal(body, &keyInfo); err != nil {
		return err
	}

	if keyInfo.Name != "" {
		snap.Raw["api_key_name"] = keyInfo.Name
	}
	if keyInfo.TeamID != "" {
		snap.Raw["team_id"] = keyInfo.TeamID
	}

	if keyInfo.RemainingBalance != nil {
		credits := core.Metric{
			Remaining: keyInfo.RemainingBalance,
			Unit:      "USD",
			Window:    "current",
		}
		if keyInfo.SpentBalance != nil {
			credits.Used = keyInfo.SpentBalance
		}
		if keyInfo.TotalGranted != nil {
			credits.Limit = keyInfo.TotalGranted
		}
		snap.Metrics["credits"] = credits

		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("$%.2f remaining", *keyInfo.RemainingBalance)
	}

	return nil
}

func (p *Provider) fetchRateLimits(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return nil
	case http.StatusTooManyRequests:
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
	}

	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	return nil
}
