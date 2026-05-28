package qwen_cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const PathHintProjectsDirKey = "projects_dir"

func defaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".qwen", "projects")
}

func resolveProjectsDir(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintProjectsDirKey, "")); override != "" {
		if dirExists(override) {
			return override
		}
	}
	if def := defaultProjectsDir(); def != "" && dirExists(def) {
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
