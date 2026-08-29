package zed

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDBKey is the AccountConfig path hint key used to override the
// resolved threads.db location. Detectors set this on auto-detected accounts;
// users can also set it in their settings.json.
const PathHintDBKey = "db_path"

// defaultThreadDBPaths returns candidate paths for Zed's threads.db file in
// priority order, per Zed's published data-locations docs.
//
//	macOS:   ~/Library/Application Support/Zed/threads/threads.db
//	Linux:   $XDG_DATA_HOME/zed/threads/threads.db
//	         (fallback ~/.local/share/zed/threads/threads.db)
//	Windows: %LOCALAPPDATA%\Zed\threads\threads.db
func defaultThreadDBPaths() []string {
	var paths []string
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			paths = append(paths,
				filepath.Join(home, "Library", "Application Support", "Zed", "threads", "threads.db"),
			)
		}
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "Zed", "threads", "threads.db"))
		}
		if home != "" {
			paths = append(paths, filepath.Join(home, "AppData", "Local", "Zed", "threads", "threads.db"))
		}
	default:
		// Linux, FreeBSD, etc.
		if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
			paths = append(paths, filepath.Join(xdg, "zed", "threads", "threads.db"))
		}
		if home != "" {
			paths = append(paths, filepath.Join(home, ".local", "share", "zed", "threads", "threads.db"))
		}
	}
	return paths
}

// resolveDBPath returns the database path for the account. An explicit
// per-account override wins outright; otherwise the default candidate paths
// are searched in priority order for the first that exists.
//
// Returns "" when the override is missing on disk, or when no override is set
// and no candidate exists; callers should treat that as "no local data" rather
// than an error.
func resolveDBPath(acct core.AccountConfig) string {
	// An explicit override is authoritative: when the user (or a detector)
	// names a database, that is the database. Falling through to the default
	// locations here would silently read a *different* database than the one
	// configured — e.g. a second Zed profile, or a path that is temporarily
	// unmounted — and report its usage as though it were the configured
	// account's. Returning "" instead surfaces an honest "not found".
	//
	// Detectors only set this hint when the file exists and re-run on every
	// startup, so this costs auto-detected accounts nothing.
	if override := strings.TrimSpace(acct.Path(PathHintDBKey, "")); override != "" {
		if fileExists(override) {
			return override
		}
		return ""
	}
	for _, candidate := range defaultThreadDBPaths() {
		if candidate == "" {
			continue
		}
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// firstCandidatePath returns the first candidate path regardless of whether
// it exists. Used by detectors when surfacing "expected location" hints.
func firstCandidatePath() string {
	for _, candidate := range defaultThreadDBPaths() {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
