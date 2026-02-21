package detect

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectOllama(result *Result) {
	bin := findBinary("ollama")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".ollama")
	logsDir := filepath.Join(configDir, "logs")
	serverConfig := filepath.Join(configDir, "server.json")
	dbPath := defaultOllamaDBPath(home)

	result.Tools = append(result.Tools, DetectedTool{
		Name:       "Ollama",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	})

	log.Printf("[detect] Found Ollama at %s", bin)

	if !dirExists(configDir) && !fileExists(dbPath) {
		log.Printf("[detect] Ollama binary found but no local config/data directories found")
		return
	}

	acct := core.AccountConfig{
		ID:        "ollama-local",
		Provider:  "ollama",
		Auth:      "local",
		Binary:    bin,
		BaseURL:   "http://127.0.0.1:11434",
		APIKeyEnv: "OLLAMA_API_KEY",
		ExtraData: make(map[string]string),
	}

	acct.ExtraData["config_dir"] = configDir
	acct.ExtraData["cloud_base_url"] = "https://ollama.com"

	if fileExists(dbPath) {
		acct.ExtraData["db_path"] = dbPath
	}
	if dirExists(logsDir) {
		acct.ExtraData["logs_dir"] = logsDir
	}
	if fileExists(serverConfig) {
		acct.ExtraData["server_config"] = serverConfig
	}

	addAccount(result, acct)
}

func defaultOllamaDBPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Ollama", "db.sqlite")
	case "linux":
		candidates := []string{
			filepath.Join(home, ".local", "share", "Ollama", "db.sqlite"),
			filepath.Join(home, ".config", "Ollama", "db.sqlite"),
		}
		for _, candidate := range candidates {
			if fileExists(candidate) {
				return candidate
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
