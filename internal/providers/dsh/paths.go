package dsh

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// PathHintSessionsDirKey overrides the resolved sessions directory location.
const PathHintSessionsDirKey = "sessions_dir"

// homeEnvVar relocates dsh's data directory away from ~/.dsh.
const homeEnvVar = "DSH_HOME"

// sessionFileName is the per-session log dsh writes under
// <sessions>/<project-slug>/<session-id>/. The version is part of the name;
// only the v4 event schema is understood here.
const sessionFileName = "session.v4.jsonl.zstd"

// defaultHomeDir returns dsh's data directory: $DSH_HOME when set, otherwise
// $HOME/.dsh.
func defaultHomeDir() string {
	if override := shared.ExpandHome(os.Getenv(homeEnvVar)); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".dsh")
}

// defaultSessionsDir returns the canonical location of dsh's session logs.
func defaultSessionsDir() string {
	home := defaultHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "sessions")
}

// resolveSessionsDir returns the path to the sessions directory, preferring
// an explicit per-account override.
//
// Returns "" when the directory does not exist; callers should treat that as
// "no local data" rather than an error.
func resolveSessionsDir(acct core.AccountConfig) string {
	if override := shared.ExpandHome(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return override
		}
	}
	if def := defaultSessionsDir(); def != "" && dirExists(def) {
		return def
	}
	return ""
}

func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
