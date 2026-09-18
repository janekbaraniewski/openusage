// Muse Code live quota: POST api.meta.ai/muse-code/key with the CLI's OAuth
// token returns the subscription snapshot (weekly + window percentages,
// resets, and the server-provided plan name) with no inference cost. When
// OAuth is absent or rejected it falls back to the same POST
// api.meta.ai/v1/responses SSE probe the TUI's `/usage` view renders — a
// minimal `store:false, stream:true, input:"hi"` probe returns
// `response.subscription_usage`, authenticated by the CLI's own keychain
// `api_key` (service `ai.meta.dev.credentials`, account `meta`; that blob
// also carries the OAuth `access_token`). No browser session or page-load
// tokens. See `openusage/OPENUSAGE-GO-MUSECODE.md` for the current design;
// the archived `MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md` is the historical
// GraphQL investigation and is not shipped.
package muse_code

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

// quotaHTTPClient builds the POST client. A var (not a plain func) so tests
// can substitute a stub RoundTripper and exercise the full request path
// without binding a socket.
var quotaHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// responsesBaseURL is the Meta provider API root behind the TUI's /usage
// view. A minimal streamed probe to POST <base>/responses yields the usage
// event with no browser session or page-load tokens.
const responsesBaseURL = "https://api.meta.ai/v1"

// responsesEndpointOverride swaps the probe target in tests.
var responsesEndpointOverride = ""

// responsesProbeModel is the model the TUI itself uses; the usage event
// arrives regardless of the tiny probe input.
const responsesProbeModel = "muse-spark-1.3-contributor"

// loadMuseAPIKey returns the CLI's API bearer: META_API_KEY when set, else
// the api_key field of the keychain blob (service
// "ai.meta.dev.credentials", account "meta", darwin only). A var so tests
// can stub it. Values are secrets; callers must never log them.
// museAPIKeyCache memoizes the keychain read within the process: macOS pops
// a keychain approval on first access, so every poll must not re-prompt.
var museAPIKeyCache struct {
	sync.Mutex
	key string
	ok  bool
	set bool
}

// museOAuthTokenCache memoizes the OAuth token within the process. The
// OAuth token authenticates the muse-code/key account endpoint, which the
// Model API key cannot touch (401). Separate from the API-key cache: the
// two credentials have different sources (META_API_KEY and
// ~/.config/openusage/muse.json carry the API key only) and different
// lifetimes.
var museOAuthTokenCache struct {
	sync.Mutex
	token string
	ok    bool
	set   bool
}

// museQuotaMemoryState holds the last successful subscription payload so a
// later 429 can be attributed to the right window. The 429 body carries
// only a reset instant — no window marker — so without memory every block
// looks like the week. Long-lived processes (daemon, TUI) get real memory;
// one-shot runs fall back to the legacy weekly assumption.
type museQuotaMemory struct {
	weeklyUsed      float64
	weeklyResetUnix int64
	windowUsed      float64
	windowResetUnix int64
	tier            string
	observedAt      time.Time
}

var (
	museQuotaMemoryMu    sync.Mutex
	museQuotaMemoryState museQuotaMemory
)

// sessionResetAmbiguity separates the windows: a 429 reset this much sooner
// than the remembered weekly reset belongs to the session window. Window
// durations are 5h; weekly resets sit days out, so 6h has wide margin.
const sessionResetAmbiguity = 6 * time.Hour

func rememberQuotaMemory(sub *subscriptionUsage) {
	if sub.Weekly.ResetsAt <= 0 {
		return
	}
	mem := museQuotaMemory{
		weeklyUsed:      sub.Weekly.UsedPercent,
		weeklyResetUnix: sub.Weekly.ResetsAt,
		windowUsed:      sub.Window.UsedPercent,
		windowResetUnix: sub.Window.ResetsAt,
		tier:            sub.Tier,
		observedAt:      time.Now(),
	}
	museQuotaMemoryMu.Lock()
	museQuotaMemoryState = mem
	museQuotaMemoryMu.Unlock()
	// Best effort: a restart during a block must still find this. Failures
	// stay silent — the poll outcome never depends on the file.
	_ = persistQuotaMemory(mem)
}

// museQuotaMemoryPath is the state file for the last successful subscription
// payload. MUSE_QUOTA_MEMORY_PATH overrides it (tests). Otherwise it lives
// next to the telemetry state so daemon, TUI, and one-shot runs share it.
func museQuotaMemoryPath() string {
	if override := strings.TrimSpace(os.Getenv("MUSE_QUOTA_MEMORY_PATH")); override != "" {
		return override
	}
	dir, err := telemetry.DefaultStateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "muse-quota-memory.json")
}

