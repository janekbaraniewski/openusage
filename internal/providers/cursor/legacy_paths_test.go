package cursor

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestNormalizeLegacyPaths(t *testing.T) {
	acct := core.AccountConfig{
		Binary:  "/tmp/tracking.db",
		BaseURL: "/tmp/state.vscdb",
	}

	normalizeLegacyPaths(&acct)

	if got := acct.Path("tracking_db", ""); got != "/tmp/tracking.db" {
		t.Fatalf("tracking_db = %q, want /tmp/tracking.db", got)
	}
	if got := acct.Path("state_db", ""); got != "/tmp/state.vscdb" {
		t.Fatalf("state_db = %q, want /tmp/state.vscdb", got)
	}
}
