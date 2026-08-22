package makora

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const ratesBody = `{"updated_at":"2026-08-22T15:13:11Z","models":[
	{"model":"deepseek-ai/DeepSeek-V4-Flash","pricing":{"prompt":"0.09","completion":"0.195","input_cache_read":"0.0196"}},
	{"model":"anon/model-x","pricing":{"prompt":"0.10","completion":"0.34","input_cache_read":"0.02"}}
]}`

const statusBody = `{"balance":13.32,"currency":"Credits","has_payment_method":true}`

const userBody = `{"pay_as_you_go_enabled":true,"spending_warning_notification":{"is_enabled":true,"amount":250.0},
	"monthly_prepaid_notification":{"is_enabled":true,"amount":400.0}}`

// usageBody: two models, one row with cached (input includes cached), one row
// without. Cached must be treated as a subset of input — not added on top.
const usageBody = `{"interval":"day","usage":[
	{"request_time":"2026-08-22T12:00:00Z","model":"deepseek-ai/DeepSeek-V4-Flash","request_count":10,
	 "input_tokens":1000000,"completion_tokens":200000,"total_tokens":1200000,"cached_tokens":400000},
	{"request_time":"2026-08-21T12:00:00Z","model":"anon/model-x","request_count":5,
	 "input_tokens":500000,"completion_tokens":100000,"total_tokens":600000,"cached_tokens":0}
]}`

// makoraTestServer fakes the app.makora.com data endpoints plus the
// be.prod.makora.com auth endpoints. authFails controls whether bearer
// requests return 401 until refreshLogin is called.
type makoraTestServer struct {
	server       *httptest.Server
	authFails    bool
	loginCalls   int
	refreshCalls int
	lastAuth     string
}

func newMakoraServer(t *testing.T, authFails bool) *makoraTestServer {
	ms := &makoraTestServer{authFails: authFails}
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == loginPath || r.URL.Path == "/v1/login/access-token":
			ms.loginCalls++
			ms.respondJSON(w, `{"access_token":"jwt-login","refresh_token":"rt-login"}`)
		case r.URL.Path == refreshPath:
			ms.refreshCalls++
			ms.respondJSON(w, `{"access_token":"jwt-refresh","refresh_token":"rt-refresh"}`)
		case r.URL.Path == ratesPath:
			ms.respondJSON(w, ratesBody)
		default:
			// Any authed data endpoint.
			if ms.authFails {
				if r.Header.Get("Authorization") != "Bearer jwt-refresh" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
			}
			ms.lastAuth = r.Header.Get("Authorization")
			switch r.URL.Path {
			case statusPath:
				ms.respondJSON(w, statusBody)
			case userPath:
				ms.respondJSON(w, userBody)
			default:
				ms.respondJSON(w, usageBody)
			}
		}
	}))
	t.Cleanup(ms.server.Close)
	return ms
}

func (ms *makoraTestServer) respondJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, body)
}

func (ms *makoraTestServer) acct() core.AccountConfig {
	return core.AccountConfig{
		ID:       "makora",
		Provider: "makora",
		BaseURL:  ms.server.URL,
	}
}

// loginAcct returns an account whose data AND login endpoints both target the
// fake server (the real login host, be.prod.makora.com, is separate).
func (ms *makoraTestServer) loginAcct() core.AccountConfig {
	acct := ms.acct()
	acct.SetHint("login_base_url", ms.server.URL)
	return acct
}

func providerForTest() *Provider { return New() }

