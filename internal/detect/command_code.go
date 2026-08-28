package detect

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectCommandCode(result *Result) {
	home := homeDir()
	cmdcConfigDir := ""
	if home != "" {
		cDir := filepath.Join(home, ".commandcode")
		if info, err := os.Stat(cDir); err == nil && info.IsDir() {
			cmdcConfigDir = cDir
		}
	}

	binPath, _ := exec.LookPath("cmdc")
	if binPath == "" {
		binPath, _ = exec.LookPath("commandcode")
	}

	if binPath == "" && cmdcConfigDir == "" {
		return
	}

	result.Tools = append(result.Tools, DetectedTool{
		Name:       "Command Code CLI",
		BinaryPath: binPath,
		ConfigDir:  cmdcConfigDir,
		Type:       "cli",
	})

	var apiKey string

	// 1. Env var check
	if envKey := os.Getenv("COMMAND_CODE_API_KEY"); strings.TrimSpace(envKey) != "" {
		apiKey = strings.TrimSpace(envKey)
	}

	// 2. ~/.commandcode/auth.json check
	if apiKey == "" && home != "" {
		authPath := filepath.Join(home, ".commandcode", "auth.json")
		if raw, err := os.ReadFile(authPath); err == nil {
			var payload map[string]any
			if json.Unmarshal(raw, &payload) == nil {
				if k, ok := payload["apiKey"].(string); ok && strings.TrimSpace(k) != "" {
					apiKey = strings.TrimSpace(k)
				} else if k, ok := payload["key"].(string); ok && strings.TrimSpace(k) != "" {
					apiKey = strings.TrimSpace(k)
				}
			}
		}
	}

	// 3. ~/.secrets/commandcode.env check
	if apiKey == "" && home != "" {
		secPath := filepath.Join(home, ".secrets", "commandcode.env")
		if raw, err := os.ReadFile(secPath); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "COMMAND_CODE_API_KEY=") {
					k := strings.TrimPrefix(line, "COMMAND_CODE_API_KEY=")
					k = strings.Trim(k, `"' `)
					if k != "" {
						apiKey = k
						break
					}
				}
			}
		}
	}

	acct := core.AccountConfig{
		ID:        "command_code",
		Provider:  "command_code",
		Auth:      "api_key",
		APIKeyEnv: "COMMAND_CODE_API_KEY",
		Token:     apiKey,
	}
	if apiKey != "" {
		acct.SetHint("credential_source", "command_code_detected")
	}
	for _, existing := range result.Accounts {
		if existing.ID == acct.ID {
			return
		}
	}
	result.Accounts = append(result.Accounts, acct)
}
