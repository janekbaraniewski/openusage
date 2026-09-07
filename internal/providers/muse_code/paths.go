package muse_code

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	// PathHintSessionsDirKey overrides the sessions directory per account.
	PathHintSessionsDirKey = "sessions_dir"
	// PathHintConfigDirKey overrides the muse config directory per account.
	PathHintConfigDirKey = "config_dir"
)

// resolveSessionsDirs returns the session-log directories to scan: the
// per-account override when set, else the standard location. Only existing
// directories are returned.
func resolveSessionsDirs(acct core.AccountConfig) []string {
	if override := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return []string{override}
		}
		return nil
	}
	dir := defaultSessionsDir()
	if dir == "" || !dirExists(dir) {
		return nil
	}
	return []string{dir}
}

// defaultSessionsDir mirrors muse itself: $XDG_DATA_HOME/muse/sessions,
// else ~/.local/share/muse/sessions.
func defaultSessionsDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "muse", "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "muse", "sessions")
}

// defaultConfigDir mirrors muse itself: $MUSE_CONFIG_DIR, else
// $XDG_CONFIG_HOME/muse, else ~/.config/muse.
func defaultConfigDir() string {
	if override := strings.TrimSpace(os.Getenv("MUSE_CONFIG_DIR")); override != "" {
		return override
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "muse")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "muse")
}

// authFilePath is the credential file written by `muse login`
// (Meta-account device flow) or `muse auth set`. MUSE_AUTH_PATH names the
// file directly when set.
func authFilePath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(os.Getenv("MUSE_AUTH_PATH")); override != "" {
		return override
	}
	if override := strings.TrimSpace(acct.Path(PathHintConfigDirKey, "")); override != "" {
		return filepath.Join(override, "auth.json")
	}
	if dir := defaultConfigDir(); dir != "" {
		return filepath.Join(dir, "auth.json")
	}
	return ""
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
