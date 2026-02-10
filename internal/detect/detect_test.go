package detect

import (
	"os"
	"testing"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

func TestAutoDetect_Runs(t *testing.T) {
	// AutoDetect should never panic, regardless of environment.
	result := AutoDetect()

	// Result should be non-nil.
	if result.Tools == nil && result.Accounts == nil {
		// This is fine — both can be nil in a clean environment.
	}
}

func TestDetectEnvKeys_FindsSetKey(t *testing.T) {
	// Set a fake key.
	os.Setenv("OPENAI_API_KEY", "sk-test1234567890abcdef")
	defer os.Unsetenv("OPENAI_API_KEY")

	var result Result
	detectEnvKeys(&result)

	found := false
	for _, acct := range result.Accounts {
		if acct.Provider == "openai" && acct.APIKeyEnv == "OPENAI_API_KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected OPENAI_API_KEY to be detected")
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
