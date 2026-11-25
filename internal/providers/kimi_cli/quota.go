package kimi_cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	defaultUsageAPIBaseURL = "https://api.kimi.com/coding/v1"
	quotaCacheTTL          = time.Minute
)

// PathHintCredentialsPathKey overrides the Kimi Code OAuth credentials file.
const PathHintCredentialsPathKey = "credentials_path"

// PathHintUsageAPIBaseKey overrides the coding API base URL.
const PathHintUsageAPIBaseKey = "usage_api_base_url"

type kimiCredentials struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"` // epoch seconds
}

type kimiUsageWindow struct {
	UsedRatio float64 `json:"used_ratio"`
	ResetTime string  `json:"reset_time"`
}

type kimiUsagesResponse struct {
	Usages        map[string]kimiUsageWindow `json:"usages"`
	BoosterWallet *kimiBoosterWallet         `json:"booster_wallet"`
}

type kimiBoosterWallet struct {
	Status                    string             `json:"status"`
	Balance                   kimiBoosterBalance `json:"balance"`
	MonthlyUsed               kimiMoney          `json:"monthlyUsed"`
	MonthlyChargeLimit        kimiMoney          `json:"monthlyChargeLimit"`
	MonthlyChargeLimitEnabled *bool              `json:"monthlyChargeLimitEnabled"`
	AutoRefillCharge          kimiMoney          `json:"autoRefillCharge"`
}

type kimiBoosterBalance struct {
	AmountLeft string `json:"amountLeft"`
}

type kimiMoney struct {
	Currency     string `json:"currency"`
	PriceInCents string `json:"priceInCents"`
}

type quotaCacheEntry struct {
	credentialsPath  string
	credentialsMtime time.Time
	baseURL          string
	fetchedAt        time.Time
	usage            *kimiUsagesResponse
	diagnosticKey    string
	diagnostic       string
}

// resolveCredentialsPath uses an explicit override or the newest Kimi Code
// credential file. A missing explicit path never falls back to another account.
func resolveCredentialsPath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintCredentialsPathKey, "")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".kimi-code", "credentials")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestMtime time.Time
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMtime) {
			newest = filepath.Join(dir, entry.Name())
			newestMtime = info.ModTime()
		}
	}
	return newest
}

func credentialMtime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func readCredentials(path string) (kimiCredentials, error) {
	var creds kimiCredentials
	data, err := os.ReadFile(path)
	if err != nil {
		return creds, err
	}
	err = json.Unmarshal(data, &creds)
	return creds, err
}

func fetchUsages(ctx context.Context, client *http.Client, baseURL, accessToken string) (*kimiUsagesResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/usages", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("kimi_cli: creating usages request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("kimi_cli: fetching usages: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("kimi_cli: usages: HTTP %d", resp.StatusCode)
	}
	var out kimiUsagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("kimi_cli: decoding usages: %w", err)
	}
	return &out, resp.StatusCode, nil
}

func (p *Provider) cachedQuota(accountID string) (quotaCacheEntry, bool) {
	p.quotaMu.Lock()
	defer p.quotaMu.Unlock()
	entry, ok := p.quotaCache[accountID]
	return entry, ok
}

func (p *Provider) storeQuota(accountID string, entry quotaCacheEntry) {
	p.quotaMu.Lock()
	defer p.quotaMu.Unlock()
	if p.quotaCache == nil {
		p.quotaCache = make(map[string]quotaCacheEntry)
	}
	p.quotaCache[accountID] = entry
}

// quotaChanged lets the daemon poll remote quota even when session logs are idle.
func (p *Provider) quotaChanged(acct core.AccountConfig) bool {
	path := resolveCredentialsPath(acct)
	baseURL := acct.Path(PathHintUsageAPIBaseKey, defaultUsageAPIBaseURL)
	entry, ok := p.cachedQuota(acct.ID)
	if !ok {
		return false
	}
	return path != entry.credentialsPath ||
		baseURL != entry.baseURL ||
		!credentialMtime(path).Equal(entry.credentialsMtime) ||
		(path != "" && p.now().Sub(entry.fetchedAt) >= quotaCacheTTL)
}