func TestMakoraFetch_HappyPath_TokenCache(t *testing.T) {
	ms := newMakoraServer(t, false)
	p := providerForTest()

	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	writeTestState(t, state, "jwt-cache", "rt-cache")

	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	acct := ms.acct()
	acct.SetPath("state_file", state)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK (%s)", snap.Status, snap.Message)
	}
	if snap.Attributes["auth_type"] != "token_cache" {
		t.Errorf("auth_type = %q, want token_cache", snap.Attributes["auth_type"])
	}

	bal, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance metric")
	}
	if bal.Remaining == nil || *bal.Remaining != 13.32 {
		t.Errorf("credit_balance.Remaining = %v, want 13.32", bal.Remaining)
	}
	lim, ok := snap.Metrics["spend_limit"]
	if !ok || lim.Limit == nil || *lim.Limit != 400 {
		t.Errorf("spend_limit = %+v, want limit 400", lim)
	}
	// Gauge reference should be wired into the balance metric.
	if bal.Limit == nil || *bal.Limit != 400 {
		t.Errorf("credit_balance.Limit = %v, want 400 (spend-limit reference)", bal.Limit)
	}

	// Per-model rows.
	if len(snap.ModelUsage) != 2 {
		t.Fatalf("ModelUsage len = %d, want 2", len(snap.ModelUsage))
	}

	// Cached-as-subset arithmetic: non-cached input = input - cached.
	deepseek := findModel(t, snap, "deepseek-ai/DeepSeek-V4-Flash")
	// nonCached = 1,000,000 - 400,000 = 600,000
	// cost = 0.6*0.09 + 0.2*0.195 + 0.4*0.0196 = 0.054 + 0.039 + 0.00784 = 0.10084
	wantCost := 0.6*0.09 + 0.2*0.195 + 0.4*0.0196
	if diff := *deepseek.TotalTokens; diff != 1200000 {
		t.Errorf("deepseek total tokens = %v, want 1200000", diff)
	}
	if v := *deepseek.CachedTokens; v != 400000 {
		t.Errorf("deepseek cached = %v, want 400000", v)
	}
	if v := *deepseek.CostUSD; abs(v-wantCost) > 1e-9 {
		t.Errorf("deepseek cost = %v, want %v (cached NOT double-counted)", v, wantCost)
	}
	if v := *deepseek.InputTokens; v != 1000000 {
		t.Errorf("deepseek input = %v, want 1000000 (cached kept as subset)", v)
	}

	// Daily series present.
	if _, ok := snap.DailySeries["cost"]; !ok {
		t.Error("missing DailySeries['cost']")
	}
	if _, ok := snap.DailySeries["tokens"]; !ok {
		t.Error("missing DailySeries['tokens']")
	}
	if snap.Metrics["monthly_spend"].Used == nil {
		t.Error("missing monthly_spend.Used")
	}
}

func TestMakoraFetch_NoCredential_StatusAuth(t *testing.T) {
	ms := newMakoraServer(t, false)
	p := providerForTest()

	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")
	// Point the state path at a nonexistent file.
	acct := ms.acct()
	acct.SetPath("state_file", filepath.Join(t.TempDir(), "nope.json"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want AUTH_REQUIRED", snap.Status)
	}
	if snap.AccountID != "makora" || snap.ProviderID != "makora" {
		t.Errorf("unexpected snapshot ids: acct=%s prov=%s", snap.AccountID, snap.ProviderID)
	}
}

func TestMakoraFetch_401ThenRefresh_SingleRetry(t *testing.T) {
	ms := newMakoraServer(t, true) // 401 until Bearer jwt-refresh issued
	p := providerForTest()

	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	writeTestState(t, state, "jwt-expired", "rt-expired")
	acct := ms.loginAcct()
	acct.SetPath("state_file", state)

	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK (%s)", snap.Status, snap.Message)
	}
	if ms.refreshCalls != 1 {
		t.Errorf("refreshCalls = %d, want 1 (re-auth once, then retry)", ms.refreshCalls)
	}
	if ms.lastAuth != "Bearer jwt-refresh" {
		t.Errorf("final auth header = %q, want Bearer jwt-refresh", ms.lastAuth)
	}
}

func TestMakoraFetch_PasswordLoginFallback(t *testing.T) {
	ms := newMakoraServer(t, false)
	p := providerForTest()

	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "user@example.com")
	t.Setenv("MAKORA_PASSWORD", "sekret")

	// No state file and no session token => password login path.
	acct := ms.loginAcct()
	acct.SetPath("state_file", filepath.Join(t.TempDir(), "missing.json"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK (%s)", snap.Status, snap.Message)
	}
	if ms.loginCalls != 1 {
		t.Errorf("loginCalls = %d, want 1", ms.loginCalls)
	}
	if snap.Attributes["auth_type"] != "password" {
		t.Errorf("auth_type = %q, want password", snap.Attributes["auth_type"])
	}
	if ms.lastAuth != "Bearer jwt-login" {
		t.Errorf("final auth header = %q, want Bearer jwt-login", ms.lastAuth)
	}
}

