// Package detect implements auto-detection of AI coding tools and API keys
// configured on the workstation.
package detect

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// DetectedTool represents a tool found on the workstation.
type DetectedTool struct {
	Name       string // e.g. "Cursor IDE", "Claude Code CLI"
	BinaryPath string // resolved path to binary, if applicable
	ConfigDir  string // path to the tool's config directory
	Type       string // "ide", "cli", "api"
}

// Result holds the full auto-detection result.
type Result struct {
	Tools    []DetectedTool
	Accounts []core.AccountConfig
}

// AutoDetect scans the workstation for known AI coding tools and API keys.
// It returns detected tools and auto-generated AccountConfig entries.
func AutoDetect() Result {
	var result Result

	// 1. Detect IDEs and CLIs
	detectCursor(&result)
	detectClaudeCode(&result)
	detectCodex(&result)
	detectAider(&result)
	detectGHCopilot(&result)
	detectGeminiCLI(&result)

	// 2. Detect API keys from environment variables
	detectEnvKeys(&result)

	return result
}

// homeDir returns the user's home directory.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// cursorConfigDir returns the OS-specific Cursor Application Support directory.
func cursorAppSupportDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor")
	case "linux":
		return filepath.Join(home, ".config", "Cursor")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Cursor")
		}
		return filepath.Join(home, "AppData", "Roaming", "Cursor")
	}
	return ""
}

