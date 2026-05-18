package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// OpenRouterURL is the canonical models endpoint used as a fallback pricing
// source. The endpoint is unauthenticated and returns per-token prices as
// decimal strings.
const OpenRouterURL = "https://openrouter.ai/api/v1/models"

type openRouterPricingRaw struct {
	Prompt          string `json:"prompt,omitempty"`
	Completion      string `json:"completion,omitempty"`
	InputCacheRead  string `json:"input_cache_read,omitempty"`
	InputCacheWrite string `json:"input_cache_write,omitempty"`
	Reasoning       string `json:"internal_reasoning,omitempty"`
}

type openRouterModelRaw struct {
	ID            string               `json:"id"`
	Name          string               `json:"name,omitempty"`
	ContextLength int                  `json:"context_length,omitempty"`
	Pricing       openRouterPricingRaw `json:"pricing"`
}

type openRouterModelsResponse struct {
	Data []openRouterModelRaw `json:"data"`
}

// OpenRouterFetcher fetches and parses the OpenRouter models list as a
// fallback pricing source.
type OpenRouterFetcher struct {
	URL     string
	Client  *http.Client
	Retries int
	Backoff time.Duration
}

// NewOpenRouterFetcher returns a fetcher with sensible defaults.
func NewOpenRouterFetcher() *OpenRouterFetcher {
	return &OpenRouterFetcher{
		URL: OpenRouterURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Retries: 3,
		Backoff: 200 * time.Millisecond,
	}
}

// Fetch returns the parsed OpenRouter table keyed by upstream model ID.
func (f *OpenRouterFetcher) Fetch(ctx context.Context) (map[string]Price, []byte, error) {
	if f == nil {
		f = NewOpenRouterFetcher()
	}
	body, err := f.fetchBytes(ctx)
	if err != nil {
		return nil, nil, err
	}
	prices, err := ParseOpenRouter(body)
	if err != nil {
		return nil, body, err
	}
	return prices, body, nil
}

func (f *OpenRouterFetcher) fetchBytes(ctx context.Context) ([]byte, error) {
	var lastErr error
	backoff := f.Backoff
	if backoff <= 0 {
		backoff = 200 * time.Millisecond
	}
	attempts := f.Retries
	if attempts <= 0 {
		attempts = 1
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	for i := 0; i < attempts; i++ {
		body, retriable, err := f.doOnce(ctx, client)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retriable {
			break
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, fmt.Errorf("pricing: fetching openrouter: %w", ctx.Err())
		}
		backoff *= 2
	}
	return nil, lastErr
}

func (f *OpenRouterFetcher) doOnce(ctx context.Context, client *http.Client) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("pricing: building openrouter request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("pricing: fetching openrouter: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("pricing: fetching openrouter: upstream status %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("pricing: fetching openrouter: upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("pricing: reading openrouter body: %w", err)
	}
	return body, false, nil
}

// ParseOpenRouter converts the raw models payload into a Price table.
// All upstream prices are strings in USD per single token; we multiply by
// 1M for our canonical $/1M tokens unit.
func ParseOpenRouter(data []byte) (map[string]Price, error) {
	if len(data) == 0 {
		return nil, errors.New("pricing: empty openrouter payload")
	}
	var parsed openRouterModelsResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("pricing: parsing openrouter: %w", err)
	}
	out := make(map[string]Price, len(parsed.Data))
	now := time.Now().UTC()
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		input := parseOpenRouterRate(m.Pricing.Prompt)
		output := parseOpenRouterRate(m.Pricing.Completion)
		if input == 0 && output == 0 {
			continue
		}
		price := Price{
			ModelID:              m.ID,
			Provider:             openRouterProvider(m.ID),
			Source:               SourceOpenRouter,
			LastUpdated:          now,
			ContextWindow:        m.ContextLength,
			InputCostPerMillion:  input,
			OutputCostPerMillion: output,
		}
		if cr := parseOpenRouterRate(m.Pricing.InputCacheRead); cr > 0 {
			price.CacheReadCostPerMillion = cr
		}
		if cw := parseOpenRouterRate(m.Pricing.InputCacheWrite); cw > 0 {
			price.CacheWriteCostPerMillion = cw
		}
		if r := parseOpenRouterRate(m.Pricing.Reasoning); r > 0 {
			price.ReasoningCostPerMillion = r
		}
		out[m.ID] = price
	}
	if len(out) == 0 {
		return nil, errors.New("pricing: parsed openrouter payload had zero usable entries")
	}
	return out, nil
}

// parseOpenRouterRate parses a per-token decimal string and returns the
// $/1M-tokens rate. Empty or malformed inputs yield 0.
func parseOpenRouterRate(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v * 1_000_000
}

func openRouterProvider(id string) string {
	if idx := strings.Index(id, "/"); idx > 0 {
		return id[:idx]
	}
	return ""
}
