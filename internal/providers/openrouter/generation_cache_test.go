package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type generationFixture struct {
	mu          sync.Mutex
	gens        []map[string]any // newest first
	listCalls   atomic.Int32
	detailCalls atomic.Int32
	detailIDs   []string
}

func (f *generationFixture) prepend(rows ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gens = append(append([]map[string]any(nil), rows...), f.gens...)
}

func (f *generationFixture) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/key":
			_, _ = w.Write([]byte(`{"data":{"label":"test","usage":1.0,"is_free_tier":false}}`))
		case "/generation":
			q := r.URL.Query()
			if id := q.Get("id"); id != "" {
				f.detailCalls.Add(1)
				f.mu.Lock()
				f.detailIDs = append(f.detailIDs, id)
				f.mu.Unlock()
				_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"provider_responses":[{"provider_name":"Fireworks","status":200}]}}`, id)
				return
			}
			f.listCalls.Add(1)
			limit, _ := strconv.Atoi(q.Get("limit"))
			offset, _ := strconv.Atoi(q.Get("offset"))
			f.mu.Lock()
			end := offset + limit
			if end > len(f.gens) {
				end = len(f.gens)
			}
			var page []map[string]any
			if offset < len(f.gens) {
				page = f.gens[offset:end]
			}
			body, err := json.Marshal(map[string]any{"data": page})
			f.mu.Unlock()
			if err != nil {
				t.Errorf("marshal page: %v", err)
			}
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// fixtureGeneration builds a list row. Rows with needsDetail have no provider
// name, so the provider has to look up /generation?id= to resolve it.
func fixtureGeneration(id string, createdAt time.Time, needsDetail bool) map[string]any {
	row := map[string]any{
		"id":                id,
		"model":             "openai/gpt-4o",
		"total_cost":        0.01,
		"tokens_prompt":     10,
		"tokens_completion": 5,
		"created_at":        createdAt.UTC().Format(time.RFC3339),
	}
	if !needsDetail {
		row["provider_name"] = "OpenAI"
	}
	return row
}

func TestFetch_GenerationCacheAcrossPolls(t *testing.T) {
	now := time.Now().UTC()
	fx := &generationFixture{}
	for i := 0; i < 250; i++ {
		fx.gens = append(fx.gens, fixtureGeneration(fmt.Sprintf("gen-%03d", i), now.Add(-time.Duration(i)*time.Minute), i%50 == 0))
	}
	server := httptest.NewServer(fx.handler(t))
	defer server.Close()

	t.Setenv("TEST_OR_KEY_GEN_CACHE", "test-key")
	p := New()
	acct := core.AccountConfig{ID: "or-cache", Provider: "openrouter", APIKeyEnv: "TEST_OR_KEY_GEN_CACHE", BaseURL: server.URL}

	poll := func() core.UsageSnapshot {
		t.Helper()
		fx.listCalls.Store(0)
		fx.detailCalls.Store(0)
		snap, err := p.Fetch(context.Background(), acct)
		if err != nil {
			t.Fatalf("Fetch() error: %v", err)
		}
		return snap
	}

	// First poll: pages through everything (100+100+50) and looks up details
	// for the 5 rows without a provider.
	snap := poll()
	if got := fx.listCalls.Load(); got != 3 {
		t.Errorf("poll 1 list calls = %d, want 3", got)
	}
	if got := fx.detailCalls.Load(); got != 5 {
		t.Errorf("poll 1 detail calls = %d, want 5", got)
	}
	if snap.Raw["generations_fetched"] != "250" {
		t.Errorf("poll 1 generations_fetched = %q, want 250", snap.Raw["generations_fetched"])
	}

	// Second poll with no new generations: one page, stops at the last-seen
	// ID, and every known detail comes from the cache.
	snap = poll()
	if got := fx.listCalls.Load(); got != 1 {
		t.Errorf("poll 2 list calls = %d, want 1", got)
	}
	if got := fx.detailCalls.Load(); got != 0 {
		t.Errorf("poll 2 detail calls = %d, want 0", got)
	}
	if snap.Raw["generations_fetched"] != "250" {
		t.Errorf("poll 2 generations_fetched = %q, want 250", snap.Raw["generations_fetched"])
	}
	if _, ok := snap.Raw["generation_provider_detail_lookups"]; ok {
		t.Errorf("poll 2 reported network detail lookups: %q", snap.Raw["generation_provider_detail_lookups"])
	}

	// Third poll after two new generations (one needing a detail lookup):
	// still one page, and only the new ID is looked up.
	fx.prepend(
		fixtureGeneration("gen-new-b", now.Add(time.Minute), true),
		fixtureGeneration("gen-new-a", now.Add(30*time.Second), false),
	)
	fx.mu.Lock()
	fx.detailIDs = nil
	fx.mu.Unlock()
	snap = poll()
	if got := fx.listCalls.Load(); got != 1 {
		t.Errorf("poll 3 list calls = %d, want 1", got)
	}
	if got := fx.detailCalls.Load(); got != 1 {
		t.Errorf("poll 3 detail calls = %d, want 1", got)
	}
	fx.mu.Lock()
	if len(fx.detailIDs) != 1 || fx.detailIDs[0] != "gen-new-b" {
		t.Errorf("poll 3 detail IDs = %v, want [gen-new-b]", fx.detailIDs)
	}
	fx.mu.Unlock()
	if snap.Raw["generations_fetched"] != "252" {
		t.Errorf("poll 3 generations_fetched = %q, want 252", snap.Raw["generations_fetched"])
	}
	if reqs := snap.Metrics["today_requests"]; reqs.Used == nil || *reqs.Used <= 0 {
		t.Errorf("poll 3 today_requests = %v, want > 0", reqs.Used)
	}
}

func TestFetchAllGenerations_StopsAtLastSeenAcrossPages(t *testing.T) {
	now := time.Now().UTC()
	fx := &generationFixture{}
	for i := 0; i < 120; i++ {
		fx.gens = append(fx.gens, fixtureGeneration(fmt.Sprintf("old-%03d", i), now.Add(-time.Duration(i+200)*time.Minute), false))
	}
	server := httptest.NewServer(fx.handler(t))
	defer server.Close()

	p := New()
	first, err := p.fetchAllGenerations(context.Background(), server.URL, "k")
	if err != nil || len(first) != 120 {
		t.Fatalf("first fetch = %d rows, err %v; want 120", len(first), err)
	}

	// 150 new rows push the last-seen ID onto the second page.
	var fresh []map[string]any
	for i := 0; i < 150; i++ {
		fresh = append(fresh, fixtureGeneration(fmt.Sprintf("new-%03d", i), now.Add(-time.Duration(i)*time.Second), false))
	}
	fx.prepend(fresh...)
	fx.listCalls.Store(0)

	rows, err := p.fetchAllGenerations(context.Background(), server.URL, "k")
	if err != nil {
		t.Fatalf("second fetch error: %v", err)
	}
	if got := fx.listCalls.Load(); got != 2 {
		t.Errorf("list calls = %d, want 2 (stop on the page holding the last-seen ID)", got)
	}
	if len(rows) != 270 {
		t.Fatalf("rows = %d, want 270", len(rows))
	}
	if rows[0].ID != "new-000" || rows[149].ID != "new-149" || rows[150].ID != "old-000" || rows[269].ID != "old-119" {
		t.Errorf("unexpected merge order: %s %s %s %s", rows[0].ID, rows[149].ID, rows[150].ID, rows[269].ID)
	}
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if seen[r.ID] {
			t.Fatalf("duplicate row %s", r.ID)
		}
		seen[r.ID] = true
	}

	// A different key does not share the window.
	fx.listCalls.Store(0)
	if _, err := p.fetchAllGenerations(context.Background(), server.URL, "other"); err != nil {
		t.Fatalf("other key fetch error: %v", err)
	}
	if got := fx.listCalls.Load(); got != 3 {
		t.Errorf("other key list calls = %d, want 3", got)
	}
}

func TestGenerationCache_Bounded(t *testing.T) {
	c := newGenerationCache()
	for i := 0; i < maxCachedGenerationDetails+10; i++ {
		c.storeDetail(fmt.Sprintf("g%d", i), generationDetail{ProviderName: "x"})
	}
	if len(c.details) != maxCachedGenerationDetails || len(c.detailOrder) != maxCachedGenerationDetails {
		t.Errorf("details = %d/%d, want %d", len(c.details), len(c.detailOrder), maxCachedGenerationDetails)
	}
	if _, ok := c.detail("g0"); ok {
		t.Error("oldest detail should have been evicted")
	}
	for i := 0; i < maxCachedGenerationWindows+3; i++ {
		c.storeWindow(fmt.Sprintf("w%d", i), []generationEntry{{ID: "a"}})
	}
	if len(c.windows) != maxCachedGenerationWindows {
		t.Errorf("windows = %d, want %d", len(c.windows), maxCachedGenerationWindows)
	}
}

func TestGenerationCache_ConcurrentAccess(t *testing.T) {
	c := newGenerationCache()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("g%d", i)
				c.storeDetail(id, generationDetail{ProviderName: "x"})
				c.detail(id)
				key := fmt.Sprintf("w%d", w)
				c.storeWindow(key, []generationEntry{{ID: id}})
				rows := c.window(key)
				if len(rows) > 0 {
					rows[0].ID = "mutated"
				}
				if i%50 == 0 {
					c.dropWindow(key)
				}
			}
		}(w)
	}
	wg.Wait()
	for key := range c.windows {
		if rows := c.window(key); len(rows) > 0 && rows[0].ID == "mutated" {
			t.Fatalf("window %s leaked a caller mutation", key)
		}
	}
}
