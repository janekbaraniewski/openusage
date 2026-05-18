package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOpenRouter_Golden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "openrouter_subset.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	table, err := ParseOpenRouter(data)
	if err != nil {
		t.Fatalf("ParseOpenRouter: %v", err)
	}

	if _, ok := table["empty-pricing/model"]; ok {
		t.Errorf("entries with no rates should be skipped")
	}

	sonnet, ok := table["anthropic/claude-3.5-sonnet"]
	if !ok {
		t.Fatalf("expected claude sonnet entry")
	}
	if sonnet.InputCostPerMillion != 3.0 {
		t.Errorf("input = %v, want 3.0", sonnet.InputCostPerMillion)
	}
	if sonnet.OutputCostPerMillion != 15.0 {
		t.Errorf("output = %v, want 15.0", sonnet.OutputCostPerMillion)
	}
	if sonnet.CacheReadCostPerMillion != 0.30 {
		t.Errorf("cache read = %v, want 0.30", sonnet.CacheReadCostPerMillion)
	}
	if sonnet.CacheWriteCostPerMillion != 3.75 {
		t.Errorf("cache write = %v, want 3.75", sonnet.CacheWriteCostPerMillion)
	}
	if sonnet.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", sonnet.Provider)
	}
	if sonnet.ContextWindow != 200000 {
		t.Errorf("context window = %v, want 200000", sonnet.ContextWindow)
	}
	if sonnet.Source != SourceOpenRouter {
		t.Errorf("source = %v, want openrouter", sonnet.Source)
	}

	gpt, ok := table["openai/gpt-4o"]
	if !ok {
		t.Fatalf("expected gpt-4o entry")
	}
	if gpt.CacheReadCostPerMillion != 0.125 {
		t.Errorf("gpt-4o cache read = %v, want 0.125", gpt.CacheReadCostPerMillion)
	}
}

func TestOpenRouterFetcher_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("testdata", "openrouter_subset.json"))
	}))
	defer srv.Close()

	f := &OpenRouterFetcher{URL: srv.URL, Client: srv.Client(), Retries: 1, Backoff: 1}
	table, body, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(body) == 0 {
		t.Errorf("expected raw body for caching")
	}
	if _, ok := table["anthropic/claude-3.5-sonnet"]; !ok {
		t.Errorf("expected anthropic claude entry")
	}
}
