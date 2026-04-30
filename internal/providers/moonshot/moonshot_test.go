package moonshot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// userInfoBody returns a realistic /v1/users/me response. Mirrors the live
// shape captured during API probing (org limits, tier, ids).
func userInfoBody() string {
	return `{
		"code": 0,
		"data": {
			"access_key": {"id": "ak-f9sm8hpz8yn111fsbam1"},
			"organization": {
				"id": "org-d75c68bd25b647828b1071f3aff4c229",
				"max_concurrency": 50,
				"max_request_per_minute": 200,
				"max_token_per_minute": 2000000,
				"max_token_quota": 75000000
			},
			"project": {"id": "proj-f38b7ffdd96c48f89696f2ad37c0c088"},
			"user": {"id": "u123", "user_state": "active"},
			"user_group_id": "enterprise-tier-1"
		},
		"scode": "0x0",
		"status": true
	}`
}

func balanceBody(available, voucher, cash float64) string {
	return fmt.Sprintf(`{
		"code": 0,
		"data": {
			"available_balance": %g,
			"voucher_balance": %g,
			"cash_balance": %g
		},
		"scode": "0x0",
		"status": true
	}`, available, voucher, cash)
}

// fakeMoonshot returns an httptest server that routes /v1/users/me and
// /v1/users/me/balance, with optional per-path overrides for status/body.
type fakeServerOpts struct {
	userInfoStatus int
	userInfoBody   string
	balanceStatus  int
	balanceBody    string
}

func startFake(t *testing.T, opts fakeServerOpts) *httptest.Server {
	t.Helper()
	if opts.userInfoStatus == 0 {
		opts.userInfoStatus = http.StatusOK
	}
	if opts.balanceStatus == 0 {
		opts.balanceStatus = http.StatusOK
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case userInfoPath:
			w.WriteHeader(opts.userInfoStatus)
			_, _ = w.Write([]byte(opts.userInfoBody))
		case balancePath:
			w.WriteHeader(opts.balanceStatus)
			_, _ = w.Write([]byte(opts.balanceBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setKey(t *testing.T, value string) {
	t.Helper()
	t.Setenv("TEST_MOONSHOT_KEY", value)
}

func newAcct(server, accountID string) core.AccountConfig {
	return core.AccountConfig{
		ID:        accountID,
		Provider:  "moonshot",
		APIKeyEnv: "TEST_MOONSHOT_KEY",
		BaseURL:   server,
	}
}

func TestFetch_Success_International(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoBody: userInfoBody(),
		balanceBody:  balanceBody(15, 5, 10),
	})
	defer server.Close()
	setKey(t, "sk-test")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if snap.Attributes["service_region"] != "international" {
		t.Errorf("service_region = %q, want international", snap.Attributes["service_region"])
	}
	if snap.Attributes["currency"] != "USD" {
		t.Errorf("currency = %q, want USD", snap.Attributes["currency"])
	}
	if snap.Attributes["account_tier"] != "enterprise-tier-1" {
		t.Errorf("account_tier = %q", snap.Attributes["account_tier"])
	}
	if snap.Attributes["org_id"] == "" {
		t.Error("missing org_id attribute")
	}
	if snap.Attributes["access_key_suffix"] != "bam1" {
		t.Errorf("access_key_suffix = %q, want bam1", snap.Attributes["access_key_suffix"])
	}

	avail, ok := snap.Metrics["available_balance"]
	if !ok || avail.Remaining == nil || *avail.Remaining != 15 {
		t.Errorf("available_balance = %+v, want Remaining=15", avail)
	}
	if avail.Unit != "USD" {
		t.Errorf("available_balance unit = %q, want USD", avail.Unit)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok || rpm.Limit == nil || *rpm.Limit != 200 {
		t.Errorf("rpm = %+v, want Limit=200", rpm)
	}
	tpm, ok := snap.Metrics["tpm"]
	if !ok || tpm.Limit == nil || *tpm.Limit != 2000000 {
		t.Errorf("tpm = %+v, want Limit=2000000", tpm)
	}
	if tq, ok := snap.Metrics["total_token_quota"]; !ok || tq.Limit == nil || *tq.Limit != 75000000 {
		t.Errorf("total_token_quota = %+v, want Limit=75000000", tq)
	}
}

func TestFetch_Success_China(t *testing.T) {
	// Real .cn would respond at api.moonshot.cn, but we fake it locally and rely on
	// the BaseURL string for region/currency classification.
	server := startFake(t, fakeServerOpts{
		userInfoBody: userInfoBody(),
		balanceBody:  balanceBody(100, 0, 100),
	})
	defer server.Close()
	setKey(t, "sk-test")

	// Trick classifyService by tagging the override URL with the .cn marker via a
	// path suffix won't work — classifyService inspects the host. Instead, verify
	// the classification function directly.
	if region, currency := classifyService("https://api.moonshot.cn"); region != "china" || currency != "CNY" {
		t.Fatalf("classifyService(.cn) = %s/%s, want china/CNY", region, currency)
	}
	if region, currency := classifyService("https://api.moonshot.ai"); region != "international" || currency != "USD" {
		t.Fatalf("classifyService(.ai) = %s/%s, want international/USD", region, currency)
	}

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-cn"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	// Local fake URL won't match .cn; the test server URL classifies as "international"
	// by design — the classification helper test above covers the .cn path.
	if snap.Attributes["currency"] != "USD" {
		t.Errorf("currency on fake server = %q, want USD", snap.Attributes["currency"])
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s, want OK", snap.Status)
	}
}

func TestFetch_AuthRequired_NoKey(t *testing.T) {
	os.Unsetenv("TEST_MOONSHOT_MISSING")
	acct := core.AccountConfig{
		ID:        "moonshot-ai",
		Provider:  "moonshot",
		APIKeyEnv: "TEST_MOONSHOT_MISSING",
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthRequired_401(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoStatus: http.StatusUnauthorized,
		userInfoBody:   `{"error":{"message":"Invalid Authentication","type":"invalid_authentication_error"}}`,
		balanceStatus:  http.StatusUnauthorized,
		balanceBody:    `{"error":{"message":"Invalid Authentication"}}`,
	})
	defer server.Close()
	setKey(t, "sk-bad")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
	if !strings.Contains(snap.Message, "401") {
		t.Errorf("message = %q, expected to mention 401", snap.Message)
	}
}

func TestFetch_RateLimited_429(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoStatus: http.StatusTooManyRequests,
		userInfoBody:   `{"error":{"message":"rate limited"}}`,
		balanceStatus:  http.StatusTooManyRequests,
		balanceBody:    `{"error":{"message":"rate limited"}}`,
	})
	defer server.Close()
	setKey(t, "sk-test")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("status = %s, want LIMITED", snap.Status)
	}
}

