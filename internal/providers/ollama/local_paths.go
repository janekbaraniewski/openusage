package ollama

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func resolveDesktopDBPath(acct core.AccountConfig) string {
	for _, key := range []string{"db_path", "app_db"} {
		if v := strings.TrimSpace(acct.Hint(key, "")); v != "" {
			return v
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Ollama", "db.sqlite")
	case "linux":
		candidates := []string{
			filepath.Join(home, ".local", "share", "Ollama", "db.sqlite"),
			filepath.Join(home, ".config", "Ollama", "db.sqlite"),
		}
		for _, c := range candidates {
			if fileExists(c) {
				return c
			}
		}
		return candidates[0]
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Ollama", "db.sqlite")
		}
		return filepath.Join(home, "AppData", "Roaming", "Ollama", "db.sqlite")
	default:
		return filepath.Join(home, ".ollama", "db.sqlite")
	}
}

func resolveServerConfigPath(acct core.AccountConfig) string {
	if v := strings.TrimSpace(acct.Hint("server_config", "")); v != "" {
		return v
	}
	if configDir := strings.TrimSpace(acct.Hint("config_dir", "")); configDir != "" {
		return filepath.Join(configDir, "server.json")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ollama", "server.json")
}

func resolveServerLogFiles(acct core.AccountConfig) []string {
	logDir := strings.TrimSpace(acct.Hint("logs_dir", ""))
	if logDir == "" {
		if configDir := strings.TrimSpace(acct.Hint("config_dir", "")); configDir != "" {
			logDir = filepath.Join(configDir, "logs")
		}
	}
	if logDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		logDir = filepath.Join(home, ".ollama", "logs")
	}

	pattern := filepath.Join(logDir, "server*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(files)
	return files
}
