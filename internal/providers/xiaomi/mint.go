package xiaomi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// ssoHopLimit bounds the sign-in redirect chain (serviceLogin → /sts →
	// the original console route).
	ssoHopLimit = 8
	// mintRetryInterval floors repeated sign-in attempts after a failure so a
	// rejected account session cannot become a login storm.
	mintRetryInterval = 15 * time.Minute

	ssoUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
)

// mintedCookieNames are the console cookies /sts issues, in the order they
// are sent back.
var mintedCookieNames = []string{serviceTokenCookie, userIDCookie, "api-platform_slh", "api-platform_ph"}

// isAccountHost reports whether a host may receive the Xiaomi account
// cookies. It is a variable so tests can point the chain at a local server.
var isAccountHost = func(host string) bool {
	return host == "xiaomi.com" || strings.HasSuffix(host, ".xiaomi.com")
}

var mintState = struct {
	sync.Mutex
	sessions map[string]string
	failedAt map[string]time.Time
}{sessions: map[string]string{}, failedAt: map[string]time.Time{}}

func cachedMintedSession(accountID string) (string, bool) {
	mintState.Lock()
	defer mintState.Unlock()
	header, ok := mintState.sessions[accountID]
	return header, ok
}

func storeMintedSession(accountID, header string) {
	mintState.Lock()
	defer mintState.Unlock()
	mintState.sessions[accountID] = header
	delete(mintState.failedAt, accountID)
}

func forgetMintedSession(accountID string) {
	mintState.Lock()
	defer mintState.Unlock()
	delete(mintState.sessions, accountID)
}

func recordMintFailure(accountID string) {
	mintState.Lock()
	defer mintState.Unlock()
	mintState.failedAt[accountID] = time.Now()
}

func mintAllowed(accountID string) bool {
	mintState.Lock()
	defer mintState.Unlock()
	failed, ok := mintState.failedAt[accountID]
	return !ok || time.Since(failed) >= mintRetryInterval
}

// mintConsoleSession runs the console's silent sign-in chain and returns the
// Cookie header for the console session it yields.
//
// An unauthenticated console route answers 401 with a loginUrl on
// account.xiaomi.com. Presenting the account cookies there redirects to the
// console's /sts route, which sets the service-token cookies. The account
// cookies are only ever sent to Xiaomi account hosts, and the chain may only
// visit those hosts and the console host.
func mintConsoleSession(ctx context.Context, accountCookies string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	base, err := url.Parse(consoleBase())
	if err != nil {
		return "", err
	}

	current, err := loginURL(ctx, client, base)
	if err != nil {
		return "", err
	}

	minted := map[string]string{}
	for hop := 0; hop < ssoHopLimit; hop++ {
		target, err := url.Parse(current)
		if err != nil {
			return "", fmt.Errorf("xiaomi sso: bad redirect: %w", err)
		}
		host := target.Hostname()
		toAccount := isAccountHost(host)
		if !toAccount && host != base.Hostname() {
			return "", fmt.Errorf("xiaomi sso: unexpected redirect host %q", host)
		}
		// Xiaomi issues http:// callbacks for the console host.
		if target.Scheme == "http" && base.Scheme == "https" {
			target.Scheme = "https"
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("User-Agent", ssoUserAgent)
		if toAccount {
			req.Header.Set("Cookie", accountCookies)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("xiaomi sso: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if !toAccount {
			collectSetCookies(resp.Header, minted)
		}
		location := resp.Header.Get("Location")
		if location == "" || resp.StatusCode < 300 || resp.StatusCode >= 400 {
			break
		}
		next, err := target.Parse(location)
		if err != nil {
			return "", fmt.Errorf("xiaomi sso: bad redirect: %w", err)
		}
		current = next.String()
	}

	if minted[serviceTokenCookie] == "" {
		return "", errors.New("xiaomi sso: account session was not accepted")
	}
	var pairs []string
	for _, name := range mintedCookieNames {
		if value := minted[name]; value != "" {
			pairs = append(pairs, name+"="+value)
		}
	}
	return strings.Join(pairs, "; "), nil
}

// loginURL asks an unauthenticated console route for the sign-in URL.
func loginURL(ctx context.Context, client *http.Client, base *url.URL) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/balance", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", ssoUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("xiaomi sso: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var envelope struct {
		LoginURL string `json:"loginUrl"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.LoginURL == "" {
		return "", fmt.Errorf("xiaomi sso: console returned no login URL (HTTP %d)", resp.StatusCode)
	}
	return envelope.LoginURL, nil
}

// collectSetCookies records name=value pairs verbatim. The service token is
// issued wrapped in double quotes, which net/http's cookie parser strips, so
// the headers are split by hand.
func collectSetCookies(header http.Header, into map[string]string) {
	for _, line := range header.Values("Set-Cookie") {
		pair, _, _ := strings.Cut(line, ";")
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		into[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
}
