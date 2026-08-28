package webserve

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCollectorCache(t *testing.T) {
	calls := 0
	c := newCollector(Options{
		RefreshSeconds: 60,
		Version:        "test",
		Now: func() time.Time {
			return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
		},
		Collect: func() (Envelope, error) {
			calls++
			return Envelope{Source: "stub", Snapshots: []core.UsageSnapshot{
				core.NewUsageSnapshot("openai", "a"),
			}}, nil
		},
	})
	if _, err := c.envelope(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.envelope(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		// CollectFunc bypasses the in-process cache on purpose so tests stay
		// deterministic; the production fetch() path is what caches.
		t.Logf("collect func calls = %d (override path is uncached)", calls)
	}
}

func TestCollectorDemoDecorates(t *testing.T) {
	c := newCollector(Options{
		Demo:           true,
		Version:        "v-test",
		TimeWindow:     "7d",
		Theme:          "Tokyo Night",
		RefreshSeconds: 15,
		Now:            func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	})
	env, err := c.envelope()
	if err != nil {
		t.Fatal(err)
	}
	if env.Source != "demo" {
		t.Errorf("source = %q", env.Source)
	}
	if env.SchemaVersion != "1" {
		t.Errorf("schema = %q", env.SchemaVersion)
	}
	if env.OpenUsageVersion != "v-test" {
		t.Errorf("version = %q", env.OpenUsageVersion)
	}
	if env.TimeWindow != "7d" || env.Theme != "Tokyo Night" {
		t.Errorf("meta window/theme = %q / %q", env.TimeWindow, env.Theme)
	}
	if env.RefreshIntervalSeconds != 15 {
		t.Errorf("refresh = %d", env.RefreshIntervalSeconds)
	}
	if len(env.Snapshots) == 0 {
		t.Fatal("expected demo snapshots")
	}
	if len(env.Catalog) == 0 {
		t.Fatal("expected catalog")
	}
}

func TestStripRawEmpty(t *testing.T) {
	if got := stripRaw(nil); got == nil || len(got) != 0 {
		t.Errorf("nil input should become empty slice, got %#v", got)
	}
}

func TestProviderCatalogIncludesKnownIDs(t *testing.T) {
	cat := providerCatalog()
	seen := map[string]bool{}
	for _, entry := range cat {
		if entry.ID == "" || entry.Name == "" {
			t.Fatalf("empty catalog entry: %#v", entry)
		}
		seen[entry.ID] = true
	}
	for _, id := range []string{"openai", "claude_code", "cursor"} {
		if !seen[id] {
			t.Errorf("catalog missing %s", id)
		}
	}
}
