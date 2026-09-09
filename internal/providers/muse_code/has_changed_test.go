package muse_code

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// uncredentialedAcct points sessions + config at temp dirs so HasCredential
// sees neither META_API_KEY nor a real auth.json.
func uncredentialedAcct(t *testing.T, sessionsDir string) core.AccountConfig {
	t.Helper()
	t.Setenv("META_API_KEY", "")
	t.Setenv("MUSE_AUTH_PATH", "")
	emptyCfg := t.TempDir()
	t.Setenv("MUSE_CONFIG_DIR", emptyCfg)
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	acct.SetHint(PathHintSessionsDirKey, sessionsDir)
	acct.SetHint(PathHintConfigDirKey, emptyCfg)
	return acct
}

func writeNestedSession(t *testing.T, root string) string {
	t.Helper()
	nested := filepath.Join(root, "2026", "09", "08", "sess-1")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(nested, "session.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Appends to a nested transcript must count as a change even though the
// sessions root mtime doesn't move — this is the stale-tile case.
func TestHasChanged_NestedSessionWrite(t *testing.T) {
	root := t.TempDir()
	p := writeNestedSession(t, root)
	acct := uncredentialedAcct(t, root)

	since := time.Now().Add(-time.Hour)
	if changed, _ := New().HasChanged(acct, since); !changed {
		t.Fatal("nested session write not detected")
	}

	// Touch the file back 2h so nothing changed since 1h ago.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// Root dir itself is fresh (just created) — pin it old too so only the
	// recursive scan can speak.
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatal(err)
	}
	if changed, _ := New().HasChanged(acct, time.Now().Add(-time.Hour)); changed {
		t.Fatal("reported change with no writes since `since`")
	}
}

func TestHasChanged_CredentialedAlwaysPolls(t *testing.T) {
	root := t.TempDir()
	writeNestedSession(t, root)
	acct := uncredentialedAcct(t, root)
	t.Setenv("META_API_KEY", "test-key")

	if changed, _ := New().HasChanged(acct, time.Now()); !changed {
		t.Fatal("credentialed account should always re-poll for quota drift")
	}
}

// Missing sessions dir keeps the pinned cross-provider contract (daemon
// change_detection_test.go): no paths to observe means no change.
func TestHasChanged_MissingDirsNoChange(t *testing.T) {
	acct := uncredentialedAcct(t, filepath.Join(t.TempDir(), "nope"))
	if changed, _ := New().HasChanged(acct, time.Now()); changed {
		t.Fatal("missing sessions dir should report no change")
	}
}
