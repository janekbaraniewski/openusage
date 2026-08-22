package detect

import (
	"log"
	"os"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// makoraStatePath returns the shared makora-usage token cache path.
func makoraStatePath() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "makora-usage", "state.json")
}

// detectMakora registers a Makora account when a session credential is
// present via any of its supported signals:
//   - MAKORA_SESSION_TOKEN env var (dashboard session JWT)
//   - the makora-usage token cache (~/.config/makora-usage/state.json)
//   - MAKORA_EMAIL + MAKORA_PASSWORD env vars (password login)
//   - the `makora-usage` helper binary on PATH
//
// The provider resolves the actual token at fetch time; detection only
// records that a credential path exists. The account id is "makora".
func detectMakora(result *Result) {
	bin := findBinary("makora-usage")
	state := makoraStatePath()
	hasState := state != "" && fileExists(state)
	hasSessionToken := os.Getenv("MAKORA_SESSION_TOKEN") != ""
	hasLogin := os.Getenv("MAKORA_EMAIL") != "" && os.Getenv("MAKORA_PASSWORD") != ""

	if !hasSessionToken && !hasState && !hasLogin && bin == "" {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found makora-usage at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "makora-usage",
			BinaryPath: bin,
			ConfigDir:  filepath.Dir(state),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:       "makora",
		Provider: "makora",
		Auth:     "token",
	}
	if hasState {
		acct.SetPath("state_file", state)
		acct.SetHint("state_file", state)
		log.Printf("[detect] Makora token cache at %s", state)
	}
	if hasSessionToken {
		acct.SetHint("auth_source", "env_session")
	}
	if hasLogin {
		acct.SetHint("auth_source", "password")
	}

	addAccount(result, acct)
}
