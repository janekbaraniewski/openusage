package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestResolver wires both upstreams to httptest servers backed by the
// checked-in fixtures, isolates the disk cache under a temp dir, and
// returns the resolver together with hit counters.
func newTestResolver(t *testing.T, litellmFixture, openrouterFixture string, opts ...ResolverOption) (*Resolver, *int32, *int32) {
	t.Helper()
	var litellmHits, openrouterHits int32
	litellmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&litellmHits, 1)
		http.ServeFile(w, r, filepath.Join("testdata", litellmFixture))
	}))
	t.Cleanup(litellmSrv.Close)
	openrouterSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&openrouterHits, 1)
		http.ServeFile(w, r, filepath.Join("testdata", openrouterFixture))
	}))
	t.Cleanup(openrouterSrv.Close)

	cache := NewDiskCacheAt(t.TempDir())
	cache.SetTTL(time.Hour)
	args := append([]ResolverOption{
		WithCache(cache),
		WithLiteLLMFetcher(&LiteLLMFetcher{URL: litellmSrv.URL, Client: litellmSrv.Client(), Retries: 1, Backoff: 1}),
		WithOpenRouterFetcher(&OpenRouterFetcher{URL: openrouterSrv.URL, Client: openrouterSrv.Client(), Retries: 1, Backoff: 1}),
	}, opts...)
	r, err := NewResolver(args...)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return r, &litellmHits, &openrouterHits
}

func TestLookup_ChainPrefersLiteLLM(t *testing.T) {
	r, litellmHits, openrouterHits := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")
	p, err := r.Lookup(context.Background(), "claude-3-5-sonnet-20251101", 0)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.Source != SourceLiteLLM {
		t.Errorf("source = %v, want litellm", p.Source)
	}
	if p.InputCostPerMillion != 3.0 {
		t.Errorf("input = %v, want 3.0", p.InputCostPerMillion)
	}
	if atomic.LoadInt32(litellmHits) != 1 {
		t.Errorf("expected 1 litellm hit, got %d", atomic.LoadInt32(litellmHits))
	}
	if atomic.LoadInt32(openrouterHits) != 0 {
		t.Errorf("openrouter should not be called on litellm hit; got %d", atomic.LoadInt32(openrouterHits))
	}
}

func TestLookup_AppliesTierAt200k(t *testing.T) {
	r, _, _ := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")
	p, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 250_000)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.InputCostPerMillion != 6.0 {
		t.Errorf("input @ 250k = %v, want 6.0", p.InputCostPerMillion)
	}
	if p.OutputCostPerMillion != 30.0 {
		t.Errorf("output @ 250k = %v, want 30.0", p.OutputCostPerMillion)
	}
}

func TestLookup_CacheHitSkipsNetworkOnSecondCall(t *testing.T) {
	r, litellmHits, _ := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")
	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0); err != nil {
		t.Fatalf("Lookup #1: %v", err)
	}
	if _, err := r.Lookup(context.Background(), "gpt-4o", 0); err != nil {
		t.Fatalf("Lookup #2: %v", err)
	}
	if got := atomic.LoadInt32(litellmHits); got != 1 {
		t.Errorf("expected single network round-trip, got %d", got)
	}
}

func TestLookup_FallsThroughToOpenRouter(t *testing.T) {
	// LiteLLM fixture omits gemini-2.0-flash; OpenRouter has it.
	r, litellmHits, openrouterHits := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")
	p, err := r.Lookup(context.Background(), "google/gemini-2-0-flash", 0)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.Source != SourceOpenRouter {
		t.Errorf("source = %v, want openrouter", p.Source)
	}
	if diff := p.InputCostPerMillion - 0.10; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("input = %v, want ~0.10", p.InputCostPerMillion)
	}
	if atomic.LoadInt32(litellmHits) != 1 {
		t.Errorf("litellm should still be polled once, got %d", atomic.LoadInt32(litellmHits))
	}
	if atomic.LoadInt32(openrouterHits) != 1 {
		t.Errorf("openrouter should be polled once, got %d", atomic.LoadInt32(openrouterHits))
	}
}

func TestLookup_HardcodedFallbackWhenUpstreamsDown(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	cache := NewDiskCacheAt(t.TempDir())
	cache.SetTTL(time.Hour)
	r, err := NewResolver(
		WithCache(cache),
		WithLiteLLMFetcher(&LiteLLMFetcher{URL: failing.URL, Client: failing.Client(), Retries: 1, Backoff: 1}),
		WithOpenRouterFetcher(&OpenRouterFetcher{URL: failing.URL, Client: failing.Client(), Retries: 1, Backoff: 1}),
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	p, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.Source != SourceHardcoded {
		t.Errorf("source = %v, want hardcoded", p.Source)
	}
	if p.InputCostPerMillion <= 0 {
		t.Errorf("hardcoded fallback should have non-zero rate")
	}
}

func TestLookup_PopulatesDiskCache(t *testing.T) {
	r, _, _ := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")
	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	data, _, fresh, err := r.cache.Load(litellmCacheName)
	if err != nil {
		t.Fatalf("cache Load: %v", err)
	}
	if !fresh {
		t.Errorf("expected fresh cache entry")
	}
	if len(data) == 0 {
		t.Errorf("expected non-empty cache bytes")
	}
}

func TestEstimate(t *testing.T) {
	p := &Price{InputCostPerMillion: 3, OutputCostPerMillion: 15, CacheReadCostPerMillion: 0.3, CacheWriteCostPerMillion: 3.75}
	got := Estimate(p, 0, Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 500_000, CacheWriteTokens: 50_000})
	// expected: 3 + (100k * 15 / 1M = 1.5) + (500k * 0.3 / 1M = 0.15) + (50k * 3.75 / 1M = 0.1875)
	want := 3 + 1.5 + 0.15 + 0.1875
	if diff := got - want; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("Estimate = %v, want %v", got, want)
	}
	if Estimate(nil, 0, Usage{InputTokens: 1}) != 0 {
		t.Errorf("nil price should yield zero cost")
	}
}

