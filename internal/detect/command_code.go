package detect

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// commandCodeAccountID is the canonical account ID for the Command Code
// provider; it is shared by the env-var path (detectEnvKeys) and the local
// CLI path so addAccount de-dupes them into one tile.
const commandCodeAccountID = "command_code"

// commandCodeEnvVar is the documented env var the CLI and Provider API read.
const commandCodeEnvVar = "COMMAND_CODE_API_KEY"

// detectCommandCode registers a Command Code account when the CLI is installed
// or has been logged in at ~/.commandcode. The CLI stores its API key at
// ~/.commandcode/auth.json; we adopt it as a runtime token so the provider
// needs no extra configuration (the env var still wins when set).
func detectCommandCode(result *Result) {
	home := homeDir()
	configDir := ""
	if home != "" {
		configDir = filepath.Join(home, ".commandcode")
	}
	authFile := ""
	if configDir != "" {
		authFile = filepath.Join(configDir, "auth.json")
	}

	// "command-code" unambiguously identifies the CLI. "cmd"/"cmdc" are
	// ambiguous names, so they only count as evidence alongside a Command Code
	// config dir or auth file.
	bin := findBinary("command-code")
	aliasBin := ""
	if bin == "" {
		aliasBin = findBinary("cmd")
		if aliasBin == "" {
			aliasBin = findBinary("cmdc")
		}
	}
	binPath := bin
	if binPath == "" {
		binPath = aliasBin
	}

	hasConfig := configDir != "" && dirExists(configDir)
	hasAuth := authFile != "" && fileExists(authFile)

	if bin == "" && !hasConfig && !hasAuth {
		return
	}

	if binPath != "" {
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Command Code CLI",
			BinaryPath: binPath,
			ConfigDir:  configDir,
			Type:       "cli",
		})
		log.Printf("[detect] Found Command Code CLI at %s", binPath)
	}

	acct := core.AccountConfig{
		ID:           commandCodeAccountID,
		Provider:     commandCodeAccountID,
		Auth:         "api_key",
		APIKeyEnv:    commandCodeEnvVar,
		Binary:       binPath,
		RuntimeHints: make(map[string]string),
	}
	if configDir != "" {
		acct.SetHint("config_dir", configDir)
	}
	if hasAuth {
		acct.SetHint("auth_file", authFile)
		// Process env wins over the stored file (mirrors detectEnvKeys
		// precedence); only adopt the file key when no env var is set.
		if os.Getenv(commandCodeEnvVar) == "" {
			if key := readCommandCodeAuthKey(authFile); key != "" {
				acct.Token = key
				acct.SetHint("credential_source", "commandcode_auth_json")
				log.Printf("[detect] Adopted Command Code API key from %s (key=%s)", authFile, maskKey(key))
			}
		}
	}

	addAccount(result, acct)
}

func readCommandCodeAuthKey(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var auth struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return ""
	}
	return strings.TrimSpace(auth.APIKey)
}