func TestMakoraFetch_EnvSessionToken(t *testing.T) {
	ms := newMakoraServer(t, false)
	p := providerForTest()

	t.Setenv("MAKORA_SESSION_TOKEN", "jwt-env")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")
	acct := ms.acct()
	acct.SetPath("state_file", filepath.Join(t.TempDir(), "missing.json"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK (%s)", snap.Status, snap.Message)
	}
	if snap.Attributes["auth_type"] != "env_session" {
		t.Errorf("auth_type = %q, want env_session", snap.Attributes["auth_type"])
	}
}

func TestMakoraFetch_MissingTokenFile_IsNotHazard(t *testing.T) {
	// A nonexistent state file should simply fall through to auth-required
	// (or another credential path) rather than erroring.
	ms := newMakoraServer(t, false)
	p := providerForTest()

	t.Setenv("MAKORA_SESSION_TOKEN", "")
	t.Setenv("MAKORA_EMAIL", "")
	t.Setenv("MAKORA_PASSWORD", "")
	acct := ms.acct()
	acct.SetPath("state_file", filepath.Join(t.TempDir(), "definitely-missing.json"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err) // no credential path should NOT be a hard error
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want AUTH_REQUIRED", snap.Status)
	}
}

func TestMakoraParseUsage_CachedIsSubset_NoDoubleCount(t *testing.T) {
	rc, err := parseRateCard([]byte(ratesBody))
	if err != nil {
		t.Fatalf("parseRateCard: %v", err)
	}
	snap := core.NewUsageSnapshot("makora", "makora")

	if err := parseUsage(&snap, []byte(usageBody), "", rc); err != nil {
		t.Fatalf("parseUsage: %v", err)
	}

	deepseek := findModel(t, snap, "deepseek-ai/DeepSeek-V4-Flash")
	modelX := findModel(t, snap, "anon/model-x")

	// deepseek: input 1e6, cached 400k => nonCached 600k
	// cost = 0.6*0.09 + 0.2*0.195 + 0.4*0.0196
	wantDS := 0.6*0.09 + 0.2*0.195 + 0.4*0.0196
	if v := *deepseek.CostUSD; abs(v-wantDS) > 1e-9 {
		t.Errorf("deepseek cost = %v, want %v", v, wantDS)
	}
	if v := *deepseek.InputTokens; v != 1000000 {
		t.Errorf("deepseek input = %v, want 1000000 (cached is subset, not additive)", v)
	}

	// modelX: input 500k, cached 0 => cost = 0.5*0.10 + 0.1*0.34
	wantMX := 0.5*0.10 + 0.1*0.34
	if v := *modelX.CostUSD; abs(v-wantMX) > 1e-9 {
		t.Errorf("modelX cost = %v, want %v", v, wantMX)
	}

	// Total month spend = sum of both.
	total := *snap.Metrics["monthly_spend"].Used
	wantTotal := wantDS + wantMX
	if abs(total-wantTotal) > 1e-9 {
		t.Errorf("monthly_spend = %v, want %v", total, wantTotal)
	}

	// Daily series has two distinct days.
	costSeries := snap.DailySeries["cost"]
	if len(costSeries) != 2 {
		t.Errorf("DailySeries['cost'] len = %d, want 2", len(costSeries))
	}
	if costSeries[0].Date > costSeries[1].Date {
		t.Errorf("daily series not sorted: %s then %s", costSeries[0].Date, costSeries[1].Date)
	}
}

func TestMakoraParseUsage_UnknownModel_ConservZero(t *testing.T) {
	// A model missing from the rate card must not blow up; cost stays 0.
	rc, _ := parseRateCard([]byte(ratesBody))
	snap := core.NewUsageSnapshot("makora", "makora")
	body := `{"interval":"day","usage":[{"request_time":"2026-08-22T12:00:00Z","model":"totally/unknown",
		"request_count":1,"input_tokens":100,"completion_tokens":100,"total_tokens":200,"cached_tokens":0}]}`
	if err := parseUsage(&snap, []byte(body), "", rc); err != nil {
		t.Fatalf("parseUsage: %v", err)
	}
	rec := findModel(t, snap, "totally/unknown")
	if *rec.CostUSD != 0 {
		t.Errorf("unknown model cost = %v, want 0", *rec.CostUSD)
	}
}

func TestMakoraJWTPayloadEmail(t *testing.T) {
	// Real-format JWT header.payload.signature (payload only is read).
	header := base64URL("{\"alg\":\"HS256\"}")
	payload := base64URL(`{"sub":"abc","sub_info":{"email":"illusivejosiah@gmail.com","roles":[]},"aud":"a2labs:auth"}`)
	tok := header + "." + payload + ".sig"
	if got := jwtPayloadEmail(tok); got != "illusivejosiah@gmail.com" {
		t.Errorf("jwtPayloadEmail = %q", got)
	}
	if got := jwtPayloadEmail("not-a-jwt"); got != "" {
		t.Errorf("jwtPayloadEmail(malformed) = %q, want empty", got)
	}
	if got := jwtPayloadEmail(""); got != "" {
		t.Errorf("jwtPayloadEmail(empty) = %q, want empty", got)
	}
}

// --- helpers ---

func writeTestState(t *testing.T, path, access, refresh string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"exp":           time.Now().Add(24 * time.Hour).Unix(),
	})
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func findModel(t *testing.T, snap core.UsageSnapshot, id string) *core.ModelUsageRecord {
	t.Helper()
	for i := range snap.ModelUsage {
		if snap.ModelUsage[i].RawModelID == id {
			return &snap.ModelUsage[i]
		}
	}
	t.Fatalf("model %q not found in ModelUsage", id)
	return nil
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func base64URL(s string) string {
	e := base64.StdEncoding.EncodeToString([]byte(s))
	e = strings.TrimRight(e, "=")
	e = strings.ReplaceAll(e, "+", "-")
	e = strings.ReplaceAll(e, "/", "_")
	return e
}
