package kimi_cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintSessionsDirKey overrides the resolved sessions directory location.
const PathHintSessionsDirKey = "sessions_dir"

// PathHintConfigPathKey overrides the resolved config.json location.
const PathHintConfigPathKey = "config_path"

// kimiDataDirNames lists the per-user data directories of the supported
// Kimi clients in preference order: ".kimi" is the original Python Kimi CLI,
// ".kimi-code" is the Kimi Code CLI.
var kimiDataDirNames = []string{".kimi", ".kimi-code"}

// defaultSessionsDir returns the canonical location of Kimi CLI's
// per-session wire.jsonl files: $HOME/.kimi/sessions, falling back to
// $HOME/.kimi-code/sessions for Kimi Code CLI installs.
func defaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	for _, name := range kimiDataDirNames {
		candidate := filepath.Join(home, name, "sessions")
		if dirExists(candidate) {
			return candidate
		}
	}
	return filepath.Join(home, kimiDataDirNames[0], "sessions")
}

// defaultConfigPath returns the canonical location of the client's
// config.json at $HOME/.kimi/config.json, falling back to
// $HOME/.kimi-code/config.json.
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	for _, name := range kimiDataDirNames {
		candidate := filepath.Join(home, name, "config.json")
		if fileExists(candidate) {
			return candidate
		}
	}
	return filepath.Join(home, kimiDataDirNames[0], "config.json")
}

// resolveSessionsDir returns the path to the sessions directory, preferring
// an explicit per-account override.
//
// Returns "" when the directory does not exist; callers should treat that as
// "no local data" rather than an error.
func resolveSessionsDir(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return override
		}
	}
	if def := defaultSessionsDir(); def != "" && dirExists(def) {
		return def
	}
	return ""
}

// resolveConfigPath returns the path to config.json, preferring an explicit
// per-account override. Returns "" when no readable file is found.
func resolveConfigPath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintConfigPathKey, "")); override != "" {
		if fileExists(override) {
			return override
		}
	}
	if def := defaultConfigPath(); def != "" && fileExists(def) {
		return def
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

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
