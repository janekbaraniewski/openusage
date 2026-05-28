package qwen_cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestResolveProjectsDir_OverrideWins(t *testing.T) {
	dir := t.TempDir()
	acct := core.AccountConfig{}
	acct.SetPath(PathHintProjectsDirKey, dir)

	if got := resolveProjectsDir(acct); got != dir {
		t.Errorf("resolveProjectsDir = %q, want %q", got, dir)
	}
}

func TestResolveProjectsDir_OverrideMissingFallsThrough(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	acct := core.AccountConfig{}
	acct.SetPath(PathHintProjectsDirKey, missing)

	t.Setenv("HOME", t.TempDir())
	if got := resolveProjectsDir(acct); got != "" {
		t.Errorf("resolveProjectsDir = %q, want empty when override missing and no default", got)
	}
}

func TestResolveProjectsDir_DefaultUsedWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	def := filepath.Join(home, ".qwen", "projects")
	if err := os.MkdirAll(def, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if got := resolveProjectsDir(core.AccountConfig{}); got != def {
		t.Errorf("resolveProjectsDir = %q, want %q", got, def)
	}
}

func TestResolveProjectsDir_EmptyWhenNothingExists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := resolveProjectsDir(core.AccountConfig{}); got != "" {
		t.Errorf("resolveProjectsDir = %q, want empty", got)
	}
}
