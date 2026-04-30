package moonshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// On a series of polls where the balance only goes down, the gauge's Limit
// must stay pinned at the original peak — that's the whole point of the
// high-water-mark mechanism. Otherwise gauges would always show 0% used.
func TestPeak_PinsLimitAcrossSpend(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "moonshot-state.json")
	t.Setenv("TEST_MOONSHOT_KEY", "sk-test")
	t.Setenv("OPENUSAGE_MOONSHOT_STATE_PATH", statePath)

	// Ramp the server's "current balance" down across polls.
	balances := []float64{15.0, 14.5, 12.0, 10.0}
	step := 0
	server := httptest.NewServer(handlerStub(t, func() string {
		i := step
		if i >= len(balances) {
			i = len(balances) - 1
		}
		return balanceBody(balances[i], 5, balances[i]-5)
	}))
	defer server.Close()

	acct := core.AccountConfig{
		ID:        "moonshot-ai",
		Provider:  "moonshot",
		APIKeyEnv: "TEST_MOONSHOT_KEY",
		BaseURL:   server.URL,
	}

	for ; step < len(balances); step++ {
		snap, err := New().Fetch(context.Background(), acct)
		if err != nil {
			t.Fatalf("step %d Fetch error: %v", step, err)
		}
		bal := snap.Metrics["available_balance"]
		if bal.Limit == nil || *bal.Limit != 15.0 {
			t.Errorf("step %d Limit = %v, want 15 (peak should be pinned)", step, bal.Limit)
		}
		if bal.Remaining == nil || *bal.Remaining != balances[step] {
			t.Errorf("step %d Remaining = %v, want %v", step, bal.Remaining, balances[step])
		}
		wantUsed := 15.0 - balances[step]
		if bal.Used == nil || *bal.Used != wantUsed {
			t.Errorf("step %d Used = %v, want %v", step, bal.Used, wantUsed)
		}
	}
}

// A top-up bumps the peak. The gauge's Limit grows to match the new high.
func TestPeak_TopUpRaisesLimit(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "moonshot-state.json")
	t.Setenv("TEST_MOONSHOT_KEY", "sk-test")
	t.Setenv("OPENUSAGE_MOONSHOT_STATE_PATH", statePath)

	currentBalance := 15.0
	server := httptest.NewServer(handlerStub(t, func() string {
		return balanceBody(currentBalance, 5, currentBalance-5)
	}))
	defer server.Close()

	acct := core.AccountConfig{
		ID:        "moonshot-ai",
		Provider:  "moonshot",
		APIKeyEnv: "TEST_MOONSHOT_KEY",
		BaseURL:   server.URL,
	}

	// First poll: peak = 15
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatal(err)
	}
	if got := *snap.Metrics["available_balance"].Limit; got != 15 {
		t.Fatalf("initial Limit = %v, want 15", got)
	}

	// Spend down to 5, peak still 15
	currentBalance = 5.0
	snap, err = New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatal(err)
	}
	if got := *snap.Metrics["available_balance"].Limit; got != 15 {
		t.Fatalf("post-spend Limit = %v, want 15 (peak pinned)", got)
	}

	// Top-up to 50 — peak must follow.
	currentBalance = 50.0
	snap, err = New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatal(err)
	}
	if got := *snap.Metrics["available_balance"].Limit; got != 50 {
		t.Fatalf("post-topup Limit = %v, want 50", got)
	}
	if got := *snap.Metrics["available_balance"].Used; got != 0 {
		t.Errorf("post-topup Used = %v, want 0 (full balance)", got)
	}
}

// Per-account peaks are isolated — one account's top-up doesn't bleed into
// another account's gauge.
func TestPeak_PerAccountIsolation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "moonshot-state.json")
	t.Setenv("TEST_MOONSHOT_KEY", "sk-test")
	t.Setenv("OPENUSAGE_MOONSHOT_STATE_PATH", statePath)

	balances := map[string]float64{
		"moonshot-ai": 100,
		"moonshot-cn": 5,
	}
	currentAccount := ""
	server := httptest.NewServer(handlerStub(t, func() string {
		return balanceBody(balances[currentAccount], 0, balances[currentAccount])
	}))
	defer server.Close()

	for _, accountID := range []string{"moonshot-ai", "moonshot-cn"} {
		currentAccount = accountID
		acct := core.AccountConfig{
			ID:        accountID,
			Provider:  "moonshot",
			APIKeyEnv: "TEST_MOONSHOT_KEY",
			BaseURL:   server.URL,
		}
		snap, err := New().Fetch(context.Background(), acct)
		if err != nil {
			t.Fatalf("%s: %v", accountID, err)
		}
		want := balances[accountID]
		if got := *snap.Metrics["available_balance"].Limit; got != want {
			t.Errorf("%s peak Limit = %v, want %v", accountID, got, want)
		}
	}
}

// handlerStub returns a request handler that responds with userInfoBody for
// /v1/users/me and the result of bodyFn() for /v1/users/me/balance.
func handlerStub(t *testing.T, bodyFn func() string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case userInfoPath:
			_, _ = w.Write([]byte(userInfoBody()))
		case balancePath:
			_, _ = w.Write([]byte(bodyFn()))
		default:
			w.WriteHeader(404)
		}
	})
}
