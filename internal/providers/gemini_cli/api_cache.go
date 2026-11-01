package gemini_cli

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// accessTokenRefreshSkew is how close to expiry an access token may get
	// before we stop reusing it and mint a new one from the refresh token.
	accessTokenRefreshSkew = 5 * time.Minute
	// defaultAccessTokenLifetime is assumed when the token endpoint omits
	// expires_in. Google access tokens live for an hour.
	defaultAccessTokenLifetime = time.Hour
	// loadCodeAssistTTL bounds how long loadCodeAssist metadata (project,
	// tier, upgrade links) is reused. It changes only on plan changes.
	loadCodeAssistTTL = time.Hour
	// maxAPICacheEntries bounds each map; one entry per configured account.
	maxAPICacheEntries = 32
)

type cachedAccessToken struct {
	token     string
	expiresAt time.Time
}

type cachedLoadCodeAssist struct {
	resp      loadCodeAssistResponse
	fetchedAt time.Time
}

// apiCache holds per-account OAuth access tokens and loadCodeAssist results
// in memory only. It never persists or logs tokens. Safe for concurrent use.
type apiCache struct {
	mu         sync.Mutex
	tokens     map[string]cachedAccessToken
	loads      map[string]cachedLoadCodeAssist
	tokenOrder []string
	loadOrder  []string
}

func newAPICache() *apiCache {
	return &apiCache{
		tokens: make(map[string]cachedAccessToken),
		loads:  make(map[string]cachedLoadCodeAssist),
	}
}

// apiCacheKey scopes cache entries to an account and its refresh token, so a
// re-authentication (new refresh token) never reuses state from the old
// login. The refresh token is hashed; it is never stored as a key.
func apiCacheKey(accountID, refreshToken string) string {
	sum := sha256.Sum256([]byte(refreshToken))
	return accountID + "\x00" + hex.EncodeToString(sum[:8])
}

func (c *apiCache) token(key string, now time.Time) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[key]
	if !ok || !tokenUsable(t.expiresAt, now) {
		return "", false
	}
	return t.token, true
}

func (c *apiCache) storeToken(key, token string, expiresAt time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tokens[key]; !ok {
		c.tokenOrder = append(c.tokenOrder, key)
	}
	c.tokens[key] = cachedAccessToken{token: token, expiresAt: expiresAt}
	for len(c.tokenOrder) > maxAPICacheEntries {
		delete(c.tokens, c.tokenOrder[0])
		c.tokenOrder = c.tokenOrder[1:]
	}
}

func (c *apiCache) dropToken(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, key)
	c.tokenOrder = removeKey(c.tokenOrder, key)
}

func (c *apiCache) loadCodeAssist(key string, now time.Time) (*loadCodeAssistResponse, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.loads[key]
	if !ok || now.Sub(l.fetchedAt) >= loadCodeAssistTTL {
		return nil, false
	}
	resp := l.resp
	return &resp, true
}

func (c *apiCache) storeLoadCodeAssist(key string, resp *loadCodeAssistResponse, now time.Time) {
	if c == nil || resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.loads[key]; !ok {
		c.loadOrder = append(c.loadOrder, key)
	}
	c.loads[key] = cachedLoadCodeAssist{resp: *resp, fetchedAt: now}
	for len(c.loadOrder) > maxAPICacheEntries {
		delete(c.loads, c.loadOrder[0])
		c.loadOrder = c.loadOrder[1:]
	}
}

func removeKey(keys []string, key string) []string {
	for i, k := range keys {
		if k == key {
			return append(keys[:i], keys[i+1:]...)
		}
	}
	return keys
}

// tokenUsable reports whether a token expiring at expiresAt can still be used
// at now without dipping inside the refresh skew window.
func tokenUsable(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && now.Add(accessTokenRefreshSkew).Before(expiresAt)
}
