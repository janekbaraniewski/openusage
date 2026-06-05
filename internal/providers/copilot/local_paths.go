package copilot

import (
	"os"
	"path/filepath"
)

// LocalSourcePaths returns the on-disk locations the provider reads when no
// config_dir hint is provided. Mirrors the resolution in fetchLocalData (see
// copilot.go).
func (p *Provider) LocalSourcePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	copilotDir := filepath.Join(home, ".copilot")
	return []string{
		filepath.Join(copilotDir, "logs"),
		filepath.Join(copilotDir, "session-state"),
		filepath.Join(copilotDir, "config.json"),
	}
}
