package kimi_cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// setHome redirects the home directory for the test. defaultSessionsDir() and
// defaultConfigPath() resolve via os.UserHomeDir(), which reads %USERPROFILE%
// on Windows, not $HOME, so we must set both for tests to be portable.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

func TestResolveSessionsDir_DefaultAndOverride(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// No directory exists yet → empty result.
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	if got := resolveSessionsDir(acct); got != "" {
		t.Errorf("no dir: resolveSessionsDir = %q, want empty", got)
	}

	// Create the default location.
	defDir := filepath.Join(home, ".kimi", "sessions")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatalf("mkdir default: %v", err)
	}
	if got := resolveSessionsDir(acct); got != defDir {
		t.Errorf("default: resolveSessionsDir = %q, want %q", got, defDir)
	}

	// Override wins when present.
	override := t.TempDir()
	acct.SetPath(PathHintSessionsDirKey, override)
	if got := resolveSessionsDir(acct); got != override {
		t.Errorf("override: resolveSessionsDir = %q, want %q", got, override)
	}

	// Non-existent override falls back to default.
	acctMissing := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli"}
	acctMissing.SetPath(PathHintSessionsDirKey, filepath.Join(t.TempDir(), "nope"))
	if got := resolveSessionsDir(acctMissing); got != defDir {
		t.Errorf("missing override: resolveSessionsDir = %q, want %q (default)", got, defDir)
	}
}

func TestResolveConfigPath_DefaultAndOverride(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli"}
	if got := resolveConfigPath(acct); got != "" {
		t.Errorf("no file: resolveConfigPath = %q, want empty", got)
	}

	defFile := filepath.Join(home, ".kimi", "config.json")
	if err := os.MkdirAll(filepath.Dir(defFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defFile, []byte(`{"model":"kimi-for-coding"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := resolveConfigPath(acct); got != defFile {
		t.Errorf("default: resolveConfigPath = %q, want %q", got, defFile)
	}

	override := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(override, []byte(`{"model":"k2"}`), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	acct.SetPath(PathHintConfigPathKey, override)
	if got := resolveConfigPath(acct); got != override {
		t.Errorf("override: resolveConfigPath = %q, want %q", got, override)
	}
}

func TestResolveSessionsDir_KimiCodeFallback(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}

	// Only the Kimi Code CLI location exists → picked up as the default.
	kimiCodeDir := filepath.Join(home, ".kimi-code", "sessions")
	if err := os.MkdirAll(kimiCodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := resolveSessionsDir(acct); got != kimiCodeDir {
		t.Errorf("kimi-code fallback: resolveSessionsDir = %q, want %q", got, kimiCodeDir)
	}

	// The Python Kimi CLI location wins when both exist.
	kimiDir := filepath.Join(home, ".kimi", "sessions")
	if err := os.MkdirAll(kimiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := resolveSessionsDir(acct); got != kimiDir {
		t.Errorf("preference: resolveSessionsDir = %q, want %q", got, kimiDir)
	}
}
