package detect

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// opencodeAuthEntry mirrors one provider's slot inside OpenCode's auth.json.
// OpenCode stores either OAuth credentials (refresh + access + expires) or a
// raw API key under the same dict key. We only care about API-key entries
// here; OAuth handling for openai/anthropic/google would require token-
// exchange against opencode.ai's auth server and is a separate piece of work.
type opencodeAuthEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// opencodeAuthMapping maps an OpenCode auth.json provider key to the matching
// openusage provider id and the canonical account id we want the credential
// to land on. The account id is intentionally aligned with what
// detectEnvKeys produces — addAccount() de-dupes by id, so when the user
// has both an env var and an OpenCode-stored key the env-var path wins
// (it runs first in AutoDetect).
var opencodeAuthMapping = map[string]struct {
	Provider  string
	AccountID string
}{
	"moonshotai":   {"moonshot", "moonshot-ai"},
	"openrouter":   {"openrouter", "openrouter"},
	"zai":          {"zai", "zai"},
	"opencode":     {"opencode", "opencode"},
	"ollama-cloud": {"ollama", "ollama-cloud"},
}

// opencodeAuthPath returns the platform-appropriate path to OpenCode's
// auth.json. macOS and Linux use ~/.local/share/opencode/auth.json (the
// XDG state path OpenCode picks regardless of XDG_DATA_HOME on darwin).
// Windows isn't supported by OpenCode officially yet but if the file exists
// at %APPDATA%/opencode/auth.json we'll read it.
func opencodeAuthPath() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "opencode", "auth.json")
		}
		return filepath.Join(home, "AppData", "Roaming", "opencode", "auth.json")
	default:
		return filepath.Join(home, ".local", "share", "opencode", "auth.json")
	}
}

// detectOpenCodeAuth reads OpenCode's auth.json and registers an account for
// every provider whose entry is an API key (type=="api"). OAuth entries are
// skipped: openusage's anthropic/openai/google providers expect API keys for
// their poll-time probes; using OpenCode's chat-scoped OAuth tokens against
// /v1/usage / rate-limit endpoints would mostly 401.
func detectOpenCodeAuth(result *Result) {
	path := opencodeAuthPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[detect] OpenCode auth.json read error: %v", err)
		}
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("[detect] OpenCode auth.json parse error: %v", err)
		return
	}

	matched := 0
	skipped := 0
	for opencodeKey, target := range opencodeAuthMapping {
		slot, ok := raw[opencodeKey]
		if !ok {
			continue
		}
		var entry opencodeAuthEntry
		if err := json.Unmarshal(slot, &entry); err != nil {
			log.Printf("[detect] OpenCode auth.json[%s] parse error: %v", opencodeKey, err)
			continue
		}
		if entry.Type != "api" {
			// OAuth or unrecognised; surface counts but don't try to use it.
			skipped++
			continue
		}
		if entry.Key == "" {
			continue
		}

		// Token is a runtime-only field (json:"-"); it lives in the account
		// in-memory and is re-populated on each AutoDetect run.
		acct := core.AccountConfig{
			ID:       target.AccountID,
			Provider: target.Provider,
			Auth:     "api_key",
			Token:    entry.Key,
		}
		acct.SetHint("credential_source", "opencode_auth_json")

		// addAccount de-dupes by ID, so if env-var detection already put
		// something on the same slot, this is a no-op — env var wins.
		before := len(result.Accounts)
		addAccount(result, acct)
		if len(result.Accounts) > before {
			matched++
			masked := maskKey(entry.Key)
			log.Printf("[detect] OpenCode auth.json: %s → %s/%s (key=%s)",
				opencodeKey, target.Provider, target.AccountID, masked)
		}
	}
	if matched > 0 || skipped > 0 {
		log.Printf("[detect] OpenCode auth.json: %d api-key accounts adopted, %d oauth/other entries skipped", matched, skipped)
	}
}
