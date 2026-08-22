package makora

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// errAuthFailed signals that authentication genuinely failed after any
// available retry path (401/403 with no working refresh or one that also
// failed). Callers promote the snapshot to StatusAuth.
var errAuthFailed = errors.New("authentication failed")

// session holds the bearer access token plus an optional refresh token.
// The refresh token is only populated when a password login or the shared
// token cache supplied one.
type session struct {
	accessToken  string
	refreshToken string
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	st, err := p.resolveSession(ctx, acct, &snap)
	if err != nil {
		return snap, err
	}
	if st.accessToken == "" {
		// resolveSession already set a StatusAuth snapshot.
		return snap, nil
	}

	if err := p.fetchStatus(ctx, baseURL, st, acct, &snap); err != nil {
		if errors.Is(err, errAuthFailed) {
			snap.Status = core.StatusAuth
			snap.Message = "Makora authentication failed (check MAKORA_SESSION_TOKEN, MAKORA_EMAIL+MAKORA_PASSWORD, or the ~/.config/makora-usage/state.json cache)"
			return snap, nil
		}
		snap.Raw["balance_error"] = err.Error()
	}

	if err := p.fetchUser(ctx, baseURL, st, acct, &snap); err != nil {
		snap.Raw["user_error"] = err.Error()
	}

	rc, err := p.fetchRates(ctx, baseURL, &snap)
	if err != nil {
		snap.Raw["rates_error"] = err.Error()
	}

	// Month-to-date usage window: the last 30 days, inclusive of today.
	end := time.Now().UTC().Format(time.RFC3339)
	start := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	if err := p.fetchUsage(ctx, baseURL, st, acct, start, end, rc, &snap); err != nil {
		snap.Raw["usage_error"] = err.Error()
	}

	finalizeStatus(&snap)
	return snap, nil
}

// resolveSession establishes the bearer token in precedence order:
//  1. MAKORA_SESSION_TOKEN env var
//  2. ~/.config/makora-usage/state.json token cache (read-only)
//  3. MAKORA_EMAIL + MAKORA_PASSWORD env vars → password login
//
// On a successful password login the cache file is written (0600), since this
// flow itself performed the login. If no credential is available it returns an
// empty session after setting a StatusAuth snapshot.
func (p *Provider) resolveSession(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) (*session, error) {
	if tok := os.Getenv("MAKORA_SESSION_TOKEN"); tok != "" {
		snap.SetAttribute("auth_type", "env_session")
		return &session{accessToken: tok}, nil
	}

	if st, ok := p.loadStateFile(acct); ok {
		snap.SetAttribute("auth_type", "token_cache")
		return st, nil
	}

	email := os.Getenv("MAKORA_EMAIL")
	password := os.Getenv("MAKORA_PASSWORD")
	if email != "" && password != "" {
		st, err := p.passwordLogin(ctx, acct, email, password)
		if err != nil {
			snap.SetAttribute("auth_type", "password")
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("Makora password login failed: %v", err)
			return &session{}, nil
		}
		snap.SetAttribute("auth_type", "password")
		p.writeStateFile(acct, *st)
		return st, nil
	}

	snap.Status = core.StatusAuth
	snap.Message = "no Makora session token (set MAKORA_SESSION_TOKEN, MAKORA_EMAIL+MAKORA_PASSWORD, or a ~/.config/makora-usage/state.json cache)"
	return &session{}, nil
}

func (p *Provider) fetchStatus(ctx context.Context, baseURL string, st *session, acct core.AccountConfig, snap *core.UsageSnapshot) error {
	body, err := p.authedGET(ctx, baseURL+statusPath, st, acct)
	if err != nil {
		return err
	}
	resp, err := parseCreditsStatus(body)
	if err != nil {
		return err
	}
	currency := resp.Currency
	if currency == "" {
		currency = "Credits"
	}
	snap.SetAttribute("currency", currency)
	if resp.HasPaymentMethod {
		snap.SetAttribute("has_payment_method", "true")
	}
	snap.Metrics["credit_balance"] = core.Metric{
		Remaining: core.Float64Ptr(resp.Balance),
		Unit:      currency,
		Window:    "current",
	}
	return nil
}

