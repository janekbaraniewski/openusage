package opencode

import (
	"os"
	"path/filepath"
)

// LocalSourcePaths returns the on-disk locations the provider reads. Mirrors
// the defaults in DefaultCollectOptions (see telemetry.go); active-tool
// detection only needs to know whether any of these paths have recent
// activity.
func (p *Provider) LocalSourcePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "opencode", "opencode.db"),
		filepath.Join(home, ".opencode", "events"),
		filepath.Join(home, ".opencode", "logs"),
		filepath.Join(home, ".local", "state", "opencode", "events"),
		filepath.Join(home, ".local", "state", "opencode", "logs"),
	}
}
