//go:build darwin

package detect

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectMacOSKeychainCredentials probes well-known macOS keychain entries
// produced by AI CLIs and ensures we surface any account they imply.
//
// We do NOT extract the secret value here. Each consuming provider already
// knows how to read its own keychain entry at fetch time (claude_code does
// this in usage_api.go). This detector exists so:
//
//  1. Auto-detect picks up the account even if file-based detection missed
//     it — for example when the CLI's binary isn't on $PATH but its
//     keychain entry is present (SSH/devcontainer scenarios).
//  2. The `openusage detect` debug command can show the user which keychain
//     entries are populated and where each credential comes from.
//
// The probe uses /usr/bin/security with a short timeout. The user gets a
// keychain unlock prompt the first time; subsequent calls within the same
// session are silent.
// keychainProbe describes one well-known credential entry in the macOS
// keychain. Adding a new AI CLI's keychain integration is a one-liner here.
type keychainProbe struct {
	Service          string // keychain item service name
	AccountID        string // account ID we annotate or create
	Provider         string // provider ID for new-account path
	Auth             string // auth mode for new-account path
	ProvenanceSource string // value written to the credential_source hint
	DefaultConfigDir string // optional: relative-to-home dir set on new accounts
}

var keychainProbes = []keychainProbe{
	// Anthropic Claude Code CLI on macOS. Service name confirmed via
	// anthropics/claude-code issues #9403, #37512, #44089.
	{
		Service:          "Claude Code-credentials",
		AccountID:        "claude-code",
		Provider:         "claude_code",
		Auth:             "local",
		ProvenanceSource: "keychain:Claude Code-credentials",
		DefaultConfigDir: ".claude",
	},
	// OpenAI Codex CLI when cli_auth_credentials_store=keyring (the default
	// on macOS when keychain is reachable). The stored value is an OpenAI
	// OAuth access token; the codex provider reads its own auth.json and
	// can refresh as needed. We annotate so users can see where the secret
	// is held. Service confirmed via openai/codex issue #16728.
	{
		Service:          "Codex Auth",
		AccountID:        "codex-cli",
		Provider:         "codex",
		Auth:             "local",
		ProvenanceSource: "keychain:Codex Auth",
		DefaultConfigDir: ".codex",
	},
}

func detectMacOSKeychainCredentials(result *Result) {
	for _, p := range keychainProbes {
		if !keychainGenericPasswordExists(p.Service) {
			continue
		}
		log.Printf("[detect] macOS keychain entry present: %s", p.Service)

		// Annotate the existing account if file-based detection already
		// registered it.
		annotated := false
		for i := range result.Accounts {
			if result.Accounts[i].ID == p.AccountID {
				result.Accounts[i].SetHint("credential_source", p.ProvenanceSource)
				annotated = true
				break
			}
		}
		if annotated {
			continue
		}

		// File-based detection didn't fire (binary off PATH, config dir
		// missing, etc). Register a minimal account so the provider has
		// something to bind to.
		acct := core.AccountConfig{
			ID:       p.AccountID,
			Provider: p.Provider,
			Auth:     p.Auth,
		}
		acct.SetHint("credential_source", p.ProvenanceSource)
		if home := homeDir(); home != "" && p.DefaultConfigDir != "" {
			acct.SetPath("config_dir", filepath.Join(home, p.DefaultConfigDir))
		}
		addAccount(result, acct)
	}
}

// keychainGenericPasswordExists returns true if `security find-generic-password
// -s <service>` succeeds (i.e. an item with that service name exists). We
// don't request -g (the password) so this probe doesn't show the secret in
// stdout, doesn't decrypt anything, and triggers a quieter UX.
func keychainGenericPasswordExists(service string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
