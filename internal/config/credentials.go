package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Credentials struct {
	Keys     map[string]string         `json:"keys"`               // account ID → API key
	Sessions map[string]BrowserSession `json:"sessions,omitempty"` // account ID → browser-session credential
}

// BrowserSession stores a single account's browser-session credential. Used
// by providers whose dashboard data is gated by session cookies — see
// docs/BROWSER_SESSION_AUTH_DESIGN.md. The cookie value lives only in this
// file (not in settings.json), and the file is written with 0o600 perms;
// that's the same encryption-at-rest posture as the existing api-key store.
type BrowserSession struct {
	// Domain and CookieName are mirrors of the AccountConfig.BrowserCookie
	// reference, persisted here so the credential is self-contained
	// (re-extraction works even if settings.json is regenerated).
	Domain     string `json:"domain"`
	CookieName string `json:"cookie_name"`

	// Value is the cookie value. Treated as a high-sensitivity credential.
	Value string `json:"value"`

	// SourceBrowser is the canonical browser name the cookie was last
	// extracted from ("chrome", "firefox", etc.). Used as a hint to the
	// extractor so it tries that browser first on the next refresh and
	// avoids triggering keychain prompts on others.
	SourceBrowser string `json:"source_browser,omitempty"`

	// CapturedAt is when openusage last successfully extracted this cookie
	// from the browser. ExpiresAt is the cookie's own Set-Cookie expiry —
	// zero for session-only cookies. Both are RFC3339 strings on the wire
	// for human readability.
	CapturedAt string `json:"captured_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// credMu guards read-modify-write cycles on the credentials file.
var credMu sync.Mutex

func CredentialsPath() string {
	return filepath.Join(ConfigDir(), "credentials.json")
}

func LoadCredentials() (Credentials, error) {
	return LoadCredentialsFrom(CredentialsPath())
}

func LoadCredentialsFrom(path string) (Credentials, error) {
	creds := Credentials{Keys: make(map[string]string)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return creds, nil
		}
		return creds, fmt.Errorf("reading credentials: %w", err)
	}

	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{Keys: make(map[string]string)}, fmt.Errorf("parsing credentials %s: %w", path, err)
	}

	if creds.Keys == nil {
		creds.Keys = make(map[string]string)
	}
	if creds.Sessions == nil {
		creds.Sessions = make(map[string]BrowserSession)
	}
	if len(creds.Keys) > 0 {
		normalized := make(map[string]string, len(creds.Keys))
		for accountID, key := range creds.Keys {
			id := normalizeAccountID(accountID)
			if id == "" {
				continue
			}
			if _, exists := normalized[id]; !exists || accountID == id {
				normalized[id] = key
			}
		}
		creds.Keys = normalized
	}

	return creds, nil
}

func SaveCredential(accountID, apiKey string) error {
	return SaveCredentialTo(CredentialsPath(), accountID, apiKey)
}

func SaveCredentialTo(path, accountID, apiKey string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api key is empty")
	}

	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		creds = Credentials{Keys: make(map[string]string)}
	}

	creds.Keys[accountID] = apiKey

	return writeCredentials(path, creds)
}

func DeleteCredential(accountID string) error {
	return DeleteCredentialFrom(CredentialsPath(), accountID)
}

func DeleteCredentialFrom(path, accountID string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return err
	}

	delete(creds.Keys, accountID)

	return writeCredentials(path, creds)
}

// SaveSession persists a browser-session credential under the given account.
// The credential is encrypted-at-rest only via filesystem perms (0o600) —
// same posture as API keys in this store. Cookie values must never travel
// outside this file or the runtime memory of the daemon.
func SaveSession(accountID string, session BrowserSession) error {
	return SaveSessionTo(CredentialsPath(), accountID, session)
}

func SaveSessionTo(path, accountID string, session BrowserSession) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}
	if strings.TrimSpace(session.Value) == "" {
		return fmt.Errorf("session value is empty")
	}
	if strings.TrimSpace(session.Domain) == "" || strings.TrimSpace(session.CookieName) == "" {
		return fmt.Errorf("session domain and cookie_name are required")
	}

	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		creds = Credentials{Keys: make(map[string]string), Sessions: make(map[string]BrowserSession)}
	}
	if creds.Sessions == nil {
		creds.Sessions = make(map[string]BrowserSession)
	}
	creds.Sessions[accountID] = session
	return writeCredentials(path, creds)
}

// DeleteSession removes a browser-session credential. Safe to call when no
// entry exists.
func DeleteSession(accountID string) error {
	return DeleteSessionFrom(CredentialsPath(), accountID)
}

func DeleteSessionFrom(path, accountID string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return err
	}
	delete(creds.Sessions, accountID)
	return writeCredentials(path, creds)
}

// LoadSession returns the stored browser-session credential for an account
// along with a found flag. Use this rather than poking creds.Sessions
// directly so the normalization / lookup stays in one place.
func LoadSession(accountID string) (BrowserSession, bool, error) {
	return LoadSessionFrom(CredentialsPath(), accountID)
}

func LoadSessionFrom(path, accountID string) (BrowserSession, bool, error) {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return BrowserSession{}, false, fmt.Errorf("account ID is empty")
	}
	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return BrowserSession{}, false, err
	}
	s, ok := creds.Sessions[accountID]
	return s, ok, nil
}

func writeCredentials(path string, creds Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	// Enforce permissions even if the file pre-existed with wrong mode.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("setting credentials permissions: %w", err)
	}
	return nil
}