type museQuotaMemoryFile struct {
	SchemaVersion   int     `json:"schema_version"`
	WeeklyUsed      float64 `json:"weekly_used"`
	WeeklyResetUnix int64   `json:"weekly_reset_unix"`
	WindowUsed      float64 `json:"window_used"`
	WindowResetUnix int64   `json:"window_reset_unix"`
	Tier            string  `json:"tier"`
	ObservedAtUnix  int64   `json:"observed_at_unix"`
}

func persistQuotaMemory(mem museQuotaMemory) error {
	path := museQuotaMemoryPath()
	if path == "" {
		return fmt.Errorf("no state dir")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(museQuotaMemoryFile{
		SchemaVersion:   2,
		WeeklyUsed:      mem.weeklyUsed,
		WeeklyResetUnix: mem.weeklyResetUnix,
		WindowUsed:      mem.windowUsed,
		WindowResetUnix: mem.windowResetUnix,
		Tier:            mem.tier,
		ObservedAtUnix:  mem.observedAt.Unix(),
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".muse-quota-memory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func loadQuotaMemory() (museQuotaMemory, bool) {
	path := museQuotaMemoryPath()
	if path == "" {
		return museQuotaMemory{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return museQuotaMemory{}, false
	}
	var file museQuotaMemoryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return museQuotaMemory{}, false
	}
	if file.WeeklyResetUnix <= 0 {
		return museQuotaMemory{}, false
	}
	// Schema is additive: v1 files simply lack the window fields and
	// carry the week only.
	return museQuotaMemory{
		weeklyUsed:      file.WeeklyUsed,
		weeklyResetUnix: file.WeeklyResetUnix,
		windowUsed:      file.WindowUsed,
		windowResetUnix: file.WindowResetUnix,
		tier:            file.Tier,
		observedAt:      time.Unix(file.ObservedAtUnix, 0),
	}, true
}

// currentQuotaMemory returns the valid unexpired memory, consulting the
// state file when the process started cold (e.g. restarted mid-block).
func currentQuotaMemory(now time.Time) (museQuotaMemory, bool) {
	museQuotaMemoryMu.Lock()
	mem := museQuotaMemoryState
	museQuotaMemoryMu.Unlock()
	if mem.weeklyResetUnix <= 0 || mem.observedAt.IsZero() {
		fileMem, ok := loadQuotaMemory()
		if !ok {
			return museQuotaMemory{}, false
		}
		museQuotaMemoryMu.Lock()
		museQuotaMemoryState = fileMem
		museQuotaMemoryMu.Unlock()
		mem = fileMem
	}
	if now.Unix() >= mem.weeklyResetUnix {
		return museQuotaMemory{}, false // memory expired with the old week
	}
	return mem, true
}

// classifyQuotaExhaustion reports whether a 429 reset belongs to the session
// window, returning the memory it decided on. False means weekly (or
// unknown): the legacy assumption.
func classifyQuotaExhaustion(resetsAt int64, now time.Time) (bool, museQuotaMemory) {
	mem, ok := currentQuotaMemory(now)
	if !ok {
		return false, museQuotaMemory{}
	}
	if time.Unix(mem.weeklyResetUnix, 0).Sub(time.Unix(resetsAt, 0)) <= sessionResetAmbiguity {
		return false, museQuotaMemory{}
	}
	return true, mem
}

var loadMuseAPIKey = func(ctx context.Context) (string, bool) {
	if k := strings.TrimSpace(os.Getenv("META_API_KEY")); k != "" {
		return k, true
	}
	if k, ok := loadMuseAPIKeyFromFile(); ok {
		return k, true
	}
	museAPIKeyCache.Lock()
	cached, cachedOK, set := museAPIKeyCache.key, museAPIKeyCache.ok, museAPIKeyCache.set
	museAPIKeyCache.Unlock()
	if set {
		return cached, cachedOK
	}
	key, ok := readMuseAPIKeyFromKeychain(ctx)
	museAPIKeyCache.Lock()
	museAPIKeyCache.key, museAPIKeyCache.ok, museAPIKeyCache.set = key, ok, true
	museAPIKeyCache.Unlock()
	if ok && key != "" {
		// Best-effort persist to the app-owned file so the next
		// launch (or the Swift app) doesn't need a keychain prompt.
		// Mirrors Swift's UserAPIKeyStore auto-save.
		_ = saveMuseAPIKeyToFile(key)
	}
	return key, ok
}

func loadMuseAPIKeyFromFile() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	path := filepath.Join(home, ".config", "openusage", "muse.json")
	data, err := os.ReadFile(path)
	if err == nil {
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			var obj map[string]any
			if err := json.Unmarshal(data, &obj); err == nil {
				for _, field := range []string{"apiKey", "api_key", "key"} {
					if v, ok := obj[field].(string); ok && strings.TrimSpace(v) != "" {
						return strings.TrimSpace(v), true
					}
				}
			} else if !strings.Contains(trimmed, "{") {
				return trimmed, true
			}
		}
	}
	// Fallback: the Muse CLI's own auth file on Linux stores the api_key
	// inline as providers.meta.api_key (alongside the dca: access_token).
	// On darwin it is in the keychain, but on Linux the file is the source
	// of truth and avoids needing a separate ~/.config/openusage/muse.json
	// copy. This makes `muse login` on Linux immediately quota-capable.
	authPath := filepath.Join(home, ".config", "muse", "auth.json")
	if data, err := os.ReadFile(authPath); err == nil {
		var doc struct {
			Providers map[string]struct {
				APIKey string `json:"api_key"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(data, &doc); err == nil {
			if prov, ok := doc.Providers["meta"]; ok && strings.TrimSpace(prov.APIKey) != "" {
				return strings.TrimSpace(prov.APIKey), true
			}
		}
	}
	return "", false
}

// loadMuseOAuthTokenFromCLIFile reads the OAuth token from the CLI's own
// auth file. On Linux `muse login` (device_code) stores providers.meta
// inline including access_token; on darwin the file is metadata-only and
// the secret lives in the keychain, so this returns nothing there — the
// keychain path below covers darwin.
func loadMuseOAuthTokenFromCLIFile() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "muse", "auth.json"))
	if err != nil {
		return "", false
	}
	var doc struct {
		Providers map[string]struct {
			AccessToken string `json:"access_token"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	if prov, ok := doc.Providers["meta"]; ok && strings.TrimSpace(prov.AccessToken) != "" {
		return strings.TrimSpace(prov.AccessToken), true
	}
	return "", false
}

// loadMuseOAuthTokenFromOwnedFile reads the cached token from the
// app-owned file. Silent and prompt-free: the preferred source after the
// first successful bootstrap.
func loadMuseOAuthTokenFromOwnedFile() (string, bool) {
	obj, _, err := readMuseOwnedKeys()
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(obj["oauthToken"])
	return token, token != ""
}

// saveMuseOAuthTokenToOwnedFile caches the token alongside any saved API
// key. Refuses content it couldn't parse rather than overwriting it.
func saveMuseOAuthTokenToOwnedFile(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("empty token")
	}
	obj, _, err := readMuseOwnedKeys()
	if err != nil {
		return err
	}
	if obj["oauthToken"] == token {
		return nil // already cached
	}
	obj["oauthToken"] = token
	return writeMuseOwnedKeys(obj)
}

// clearMuseOAuthTokenOwnedFile drops the cached token (e.g. after a 401)
// while preserving a saved API key. An absent file or field is a no-op;
// unparseable content is left untouched.
func clearMuseOAuthTokenOwnedFile() {
	obj, present, err := readMuseOwnedKeys()
	if err != nil || !present {
		return
	}
	if _, ok := obj["oauthToken"]; !ok {
		return
	}
	delete(obj, "oauthToken")
	if len(obj) == 0 {
		if path, ok := museOwnedKeysPath(); ok {
			_ = os.Remove(path)
		}
		return
	}
	_ = writeMuseOwnedKeys(obj)
}

// loadMuseOAuthToken returns the CLI's OAuth token for the muse-code/key
// account endpoint: owned copy first, then CLI sources with memoization.
// Unlike the API key it has no env override: META_API_KEY carries the
// Model API key only, which that endpoint rejects. A var so tests can
// stub it.
var loadMuseOAuthToken = func(ctx context.Context) (string, bool) {
	// Owned copy first: silent, no keychain prompt after the first boot.
	if token, ok := loadMuseOAuthTokenFromOwnedFile(); ok {
		return token, true
	}
	return readAndMemoizeMuseOAuthToken(ctx)
}

// readAndMemoizeMuseOAuthToken resolves the token from CLI sources and
// caches a copy in the owned file, so later boots never touch the CLI
// keychain entry (and its approval prompt) again.
func readAndMemoizeMuseOAuthToken(ctx context.Context) (string, bool) {
	museOAuthTokenCache.Lock()
	cached, cachedOK, set := museOAuthTokenCache.token, museOAuthTokenCache.ok, museOAuthTokenCache.set
	museOAuthTokenCache.Unlock()
	if set {
		return cached, cachedOK
	}
	token, ok := readMuseOAuthTokenFromCLISources(ctx)
	museOAuthTokenCache.Lock()
	museOAuthTokenCache.token, museOAuthTokenCache.ok, museOAuthTokenCache.set = token, ok, true
	museOAuthTokenCache.Unlock()
	if ok {
		_ = saveMuseOAuthTokenToOwnedFile(token)
	}
	return token, ok
}

// readMuseOAuthTokenFromCLISources reads the Linux auth file, then the
// macOS keychain blob. No owned file, no memo: the refresh path.
func readMuseOAuthTokenFromCLISources(ctx context.Context) (string, bool) {
	if token, ok := loadMuseOAuthTokenFromCLIFile(); ok {
		return token, true
	}
	if raw, found := readMuseSecretBlobFromKeychain(ctx); found {
		if parsed, _ := parseMuseSecretBlob(raw); parsed != "" {
			return parsed, true
		}
	}
	return "", false
}

// refreshMuseOAuthToken drops the owned copy and its memo, then re-reads
// from CLI sources (which the CLI keeps fresh) and caches the result.
// Called once after a 401 before falling back to the Responses probe.
func refreshMuseOAuthToken(ctx context.Context) (string, bool) {
	clearMuseOAuthTokenOwnedFile()
	museOAuthTokenCache.Lock()
	museOAuthTokenCache.token, museOAuthTokenCache.ok, museOAuthTokenCache.set = "", false, false
	museOAuthTokenCache.Unlock()
	return readAndMemoizeMuseOAuthToken(ctx)
}

// museOwnedKeysPath is the app-owned credential file shared by the saved
// API key and the cached OAuth token. 0600, never logged.
func museOwnedKeysPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	return filepath.Join(home, ".config", "openusage", "muse.json"), true
}

// readMuseOwnedKeys parses the owned file into a mutable map. A missing
// file means no cache (not an error); present-but-unparseable stays an
// error so callers never silently overwrite content they couldn't read
// (e.g. a hand-maintained raw-string key file).
func readMuseOwnedKeys() (obj map[string]string, present bool, err error) {
	path, ok := museOwnedKeysPath()
	if !ok {
		return nil, false, fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, false, nil
		}
		return nil, false, err
	}
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, true, err
	}
	if parsed == nil {
		parsed = map[string]string{}
	}
	return parsed, true, nil
}

