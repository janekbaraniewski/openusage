package kimi_cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Quota support: Kimi Code CLI stores OAuth credentials in
// ~/.kimi-code/credentials/<env>.json and the coding API exposes
// GET {base}/usages with subscription quota windows (5h and monthly pools)
// plus request-rate limits. The mapping mirrors claude_code's
// usage_five_hour conventions so gauges, reset chips and the hero summary
// render the same way.

const (
	defaultUsageAPIBaseURL = "https://api.kimi.com/coding/v1"
	defaultOAuthHost       = "https://auth.kimi.ai"
	// Public OAuth client id used by the Kimi Code CLI itself.
	kimiOAuthClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"

	// quotaCacheTTL bounds how often the usages endpoint is hit; the daemon
	// polls providers far more frequently than quota windows move.
	quotaCacheTTL = time.Minute
	// tokenRefreshSkew refreshes the access token slightly before expiry.
	tokenRefreshSkew = 60 * time.Second
)

// PathHintCredentialsPathKey overrides the OAuth credentials file location.
const PathHintCredentialsPathKey = "credentials_path"

// PathHintUsageAPIBaseKey overrides the coding API base URL.
const PathHintUsageAPIBaseKey = "usage_api_base_url"

// PathHintOAuthHostKey overrides the OAuth host used for token refresh.
const PathHintOAuthHostKey = "oauth_host"

// kimiCredentials mirrors the subset of the credentials file we read.
type kimiCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // epoch seconds
	TokenType    string `json:"token_type"`
}

// kimiUsageWindow is one entry of the usages map: a quota pool with a
// used ratio in [0,1] and a reset timestamp.
type kimiUsageWindow struct {
	UsedRatio float64 `json:"used_ratio"`
	ResetTime string  `json:"reset_time"`
}

// kimiUsagesResponse mirrors GET {base}/usages.
type kimiUsagesResponse struct {
	Limits []struct {
		Window struct {
			Duration int    `json:"duration"`
			TimeUnit string `json:"timeUnit"`
		} `json:"window"`
		Detail struct {
			Limit     string `json:"limit"`
			Used      string `json:"used"`
			Remaining string `json:"remaining"`
			ResetTime string `json:"resetTime"`
		} `json:"detail"`
	} `json:"limits"`
	Usages map[string]kimiUsageWindow `json:"usages"`
}

// resolveCredentialsPath returns the credentials file to use, preferring an
// explicit per-account override, then the newest credentials file across the
// known Kimi data directories. Returns "" when none is found.
func resolveCredentialsPath(acct core.AccountConfig) string {
	if override := acct.Path(PathHintCredentialsPathKey, ""); override != "" {
		if fileExists(override) {
			return override
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	var newest string
	var newestMtime time.Time
	for _, name := range kimiDataDirNames {
		dir := filepath.Join(home, name, "credentials")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
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
	}
	return newest
}

// readCredentials loads the credentials file, keeping the raw map so token
// refresh can write the file back without dropping unknown fields.
func readCredentials(path string) (kimiCredentials, map[string]any, error) {
	var creds kimiCredentials
	data, err := os.ReadFile(path)
	if err != nil {
		return creds, nil, err
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return creds, nil, err
	}
	raw := map[string]any{}
	_ = json.Unmarshal(data, &raw)
	return creds, raw, nil
}

// kimiTokenResponse mirrors POST {oauthHost}/oauth/token.
type kimiTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// refreshCredentials exchanges the refresh token for a new access token and
// persists the rotated credentials back to the file (refresh tokens are
// single-use, so losing the new one would break the CLI too).
func refreshCredentials(ctx context.Context, client *http.Client, oauthHost, path string, creds kimiCredentials, raw map[string]any) (kimiCredentials, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {kimiOAuthClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthHost+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return creds, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return creds, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return creds, fmt.Errorf("token refresh: HTTP %d", resp.StatusCode)
	}
	var tok kimiTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return creds, err
	}
	if tok.AccessToken == "" {
		return creds, fmt.Errorf("token refresh: empty access_token")
	}

	creds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		creds.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Unix() + tok.ExpiresIn
	}
	if tok.TokenType != "" {
		creds.TokenType = tok.TokenType
	}

	if raw == nil {
		raw = map[string]any{}
	}
	raw["access_token"] = creds.AccessToken
	raw["refresh_token"] = creds.RefreshToken
	raw["expires_at"] = creds.ExpiresAt
	raw["token_type"] = creds.TokenType
	if err := writeFileAtomic(path, raw); err != nil {
		return creds, err
	}
	return creds, nil
}

func writeFileAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	return os.Rename(tmpName, path)
}

// fetchUsages calls GET {base}/usages with the access token.
func fetchUsages(ctx context.Context, client *http.Client, baseURL, accessToken string) (*kimiUsagesResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/usages", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usages: HTTP %d", resp.StatusCode)
	}
	var out kimiUsagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// addQuota enriches the snapshot with live subscription quota when OAuth