func TestLookup_RepeatedQueriesAreCached(t *testing.T) {
	r, litellmHits, _ := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json")

	for i := 0; i < 100; i++ {
		if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet-20251101", 0); err != nil {
			t.Fatalf("Lookup #%d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(litellmHits); got != 1 {
		t.Errorf("repeated lookups should hit upstream once, got %d hits", got)
	}
}

func TestFuzzyIndexFor_RebuildsOnTableChange(t *testing.T) {
	r := &Resolver{}
	tableA := map[string]Price{"claude-3-5-sonnet-20241022": {}}
	idxA := r.fuzzyIndexFor(tableA, &r.liteLLMKeysCache)
	if idxA == nil || idxA.sourceLen != 1 {
		t.Fatalf("first index = %v", idxA)
	}
	idxA2 := r.fuzzyIndexFor(tableA, &r.liteLLMKeysCache)
	if idxA2 != idxA {
		t.Errorf("same table should reuse cached index")
	}
	tableB := map[string]Price{"a": {}, "b": {}}
	idxB := r.fuzzyIndexFor(tableB, &r.liteLLMKeysCache)
	if idxB == idxA {
		t.Errorf("different table should produce different index")
	}
	if idxB.sourceLen != 2 {
		t.Errorf("rebuilt index source len = %d, want 2", idxB.sourceLen)
	}
}

// fakeClock is a manually-advanced clock for TTL tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestLookup_InMemoryTableExpiresWithTTL(t *testing.T) {
	// A long-lived process (the telemetry daemon) must not latch the
	// pricing table in memory forever: once the TTL lapses the resolver
	// re-validates against disk/upstream instead of serving stale rates.
	clk := &fakeClock{t: time.Now()}
	r, litellmHits, _ := newTestResolver(t, "litellm_subset.json", "openrouter_subset.json", withClock(clk.Now))

	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0); err != nil {
		t.Fatalf("Lookup #1: %v", err)
	}
	if got := atomic.LoadInt32(litellmHits); got != 1 {
		t.Fatalf("first lookup: got %d upstream hits, want 1", got)
	}

	// Still inside the TTL: no new round-trip.
	clk.Advance(30 * time.Minute)
	if _, err := r.Lookup(context.Background(), "gpt-4o", 0); err != nil {
		t.Fatalf("Lookup #2: %v", err)
	}
	if got := atomic.LoadInt32(litellmHits); got != 1 {
		t.Errorf("within TTL: got %d upstream hits, want 1", got)
	}

	// Past the TTL: the resolver must go back out.
	clk.Advance(2 * time.Hour)
	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0); err != nil {
		t.Fatalf("Lookup #3: %v", err)
	}
	if got := atomic.LoadInt32(litellmHits); got != 2 {
		t.Errorf("after TTL: got %d upstream hits, want 2", got)
	}
}

func TestLookup_StaleFallbackBacksOffBeforeRetrying(t *testing.T) {
	// When upstream is down and we are serving a stale disk copy, the
	// resolver must not re-attempt the network on every single Lookup.
	var hits int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	cache := NewDiskCacheAt(t.TempDir())
	cache.SetTTL(time.Hour)
	fixture, err := os.ReadFile(filepath.Join("testdata", "litellm_subset.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := cache.Store(litellmCacheName, fixture); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	// Age the seeded copy past the TTL so it is stale on first read.
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(cache.Path(litellmCacheName), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	clk := &fakeClock{t: time.Now()}
	r, err := NewResolver(
		WithCache(cache),
		WithLiteLLMFetcher(&LiteLLMFetcher{URL: failing.URL, Client: failing.Client(), Retries: 1, Backoff: 1}),
		WithOpenRouterFetcher(&OpenRouterFetcher{URL: failing.URL, Client: failing.Client(), Retries: 1, Backoff: 1}),
		withClock(clk.Now),
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 0); err != nil {
		t.Fatalf("Lookup #1: %v", err)
	}
	first := atomic.LoadInt32(&hits)
	if first == 0 {
		t.Fatal("expected an upstream attempt on first lookup")
	}

	// Inside the backoff window: serve the stale copy, do not re-dial.
	clk.Advance(StaleRetryInterval / 2)
	if _, err := r.Lookup(context.Background(), "gpt-4o", 0); err != nil {
		t.Fatalf("Lookup #2: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != first {
		t.Errorf("within backoff: got %d upstream attempts, want %d", got, first)
	}

	// Past the backoff window: try again.
	clk.Advance(StaleRetryInterval * 2)
	if _, err := r.Lookup(context.Background(), "claude-3-5-sonnet", 1000); err != nil {
		t.Fatalf("Lookup #3: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got <= first {
		t.Errorf("after backoff: got %d upstream attempts, want more than %d", got, first)
	}
}