// writeMuseOwnedKeys persists the map atomically with 0600.
func writeMuseOwnedKeys(obj map[string]string) error {
	path, ok := museOwnedKeysPath()
	if !ok {
		return fmt.Errorf("no home dir")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	tmp := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveMuseAPIKeyToFile(key string) error {
	path, ok := museOwnedKeysPath()
	if !ok {
		return fmt.Errorf("no home dir")
	}
	if _, err := os.Stat(path); err == nil {
		// Don't overwrite an existing file — user may have set it
		// explicitly or Swift already persisted it.
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"apiKey": strings.TrimSpace(key)})
	tmp := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// parseMuseSecretBlob extracts the API key and OAuth token from the CLI's
// secret blob (secret_schema_version, api_key, access_token). Either may be
// absent — API-key-only setups have no access_token. Empty means missing.
// Values are secrets and must never be logged.
func parseMuseSecretBlob(data []byte) (apiKey, oauthToken string) {
	var blob struct {
		APIKey      string `json:"api_key"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &blob); err != nil {
		return "", ""
	}
	return strings.TrimSpace(blob.APIKey), strings.TrimSpace(blob.AccessToken)
}

// readMuseSecretBlobFromKeychain is a var so tests can stub the keychain
// without popping a real approval dialog.
var readMuseSecretBlobFromKeychain = func(ctx context.Context) ([]byte, bool) {
	if runtime.GOOS != "darwin" {
		return nil, false
	}
	probe, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, "/usr/bin/security", "find-generic-password", "-s", "ai.meta.dev.credentials", "-a", "meta", "-w").Output()
	if err != nil {
		return nil, false
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, false
	}
	return []byte(trimmed), true
}

func readMuseAPIKeyFromKeychain(ctx context.Context) (string, bool) {
	raw, ok := readMuseSecretBlobFromKeychain(ctx)
	if !ok {
		return "", false
	}
	key, _ := parseMuseSecretBlob(raw)
	return key, key != ""
}

// subscriptionUsage mirrors the response.subscription_usage SSE event.
type subscriptionUsage struct {
	Tier string `json:"tier"`
	// PlanName is the server-provided display label ("Muse Code High
	// Usage"). The SSE event carries only the opaque tier ID, so this
	// stays empty on the probe path and plan_name falls back to
	// quotaPlanName(Tier). The muse-code/key path sets it from
	// subs_tier_name.
	PlanName string `json:"-"`
	// UpgradeAvailable mirrors is_subs_upgrade_available from the
	// muse-code/key account snapshot. Only the key path knows it; the
	// Responses probe leaves it false and the limit-reached message
	// omits the /upgrade hint rather than assuming it.
	UpgradeAvailable bool `json:"-"`
	Weekly           struct {
		ResetsAt    int64   `json:"resets_at"`
		UsedPercent float64 `json:"used_percent"`
	} `json:"weekly"`
	Window struct {
		ResetsAt           int64   `json:"resets_at"`
		UsedPercent        float64 `json:"used_percent"`
		WindowDurationMins int     `json:"window_duration_mins"`
	} `json:"window"`
}

// parseQuotaExhaustedResetsAt extracts the reset instant from a 429
// "Subscription quota exhausted" error. The Responses probe returns
// `{"error":{"code":"rate_limit_exceeded","message":"Subscription quota
// exhausted…","resets_at":1789344000}}` instead of an SSE stream when the
// account is at its limit. We surface the blocked state with the reset
// rather than fabricating 100% for both windows (P1-1).
func parseQuotaExhaustedResetsAt(body string) int64 {
	var payload struct {
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			ResetsAt int64  `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return 0
	}
	if payload.Error.Code != "rate_limit_exceeded" {
		return 0
	}
	if !strings.Contains(strings.ToLower(payload.Error.Message), "quota") {
		return 0
	}
	return payload.Error.ResetsAt
}

// quotaExhaustedError is the blocked-subscription signal from a 429
// "Subscription quota exhausted" probe response. Carries the reset instant
// as a typed field so callers don't string-parse it back out of an error.
type quotaExhaustedError struct {
	ResetsAt int64
}

func (e *quotaExhaustedError) Error() string {
	return fmt.Sprintf("quota exhausted, resets at %d", e.ResetsAt)
}

// postSubscriptionUsage sends the minimal streamed probe and returns the
// first response.subscription_usage event payload.
func postSubscriptionUsage(ctx context.Context, apiKey string) (*subscriptionUsage, int, error) {
	base := responsesBaseURL
	if responsesEndpointOverride != "" {
		base = responsesEndpointOverride
	}
	payload, err := json.Marshal(map[string]any{
		"model":  responsesProbeModel,
		"store":  false,
		"stream": true,
		"input":  "hi",
	})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := quotaHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		trimmed := strings.TrimSpace(string(body))
		// 429 with quota-exhausted is the blocked-subscription signal, not
		// a transient probe rate limit. Don't fabricate 100% for both windows
		// (P1-1); surface the blocked state with the reset so the provider
		// can render it without claiming measured percentages.
		if resp.StatusCode == http.StatusTooManyRequests {
			if resetsAt := parseQuotaExhaustedResetsAt(trimmed); resetsAt != 0 {
				return nil, resp.StatusCode, &quotaExhaustedError{ResetsAt: resetsAt}
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, shared.Truncate(trimmed, 160))
	}
	// Stream the SSE frames and stop at the usage event instead of reading
	// to EOF: provider polls run under a tight per-fetch timeout.
	var data string
	var event string
	scan := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			event = ""
			continue
		}
		if rest, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(rest)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			if rest == "[DONE]" || strings.TrimSpace(rest) == "[DONE]" {
				break
			}
			if event == "response.subscription_usage" {
				data = strings.TrimSpace(rest)
				break
			}
			continue
		}
	}
	if err := scan.Err(); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading usage stream: %w", err)
	}
	if data == "" {
		return nil, resp.StatusCode, fmt.Errorf("response stream contained no subscription_usage event")
	}
	var ev struct {
		Subscription subscriptionUsage `json:"subscription"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("cannot parse subscription_usage event: %w", err)
	}
	return &ev.Subscription, resp.StatusCode, nil
}

// keyBaseURL is the Meta account API root behind the CLI's own payment
// poll. POST <base>/muse-code/key with the OAuth token returns the
// subscription snapshot including the server-provided plan display name
// (subs_tier_name) — no inference, so unlike the Responses probe it costs
// no usage. The Model API key is rejected here (401); that path falls back
// to the Responses probe.
const keyBaseURL = "https://api.meta.ai"

// keyEndpointOverride swaps the account endpoint target in tests.
var keyEndpointOverride = ""

// keySubscription mirrors POST muse-code/key.
type keySubscription struct {
	SubsTierID         string `json:"subs_tier_id"`
	SubsTierName       string `json:"subs_tier_name"`
	IsSubsUpgradeAvail bool   `json:"is_subs_upgrade_available"`
	SubsUsage          struct {
		Window struct {
			UsedPercent        float64 `json:"used_percent"`
			ResetsAt           int64   `json:"resets_at"`
			WindowDurationMins int     `json:"window_duration_mins"`
		} `json:"window"`
		Weekly struct {
			UsedPercent float64 `json:"used_percent"`
			ResetsAt    int64   `json:"resets_at"`
		} `json:"weekly"`
		Tier string `json:"tier"`
	} `json:"subs_usage"`
}

// postKeySubscription fetches the account subscription snapshot. A 401
// means the OAuth token is expired or API-key-only: the caller falls back
// to the Responses probe rather than failing the poll.
func postKeySubscription(ctx context.Context, oauthToken string) (*keySubscription, int, error) {
	base := keyBaseURL
	if keyEndpointOverride != "" {
		base = keyEndpointOverride
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/muse-code/key", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+oauthToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := quotaHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, shared.Truncate(strings.TrimSpace(string(body)), 160))
	}
	var sub keySubscription
	if err := json.Unmarshal(body, &sub); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("cannot parse key subscription: %w", err)
	}
	return &sub, resp.StatusCode, nil
}

// subscriptionUsageFromKey converts the account snapshot to the shared
// subscription shape. Tier keeps the stable numeric ID (memory and
// attribution key off it on both paths); PlanName carries the
// server-provided display label through quotaPlanName's prefix trim.
func subscriptionUsageFromKey(k *keySubscription) *subscriptionUsage {
	sub := &subscriptionUsage{Tier: k.SubsUsage.Tier}
	if sub.Tier == "" {
		sub.Tier = k.SubsTierID
	}
	sub.PlanName = quotaPlanName(k.SubsTierName)
	sub.UpgradeAvailable = k.IsSubsUpgradeAvail
	sub.Weekly.UsedPercent = k.SubsUsage.Weekly.UsedPercent
	sub.Weekly.ResetsAt = k.SubsUsage.Weekly.ResetsAt
	sub.Window.UsedPercent = k.SubsUsage.Window.UsedPercent
	sub.Window.ResetsAt = k.SubsUsage.Window.ResetsAt
	sub.Window.WindowDurationMins = k.SubsUsage.Window.WindowDurationMins
	return sub
}

// quotaPlanName turns a raw tier into a display name: "Muse Code Everyday
// Usage" becomes "Everyday Usage". Opaque IDs (the Responses event carries
// an account-scoped tier ID, not a plan name) pass through unchanged rather
// than guessed at.
func quotaPlanName(tier string) string {
	trimmed := strings.TrimSpace(tier)
	if name := strings.TrimSpace(strings.TrimPrefix(trimmed, "Muse Code ")); name != "" {
		return name
	}
	return trimmed
}

// PlanNamePathKey is the provider_paths key for a user-attested plan label
// ("Everyday Usage", "High Usage", "Power Usage"). The quota probe only
// returns an opaque account-scoped tier ID, so the display name cannot be
// derived — the subscriber sets it once and it wins over the tier-derived
// value everywhere the tile reads plan_name.
const PlanNamePathKey = "plan_name"

// applyPlanNameOverride stamps the user-attested plan label onto the
// snapshot, winning over the tier-derived plan_name from enrichment.
func applyPlanNameOverride(acct core.AccountConfig, snap *core.UsageSnapshot) {
	name := strings.TrimSpace(acct.Path(PlanNamePathKey, ""))
	if name == "" {
		return
	}
	snap.EnsureMaps()
	snap.Raw["plan_name"] = name
}

func applySubscriptionUsage(snap *core.UsageSnapshot, sub *subscriptionUsage) {
	snap.EnsureMaps()
	rememberQuotaMemory(sub)
	hundred := 100.0
	window, weekly := sub.Window.UsedPercent, sub.Weekly.UsedPercent
	snap.Metrics["muse.session"] = core.Metric{Used: &window, Limit: &hundred, Unit: "quota", Window: "session"}
	if sub.Window.ResetsAt > 0 {
		snap.Resets["muse.session"] = time.Unix(sub.Window.ResetsAt, 0).UTC()
	}
	snap.Metrics["muse.weekly"] = core.Metric{Used: &weekly, Limit: &hundred, Unit: "quota", Window: "weekly"}
	if sub.Weekly.ResetsAt > 0 {
		snap.Resets["muse.weekly"] = time.Unix(sub.Weekly.ResetsAt, 0).UTC()
	}
	if sub.Tier != "" {
		snap.SetAttribute("muse_quota_tier", sub.Tier)
		if snap.Raw == nil {
			snap.Raw = make(map[string]string)
		}
		name := sub.PlanName
		if name == "" {
			name = quotaPlanName(sub.Tier)
		}
		snap.Raw["plan_name"] = name
	}
	// The server reports whole percentages, floored: a live 99% reads as
	// exhausted while the gauge still suggests 1% left (real requests 429
	// against the sliver). Match the app's own "Usage limit reached"
	// language and surface the reset instead of a healthy-looking 99%.
	// Measured values stay untouched — only the blocked state is added,
	// reusing the muse_quota_blocked key so the LIMIT header follows.
	markFlooredExhaustion(snap, "weekly", sub.Weekly.UsedPercent, sub.Weekly.ResetsAt, sub.UpgradeAvailable)
	markFlooredExhaustion(snap, "session", sub.Window.UsedPercent, sub.Window.ResetsAt, sub.UpgradeAvailable)
	if summary := quotaSummary(snap); summary != "quota n/a" {
		if snap.Message != "" {
			snap.Message += " · "
		}
		snap.Message += summary
	}
}

// tryKeySubscription polls the account endpoint when an OAuth token is
// available. Decided means the quota outcome is settled; unauthorized
// means the token was rejected (as opposed to a transient failure the
// probe's stale-memory path handles better) and the caller may want to
// re-bootstrap the token before falling through to the Responses probe.
func tryKeySubscription(ctx context.Context, snap *core.UsageSnapshot, oauth string) (decided, unauthorized bool) {
	sub, status, err := postKeySubscription(ctx, oauth)
	if err != nil {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			snap.SetDiagnostic("muse_quota_oauth", "OAuth token rejected — re-authenticate via `muse login`; falling back to API-key probe")
			return false, true
		}
		snap.SetDiagnostic("muse_quota_key", fmt.Sprintf("account endpoint failed: %v", shared.Truncate(err.Error(), 120)))
		return false, false
	}
	applySubscriptionUsage(snap, subscriptionUsageFromKey(sub))
	return true, false
}

// museUpgradeURL is the accounts-center upsell link from the app's own
// limit-reached message ("... /upgrade (<url>) for increased limits ...").
const museUpgradeURL = "https://accountscenter.meta.com/muse_code/?ep=xgrade"

// flooredExhaustionThreshold is the reported whole percent at which the
// bucket reads as exhausted. The server floors, so a live 99% means
// 99.0–99.99% with no usable room for real requests.
const flooredExhaustionThreshold = 99.0

// markFlooredExhaustion surfaces the app's "Usage limit reached" state for
// a window whose reported percentage sits at the floor threshold. It sets
// the shared muse_quota_blocked diagnostic (driving the LIMIT header)
// without touching the measured gauge values.
func markFlooredExhaustion(snap *core.UsageSnapshot, window string, usedPercent float64, resetsAt int64, upgradeAvailable bool) {
	if usedPercent < flooredExhaustionThreshold || resetsAt <= 0 {
		return
	}
	if _, ok := snap.Diagnostics["muse_quota_blocked"]; ok {
		return // 429 path already stated the block with its own reset
	}
	resetLocal := time.Unix(resetsAt, 0).Local().Format("Jan 2 at 3:04 PM")
	var msg string
	if upgradeAvailable {
		msg = fmt.Sprintf("Usage limit reached · /upgrade (%s) for increased limits, or wait for %s usage to reset at %s", museUpgradeURL, window, resetLocal)
	} else {
		msg = fmt.Sprintf("Usage limit reached, wait for %s usage to reset at %s", window, resetLocal)
	}
	snap.EnsureMaps()
	snap.SetDiagnostic("muse_quota_blocked", msg)
	snap.SetAttribute("muse_quota_blocked_resets_at", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339))
}

// trySubscriptionUsage polls the account endpoint first (OAuth, no
// inference cost), then the Responses SSE probe when an API key is
// available. It reports true when the quota outcome is decided either way,
// so enrichQuota only falls through to the legacy dashboard-cookie path when
// no key exists.
func trySubscriptionUsage(ctx context.Context, snap *core.UsageSnapshot) bool {
	// OAuth account endpoint first: same percentages plus the
	// server-provided plan name. Falls through to the Responses probe
	// when OAuth is absent or fails.
	if oauth, ok := loadMuseOAuthToken(ctx); ok && oauth != "" {
		decided, unauthorized := tryKeySubscription(ctx, snap, oauth)
		if decided {
			return true
		}
		if unauthorized {
			// The owned copy may have gone stale while the CLI refreshed
			// its own token. Re-bootstrap once from CLI sources and retry
			// before paying for a probe.
			if fresh, ok := refreshMuseOAuthToken(ctx); ok && fresh != "" && fresh != oauth {
				if decided, _ := tryKeySubscription(ctx, snap, fresh); decided {
					delete(snap.Diagnostics, "muse_quota_oauth")
					return true
				}
			}
		}
	}
	key, ok := loadMuseAPIKey(ctx)
	if !ok {
		return false
	}
	sub, status, err := postSubscriptionUsage(ctx, key)
	if err != nil {
		// Quota exhausted is a known blocked state, not a transient probe
		// failure. Don't fabricate 100% for both windows (P1-1); surface the
		// blocked state with the reset so the TUI can render it without
		// claiming measured percentages.
		var exhausted *quotaExhaustedError
		if status == http.StatusTooManyRequests && errors.As(err, &exhausted) {
			resetsAt := exhausted.ResetsAt
			if resetsAt > 0 {
				// The 429 names no window, so attribute it with memory: a
				// reset far sooner than the remembered weekly reset is the
				// session window exhausting while the week still has room.
				if session, mem := classifyQuotaExhaustion(resetsAt, time.Now()); session {
					snap.EnsureMaps()
					hundred := 100.0
					snap.Metrics["muse.session"] = core.Metric{Used: &hundred, Limit: &hundred, Unit: "quota", Window: "session"}
					snap.Resets["muse.session"] = time.Unix(resetsAt, 0).UTC()
					weekly := mem.weeklyUsed
					snap.Metrics["muse.weekly"] = core.Metric{Used: &weekly, Limit: &hundred, Unit: "quota", Window: "weekly"}
					snap.Resets["muse.weekly"] = time.Unix(mem.weeklyResetUnix, 0).UTC()
					snap.SetDiagnostic("muse_quota_blocked", fmt.Sprintf("session quota exhausted, resets at %s", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339)))
					snap.SetDiagnostic("muse_quota_weekly_stale", fmt.Sprintf("weekly %.0f%% as of %s; live probe blocked", mem.weeklyUsed, mem.observedAt.Format("15:04")))
					snap.SetAttribute("muse_quota_blocked_resets_at", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339))
					if summary := quotaSummary(snap); summary != "quota n/a" {
						if snap.Message != "" {
							snap.Message += " · "
						}
						snap.Message += summary
					}
					return true
				}
				// No (or matching) memory: the reset belongs to the week, or
				// can't be placed. Surface the weekly window as 100% with the
				// reset from the error. We don't fabricate both windows at
				// 100%; the diagnostic makes the blocked state explicit.
				snap.EnsureMaps()
				hundred := 100.0
				snap.Metrics["muse.weekly"] = core.Metric{Used: &hundred, Limit: &hundred, Unit: "quota", Window: "weekly"}
				snap.Resets["muse.weekly"] = time.Unix(resetsAt, 0).UTC()
				snap.SetDiagnostic("muse_quota_blocked", fmt.Sprintf("quota exhausted, resets at %s", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339)))
				snap.SetAttribute("muse_quota_blocked_resets_at", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339))
				if summary := quotaSummary(snap); summary != "quota n/a" {
					if snap.Message != "" {
						snap.Message += " · "
					}
					snap.Message += summary
				}
				return true
			}
			snap.SetDiagnostic("muse_quota_blocked", "quota exhausted")
			return true
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			// A rejected key may mean a different account: never carry
			// memory across it.
			snap.SetDiagnostic("muse_quota_auth", "quota API key rejected — re-authenticate via `muse login`, then re-poll")
		} else if mem, ok := currentQuotaMemory(time.Now()); ok {
			// Transient probe failure with valid memory: carry the
			// remembered meters forward marked stale instead of wiping
			// the gauges and flipping the tile to Credits.
			snap.EnsureMaps()
			hundred := 100.0
			if mem.windowResetUnix > 0 {
				window := mem.windowUsed
				snap.Metrics["muse.session"] = core.Metric{Used: &window, Limit: &hundred, Unit: "quota", Window: "session"}
				snap.Resets["muse.session"] = time.Unix(mem.windowResetUnix, 0).UTC()
			}
			weekly := mem.weeklyUsed
			snap.Metrics["muse.weekly"] = core.Metric{Used: &weekly, Limit: &hundred, Unit: "quota", Window: "weekly"}
			snap.Resets["muse.weekly"] = time.Unix(mem.weeklyResetUnix, 0).UTC()
			snap.SetDiagnostic("muse_quota_stale", fmt.Sprintf("quota as of %s; live probe failed: %v", mem.observedAt.Format("15:04"), shared.Truncate(err.Error(), 120)))
			if summary := quotaSummary(snap); summary != "quota n/a" {
				if snap.Message != "" {
					snap.Message += " · "
				}
				snap.Message += summary
			}
		} else {
			snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("quota probe failed: %v", shared.Truncate(err.Error(), 160)))
		}
		return true
	}
	applySubscriptionUsage(snap, sub)
	return true
}

