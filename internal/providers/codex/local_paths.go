package codex

import (
	"os"
	"path/filepath"
)

// LocalSourcePaths returns the on-disk locations the provider reads. Mirrors
// the resolution in Provider.Fetch (see codex.go) for the default,
// no-account-hint case. Used by internal/tmux active-tool detection.
func (p *Provider) LocalSourcePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	configDir := filepath.Join(home, defaultCodexConfigDir)
	return []string{
		filepath.Join(configDir, "sessions"),
		filepath.Join(configDir, "version.json"),
		filepath.Join(configDir, "auth.json"),
		filepath.Join(configDir, "config.toml"),
	}
}
