package detect

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
//
// Both "opencode" (the Zen catalog) and "opencode-go" (the lower-cost Go
// subscription) land on the same openusage account id because they share the
// OPENCODE_API_KEY env var upstream and there's no operational benefit to
// representing them as two separate tiles — they hit the same Zen models
// endpoint with the same key (see github.com/anomalyco/opencode dialog-
// provider.tsx and our provider.go).
var opencodeAuthMapping = map[string]struct {
	Provider  string
	AccountID string
}{
	"moonshotai":   {"moonshot", "moonshot-ai"},
	"openrouter":   {"openrouter", "openrouter"},
	"zai":          {"zai", "zai"},
	"opencode":     {"opencode", "opencode"},
	"opencode-go":  {"opencode", "opencode"},
	"ollama-cloud": {"ollama", "ollama-cloud"},
}

// opencodeAuthPaths returns every platform-appropriate candidate path for
// OpenCode's auth.json, in priority order. Detection short-circuits on the
// first one that exists.
//
// XDG_DATA_HOME, if set, wins on every platform. The remaining per-OS
// candidates are provided by opencodeAuthPlatformPaths() in the
// opencode_auth_paths_*.go files. OpenCode resolves its data dir through the
// `xdg-basedir` JS package, which has no Windows special-case and therefore
// writes to ~/.local/share/opencode/auth.json even on Windows (see
// anomalyco/opencode#8235) — hence the XDG-style default is probed first there.
func opencodeAuthPaths() []string {
	home := homeDir()
	if home == "" {
		return nil
	}

	var paths []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "opencode", "auth.json"))
	}
	paths = append(paths, opencodeAuthPlatformPaths(home)...)

	// Deduplicate while preserving order (XDG_DATA_HOME may resolve to the
	// same path as the default).
	seen := make(map[string]struct{}, len(paths))
	out := paths[:0]
	for _, p := range paths {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// opencodeAuthPath returns the first existing candidate from
// opencodeAuthPaths(). Kept for backward compatibility with call sites and
// tests that wanted a single "the path" string; callers wanting fallback
// behaviour should iterate opencodeAuthPaths() directly.
func opencodeAuthPath() string {
	for _, p := range opencodeAuthPaths() {
		if fileExists(p) {
			return p
		}
	}
	// Nothing on disk — return the first candidate so callers that surface
	// "expected here" diagnostics have something to show.
	if paths := opencodeAuthPaths(); len(paths) > 0 {
		return paths[0]
	}
	return ""
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

	// Auto-detect all active opencode-box container profiles in ~/.opencode-containers
	containersDir := filepath.Join(homeDir(), ".opencode-containers")
	if dirExists(containersDir) {
		entries, err := os.ReadDir(containersDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				boxName := entry.Name()
				boxAuthFile := filepath.Join(containersDir, boxName, "share", "auth.json")
				boxDBFile := filepath.Join(containersDir, boxName, "share", "opencode.db")

				acct := core.AccountConfig{
					ID:           fmt.Sprintf("opencode-%s", boxName),
					Provider:     "opencode",
					Auth:         "api_key",
					RuntimeHints: make(map[string]string),
				}
				acct.SetHint("db_path", boxDBFile)
				acct.SetHint("auth_path", boxAuthFile)
				acct.SetHint("container", boxName)

				if fileExists(boxAuthFile) {
					if authData, err := os.ReadFile(boxAuthFile); err == nil {
						var boxRaw map[string]json.RawMessage
						if err := json.Unmarshal(authData, &boxRaw); err == nil {
							for _, keyName := range []string{"opencode-go", "opencode"} {
								if slot, ok := boxRaw[keyName]; ok {
									var boxEntry opencodeAuthEntry
									if err := json.Unmarshal(slot, &boxEntry); err == nil && boxEntry.Key != "" {
										acct.Token = boxEntry.Key
										acct.SetHint("credential_source", "opencode_box_auth_json")
										break
									}
								}
							}
						}
					}
				}
				addAccount(result, acct)
			}
		}
	}
}
