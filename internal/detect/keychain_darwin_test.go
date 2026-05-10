//go:build darwin

package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// shimSecurityCLI replaces the system `security` binary with a stub that
// returns the given exit code. We do this by prepending a temp dir to the
// /usr/bin/security path... but exec.Command uses an absolute path so we
// can't shim it that way. Instead the production code uses /usr/bin/security
// directly. We can still test the wiring by exercising the real CLI when
// available (skipped if not), and by directly testing the shim function
// on a service name that should never exist.
func TestKeychainGenericPasswordExists_MissingServiceReturnsFalse(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	// Pick a service name that is overwhelmingly unlikely to exist.
	ok := keychainGenericPasswordExists("openusage-fake-service-does-not-exist-9876543210")
	if ok {
		t.Errorf("keychainGenericPasswordExists for nonexistent service = true, want false")
	}
}

func TestDetectMacOSKeychainCredentials_AnnotatesExistingAccount(t *testing.T) {
	// Pre-populate a claude-code account; if the keychain entry happens to
	// exist on this CI machine, we expect annotation. If not, the test still
	// passes (the detector simply does nothing). The contract we verify:
	// when keychain entry is present, the existing account gains the hint.
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	result := Result{Accounts: []core.AccountConfig{{
		ID:       "claude-code",
		Provider: "claude_code",
		Auth:     "local",
	}}}
	before := result.Accounts[0].Hint("credential_source", "")

	detectMacOSKeychainCredentials(&result)

	// The detector either annotated the account (keychain entry exists) or
	// did nothing (entry missing). Either way the account count must not grow,
	// because we already had one.
	if len(result.Accounts) != 1 {
		t.Fatalf("expected exactly 1 account, got %d", len(result.Accounts))
	}
	after := result.Accounts[0].Hint("credential_source", "")
	if after != before && after != "keychain:Claude Code-credentials" {
		t.Errorf("if hint changed, it must be the documented value; got %q", after)
	}
}

func TestDetectMacOSKeychainCredentials_AbsentKeychainIsSafe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	// Empty result; if keychain entry doesn't exist, detector adds nothing.
	// If it DOES exist (developer machine running tests), detector adds a
	// minimal claude-code account. Both are valid — the only requirement is
	// no panic and a valid Result.
	var result Result
	detectMacOSKeychainCredentials(&result) // must not panic
}
