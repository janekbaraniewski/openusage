package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LiteLLMURL is the canonical location of the LiteLLM pricing JSON.
// Override via NewLiteLLMFetcher / WithLiteLLMURL when testing.
const LiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// liteLLMRaw is the shape of one entry in the upstream JSON. All numeric
// fields are USD per single token, hence the multiplications below.
type liteLLMRaw struct {
	InputCostPerToken                    *float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken                   *float64 `json:"output_cost_per_token,omitempty"`
	CacheReadInputTokenCost              *float64 `json:"cache_read_input_token_cost,omitempty"`
	CacheCreationInputTokenCost          *float64 `json:"cache_creation_input_token_cost,omitempty"`
	OutputCostPerReasoningToken          *float64 `json:"output_cost_per_reasoning_token,omitempty"`
	InputCostPerTokenAbove128kTokens     *float64 `json:"input_cost_per_token_above_128k_tokens,omitempty"`
	OutputCostPerTokenAbove128kTokens    *float64 `json:"output_cost_per_token_above_128k_tokens,omitempty"`
	CacheReadInputTokenCostAbove128k     *float64 `json:"cache_read_input_token_cost_above_128k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove128k *float64 `json:"cache_creation_input_token_cost_above_128k_tokens,omitempty"`
	InputCostPerTokenAbove200kTokens     *float64 `json:"input_cost_per_token_above_200k_tokens,omitempty"`
	OutputCostPerTokenAbove200kTokens    *float64 `json:"output_cost_per_token_above_200k_tokens,omitempty"`
	CacheReadInputTokenCostAbove200k     *float64 `json:"cache_read_input_token_cost_above_200k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove200k *float64 `json:"cache_creation_input_token_cost_above_200k_tokens,omitempty"`
	InputCostPerTokenAbove256kTokens     *float64 `json:"input_cost_per_token_above_256k_tokens,omitempty"`
	OutputCostPerTokenAbove256kTokens    *float64 `json:"output_cost_per_token_above_256k_tokens,omitempty"`
	InputCostPerTokenAbove272kTokens     *float64 `json:"input_cost_per_token_above_272k_tokens,omitempty"`
	OutputCostPerTokenAbove272kTokens    *float64 `json:"output_cost_per_token_above_272k_tokens,omitempty"`
	MaxInputTokens                       *int     `json:"max_input_tokens,omitempty"`
	MaxTokens                            *int     `json:"max_tokens,omitempty"`
	LiteLLMProvider                      string   `json:"litellm_provider,omitempty"`
}

// LiteLLMFetcher fetches and parses the LiteLLM pricing table.
type LiteLLMFetcher struct {
	URL     string
	Client  *http.Client
	Retries int
	Backoff time.Duration
}

// NewLiteLLMFetcher returns a fetcher with sensible defaults.
func NewLiteLLMFetcher() *LiteLLMFetcher {
	return &LiteLLMFetcher{
		URL: LiteLLMURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Retries: 3,
		Backoff: 200 * time.Millisecond,
	}
}

// Fetch returns the parsed LiteLLM table keyed by upstream model ID.
// Network errors are wrapped with the "pricing:" prefix. Callers may pass
// `raw` separately if they want to persist the original bytes to disk.
func (f *LiteLLMFetcher) Fetch(ctx context.Context) (map[string]Price, []byte, error) {
	if f == nil {
		f = NewLiteLLMFetcher()
	}
	body, err := f.fetchBytes(ctx)
	if err != nil {
		return nil, nil, err
	}
	prices, err := ParseLiteLLM(body)
	if err != nil {
		return nil, body, err
	}
	return prices, body, nil
}

func (f *LiteLLMFetcher) fetchBytes(ctx context.Context) ([]byte, error) {
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
			return nil, fmt.Errorf("pricing: fetching litellm: %w", ctx.Err())
		}
		backoff *= 2
	}
	return nil, lastErr
}

func (f *LiteLLMFetcher) doOnce(ctx context.Context, client *http.Client) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("pricing: building litellm request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("pricing: fetching litellm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("pricing: fetching litellm: upstream status %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("pricing: fetching litellm: upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("pricing: reading litellm body: %w", err)
	}
	return body, false, nil
}

