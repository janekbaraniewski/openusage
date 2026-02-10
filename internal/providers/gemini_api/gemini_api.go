package gemini_api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

type modelsResponse struct {
	Models []modelInfo `json:"models"`
}

type modelInfo struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	InputTokenLimit            int      `json:"inputTokenLimit"`
	OutputTokenLimit           int      `json:"outputTokenLimit"`
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "gemini_api" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Google Gemini API",
		Capabilities: []string{"headers", "model_limits", "auth_check"},
		DocURL:       "https://ai.google.dev/gemini-api/docs/rate-limits",
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

	modelsURL := fmt.Sprintf("%s/models?key=%s", baseURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return snap, fmt.Errorf("gemini_api: creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return snap, fmt.Errorf("gemini_api: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap.Raw = parsers.RedactHeaders(resp.Header)

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return snap, nil
	case http.StatusTooManyRequests:
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
		p.parseRetryInfo(resp.Body, &snap)
		return snap, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		snap.Status = core.StatusError
		snap.Message = "failed to read models response"
		return snap, nil
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		snap.Status = core.StatusError
		snap.Message = "failed to parse models response"
		return snap, nil
	}

	generativeModels := p.extractGenerativeModels(modelsResp.Models)

	modelCount := float64(len(generativeModels))
	snap.Metrics["available_models"] = core.Metric{
		Used:   &modelCount,
		Unit:   "models",
		Window: "current",
	}

	p.extractTokenLimits(modelsResp.Models, &snap)

	if len(generativeModels) > 5 {
		generativeModels = generativeModels[:5]
	}
	if len(generativeModels) > 0 {
		snap.Raw["models_sample"] = strings.Join(generativeModels, ", ")
	}
	snap.Raw["total_models"] = fmt.Sprintf("%d", int(modelCount))

	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit", "x-ratelimit-remaining", "x-ratelimit-reset")

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("auth OK; %d models available", int(modelCount))

	return snap, nil
}

func (p *Provider) parseRetryInfo(body io.Reader, snap *core.QuotaSnapshot) {
	data, err := io.ReadAll(body)
	if err != nil {
		return
	}
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Details []struct {
				Metadata map[string]string `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &errResp) == nil {
		for _, d := range errResp.Error.Details {
			if retry, ok := d.Metadata["retryDelay"]; ok {
				snap.Raw["retry_delay"] = retry
			}
		}
	}
}

func (p *Provider) extractGenerativeModels(models []modelInfo) []string {
	var names []string
	for _, m := range models {
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				names = append(names, strings.TrimPrefix(m.Name, "models/"))
				break
			}
		}
	}
	return names
}

func (p *Provider) extractTokenLimits(models []modelInfo, snap *core.QuotaSnapshot) {
	for _, m := range models {
		if strings.Contains(m.Name, "gemini-2.5-flash") || strings.Contains(m.Name, "gemini-2.0-flash") {
			if m.InputTokenLimit > 0 {
				inputLimit := float64(m.InputTokenLimit)
				snap.Metrics["input_token_limit"] = core.Metric{Limit: &inputLimit, Unit: "tokens", Window: "per-request"}
				snap.Raw["model_name"] = m.DisplayName
			}
			if m.OutputTokenLimit > 0 {
				outputLimit := float64(m.OutputTokenLimit)
				snap.Metrics["output_token_limit"] = core.Metric{Limit: &outputLimit, Unit: "tokens", Window: "per-request"}
			}
			return
		}
	}
}