// addQuota uses only the CLI's access token. Kimi Code owns refresh and
// persistence of its single-use refresh token.
func (p *Provider) addQuota(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	now := p.now()
	path := resolveCredentialsPath(acct)
	baseURL := acct.Path(PathHintUsageAPIBaseKey, defaultUsageAPIBaseURL)
	mtime := credentialMtime(path)
	if cached, ok := p.cachedQuota(acct.ID); ok &&
		cached.credentialsPath == path && cached.baseURL == baseURL &&
		cached.credentialsMtime.Equal(mtime) && now.Sub(cached.fetchedAt) < quotaCacheTTL {
		if cached.usage != nil {
			applyQuotaToSnapshot(snap, cached.usage, now)
		}
		if cached.diagnostic != "" {
			snap.SetDiagnostic(cached.diagnosticKey, cached.diagnostic)
		}
		return
	}

	entry := quotaCacheEntry{
		credentialsPath:  path,
		credentialsMtime: mtime,
		baseURL:          baseURL,
		fetchedAt:        now,
	}
	defer func() { p.storeQuota(acct.ID, entry) }()
	diagnose := func(key, message string) {
		entry.diagnosticKey, entry.diagnostic = key, message
		snap.SetDiagnostic(key, message)
	}

	if path == "" {
		diagnose("quota", "no credentials found")
		return
	}
	creds, err := readCredentials(path)
	if err != nil {
		diagnose("quota", "credentials unreadable")
		return
	}
	if creds.AccessToken == "" {
		diagnose("quota", "credentials without access_token")
		return
	}
	if creds.ExpiresAt > 0 && now.Unix() >= creds.ExpiresAt {
		diagnose("quota", "access token expired")
		return
	}

	usage, status, err := fetchUsages(ctx, p.Client(), baseURL, creds.AccessToken)
	if status == http.StatusUnauthorized {
		// Kimi Code may have rotated credentials between our read and GET.
		fresh, readErr := readCredentials(path)
		if readErr == nil && fresh.AccessToken != "" && fresh.AccessToken != creds.AccessToken &&
			(fresh.ExpiresAt == 0 || now.Unix() < fresh.ExpiresAt) {
			usage, _, err = fetchUsages(ctx, p.Client(), baseURL, fresh.AccessToken)
			entry.credentialsMtime = credentialMtime(path)
		}
	}
	if err != nil {
		diagnose("quota_error", err.Error())
		return
	}
	entry.usage = usage
	applyQuotaToSnapshot(snap, usage, now)
}

// applyQuotaToSnapshot maps subscription windows onto the standard gauge
// metrics. Transient request-rate limits are not subscription quota.
func applyQuotaToSnapshot(snap *core.UsageSnapshot, usage *kimiUsagesResponse, now time.Time) {
	snap.EnsureMaps()
	for _, item := range []struct {
		source, metric, window string
	}{
		{"limit_5h", "usage_five_hour", "5h"},
		{"limit_7d", "usage_seven_day", "7d"},
		{"limit_month_total", "usage_monthly", "30d"},
		{"limit_month_code", "usage_monthly_code", "30d"},
	} {
		w, ok := usage.Usages[item.source]
		if !ok {
			continue
		}
		used := w.UsedRatio * 100
		if used < 0 {
			used = 0
		}
		if resetAt, ok := parseResetTime(w.ResetTime); ok {
			if !resetAt.After(now) {
				used = 0
			}
			snap.Resets[item.metric] = resetAt
		}
		limit := 100.0
		snap.Metrics[item.metric] = core.Metric{
			Used:   &used,
			Limit:  &limit,
			Unit:   "%",
			Window: item.window,
		}
	}
	applyBoosterWalletToSnapshot(snap, usage.BoosterWallet)
}

func applyBoosterWalletToSnapshot(snap *core.UsageSnapshot, wallet *kimiBoosterWallet) {
	if wallet == nil {
		return
	}
	currency := core.FirstNonEmpty(wallet.MonthlyUsed.Currency, wallet.MonthlyChargeLimit.Currency, wallet.AutoRefillCharge.Currency)
	if balance, ok := parseFixedPointCents(wallet.Balance.AmountLeft); ok {
		snap.Metrics["extra_usage_balance"] = core.Metric{
			Remaining: &balance,
			Unit:      currency,
			Window:    "current",
		}
	}
	if monthlyUsed, ok := parseCents(wallet.MonthlyUsed.PriceInCents); ok {
		metric := core.Metric{Used: &monthlyUsed, Unit: currency, Window: "billing-cycle"}
		if wallet.MonthlyChargeLimitEnabled != nil && *wallet.MonthlyChargeLimitEnabled {
			if limit, ok := parseCents(wallet.MonthlyChargeLimit.PriceInCents); ok {
				metric.Limit = &limit
			}
		}
		snap.Metrics["extra_usage_monthly_spend"] = metric
	}
	if wallet.Status != "" {
		snap.SetAttribute("extra_usage_status", strings.TrimPrefix(wallet.Status, "STATUS_"))
	}
}

func parseFixedPointCents(raw string) (float64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return float64(value) / 100_000_000, true
}

func parseCents(raw string) (float64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return float64(value) / 100, true
}

func parseResetTime(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}
