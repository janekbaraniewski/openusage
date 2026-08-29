package codex

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// TestMain installs a stub priceLookup so the existing codex tests remain
// deterministic and offline. Tests that want to exercise the resolver path
// override priceLookup locally and restore it via t.Cleanup.
func TestMain(m *testing.M) {
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return nil, errors.New("pricing disabled in tests")
	}
	fetchCodexRateLimitsRPC = func(_ context.Context, _ core.AccountConfig, _ string) (codexCLIRateLimitsResult, error) {
		return codexCLIRateLimitsResult{}, errors.New("cli rate limits disabled in tests")
	}
	os.Exit(m.Run())
}