func (p *Provider) fetchUser(ctx context.Context, baseURL string, st *session, acct core.AccountConfig, snap *core.UsageSnapshot) error {
	body, err := p.authedGET(ctx, baseURL+userPath, st, acct)
	if err != nil {
		if errors.Is(err, errAuthFailed) {
			return err
		}
		return err
	}
	resp, err := parseUserCredits(body)
	if err != nil {
		return err
	}
	if resp.PayAsYouGoEnabled {
		snap.SetAttribute("pay_as_you_go_enabled", "true")
	}
	if resp.MonthlyPrepaid.Amount > 0 {
		limit := resp.MonthlyPrepaid.Amount
		snap.Metrics["spend_limit"] = core.Metric{
			Limit:  core.Float64Ptr(limit),
			Unit:   "Credits",
			Window: "month",
		}
		snap.SetAttribute("spend_limit", fmt.Sprintf("%.0f", limit))
	}
	return nil
}

// fetchRates loads the public per-1M-token price card. It requires NO auth —
// a plain GET — and is cached in memory for the run via the returned card.
// On failure an empty card is returned so usage parsing still produces rows
// (with conservative zero cost) rather than aborting the whole fetch.
func (p *Provider) fetchRates(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (rateCard, error) {
	rc := rateCard{prices: make(map[string]pricing)}
	status, body, err := p.doGET(ctx, baseURL+ratesPath, "")
	if err != nil {
		return rc, err
	}
	if status != http.StatusOK {
		return rc, fmt.Errorf("rate card HTTP %d", status)
	}
	parsed, err := parseRateCard(body)
	if err != nil {
		return rc, err
	}
	if !parsed.UpdatedAt.IsZero() {
		snap.SetAttribute("rate_card_updated_at", parsed.UpdatedAt.Format(time.RFC3339))
	}
	snap.SetAttribute("models_priced", fmt.Sprintf("%d", len(parsed.prices)))
	return parsed, nil
}

func (p *Provider) fetchUsage(ctx context.Context, baseURL string, st *session, acct core.AccountConfig, start, end string, rc rateCard, snap *core.UsageSnapshot) error {
	body, err := p.authedGET(ctx, baseURL+usagePrefix+start+"/"+end+"?paygo=true", st, acct)
	if err != nil {
		return err
	}
	snap.SetAttribute("usage_range", start[:10]+" → "+end[:10])
	return parseUsage(snap, body, "", rc)
}

// authedGET performs an authenticated GET with a one-shot 401/403 → re-auth
// retry. If re-auth succeeds the request is retried once; if auth still fails
// it returns errAuthFailed so callers can surface StatusAuth.
func (p *Provider) authedGET(ctx context.Context, url string, st *session, acct core.AccountConfig) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		status, body, err := p.doGET(ctx, url, st.accessToken)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return body, nil
		}
		if (status == http.StatusUnauthorized || status == http.StatusForbidden) && attempt == 0 {
			if p.reAuth(ctx, st, acct) {
				continue
			}
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, errAuthFailed
		}
		return nil, fmt.Errorf("HTTP %d", status)
	}
	return nil, errAuthFailed
}

// reAuth refreshes the session: prefer the refresh token, else fall back to a
// password login. On success it updates st in place and refreshes the shared
// token cache (this flow performed the login). Returns false when no
// credential path is available or every path fails.
func (p *Provider) reAuth(ctx context.Context, st *session, acct core.AccountConfig) bool {
	if st.refreshToken != "" {
		next, err := p.refreshLogin(ctx, acct, st.refreshToken)
		if err == nil && next.accessToken != "" {
			*st = *next
			p.writeStateFile(acct, *st)
			return true
		}
		return false
	}
	email := os.Getenv("MAKORA_EMAIL")
	password := os.Getenv("MAKORA_PASSWORD")
	if email != "" && password != "" {
		next, err := p.passwordLogin(ctx, acct, email, password)
		if err == nil && next.accessToken != "" {
			*st = *next
			p.writeStateFile(acct, *st)
			return true
		}
	}
	return false
}

