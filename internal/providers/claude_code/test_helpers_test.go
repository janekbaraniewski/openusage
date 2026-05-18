package claude_code

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// TestMain installs a stub priceLookup so the existing test suite remains
// deterministic and offline. Individual tests that exercise the resolver
// path override priceLookup locally and restore it via t.Cleanup.
func TestMain(m *testing.M) {
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return nil, errors.New("pricing disabled in tests")
	}
	os.Exit(m.Run())
}

func testClaudeAccount(id, statsPath, accountPath string) core.AccountConfig {
	return core.AccountConfig{
		ID:      id,
		Binary:  statsPath,
		BaseURL: accountPath,
	}
}

func testClaudeAccountWithDir(id, statsPath, accountPath, claudeDir string) core.AccountConfig {
	acct := testClaudeAccount(id, statsPath, accountPath)
	acct.RuntimeHints = map[string]string{"claude_dir": claudeDir}
	acct.SetHint("claude_dir", claudeDir)
	return acct
}
