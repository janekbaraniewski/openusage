package tmux

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// stubProvider implements core.UsageProvider + LocalSourceProvider for tests.
// It carries an ID and a list of paths so tests can fabricate provider
// snapshots without touching the real provider registry.
type stubProvider struct {
	id    string
	paths []string
}

func (s *stubProvider) ID() string                  { return s.id }
func (s *stubProvider) Describe() core.ProviderInfo { return core.ProviderInfo{} }
func (s *stubProvider) Spec() core.ProviderSpec     { return core.ProviderSpec{} }
func (s *stubProvider) DashboardWidget() core.DashboardWidget {
	return core.DashboardWidget{}
}
func (s *stubProvider) DetailWidget() core.DetailWidget { return core.DetailWidget{} }
func (s *stubProvider) Fetch(_ context.Context, _ core.AccountConfig) (core.UsageSnapshot, error) {
	return core.UsageSnapshot{}, nil
}
func (s *stubProvider) LocalSourcePaths() []string { return s.paths }

// makeStubFile writes an empty file with the given mtime. Returns the path.
func makeStubFile(t *testing.T, dir, name string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func TestDetectRecencyPicksFreshestProvider(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	freshPath := makeStubFile(t, dir, "claude.json", now.Add(-10*time.Minute))
	stalePath := makeStubFile(t, dir, "codex.json", now.Add(-1*time.Hour))

	provs := []core.UsageProvider{
		&stubProvider{id: "codex", paths: []string{stalePath}},
		&stubProvider{id: "claude_code", paths: []string{freshPath}},
	}

	res := Detect(DetectOptions{
		Strategy:  "recency",
		Now:       now,
		NoCache:   true,
		Providers: provs,
	})
	if res.Primary != "claude_code" {
		t.Fatalf("expected claude_code, got %q (source=%s, ordered=%v)", res.Primary, res.Source, res.Ordered)
	}
	if res.Source != "recency" {
		t.Fatalf("expected source=recency, got %q", res.Source)
	}
	if len(res.Ordered) != 2 || res.Ordered[0] != "claude_code" || res.Ordered[1] != "codex" {
		t.Fatalf("ordered list wrong: %v", res.Ordered)
	}
}

func TestDetectRecencySkipsStaleEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := makeStubFile(t, dir, "old.json", now.Add(-24*time.Hour))

	provs := []core.UsageProvider{
		&stubProvider{id: "claude_code", paths: []string{stale}},
	}

	res := Detect(DetectOptions{
		Strategy:      "recency",
		Now:           now,
		NoCache:       true,
		Providers:     provs,
		RecencyWindow: 4 * time.Hour,
	})
	if res.Primary != "" {
		t.Fatalf("expected no match for stale source, got %q", res.Primary)
	}
}

func TestDetectPriorityFallsThroughToFirstExisting(t *testing.T) {
	dir := t.TempDir()
	existing := makeStubFile(t, dir, "cursor.json", time.Now())

	provs := []core.UsageProvider{
		&stubProvider{id: "cursor", paths: []string{existing}},
		&stubProvider{id: "codex", paths: []string{"/no/such/codex"}},
	}

	res := Detect(DetectOptions{
		Strategy:      "priority",
		PriorityOrder: []string{"codex", "cursor", "claude_code"},
		NoCache:       true,
		Providers:     provs,
	})
	if res.Primary != "cursor" {
		t.Fatalf("expected cursor (codex doesn't exist), got %q", res.Primary)
	}
}

func TestDetectPinnedShortCircuits(t *testing.T) {
	res := Detect(DetectOptions{
		Pinned:  "claude_code",
		Now:     time.Now(),
		NoCache: true,
	})
	if res.Primary != "claude_code" || res.Source != "pinned" {
		t.Fatalf("pinned not honored: %+v", res)
	}
}