// ParseLiteLLM converts raw bytes into a Price table.
//
// LiteLLM ships a `"sample_spec"` documentation entry at the top of the
// file that has no real pricing -- we filter it out alongside any entry
// missing both base input and output rates.
func ParseLiteLLM(data []byte) (map[string]Price, error) {
	if len(data) == 0 {
		return nil, errors.New("pricing: empty litellm payload")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("pricing: parsing litellm: %w", err)
	}
	out := make(map[string]Price, len(raw))
	now := time.Now().UTC()
	for id, blob := range raw {
		if id == "" || id == "sample_spec" {
			continue
		}
		var entry liteLLMRaw
		if err := json.Unmarshal(blob, &entry); err != nil {
			// skip malformed entries silently; one bad row shouldn't break
			// the whole table.
			continue
		}
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil {
			continue
		}
		p := Price{
			ModelID:     id,
			Provider:    entry.LiteLLMProvider,
			Source:      SourceLiteLLM,
			LastUpdated: now,
		}
		if entry.MaxInputTokens != nil {
			p.ContextWindow = *entry.MaxInputTokens
		} else if entry.MaxTokens != nil {
			p.ContextWindow = *entry.MaxTokens
		}
		if entry.InputCostPerToken != nil {
			p.InputCostPerMillion = *entry.InputCostPerToken * 1_000_000
		}
		if entry.OutputCostPerToken != nil {
			p.OutputCostPerMillion = *entry.OutputCostPerToken * 1_000_000
		}
		if entry.CacheReadInputTokenCost != nil {
			p.CacheReadCostPerMillion = *entry.CacheReadInputTokenCost * 1_000_000
		}
		if entry.CacheCreationInputTokenCost != nil {
			p.CacheWriteCostPerMillion = *entry.CacheCreationInputTokenCost * 1_000_000
		}
		if entry.OutputCostPerReasoningToken != nil {
			p.ReasoningCostPerMillion = *entry.OutputCostPerReasoningToken * 1_000_000
		}
		p.Tiers = liteLLMTiers(entry)
		out[id] = p
	}
	if len(out) == 0 {
		return nil, errors.New("pricing: parsed litellm payload had zero usable entries")
	}
	return out, nil
}

func liteLLMTiers(e liteLLMRaw) TierOverrides {
	t := TierOverrides{}
	if r := tierFromPair(e.InputCostPerTokenAbove128kTokens, e.OutputCostPerTokenAbove128kTokens,
		e.CacheReadInputTokenCostAbove128k, e.CacheCreationInputTokenCostAbove128k); r != nil {
		t.Above128k = r
	}
	if r := tierFromPair(e.InputCostPerTokenAbove200kTokens, e.OutputCostPerTokenAbove200kTokens,
		e.CacheReadInputTokenCostAbove200k, e.CacheCreationInputTokenCostAbove200k); r != nil {
		t.Above200k = r
	}
	if r := tierFromPair(e.InputCostPerTokenAbove256kTokens, e.OutputCostPerTokenAbove256kTokens, nil, nil); r != nil {
		t.Above256k = r
	}
	if r := tierFromPair(e.InputCostPerTokenAbove272kTokens, e.OutputCostPerTokenAbove272kTokens, nil, nil); r != nil {
		t.Above272k = r
	}
	return t
}

func tierFromPair(in, out, cacheRead, cacheWrite *float64) *TierRates {
	if in == nil && out == nil && cacheRead == nil && cacheWrite == nil {
		return nil
	}
	r := &TierRates{}
	if in != nil {
		v := *in * 1_000_000
		r.InputCostPerMillion = &v
	}
	if out != nil {
		v := *out * 1_000_000
		r.OutputCostPerMillion = &v
	}
	if cacheRead != nil {
		v := *cacheRead * 1_000_000
		r.CacheReadCostPerMillion = &v
	}
	if cacheWrite != nil {
		v := *cacheWrite * 1_000_000
		r.CacheWriteCostPerMillion = &v
	}
	return r
}

// stripLiteLLMProviderPrefix removes the optional "<provider>/" prefix
// LiteLLM uses on some keys (e.g. "openai/gpt-4o").
func stripLiteLLMProviderPrefix(id string) string {
	if idx := strings.Index(id, "/"); idx >= 0 && idx < len(id)-1 {
		return id[idx+1:]
	}
	return id
}
