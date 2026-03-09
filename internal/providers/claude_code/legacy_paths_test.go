package claude_code

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestNormalizeLegacyPaths(t *testing.T) {
	acct := core.AccountConfig{
		Binary:  "/tmp/stats-cache.json",
		BaseURL: "/tmp/.claude.json",
	}

	normalizeLegacyPaths(&acct)

	if got := acct.Path("stats_cache", ""); got != "/tmp/stats-cache.json" {
		t.Fatalf("stats_cache = %q, want /tmp/stats-cache.json", got)
	}
	if got := acct.Path("account_config", ""); got != "/tmp/.claude.json" {
		t.Fatalf("account_config = %q, want /tmp/.claude.json", got)
	}
}