// enrichQuota adds live quota (Responses SSE) to the snapshot.
// It is non-fatal: on any failure it records a diagnostic and leaves the
// local spend meters untouched. No browser session is required.
func enrichQuota(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	// OAuth account endpoint first, Responses probe fallback: POST
	// api.meta.ai/v1/responses with the keychain or file API key. No
	// GraphQL fallback — that path required opening dev.meta.ai
	// periodically and is intentionally removed. The history remains in
	// git (MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md) for reference.
	if trySubscriptionUsage(ctx, snap) {
		return
	}
	// No API key: quota is unavailable, but local spend is still valid.
	// Keep the diagnostic minimal and honest — don't imply a browser
	// visit would fix it when the clean path is just `muse login` or
	// `META_API_KEY` / `~/.config/openusage/muse.json`.
	snap.SetDiagnostic("muse_quota", "quota unavailable — run `muse login` or set META_API_KEY / ~/.config/openusage/muse.json to enable quota")
}

func quotaSummary(snap *core.UsageSnapshot) string {
	parts := []string{}
	for _, key := range []string{"muse.session", "muse.weekly"} {
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil || m.Limit == nil || *m.Limit <= 0 {
			continue
		}
		short := strings.TrimPrefix(key, "muse.")
		parts = append(parts, fmt.Sprintf("quota %s %.0f%%", short, *m.Used / *m.Limit * 100))
	}
	if len(parts) == 0 {
		return "quota n/a"
	}
	return strings.Join(parts, " / ")
}
