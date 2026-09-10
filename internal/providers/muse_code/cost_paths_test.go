package muse_code

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestResolveSessionsDirs_Override(t *testing.T) {
	root := t.TempDir()
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	acct.SetPath("sessions_dir", filepath.Join(root, "missing"))
	if dirs := resolveSessionsDirs(acct); len(dirs) != 0 {
		t.Errorf("missing override returned %v, want nil", dirs)
	}

	present := filepath.Join(root, "sessions")
	if err := os.MkdirAll(present, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	acct.SetPath("sessions_dir", present)
	dirs := resolveSessionsDirs(acct)
	if len(dirs) != 1 || dirs[0] != present {
		t.Errorf("dirs = %v, want [%s]", dirs, present)
	}
}

func TestResolveSessionsDirs_XDGDataHome(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "muse", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Dir(filepath.Dir(sessions)))
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	dirs := resolveSessionsDirs(acct)
	if len(dirs) != 1 || dirs[0] != sessions {
		t.Errorf("dirs = %v, want [%s]", dirs, sessions)
	}
}

func TestHasCredential(t *testing.T) {
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	t.Setenv("META_API_KEY", "")
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if HasCredential(acct) {
		t.Error("no credential signals, want false")
	}

	t.Setenv("META_API_KEY", "key")
	if !HasCredential(acct) {
		t.Error("META_API_KEY set, want true")
	}
	t.Setenv("META_API_KEY", "")

	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("MUSE_AUTH_PATH", authFile)
	if !HasCredential(acct) {
		t.Error("auth file present, want true")
	}
}

func TestEstimateEntryCost(t *testing.T) {
	stubPricing(t)
	ctx := context.Background()
	entry := museModelEntry{
		Model: "muse-spark-1.3", Input: 2000, Output: 500, Reasoning: 100,
		CacheRead: 8000,
	}
	// (2000 x 1.25 + 600 x 4.25 + 8000 x 0.15) / 1e6.
	cost, ok := estimateEntryCost(ctx, entry)
	if !ok {
		t.Fatal("priced model reported unpriced")
	}
	if cost < 0.006249 || cost > 0.006251 {
		t.Errorf("cost = %v, want 0.00625", cost)
	}

	if _, ok := estimateEntryCost(ctx, museModelEntry{Model: "muse-spark-9.9", Input: 1}); ok {
		t.Error("unknown model priced, want unpriced")
	}
}
