package gemini_cli

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// TestMain installs a stub priceLookup so the existing gemini_cli tests
// remain deterministic and offline. Tests that want to exercise the
// resolver path override priceLookup locally and restore it via t.Cleanup.
func TestMain(m *testing.M) {
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return nil, errors.New("pricing disabled in tests")
	}
	os.Exit(m.Run())
}

func testGeminiCLIAccount(id, configDir string) core.AccountConfig {
	acct := core.AccountConfig{
		ID:           id,
		Provider:     "gemini_cli",
		RuntimeHints: map[string]string{"config_dir": configDir},
	}
	acct.SetHint("config_dir", configDir)
	return acct
}