func (p *Provider) passwordLogin(ctx context.Context, acct core.AccountConfig, email, password string) (*session, error) {
	form := url.Values{}
	form.Set("username", email)
	form.Set("password", password)
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := p.postForm(ctx, loginBaseURL(acct)+loginPath+"?device_name="+url.QueryEscape(deviceName), form, &resp); err != nil {
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("login response missing access_token")
	}
	return &session{accessToken: resp.AccessToken, refreshToken: resp.RefreshToken}, nil
}

func (p *Provider) refreshLogin(ctx context.Context, acct core.AccountConfig, refreshToken string) (*session, error) {
	form := url.Values{}
	form.Set("refresh_token", refreshToken)
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := p.postForm(ctx, loginBaseURL(acct)+refreshPath+"?device_name="+url.QueryEscape(deviceName), form, &resp); err != nil {
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("refresh response missing access_token")
	}
	return &session{accessToken: resp.AccessToken, refreshToken: resp.RefreshToken}, nil
}

func (p *Provider) postForm(ctx context.Context, url string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.Client().Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("auth HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("parsing auth response: %w", err)
		}
	}
	return nil
}

func (p *Provider) doGET(ctx context.Context, url, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}
	if token != "" {
		// The Authorization header is never logged; the token stays in-memory.
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.Client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading body: %w", err)
	}
	return resp.StatusCode, body, nil
}

// --- token cache (shared, read-only except when this flow logs in) ---

// loginBaseURL resolves the auth (login/refresh) host. Defaults to the
// dedicated be.prod.makora.com backend, but tests and self-hosted setups can
// override it via the account's "login_base_url" hint/path.
func loginBaseURL(acct core.AccountConfig) string {
	if v := acct.Path("login_base_url", ""); v != "" {
		return v
	}
	return baseLoginURL
}

func statePath(acct core.AccountConfig) string {
	if p := acct.Path("state_file", ""); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "makora-usage", "state.json")
}

func (p *Provider) loadStateFile(acct core.AccountConfig) (*session, bool) {
	path := statePath(acct)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var s struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false
	}
	if s.AccessToken == "" {
		return nil, false
	}
	return &session{accessToken: s.AccessToken, refreshToken: s.RefreshToken}, true
}

func (p *Provider) writeStateFile(acct core.AccountConfig, st session) {
	path := statePath(acct)
	if path == "" {
		return
	}
	payload := map[string]any{
		"access_token":  st.accessToken,
		"refresh_token": st.refreshToken,
		"exp":           time.Now().Add(24 * time.Hour).Unix(),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// finalizeStatus combines the spend-limit reference into the balance gauge
// and derives the final snapshot Status/Message. Existing terminal statuses
// (auth, limited, error) are preserved.
func finalizeStatus(snap *core.UsageSnapshot) {
	bal, balOK := snap.Metrics["credit_balance"]
	if lim, ok := snap.Metrics["spend_limit"]; ok && lim.Limit != nil && balOK && bal.Remaining != nil {
		bal.Limit = lim.Limit
		snap.Metrics["credit_balance"] = bal
	}

	if snap.Status != "" && snap.Status != core.StatusOK {
		return
	}

	currency := snap.Attributes["currency"]
	if currency == "" {
		currency = "Credits"
	}
	if balOK && bal.Remaining != nil {
		if *bal.Remaining <= 0 {
			snap.Status = core.StatusLimited
			snap.Message = "balance exhausted"
			return
		}
		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("Balance: %.2f %s", *bal.Remaining, currency)
		return
	}
	snap.Status = core.StatusOK
	snap.Message = "OK"
}