// findBinary checks if a binary exists on PATH and returns its full path.
func findBinary(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// addAccount adds a new account if one with the same ID doesn't already exist.
func addAccount(result *Result, acct core.AccountConfig) {
	for _, existing := range result.Accounts {
		if existing.ID == acct.ID {
			return
		}
	}
	result.Accounts = append(result.Accounts, acct)
}

// detectAider looks for the Aider CLI and its config files.
func detectAider(result *Result) {
	bin := findBinary("aider")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".aider")

	tool := DetectedTool{
		Name:       "Aider",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	log.Printf("[detect] Found Aider at %s", bin)
	// Aider uses API keys from env vars, which are detected separately.
	// It also reads from .aider.conf.yml — but it delegates to the underlying API providers.
}

// detectGHCopilot looks for GitHub Copilot via the gh CLI.
// Only adds an account if the copilot extension is actually installed.
func detectGHCopilot(result *Result) {
	bin := findBinary("gh")
	if bin == "" {
		return
	}

	log.Printf("[detect] Found gh CLI at %s", bin)

	// Verify the copilot extension is actually installed before adding anything.
	// Without the extension, `gh copilot` commands fail and we'd just show an error.
	cmd := exec.Command(bin, "copilot", "--version")
	if err := cmd.Run(); err != nil {
		log.Printf("[detect] gh copilot extension not installed, skipping")
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".config", "github-copilot")

	tool := DetectedTool{
		Name:       "GitHub Copilot (gh CLI)",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	addAccount(result, core.AccountConfig{
		ID:       "copilot-auto",
		Provider: "copilot",
		Auth:     "cli",
		Binary:   bin,
	})
}

// detectGeminiCLI looks for the Gemini CLI binary and reads its local config.
// Only adds an account if the config directory exists with actual data files.
//
// Gemini CLI stores data at ~/.gemini/:
//   - oauth_creds.json — Google OAuth access/refresh tokens
//   - google_accounts.json — active account email
//   - settings.json — auth type, feature flags
//   - installation_id — CLI installation ID
//   - antigravity/conversations/ — session protobuf files
func detectGeminiCLI(result *Result) {
	bin := findBinary("gemini")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".gemini")

	log.Printf("[detect] Found Gemini CLI at %s", bin)

	// Verify the config directory actually exists and has useful data.
	// Without config data, the provider has nothing to report.
	if !dirExists(configDir) {
		log.Printf("[detect] Gemini CLI config dir %s not found, skipping", configDir)
		return
	}

	// Check for at least one meaningful data file
	oauthFile := filepath.Join(configDir, "oauth_creds.json")
	accountsFile := filepath.Join(configDir, "google_accounts.json")
	settingsFile := filepath.Join(configDir, "settings.json")

	hasOAuth := fileExists(oauthFile)
	hasAccounts := fileExists(accountsFile)
	hasSettings := fileExists(settingsFile)

	if !hasOAuth && !hasAccounts && !hasSettings {
		log.Printf("[detect] Gemini CLI config dir exists but no data files found, skipping")
		return
	}

	tool := DetectedTool{
		Name:       "Gemini CLI",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	acct := core.AccountConfig{
		ID:        "gemini-cli-auto",
		Provider:  "gemini_cli",
		Auth:      "oauth",
		Binary:    bin,
		ExtraData: make(map[string]string),
	}
	acct.ExtraData["config_dir"] = configDir

	// Read account email from google_accounts.json
	if hasAccounts {
		if data, err := os.ReadFile(accountsFile); err == nil {
			var accounts struct {
				Active string `json:"active"`
			}
			if json.Unmarshal(data, &accounts) == nil && accounts.Active != "" {
				acct.ExtraData["email"] = accounts.Active
				log.Printf("[detect] Gemini CLI active account: %s", accounts.Active)
			}
		}
	}

	// Capture project ID from environment (same precedence as Gemini CLI)
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		acct.ExtraData["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT: %s", v)
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		acct.ExtraData["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT_ID: %s", v)
	}

	addAccount(result, acct)
}

// envKeyMapping maps environment variable names to provider/account configurations.
var envKeyMapping = []struct {
	EnvVar    string
	Provider  string
	AccountID string
}{
	{"OPENAI_API_KEY", "openai", "openai-auto"},
	{"ANTHROPIC_API_KEY", "anthropic", "anthropic-auto"},
	{"OPENROUTER_API_KEY", "openrouter", "openrouter-auto"},
	{"GROQ_API_KEY", "groq", "groq-auto"},
	{"MISTRAL_API_KEY", "mistral", "mistral-auto"},
	{"DEEPSEEK_API_KEY", "deepseek", "deepseek-auto"},
	{"XAI_API_KEY", "xai", "xai-auto"},
	{"GEMINI_API_KEY", "gemini_api", "gemini-api-auto"},
	{"GOOGLE_API_KEY", "gemini_api", "gemini-google-auto"},
}

// detectEnvKeys scans environment variables for known AI API keys.
func detectEnvKeys(result *Result) {
	for _, mapping := range envKeyMapping {
		val := os.Getenv(mapping.EnvVar)
		if val == "" {
			continue
		}

		// Mask the key for logging
		masked := val[:4] + "..." + val[len(val)-4:]
		if len(val) < 10 {
			masked = "****"
		}
		log.Printf("[detect] Found %s=%s", mapping.EnvVar, masked)

		addAccount(result, core.AccountConfig{
			ID:        mapping.AccountID,
			Provider:  mapping.Provider,
			Auth:      "api_key",
			APIKeyEnv: mapping.EnvVar,
		})
	}
}

// Summary returns a human-readable summary of what was detected.
func (r Result) Summary() string {
	var sb strings.Builder
	if len(r.Tools) > 0 {
		sb.WriteString(fmt.Sprintf("Detected %d tool(s):\n", len(r.Tools)))
		for _, t := range r.Tools {
			sb.WriteString(fmt.Sprintf("  • %s (%s)", t.Name, t.Type))
			if t.BinaryPath != "" {
				sb.WriteString(fmt.Sprintf(" at %s", t.BinaryPath))
			}
			sb.WriteString("\n")
		}
	}
	if len(r.Accounts) > 0 {
		sb.WriteString(fmt.Sprintf("Auto-configured %d account(s):\n", len(r.Accounts)))
		for _, a := range r.Accounts {
			sb.WriteString(fmt.Sprintf("  • %s (provider: %s)\n", a.ID, a.Provider))
		}
	}
	if len(r.Tools) == 0 && len(r.Accounts) == 0 {
		sb.WriteString("No AI tools or API keys detected on this workstation.\n")
	}
	return sb.String()
}
