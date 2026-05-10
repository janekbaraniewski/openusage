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
func detectMacOSKeychainCredentials(result *Result) {
	probes := []struct {
		Service     string
		AccountID   string
		Provider    string
		Auth        string
		ProvenanceK string
	}{
		// Anthropic Claude Code CLI: stores OAuth credentials JSON.
		// Service confirmed via anthropics/claude-code issues #9403, #37512, #44089.
		{"Claude Code-credentials", "claude-code", "claude_code", "local",
			"keychain:Claude Code-credentials"},
	}

	for _, p := range probes {
		if !keychainGenericPasswordExists(p.Service) {
			continue
		}
		log.Printf("[detect] macOS keychain entry present: %s", p.Service)

		// Find an existing account for this provider; if found, just annotate.
		annotated := false
		for i := range result.Accounts {
			if result.Accounts[i].ID == p.AccountID {
				result.Accounts[i].SetHint("credential_source", p.ProvenanceK)
				annotated = true
				break
			}
		}
		if annotated {
			continue
		}

		// File-based detection didn't fire for this CLI (binary not on PATH,
		// config dir missing, etc). Register a minimal account so the provider
		// has something to bind to.
		acct := core.AccountConfig{
			ID:       p.AccountID,
			Provider: p.Provider,
			Auth:     p.Auth,
		}
		acct.SetHint("credential_source", p.ProvenanceK)
		// Best-effort: surface the home dir so the provider's local file
		// readers have a consistent default to fall back to.
		if home := homeDir(); home != "" {
			acct.SetPath("config_dir", filepath.Join(home, ".claude"))
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
