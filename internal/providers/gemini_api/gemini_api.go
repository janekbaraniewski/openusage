// Package gemini_api implements a QuotaProvider for the Google Gemini API.
//
// Gemini enforces RPM/TPM/RPD per project tier. The API does not expose
// "remaining" headers like OpenAI/Anthropic. However:
//
//   - GET /v1beta/models — lists available models (confirms auth works)
//   - GET /v1beta/models/{model} — model info with limits per tier
//   - 429 responses include error details with retry_delay hints
//
// Rate limits by tier (Gemini 2.5 Flash):
//   - Free: 10 RPM, 250K TPM, 500 RPD
//   - Pay-as-you-go Tier 1: 2000 RPM, 4M TPM
//   - Tier 2: 4000 RPM, 10M TPM
//
// We report auth status, available model list, and known tier limits.
package gemini_api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// ── Model pricing (USD per 1M tokens, as of early 2026) ────────────────────
// Source: https://ai.google.dev/gemini-api/docs/pricing
// Note: Gemini has tiered pricing — prices below are for Pay-as-you-go.
// Many models have free tiers with rate limits (e.g. 10 RPM, 500 RPD).
type modelPricing struct {
	InputPerMillion          float64
	OutputPerMillion         float64
	InputPerMillionLong      float64 // >200K context (for models that have tiered pricing)
	OutputPerMillionLong     float64
	CachedInputPerMillion    float64
	ThinkingOutputPerMillion float64 // for thinking models
}

var modelPricingTable = map[string]modelPricing{
	// Gemini 2.5 Pro
	"gemini-2.5-pro": {
		InputPerMillion: 1.25, OutputPerMillion: 10.00,
		InputPerMillionLong: 2.50, OutputPerMillionLong: 15.00,
		CachedInputPerMillion: 0.3125, ThinkingOutputPerMillion: 3.50,
	},
	// Gemini 2.5 Flash
	"gemini-2.5-flash": {
		InputPerMillion: 0.15, OutputPerMillion: 0.60,
		InputPerMillionLong: 0.30, OutputPerMillionLong: 1.20,
		CachedInputPerMillion: 0.0375, ThinkingOutputPerMillion: 3.50,
	},
	// Gemini 2.0 Flash
	"gemini-2.0-flash": {
		InputPerMillion: 0.10, OutputPerMillion: 0.40,
		CachedInputPerMillion: 0.025,
	},
	// Gemini 2.0 Flash Lite
	"gemini-2.0-flash-lite": {
		InputPerMillion: 0.075, OutputPerMillion: 0.30,
		CachedInputPerMillion: 0.01875,
	},
	// Gemini 1.5 Pro (legacy)
	"gemini-1.5-pro": {
		InputPerMillion: 1.25, OutputPerMillion: 5.00,
		InputPerMillionLong: 2.50, OutputPerMillionLong: 10.00,
		CachedInputPerMillion: 0.3125,
	},
	// Gemini 1.5 Flash (legacy)
	"gemini-1.5-flash": {
		InputPerMillion: 0.075, OutputPerMillion: 0.30,
		InputPerMillionLong: 0.15, OutputPerMillionLong: 0.60,
		CachedInputPerMillion: 0.01875,
	},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "2.5-pro: $1.25/$10 · 2.5-flash: $0.15/$0.60 (thinking: $3.50/1M) · " +
		"2.0-flash: $0.10/$0.40 · 2.0-flash-lite: $0.075/$0.30 " +
		"(input/output per 1M tokens; >200K context costs 2x)"
}

// modelsResponse is the JSON from GET /v1beta/models.
type modelsResponse struct {
	Models []modelInfo `json:"models"`
}

type modelInfo struct {
	Name                       string   `json:"name"` // "models/gemini-2.5-flash"
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	InputTokenLimit            int      `json:"inputTokenLimit"`
	OutputTokenLimit           int      `json:"outputTokenLimit"`
}

// Provider implements core.QuotaProvider for the Google Gemini API.
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
	apiKey := acct.Token
	if apiKey == "" {
		apiKey = os.Getenv(acct.APIKeyEnv)
	}
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

	// 1. List all available models to confirm auth + get model capabilities
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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusBadRequest {
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return snap, nil
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"

		// Try to extract retry info from response body
		if body, err := io.ReadAll(resp.Body); err == nil {
			var errResp struct {
				Error struct {
					Message string `json:"message"`
					Details []struct {
						Metadata map[string]string `json:"metadata"`
					} `json:"details"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &errResp) == nil {
				for _, d := range errResp.Error.Details {
					if retry, ok := d.Metadata["retryDelay"]; ok {
						snap.Raw["retry_delay"] = retry
					}
				}
			}
		}
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

	// Count available models by capability
	var generativeModels []string
	for _, m := range modelsResp.Models {
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				shortName := strings.TrimPrefix(m.Name, "models/")
				generativeModels = append(generativeModels, shortName)
				break
			}
		}
	}

	modelCount := float64(len(generativeModels))
	snap.Metrics["available_models"] = core.Metric{
		Used:   &modelCount,
		Unit:   "models",
		Window: "current",
	}

	// Get token limits for a popular model
	for _, m := range modelsResp.Models {
		if strings.Contains(m.Name, "gemini-2.5-flash") || strings.Contains(m.Name, "gemini-2.0-flash") {
			if m.InputTokenLimit > 0 {
				inputLimit := float64(m.InputTokenLimit)
				snap.Metrics["input_token_limit"] = core.Metric{
					Limit:  &inputLimit,
					Unit:   "tokens",
					Window: "per-request",
				}
				snap.Raw["model_name"] = m.DisplayName
			}
			if m.OutputTokenLimit > 0 {
				outputLimit := float64(m.OutputTokenLimit)
				snap.Metrics["output_token_limit"] = core.Metric{
					Limit:  &outputLimit,
					Unit:   "tokens",
					Window: "per-request",
				}
			}
			break
		}
	}

	// Store available models (first 5)
	shown := generativeModels
	if len(shown) > 5 {
		shown = shown[:5]
	}
	if len(shown) > 0 {
		snap.Raw["models_sample"] = strings.Join(shown, ", ")
	}
	snap.Raw["total_models"] = fmt.Sprintf("%d", len(generativeModels))

	// Parse any rate-limit headers (Gemini occasionally includes them)
	applyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit", "x-ratelimit-remaining", "x-ratelimit-reset")

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingSummary()
	// Add pricing for the detected model
	for _, m := range modelsResp.Models {
		shortName := strings.TrimPrefix(m.Name, "models/")
		for prefix, pricing := range modelPricingTable {
			if strings.HasPrefix(shortName, prefix) {
				snap.Raw[fmt.Sprintf("pricing_%s_input", prefix)] = fmt.Sprintf("$%.3f/1M tokens", pricing.InputPerMillion)
				snap.Raw[fmt.Sprintf("pricing_%s_output", prefix)] = fmt.Sprintf("$%.3f/1M tokens", pricing.OutputPerMillion)
				break
			}
		}
	}

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("auth OK; %d models available", len(generativeModels))

	return snap, nil
}

func applyRateLimitGroup(h http.Header, snap *core.QuotaSnapshot, key, unit, window, limitH, remainH, resetH string) {
	rlg := parsers.ParseRateLimitGroup(h, limitH, remainH, resetH)
	if rlg == nil {
		return
	}
	snap.Metrics[key] = core.Metric{
		Limit:     rlg.Limit,
		Remaining: rlg.Remaining,
		Unit:      unit,
		Window:    window,
	}
	if rlg.ResetTime != nil {
		snap.Resets[key+"_reset"] = *rlg.ResetTime
	}
}
