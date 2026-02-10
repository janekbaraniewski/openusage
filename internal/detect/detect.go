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
		ID:       "copilot-auto",
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
		ID:        "gemini-cli-auto",
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
