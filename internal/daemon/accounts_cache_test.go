package daemon

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func newTestAccountsCache(t *testing.T) (*accountsCache, string, *atomic.Int32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	writeTestConfig(t, path, config.Config{Accounts: []core.AccountConfig{{ID: "a", Provider: "openai"}}}, time.Now().Add(-time.Hour))

	var resolves atomic.Int32
	c := &accountsCache{
		path: path,
		load: func() (config.Config, error) { return config.LoadFrom(path) },
		resolve: func(cfg *config.Config) []core.AccountConfig {
			resolves.Add(1)
			return append([]core.AccountConfig(nil), cfg.Accounts...)
		},
		maxAge: time.Hour,
		now:    time.Now,
	}
	return c, path, &resolves
}

func writeTestConfig(t *testing.T, path string, cfg config.Config, mtime time.Time) {
	t.Helper()
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func accountIDs(accts []core.AccountConfig) []string {
	out := make([]string, 0, len(accts))
	for _, a := range accts {
		out = append(out, a.ID)
	}
	return out
}

func TestAccountsCacheResolvesOnceUntilConfigChanges(t *testing.T) {
	c, path, resolves := newTestAccountsCache(t)
	ctx := context.Background()

	for range 5 {
		got, _, err := c.get(ctx)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if ids := accountIDs(got); len(ids) != 1 || ids[0] != "a" {
			t.Fatalf("accounts = %v, want [a]", ids)
		}
	}
	c.wait()
	if n := resolves.Load(); n != 1 {
		t.Fatalf("resolves = %d, want 1 while config is unchanged", n)
	}

	// Editing the settings file (new mtime) triggers a background re-resolve
	// and the next call sees the new account.
	writeTestConfig(t, path, config.Config{Accounts: []core.AccountConfig{
		{ID: "a", Provider: "openai"},
		{ID: "b", Provider: "anthropic"},
	}}, time.Now())
	if _, _, err := c.get(ctx); err != nil {
		t.Fatalf("get after edit: %v", err)
	}
	c.wait()
	got, _, err := c.get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ids := accountIDs(got); len(ids) != 2 {
		t.Fatalf("accounts after config change = %v, want [a b]", ids)
	}
	c.wait()
	if n := resolves.Load(); n != 2 {
		t.Fatalf("resolves = %d, want 2 after one config change", n)
	}
}

func TestAccountsCacheAppliesDashboardToggleWithoutResolve(t *testing.T) {
	c, path, resolves := newTestAccountsCache(t)
	ctx := context.Background()
	if _, _, err := c.get(ctx); err != nil {
		t.Fatalf("get: %v", err)
	}
	c.wait()

	writeTestConfig(t, path, config.Config{
		Accounts: []core.AccountConfig{{ID: "a", Provider: "openai"}},
		Dashboard: config.DashboardConfig{Providers: []config.DashboardProviderConfig{
			{AccountID: "a", Enabled: false},
		}},
	}, time.Now())
	got, _, err := c.get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("disabled account still returned: %v", accountIDs(got))
	}
	c.wait()
	if n := resolves.Load(); n != 2 {
		t.Fatalf("resolves = %d, want 2 (initial + background after edit)", n)
	}
}

func TestAccountsCacheRefreshesWhenStale(t *testing.T) {
	c, _, resolves := newTestAccountsCache(t)
	ctx := context.Background()
	now := time.Now()
	c.now = func() time.Time { return now }
	if _, _, err := c.get(ctx); err != nil {
		t.Fatalf("get: %v", err)
	}
	c.wait()
	now = now.Add(2 * time.Hour)
	if _, _, err := c.get(ctx); err != nil {
		t.Fatalf("get: %v", err)
	}
	c.wait()
	if n := resolves.Load(); n != 2 {
		t.Fatalf("resolves = %d, want 2 after maxAge elapsed", n)
	}
}

func TestReadModelBudgetHeader(t *testing.T) {
	cases := map[string]time.Duration{
		"":      maxReadModelBudget,
		"abc":   maxReadModelBudget,
		"-5":    maxReadModelBudget,
		"700":   700 * time.Millisecond,
		"60000": maxReadModelBudget,
	}
	for raw, want := range cases {
		r := httptest.NewRequest("POST", "/v1/read-model", nil)
		if raw != "" {
			r.Header.Set(readModelBudgetHeader, raw)
		}
		if got := readModelBudget(r); got != want {
			t.Errorf("budget(%q) = %s, want %s", raw, got, want)
		}
	}
}

func TestCloneAccountsIsolatesMaps(t *testing.T) {
	in := []core.AccountConfig{{ID: "a", RuntimeHints: map[string]string{"k": "v"}}}
	out := cloneAccounts(in)
	out[0].RuntimeHints["k"] = "changed"
	if in[0].RuntimeHints["k"] != "v" {
		t.Fatal("cloneAccounts shared the RuntimeHints map")
	}
}