// credentials are available. All failures degrade to diagnostics: local
// session stats stay usable regardless.
func (p *Provider) addQuota(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	credPath := resolveCredentialsPath(acct)
	if credPath == "" {
		snap.SetDiagnostic("quota", "no credentials found")
		return
	}

	if p.quotaCache != nil && p.now().Sub(p.quotaFetchedAt) < quotaCacheTTL {
		applyQuotaToSnapshot(snap, p.quotaCache, p.now())
		return
	}

	baseURL := acct.Path(PathHintUsageAPIBaseKey, defaultUsageAPIBaseURL)
	oauthHost := acct.Path(PathHintOAuthHostKey, defaultOAuthHost)
	client := p.Client()

	creds, raw, err := readCredentials(credPath)
	if err != nil {
		snap.SetDiagnostic("quota", "credentials unreadable")
		return
	}
	if creds.AccessToken == "" {
		snap.SetDiagnostic("quota", "credentials without access_token")
		return
	}

	usage, err := p.fetchQuotaWithRefresh(ctx, client, oauthHost, baseURL, credPath, creds, raw)
	if err != nil {
		snap.SetDiagnostic("quota_error", err.Error())
		return
	}
	p.quotaCache = usage
	p.quotaFetchedAt = p.now()
	applyQuotaToSnapshot(snap, usage, p.now())
}

// fetchQuotaWithRefresh refreshes the access token when expired and retries
// once after re-reading the credentials file on auth failure (the CLI may
// have rotated the token concurrently).
func (p *Provider) fetchQuotaWithRefresh(ctx context.Context, client *http.Client, oauthHost, baseURL, credPath string, creds kimiCredentials, raw map[string]any) (*kimiUsagesResponse, error) {
	if creds.RefreshToken != "" && time.Now().Unix()+int64(tokenRefreshSkew.Seconds()) >= creds.ExpiresAt {
		refreshed, err := refreshCredentials(ctx, client, oauthHost, credPath, creds, raw)
		if err != nil {
			return nil, err
		}
		creds = refreshed
	}

	usage, err := fetchUsages(ctx, client, baseURL, creds.AccessToken)
	if err == nil {
		return usage, nil
	}

	fresh, _, readErr := readCredentials(credPath)
	if readErr != nil || fresh.AccessToken == "" || fresh.AccessToken == creds.AccessToken {
		return nil, err
	}
	return fetchUsages(ctx, client, baseURL, fresh.AccessToken)
}

// applyQuotaToSnapshot maps the usages response onto snapshot metrics and
// reset timestamps following the usage_five_hour / usage_seven_day
// conventions used by other coding-tool providers.
func applyQuotaToSnapshot(snap *core.UsageSnapshot, usage *kimiUsagesResponse, now time.Time) {
	snap.EnsureMaps()

	setPct := func(key, window string, w kimiUsageWindow) {
		util := w.UsedRatio * 100
		if util < 0 {
			util = 0
		}
		if t, ok := parseResetTime(w.ResetTime); ok {
			if !t.After(now) {
				util = 0
			}
			snap.Resets[key] = t
		}
		limit := 100.0
		snap.Metrics[key] = core.Metric{
			Used:   &util,
			Limit:  &limit,
			Unit:   "%",
			Window: window,
		}
	}

	if w, ok := usage.Usages["limit_5h"]; ok {
		setPct("usage_five_hour", "5h", w)
	}
	if w, ok := usage.Usages["limit_month_total"]; ok {
		setPct("usage_monthly", "30d", w)
	}
	if w, ok := usage.Usages["limit_month_code"]; ok {
		setPct("usage_monthly_code", "30d", w)
	}

	rateKeys := []string{"rate_limit_primary", "rate_limit_secondary", "rate_limit_tertiary"}
	for i, l := range usage.Limits {
		if i >= len(rateKeys) {
			break
		}
		used, errU := strconv.ParseFloat(l.Detail.Used, 64)
		limit, errL := strconv.ParseFloat(l.Detail.Limit, 64)
		remaining, errR := strconv.ParseFloat(l.Detail.Remaining, 64)
		if errU != nil || errL != nil || limit <= 0 {
			continue
		}
		if errR != nil {
			remaining = limit - used
		}
		key := rateKeys[i]
		snap.Metrics[key] = core.Metric{
			Used:      &used,
			Limit:     &limit,
			Remaining: &remaining,
			Unit:      "requests",
			Window:    rateWindowLabel(l.Window.Duration, l.Window.TimeUnit),
		}
		if t, ok := parseResetTime(l.Detail.ResetTime); ok {
			snap.Resets[key] = t
		}
	}
}

// rateWindowLabel maps a rate-limit window onto the short window tags the
// dashboard recognises ("5h", "1d", "7d", "30d"); unknown windows yield "".
func rateWindowLabel(duration int, timeUnit string) string {
	if timeUnit != "TIME_UNIT_MINUTE" {
		return ""
	}
	switch {
	case duration == 300:
		return "5h"
	case duration == 1440:
		return "1d"
	case duration == 10080:
		return "7d"
	case duration == 43200:
		return "30d"
	}
	return ""
}

func parseResetTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
