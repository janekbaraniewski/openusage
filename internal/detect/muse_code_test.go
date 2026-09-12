package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectMuseCode_SessionsDir(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "muse", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Dir(filepath.Dir(sessions)))
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))

	var result Result
	detectMuseCode(&result)

	found := false
	for _, acct := range result.Accounts {
		if acct.Provider == "muse_code" {
			found = true
			if acct.Auth != "local" {
				t.Errorf("auth = %q, want local", acct.Auth)
			}
		}
	}
	if !found {
		t.Error("muse_code account not detected from sessions dir")
	}
}

func TestDetectMuseCode_Absent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "empty"))
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("MUSE_CONFIG_DIR", filepath.Join(t.TempDir(), "empty-config"))

	// Without the binary this must stay quiet; PATH is unpredictable across
	// machines so a present `muse` binary may still register an account.
	var result Result
	before := len(result.Accounts)
	detectMuseCode(&result)
	for _, acct := range result.Accounts[before:] {
		if acct.Provider == "muse_code" && findBinary("muse") == "" {
			t.Error("muse_code account detected with no binary, sessions, or auth")
		}
	}
}
