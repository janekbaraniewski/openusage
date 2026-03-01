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

func TestDetectEnvKeys_FindsZAIKeys(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "zai-test-key-123456")
	t.Setenv("ZHIPUAI_API_KEY", "zhipuai-test-key-123456")

	var result Result
	detectEnvKeys(&result)

	foundZAI := false
	foundZhipu := false
	for _, acct := range result.Accounts {
		if acct.Provider != "zai" {
			continue
		}
		if acct.ID == "zai" && acct.APIKeyEnv == "ZAI_API_KEY" {
			foundZAI = true
		}
		if acct.ID == "zhipuai-auto" && acct.APIKeyEnv == "ZHIPUAI_API_KEY" {
			foundZhipu = true
		}
	}
	if !foundZAI {
		t.Fatal("expected ZAI_API_KEY mapping to zai")
	}
	if !foundZhipu {
		t.Fatal("expected ZHIPUAI_API_KEY mapping to zhipuai-auto")
	}
}

func TestProviderForStoredCredential_ZAI(t *testing.T) {
	if got := providerForStoredCredential("zai"); got != "zai" {
		t.Fatalf("providerForStoredCredential(zai) = %q, want zai", got)
	}
}

func TestDetectZAICodingHelper_Config(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".chelper")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	content := `lang: en_US
plan: glm_coding_plan_china
api_key: test-zai-token
`
	if err := os.WriteFile(configFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectZAICodingHelper(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}

	acct := result.Accounts[0]
	if acct.ID != "zai-coding-plan-auto" {
		t.Fatalf("account ID = %q, want zai-coding-plan-auto", acct.ID)
	}
	if acct.Provider != "zai" {
		t.Fatalf("provider = %q, want zai", acct.Provider)
	}
	if acct.Token != "test-zai-token" {
		t.Fatalf("token = %q, want test-zai-token", acct.Token)
	}
	if acct.ExtraData == nil || acct.ExtraData["plan_type"] != "glm_coding_plan_china" {
		t.Fatalf("plan_type = %q, want glm_coding_plan_china", acct.ExtraData["plan_type"])
	}
	if acct.ExtraData["source"] != "chelper" {
		t.Fatalf("source = %q, want chelper", acct.ExtraData["source"])
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

// writeExe creates an executable shell script at dir/name with the given body.
func writeExe(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}

func TestDetectGHCopilot_StandaloneBinaryDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	// Create a standalone "copilot" binary (no "gh" in this dir).
	copilotBin := writeExe(t, tmp, "copilot", "exit 0")

	// Create ~/.copilot/ directory to confirm the CLI has been used.
	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", copilotDir, err)
	}

	// Restrict PATH to only the temp dir. Note: findBinary also searches
	// hardcoded system dirs (e.g. /opt/homebrew/bin), so gh may still be
	// found on machines where it is installed. The key assertion is that the
	// standalone copilot path ends up in ExtraData regardless.
	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "GitHub Copilot CLI" {
		t.Errorf("tool name = %q, want %q", result.Tools[0].Name, "GitHub Copilot CLI")
	}

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}

	acct := result.Accounts[0]
	if acct.ID != "copilot" {
		t.Errorf("account ID = %q, want %q", acct.ID, "copilot")
	}
	if acct.Provider != "copilot" {
		t.Errorf("account Provider = %q, want %q", acct.Provider, "copilot")
	}
	if acct.Auth != "cli" {
		t.Errorf("account Auth = %q, want %q", acct.Auth, "cli")
	}
	if acct.ExtraData == nil {
		t.Fatal("account ExtraData is nil")
	}
	if acct.ExtraData["copilot_binary"] != copilotBin {
		t.Errorf("ExtraData[copilot_binary] = %q, want %q", acct.ExtraData["copilot_binary"], copilotBin)
	}
	if acct.ExtraData["config_dir"] != copilotDir {
		t.Errorf("ExtraData[config_dir] = %q, want %q", acct.ExtraData["config_dir"], copilotDir)
	}
}

func TestDetectGHCopilot_StandaloneBinaryNoGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	// Check if gh exists in hardcoded system dirs. If it does, we cannot
	// fully isolate the "no gh" scenario without refactoring findBinary,
	// so skip this test on machines with gh installed.
	if findBinary("gh") != "" {
		t.Skip("gh binary found on system; cannot test no-gh fallback path")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	copilotBin := writeExe(t, tmp, "copilot", "exit 0")

	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", copilotDir, err)
	}

	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}

	acct := result.Accounts[0]
	// With no gh binary at all, Binary should be the standalone copilot path.
	if acct.Binary != copilotBin {
		t.Errorf("account Binary = %q, want copilot path %q (no gh available)", acct.Binary, copilotBin)
	}
}

func TestDetectGHCopilot_GHCopilotTakesPrecedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	// Create a fake gh binary that succeeds for "copilot --version".
	ghBin := writeExe(t, tmp, "gh", `exit 0`)

	// Also create a standalone copilot binary.
	writeExe(t, tmp, "copilot", "exit 0")

	// Create ~/.copilot/ directory.
	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", copilotDir, err)
	}

	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	// gh copilot path should be used, not standalone.
	if result.Tools[0].Name != "GitHub Copilot (gh CLI)" {
		t.Errorf("tool name = %q, want %q", result.Tools[0].Name, "GitHub Copilot (gh CLI)")
	}

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}

	acct := result.Accounts[0]
	if acct.Binary != ghBin {
		t.Errorf("account Binary = %q, want gh path %q", acct.Binary, ghBin)
	}
	// gh copilot path should NOT have ExtraData (legacy behavior).
	if acct.ExtraData != nil {
		t.Errorf("account ExtraData should be nil for gh copilot path, got %v", acct.ExtraData)
	}
}

func TestDetectGHCopilot_StandaloneBinaryWithGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	// Create a gh binary that FAILS for "copilot --version" (extension not installed).
	ghBin := writeExe(t, tmp, "gh", `exit 1`)

	// Create a standalone copilot binary.
	copilotBin := writeExe(t, tmp, "copilot", "exit 0")

	// Create ~/.copilot/ directory.
	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", copilotDir, err)
	}

	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(result.Accounts))
	}

	acct := result.Accounts[0]
	// gh is available but copilot extension is not, so Binary should be gh
	// (the provider uses gh api for quota calls).
	if acct.Binary != ghBin {
		t.Errorf("account Binary = %q, want gh path %q (gh available for api calls)", acct.Binary, ghBin)
	}
	if acct.ExtraData["copilot_binary"] != copilotBin {
		t.Errorf("ExtraData[copilot_binary] = %q, want %q", acct.ExtraData["copilot_binary"], copilotBin)
	}
}

func TestDetectGHCopilot_SkipsWithoutCopilotDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	// Standalone copilot binary exists, but no ~/.copilot/ directory.
	writeExe(t, tmp, "copilot", "exit 0")

	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Tools) != 0 {
		t.Errorf("expected 0 tools when ~/.copilot/ missing, got %d", len(result.Tools))
	}
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts when ~/.copilot/ missing, got %d", len(result.Accounts))
	}
}

func TestDetectGHCopilot_SkipsWhenNoBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	home := t.TempDir()

	// Empty PATH, no binaries at all.
	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")

	var result Result
	detectGHCopilot(&result)

	if len(result.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(result.Tools))
	}
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(result.Accounts))
	}
}