func TestFetch_BalancePartialFailure(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoBody:  userInfoBody(),
		balanceStatus: http.StatusInternalServerError,
		balanceBody:   `{"error":{"message":"db down"}}`,
	})
	defer server.Close()
	setKey(t, "sk-test")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	// User-info data must still be populated.
	if _, ok := snap.Metrics["rpm"]; !ok {
		t.Error("rpm should be populated even when balance fails")
	}
	// Balance metrics must NOT be present.
	if _, ok := snap.Metrics["available_balance"]; ok {
		t.Error("available_balance should be absent when balance call failed")
	}
	// Diagnostic about balance failure must be raw.
	if snap.Raw["balance_error"] == "" {
		t.Error("balance_error raw note expected")
	}
}

func TestFetch_BalanceZero_PromotesToLimited(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoBody: userInfoBody(),
		balanceBody:  balanceBody(0, 0, 0),
	})
	defer server.Close()
	setKey(t, "sk-test")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("status = %s, want LIMITED on zero balance", snap.Status)
	}
}

func TestFetch_MalformedBalanceJSON(t *testing.T) {
	server := startFake(t, fakeServerOpts{
		userInfoBody: userInfoBody(),
		balanceBody:  `{not-json`,
	})
	defer server.Close()
	setKey(t, "sk-test")

	snap, err := New().Fetch(context.Background(), newAcct(server.URL, "moonshot-ai"))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Raw["balance_error"] == "" {
		t.Error("expected balance_error raw note for malformed JSON")
	}
	// User-info metrics still present.
	if _, ok := snap.Metrics["rpm"]; !ok {
		t.Error("rpm absent — user-info must succeed independently")
	}
}

func TestClassifyService(t *testing.T) {
	cases := []struct {
		url      string
		region   string
		currency string
	}{
		{"https://api.moonshot.ai", "international", "USD"},
		{"https://api.moonshot.cn", "china", "CNY"},
		{"https://api.moonshot.cn/v1", "china", "CNY"},
		{"https://example.com", "international", "USD"},
	}
	for _, tc := range cases {
		region, currency := classifyService(tc.url)
		if region != tc.region || currency != tc.currency {
			t.Errorf("classifyService(%q) = %s/%s, want %s/%s", tc.url, region, currency, tc.region, tc.currency)
		}
	}
}

func TestLastN(t *testing.T) {
	if got := lastN("ak-f9sm8hpz8yn111fsbam1", 4); got != "bam1" {
		t.Errorf("lastN = %q, want bam1", got)
	}
	if got := lastN("abc", 5); got != "abc" {
		t.Errorf("lastN short string = %q, want abc", got)
	}
}
