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

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type DetectedTool struct {
	Name       string // e.g. "Cursor IDE", "Claude Code CLI"
	BinaryPath string // resolved path to binary, if applicable
	ConfigDir  string // path to the tool's config directory
	Type       string // "ide", "cli", "api"
}

type Result struct {
	Tools    []DetectedTool
	Accounts []core.AccountConfig
}

func AutoDetect() Result {
	var result Result

	detectCursor(&result)
	detectClaudeCode(&result)
	detectCodex(&result)
	detectAider(&result)
	detectGHCopilot(&result)
	detectGeminiCLI(&result)

	detectEnvKeys(&result)

	return result
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

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

func findBinary(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func addAccount(result *Result, acct core.AccountConfig) {
	for _, existing := range result.Accounts {
		if existing.ID == acct.ID {
			return
		}
	}
	result.Accounts = append(result.Accounts, acct)
}

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
}

func detectGHCopilot(result *Result) {
	bin := findBinary("gh")
	if bin == "" {
		return
	}

	log.Printf("[detect] Found gh CLI at %s", bin)

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
		ID:       "copilot",
		Provider: "copilot",
		Auth:     "cli",
		Binary:   bin,
	})
}

func detectGeminiCLI(result *Result) {
	bin := findBinary("gemini")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".gemini")

	log.Printf("[detect] Found Gemini CLI at %s", bin)

	if !dirExists(configDir) {
		log.Printf("[detect] Gemini CLI config dir %s not found, skipping", configDir)
		return
	}

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
		ID:        "gemini-cli",
		Provider:  "gemini_cli",
		Auth:      "oauth",
		Binary:    bin,
		ExtraData: make(map[string]string),
	}
	acct.ExtraData["config_dir"] = configDir

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

	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		acct.ExtraData["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT: %s", v)
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		acct.ExtraData["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT_ID: %s", v)
	}

	addAccount(result, acct)
}

var envKeyMapping = []struct {
	EnvVar    string
	Provider  string
	AccountID string
}{
	{"OPENAI_API_KEY", "openai", "openai"},
	{"ANTHROPIC_API_KEY", "anthropic", "anthropic"},
	{"OPENROUTER_API_KEY", "openrouter", "openrouter"},
	{"GROQ_API_KEY", "groq", "groq"},
	{"MISTRAL_API_KEY", "mistral", "mistral"},
	{"DEEPSEEK_API_KEY", "deepseek", "deepseek"},
	{"XAI_API_KEY", "xai", "xai"},
	{"ZEN_API_KEY", "zen", "zen"},
	{"OPENCODE_API_KEY", "zen", "zen"},
	{"GEMINI_API_KEY", "gemini_api", "gemini-api"},
	{"GOOGLE_API_KEY", "gemini_api", "gemini-google"},
}

func detectEnvKeys(result *Result) {
	for _, mapping := range envKeyMapping {
		val := os.Getenv(mapping.EnvVar)
		if val == "" {
			continue
		}

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

// ApplyCredentials fills in Token for accounts that have no API key from env vars,
// using stored credentials from the credentials file. It also creates new accounts
// for stored credentials that don't match any existing account.
func ApplyCredentials(result *Result) {
	creds, err := config.LoadCredentials()
	if err != nil {
		log.Printf("[detect] Failed to load credentials: %v", err)
		return
	}
	if len(creds.Keys) == 0 {
		return
	}

	// Apply to existing accounts
	applied := make(map[string]bool, len(result.Accounts))
	for i := range result.Accounts {
		acct := &result.Accounts[i]
		if acct.Token != "" || acct.ResolveAPIKey() != "" {
			applied[acct.ID] = true
			continue
		}
		if key, ok := creds.Keys[acct.ID]; ok {
			acct.Token = key
			applied[acct.ID] = true
			log.Printf("[detect] Applied stored credential for %s", acct.ID)
		}
	}

	// Create accounts for stored credentials that don't match any existing account
	for accountID, key := range creds.Keys {
		if applied[accountID] {
			continue
		}
		provider := providerForStoredCredential(accountID)
		if provider == "" {
			log.Printf("[detect] Stored credential for unknown account %s, skipping", accountID)
			continue
		}
		result.Accounts = append(result.Accounts, core.AccountConfig{
			ID:       accountID,
			Provider: provider,
			Auth:     "api_key",
			Token:    key,
		})
		log.Printf("[detect] Created account %s from stored credential", accountID)
	}
}

// providerForStoredCredential maps a stored credential's account ID to its provider.
func providerForStoredCredential(accountID string) string {
	for _, mapping := range envKeyMapping {
		if mapping.AccountID == accountID {
			return mapping.Provider
		}
	}
	return ""
}

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