func TestDetectProcessMatchesSubstring(t *testing.T) {
	res := Detect(DetectOptions{
		Strategy: "process",
		NoCache:  true,
		Now:      time.Now(),
		ProcessLister: func() ([]string, error) {
			return []string{"/Applications/Cursor.app/cursor", "bash", "zsh"}, nil
		},
	})
	if res.Primary != "cursor" {
		t.Fatalf("expected process strategy to match cursor, got %q (ordered=%v)", res.Primary, res.Ordered)
	}
}

func TestDetectStrategyComposition(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := makeStubFile(t, dir, "old.json", now.Add(-24*time.Hour))
	existing := makeStubFile(t, dir, "cursor.json", now.Add(-30*time.Minute))

	provs := []core.UsageProvider{
		&stubProvider{id: "codex", paths: []string{"/no/such/codex"}},
		&stubProvider{id: "claude_code", paths: []string{stale}}, // exists but stale
		&stubProvider{id: "cursor", paths: []string{existing}},   // fresh
	}

	// recency,priority: recency picks the freshest provider.
	res := Detect(DetectOptions{
		Strategy:      "recency,priority",
		PriorityOrder: []string{"codex", "claude_code", "cursor"},
		Now:           now,
		NoCache:       true,
		Providers:     provs,
		RecencyWindow: 4 * time.Hour,
	})
	if res.Primary != "cursor" || res.Source != "recency" {
		t.Fatalf("expected cursor via recency, got %+v", res)
	}

	// Same providers but everything stale: priority kicks in.
	res = Detect(DetectOptions{
		Strategy:      "recency,priority",
		PriorityOrder: []string{"codex", "claude_code", "cursor"},
		Now:           now.Add(10 * 24 * time.Hour), // skip past recency window
		NoCache:       true,
		Providers:     provs,
	})
	// codex doesn't exist; claude_code stale file still exists; first match wins.
	if res.Primary != "claude_code" || res.Source != "priority" {
		t.Fatalf("expected claude_code via priority, got %+v", res)
	}
}

func TestDetectMultiOrdersByRecency(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := makeStubFile(t, dir, "a.json", now.Add(-30*time.Minute))
	b := makeStubFile(t, dir, "b.json", now.Add(-10*time.Minute))

	provs := []core.UsageProvider{
		&stubProvider{id: "claude_code", paths: []string{a}},
		&stubProvider{id: "cursor", paths: []string{b}},
	}
	res := Detect(DetectOptions{
		Strategy:  "multi",
		Now:       now,
		NoCache:   true,
		Providers: provs,
	})
	if res.Primary != "cursor" {
		t.Fatalf("expected cursor first (newer), got %q", res.Primary)
	}
	if len(res.Ordered) != 2 || res.Ordered[1] != "claude_code" {
		t.Fatalf("multi ordering wrong: %v", res.Ordered)
	}
}

func TestDetectCacheRoundTrip(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "active.json")
	now := time.Now()

	// First call writes the cache.
	res := Detect(DetectOptions{
		Pinned:    "claude_code",
		Now:       now,
		CachePath: cachePath,
	})
	if res.Primary != "claude_code" {
		t.Fatalf("first call: %+v", res)
	}

	// Cache was populated only if the strategy path wrote it. Pinned bypasses
	// cache writes by design (it is already O(0)); manually exercise the
	// cache helpers to verify the round-trip works.
	writeCache(cachePath, DetectResult{Primary: "codex", Ordered: []string{"codex"}, Source: "recency"}, now)
	cached, ok := readCache(cachePath, now)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if cached.Primary != "codex" {
		t.Fatalf("cache returned wrong primary: %q", cached.Primary)
	}

	// Past TTL the cache should miss.
	_, ok = readCache(cachePath, now.Add(10*time.Second))
	if ok {
		t.Fatalf("cache should have expired after TTL")
	}
}

func TestParseStrategiesIgnoresBlanks(t *testing.T) {
	got := parseStrategies(" recency , , priority ,")
	want := []string{"recency", "priority"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
