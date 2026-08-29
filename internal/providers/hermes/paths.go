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

// resolveDBPath returns the first existing candidate path on disk, preferring
// any explicit per-account override stored in AccountConfig.
//
// Returns "" when no candidate exists; callers should treat that as "no local
// data" rather than an error.
func resolveDBPath(acct core.AccountConfig) string {
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
