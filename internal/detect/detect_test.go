package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestAutoDetect_Runs(t *testing.T) {
	result := AutoDetect()

	if result.Tools == nil && result.Accounts == nil {
	}
}

func TestDetectEnvKeys_FindsSetKey(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test1234567890abcdef")
	defer os.Unsetenv("OPENAI_API_KEY")

	var result Result
	detectEnvKeys(&result)

	found := false
	for _, acct := range result.Accounts {
		if acct.Provider == "openai" && acct.APIKeyEnv == "OPENAI_API_KEY" && acct.ID == "openai" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected OPENAI_API_KEY to be detected")
	}
}

func TestDetectEnvKeys_FindsZenKeys(t *testing.T) {
	os.Setenv("ZEN_API_KEY", "zen-test-key-123456")
	defer os.Unsetenv("ZEN_API_KEY")

	var result Result
	detectEnvKeys(&result)

	found := false
	for _, acct := range result.Accounts {
		if acct.Provider == "opencode" && acct.APIKeyEnv == "ZEN_API_KEY" && acct.ID == "opencode" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected ZEN_API_KEY to be detected")
	}
}

func TestDetectEnvKeys_FindsOpenCodeKey(t *testing.T) {
	os.Setenv("OPENCODE_API_KEY", "opencode-test-key-123456")
	defer os.Unsetenv("OPENCODE_API_KEY")

	var result Result
	detectEnvKeys(&result)

	found := false
	for _, acct := range result.Accounts {
		if acct.Provider == "opencode" && acct.APIKeyEnv == "OPENCODE_API_KEY" && acct.ID == "opencode" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected OPENCODE_API_KEY to be detected")
	}
}

func TestDetectEnvKeys_SkipsEmpty(t *testing.T) {
	os.Unsetenv("OPENAI_API_KEY")

	var result Result
	detectEnvKeys(&result)

	for _, acct := range result.Accounts {
		if acct.Provider == "openai" {
			t.Error("Should not detect openai when OPENAI_API_KEY is not set")
		}
	}
}

func TestAddAccount_NoDuplicates(t *testing.T) {
	var result Result
	addAccount(&result, core.AccountConfig{ID: "test-1", Provider: "openai"})
	addAccount(&result, core.AccountConfig{ID: "test-1", Provider: "openai"})
	addAccount(&result, core.AccountConfig{ID: "test-2", Provider: "anthropic"})

	if len(result.Accounts) != 2 {
		t.Errorf("Expected 2 accounts, got %d", len(result.Accounts))
	}
}

func TestResultSummary(t *testing.T) {
	result := Result{
		Tools: []DetectedTool{
			{Name: "Test IDE", Type: "ide", BinaryPath: "/usr/bin/test"},
		},
	}
	summary := result.Summary()
	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestResultSummary_Empty(t *testing.T) {
	result := Result{}
	summary := result.Summary()
	if summary == "" {
		t.Error("Expected non-empty summary even when nothing detected")
	}
}

func TestFindBinary_UsesExtraDetectBinDirs(t *testing.T) {
	tmp := t.TempDir()
	name := "openusage-testbin"
	path := filepath.Join(tmp, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write temp executable: %v", err)
	}

	t.Setenv("PATH", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", tmp)

	got := findBinary(name)
	if got != path {
		t.Fatalf("findBinary() = %q, want %q", got, path)
	}
}

func TestFindBinary_SkipsNonExecutableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix execute bit semantics do not apply on windows")
	}

	tmp := t.TempDir()
	name := "openusage-testbin-noexec"
	path := filepath.Join(tmp, name)
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	t.Setenv("PATH", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", tmp)

	if got := findBinary(name); got != "" {
		t.Fatalf("findBinary() = %q, want empty for non-executable", got)
	}
}
