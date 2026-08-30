package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	tokenEndpoint   = "https://oauth2.googleapis.com/token"
	oauthTokenFile  = "antigravity-oauth-token"
	tokenExpirySkew = 60 * time.Second
	// Optional OAuth client for token refresh. When unset, expired tokens are
	// renewed by pinging the box (`agy-box <name> -p ping`) instead.
	oauthClientIDEnv     = "OPENUSAGE_ANTIGRAVITY_CLIENT_ID"
	oauthClientSecretEnv = "OPENUSAGE_ANTIGRAVITY_CLIENT_SECRET"
)

type oauthTokenFilePayload struct {
	AuthMethod string     `json:"auth_method,omitempty"`
	Token      oauthToken `json:"token"`
}

type oauthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

func configDir(acct core.AccountConfig) string {
	if dir := strings.TrimSpace(acct.Path("config_dir", "")); dir != "" {
		return dir
	}
	box := strings.TrimSpace(acct.Hint("box_name", ""))
	if box == "" {
		id := strings.TrimSpace(acct.ID)
		if strings.HasPrefix(id, "antigravity-") {
			box = strings.TrimPrefix(id, "antigravity-")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if box != "" {
		return filepath.Join(home, ".agy-containers", box, ".gemini", "antigravity-cli")
	}
	if strings.TrimSpace(acct.ID) == defaultAccountID {
		return filepath.Join(home, ".gemini", "antigravity-cli")
	}
	return ""
}

func tokenFilePath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path("oauth_token_file", "")); override != "" {
		return override
	}
	dir := configDir(acct)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, oauthTokenFile)
}

func boxName(acct core.AccountConfig) string {
	if name := strings.TrimSpace(acct.Hint("box_name", "")); name != "" {
		return name
	}
	id := strings.TrimSpace(acct.ID)
	if strings.HasPrefix(id, "antigravity-") {
		return strings.TrimPrefix(id, "antigravity-")
	}
	dir := configDir(acct)
	if dir == "" {
		return ""
	}
	// ~/.agy-containers/<box>/.gemini/antigravity-cli
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] == ".agy-containers" {
			return parts[i+1]
		}
	}
	return ""
}

func loadOAuthToken(path string) (oauthTokenFilePayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return oauthTokenFilePayload{}, err
	}
	var payload oauthTokenFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return oauthTokenFilePayload{}, fmt.Errorf("parse oauth token file: %w", err)
	}
	return payload, nil
}

func writeOAuthToken(path string, payload oauthTokenFilePayload) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-oauth-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func tokenExpired(tok oauthToken, now time.Time) bool {
	access := strings.TrimSpace(tok.AccessToken)
	if access == "" {
		return true
	}
	expiry := strings.TrimSpace(tok.Expiry)
	if expiry == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, expiry)
	}
	if err != nil {
		return false
	}
	return !parsed.After(now.Add(tokenExpirySkew))
}

func oauthClientCredentials() (clientID, clientSecret string, ok bool) {
	clientID = strings.TrimSpace(os.Getenv(oauthClientIDEnv))
	clientSecret = strings.TrimSpace(os.Getenv(oauthClientSecretEnv))
	if clientID == "" || clientSecret == "" {
		return "", "", false
	}
	return clientID, clientSecret, true
}

func refreshAccessToken(ctx context.Context, refreshToken string, client *http.Client) (oauthToken, error) {
	clientID, clientSecret, ok := oauthClientCredentials()
	if !ok {
		return oauthToken{}, fmt.Errorf("oauth client not configured (%s / %s)", oauthClientIDEnv, oauthClientSecretEnv)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return oauthToken{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return oauthToken{}, fmt.Errorf("token refresh HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var decoded struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return oauthToken{}, fmt.Errorf("parse token refresh response: %w", err)
	}
	if strings.TrimSpace(decoded.AccessToken) == "" {
		return oauthToken{}, fmt.Errorf("empty access_token in refresh response")
	}
	tok := oauthToken{
		AccessToken:  decoded.AccessToken,
		RefreshToken: strings.TrimSpace(decoded.RefreshToken),
		TokenType:    decoded.TokenType,
	}
	if decoded.ExpiresIn > 0 {
		tok.Expiry = time.Now().UTC().Add(time.Duration(decoded.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	}
	return tok, nil
}

func pingBoxForToken(ctx context.Context, acct core.AccountConfig) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}
	box := boxName(acct)
	var cmd *exec.Cmd
	if box != "" {
		cmd = exec.CommandContext(ctx, "agy-box", box, "-p", "ping")
	} else {
		bin := strings.TrimSpace(acct.Binary)
		if bin == "" {
			bin = "agy"
		}
		cmd = exec.CommandContext(ctx, bin, "-p", "ping")
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if box != "" {
			return fmt.Errorf("agy-box %s -p ping: %w", box, err)
		}
		return fmt.Errorf("%s -p ping: %w", cmd.Path, err)
	}
	return nil
}

// ensureAccessToken returns a usable access token, refreshing or pinging the
// box when needed. refreshed reports whether the on-disk token file changed.
func ensureAccessToken(ctx context.Context, acct core.AccountConfig, client *http.Client) (accessToken string, path string, refreshed bool, err error) {
	path = tokenFilePath(acct)
	if path == "" {
		return "", "", false, fmt.Errorf("oauth token path unavailable (set config_dir)")
	}

	load := func() (oauthTokenFilePayload, error) {
		return loadOAuthToken(path)
	}

	payload, err := load()
	if err != nil {
		if !os.IsNotExist(err) {
			return "", path, false, err
		}
		// Missing token file: ping the box to create one.
		if pingErr := pingBoxForToken(ctx, acct); pingErr != nil {
			return "", path, false, fmt.Errorf("missing oauth token and ping failed: %w", pingErr)
		}
		payload, err = load()
		if err != nil {
			return "", path, false, fmt.Errorf("oauth token still missing after ping: %w", err)
		}
		refreshed = true
	}

	now := time.Now().UTC()
	if !tokenExpired(payload.Token, now) {
		return payload.Token.AccessToken, path, refreshed, nil
	}

	refreshTok := strings.TrimSpace(payload.Token.RefreshToken)
	if refreshTok != "" {
		tok, refreshErr := refreshAccessToken(ctx, refreshTok, client)
		if refreshErr == nil {
			if tok.RefreshToken == "" {
				tok.RefreshToken = refreshTok
			}
			payload.Token = tok
			if writeErr := writeOAuthToken(path, payload); writeErr != nil {
				return "", path, false, fmt.Errorf("persist refreshed token: %w", writeErr)
			}
			return tok.AccessToken, path, true, nil
		}
		// Fall through to ping when refresh fails.
		_ = refreshErr
	}

	if pingErr := pingBoxForToken(ctx, acct); pingErr != nil {
		if refreshTok == "" {
			return "", path, false, fmt.Errorf("no refresh token and ping failed: %w", pingErr)
		}
		return "", path, false, fmt.Errorf("token refresh failed and ping failed: %w", pingErr)
	}
	payload, err = load()
	if err != nil {
		return "", path, false, fmt.Errorf("oauth token unreadable after ping: %w", err)
	}
	if tokenExpired(payload.Token, time.Now().UTC()) && strings.TrimSpace(payload.Token.AccessToken) == "" {
		return "", path, false, fmt.Errorf("oauth token still unusable after ping")
	}
	return payload.Token.AccessToken, path, true, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
