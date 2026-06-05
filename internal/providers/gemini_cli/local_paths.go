package gemini_cli

import (
	"os"
	"path/filepath"
)

// LocalSourcePaths returns the file system locations the provider reads on
// each Fetch. Mirrors the path resolution in Provider.Fetch (see
// gemini_cli.go) for the default (no `config_dir` hint) case.
func (p *Provider) LocalSourcePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	configDir := filepath.Join(home, ".gemini")
	return []string{
		filepath.Join(configDir, "antigravity", "conversations"),
		filepath.Join(configDir, "tmp"),
		filepath.Join(configDir, "settings.json"),
		filepath.Join(configDir, "oauth_creds.json"),
	}
}
