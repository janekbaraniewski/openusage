package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestParseLiteLLM_Golden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "litellm_subset.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	table, err := ParseLiteLLM(data)
	if err != nil {
		t.Fatalf("ParseLiteLLM: %v", err)
	}

	if _, ok := table["sample_spec"]; ok {
		t.Errorf("sample_spec should be filtered out")
	}
	if _, ok := table["broken-entry"]; ok {
		t.Errorf("entry missing prices should be filtered out")
	}

	sonnet, ok := table["claude-3-5-sonnet-20241022"]
	if !ok {
		t.Fatalf("expected claude-3-5-sonnet entry")
	}
	if sonnet.InputCostPerMillion != 3.0 {
		t.Errorf("input rate = %v, want 3.0", sonnet.InputCostPerMillion)
	}
	if sonnet.OutputCostPerMillion != 15.0 {
		t.Errorf("output rate = %v, want 15.0", sonnet.OutputCostPerMillion)
	}
	if sonnet.CacheReadCostPerMillion != 0.30 {
		t.Errorf("cache read rate = %v, want 0.30", sonnet.CacheReadCostPerMillion)
	}
	if sonnet.CacheWriteCostPerMillion != 3.75 {
		t.Errorf("cache write rate = %v, want 3.75", sonnet.CacheWriteCostPerMillion)
	}
	if sonnet.ContextWindow != 200000 {
		t.Errorf("context window = %v, want 200000", sonnet.ContextWindow)
	}
	if sonnet.Source != SourceLiteLLM {
		t.Errorf("source = %v, want litellm", sonnet.Source)
	}
	if sonnet.Tiers.Above200k == nil {
		t.Fatalf("expected Above200k tier")
	}
	if sonnet.Tiers.Above200k.InputCostPerMillion == nil || *sonnet.Tiers.Above200k.InputCostPerMillion != 6.0 {
		t.Errorf("Above200k input = %v, want 6.0", sonnet.Tiers.Above200k.InputCostPerMillion)
	}

	gpt4o, ok := table["openai/gpt-4o"]
	if !ok {
		t.Fatalf("expected openai/gpt-4o entry")
	}
	if gpt4o.InputCostPerMillion != 2.5 {
		t.Errorf("gpt-4o input = %v, want 2.5", gpt4o.InputCostPerMillion)
	}
	if stripLiteLLMProviderPrefix("openai/gpt-4o") != "gpt-4o" {
		t.Errorf("stripLiteLLMProviderPrefix didn't trim")
	}

	gem, ok := table["gemini-1.5-pro"]
	if !ok {
		t.Fatalf("expected gemini-1.5-pro entry")
	}
	if gem.Tiers.Above128k == nil || *gem.Tiers.Above128k.InputCostPerMillion != 2.5 {
		t.Errorf("gemini above-128k tier missing or wrong: %+v", gem.Tiers.Above128k)
	}
}

func TestLiteLLMFetcher_RetriesOn5xxAnd429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&hits, 1)
		switch count {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.ServeFile(w, r, filepath.Join("testdata", "litellm_subset.json"))
		}
	}))
	defer srv.Close()

	f := &LiteLLMFetcher{URL: srv.URL, Client: srv.Client(), Retries: 4, Backoff: 1}
	table, body, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(body) == 0 {
		t.Errorf("expected body bytes for caching")
	}
	if _, ok := table["claude-3-5-sonnet-20241022"]; !ok {
		t.Errorf("expected claude sonnet entry after retries; got %d entries", len(table))
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3", got)
	}
}

func TestLiteLLMFetcher_FailsFastOn4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	f := &LiteLLMFetcher{URL: srv.URL, Client: srv.Client(), Retries: 4, Backoff: 1}
	_, _, err := f.Fetch(context.Background())
	if err == nil {
		t.Fatalf("expected error on 403")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1 (no retry on 4xx)", got)
	}
}
