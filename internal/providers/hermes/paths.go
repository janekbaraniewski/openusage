package hermes

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDBKey is the AccountConfig path hint key used to override the
// resolved state.db location. Detectors set this on auto-detected accounts;
// users can also set it in their settings.json.
const PathHintDBKey = "db_path"

// defaultStateDBPaths returns candidate paths for Hermes's state.db file in
// priority order.
//
// Priority:
//  1. $HERMES_HOME/state.db (explicit env override)
//  2. ~/.hermes/state.db (the documented default)
func defaultStateDBPaths() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("HERMES_HOME")); root != "" {
		paths = append(paths, filepath.Join(root, "state.db"))
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return paths
	}
	paths = append(paths, filepath.Join(home, ".hermes", "state.db"))
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
	// configured — e.g. a second Hermes profile, or a path that is temporarily
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
	for _, candidate := range defaultStateDBPaths() {
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
	for _, candidate := range defaultStateDBPaths() {
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
