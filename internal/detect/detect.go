package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
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

	// Phase 1: tool-binding detectors. These may populate Token directly
	// from local stores (Cursor state.vscdb, Codex auth.json, Z.AI YAML)
	// and register a per-tool account ID that subsequent detectors won't
	// duplicate.
	detectCursor(&result)
	detectClaudeCode(&result)
	detectCodex(&result)
	detectZAICodingHelper(&result)
	detectOllama(&result)
	detectAider(&result)
	detectGHCopilot(&result)
	detectGeminiCLI(&result)

	// Phase 2: process env vars. Most authoritative; runs before any
	// file-based credential adoption so a freshly-set env var always
	// overrides stale values found in dotfiles.
	detectEnvKeys(&result)

	// Phase 3: file-based credential adoption. Each detector here
	// re-checks os.Getenv per-var so it skips anything Phase 2 already
	// adopted, and addAccount is idempotent on account ID.
	detectShellRC(&result)
	detectOpenCodeAuth(&result)
	detectAiderConfig(&result)

	// Phase 4: OS keychain probes. We only annotate accounts here — the
	// providers themselves still read the secret value at fetch time.
	detectMacOSKeychainCredentials(&result)

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
		for _, dir := range candidateBinaryDirs() {
			candidate := filepath.Join(dir, name)
			if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
				candidate += ".exe"
			}
			if isExecutableFile(candidate) {
				return candidate
			}
		}
		return ""
	}
	return path
}

func candidateBinaryDirs() []string {
	var dirs []string

	// When OPENUSAGE_DETECT_BIN_DIRS is explicitly set (even to empty), use
	// only its dirs + PATH and skip hardcoded system dirs. This gives tests
	// full control over binary lookup isolation.
	customVal, customSet := os.LookupEnv("OPENUSAGE_DETECT_BIN_DIRS")
	if customSet && customVal != "" {
		parts := strings.Split(customVal, string(os.PathListSeparator))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				dirs = append(dirs, part)
			}
		}
	}

	if pathEnv := strings.TrimSpace(os.Getenv("PATH")); pathEnv != "" {
		for _, part := range strings.Split(pathEnv, string(os.PathListSeparator)) {
			part = strings.TrimSpace(part)
			if part != "" {
				dirs = append(dirs, part)
			}
		}
	}

	if !customSet {
		home := homeDir()
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".local", "bin"),
				filepath.Join(home, "bin"),
			)
		}

		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	}
	return lo.Uniq(dirs)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
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
	if lo.ContainsBy(result.Accounts, func(existing core.AccountConfig) bool { return existing.ID == acct.ID }) {
		return
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
	home := homeDir()
	if home == "" {
		return
	}

	ghBin := findBinary("gh")
	ghCopilotOK := false

	// Try gh copilot extension first (existing/deprecated path).
	// Use a 5-second timeout to prevent hanging if gh CLI is broken,
	// unauthenticated, or blocked by network/proxy issues.
	if ghBin != "" {
		log.Printf("[detect] Found gh CLI at %s", ghBin)
		ghCtx, ghCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ghCancel()
		cmd := exec.CommandContext(ghCtx, ghBin, "copilot", "--version")
		if err := cmd.Run(); err == nil {
			ghCopilotOK = true
		} else {
			log.Printf("[detect] gh copilot extension not installed or timed out: %v", err)
		}
	}

	// If gh copilot works, register it as before.
	if ghCopilotOK {
		configDir := filepath.Join(home, ".config", "github-copilot")
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "GitHub Copilot (gh CLI)",
			BinaryPath: ghBin,
			ConfigDir:  configDir,
			Type:       "cli",
		})
		addAccount(result, core.AccountConfig{
			ID:       "copilot",
			Provider: "copilot",
			Auth:     "cli",
			Binary:   ghBin,
		})
		return
	}

	// Fall back to standalone copilot binary.
	copilotBin := findBinary("copilot")
	if copilotBin == "" {
		log.Printf("[detect] No gh copilot extension or standalone copilot binary found, skipping")
		return
	}

	log.Printf("[detect] Found standalone copilot binary at %s", copilotBin)

	// Confirm the CLI has been used by checking for ~/.copilot/ directory.
	copilotDir := filepath.Join(home, ".copilot")
	if !dirExists(copilotDir) {
		log.Printf("[detect] Standalone copilot binary found but %s does not exist, skipping", copilotDir)
		return
	}

	// Determine the Binary field: prefer gh (for gh api quota calls), fall back to copilot path.
	binaryPath := copilotBin
	if ghBin != "" {
		binaryPath = ghBin
	}

	result.Tools = append(result.Tools, DetectedTool{
		Name:       "GitHub Copilot CLI",
		BinaryPath: copilotBin,
		ConfigDir:  copilotDir,
		Type:       "cli",
	})

	addAccount(result, core.AccountConfig{
		ID:       "copilot",
		Provider: "copilot",
		Auth:     "cli",
		Binary:   binaryPath,
		RuntimeHints: map[string]string{
			"copilot_binary": copilotBin,
			"config_dir":     copilotDir,
		},
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
		RuntimeHints: make(map[string]string),
	}
	acct.SetHint("config_dir", configDir)
	acct.RuntimeHints["config_dir"] = configDir

	if hasAccounts {
		if data, err := os.ReadFile(accountsFile); err == nil {
			var accounts struct {
				Active string `json:"active"`
			}
			if json.Unmarshal(data, &accounts) == nil && accounts.Active != "" {
				acct.RuntimeHints["email"] = accounts.Active
				log.Printf("[detect] Gemini CLI active account: %s", accounts.Active)
			}
		}
	}

	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		acct.SetHint("project_id", v)
		acct.RuntimeHints["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT: %s", v)
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		acct.SetHint("project_id", v)
		acct.RuntimeHints["project_id"] = v
		log.Printf("[detect] Gemini CLI project from GOOGLE_CLOUD_PROJECT_ID: %s", v)
	}

	addAccount(result, acct)
}

// envKeyMappingEntry is the single source of truth for "this env var name
// belongs to this provider/account". Every file-based detector that adopts a
// raw API key — shell rc parsing, Aider .env/.aider.conf.yml, future Tier-1
// detectors — funnels through this table.
//
// AiderShortNames lists the provider tokens Aider accepts in its list-form
// `api-key:` config (e.g. `gemini=...`, `moonshotai=...`). Add new short
// names alongside the env-var entry; aider.go looks them up via
// envKeyByAiderShortName().
type envKeyMappingEntry struct {
	EnvVar          string
	Provider        string
	AccountID       string
	AiderShortNames []string
}

var envKeyMapping = []envKeyMappingEntry{
	{EnvVar: "OPENAI_API_KEY", Provider: "openai", AccountID: "openai", AiderShortNames: []string{"openai"}},
	{EnvVar: "ANTHROPIC_API_KEY", Provider: "anthropic", AccountID: "anthropic", AiderShortNames: []string{"anthropic"}},
	{EnvVar: "OPENROUTER_API_KEY", Provider: "openrouter", AccountID: "openrouter", AiderShortNames: []string{"openrouter"}},
	{EnvVar: "GROQ_API_KEY", Provider: "groq", AccountID: "groq", AiderShortNames: []string{"groq"}},
	{EnvVar: "MISTRAL_API_KEY", Provider: "mistral", AccountID: "mistral", AiderShortNames: []string{"mistral"}},
	{EnvVar: "DEEPSEEK_API_KEY", Provider: "deepseek", AccountID: "deepseek", AiderShortNames: []string{"deepseek"}},
	{EnvVar: "MOONSHOT_API_KEY", Provider: "moonshot", AccountID: "moonshot-ai", AiderShortNames: []string{"moonshot", "moonshotai"}},
	{EnvVar: "XAI_API_KEY", Provider: "xai", AccountID: "xai", AiderShortNames: []string{"xai", "grok"}},
	{EnvVar: "ZAI_API_KEY", Provider: "zai", AccountID: "zai", AiderShortNames: []string{"zai", "zhipuai"}},
	{EnvVar: "ZHIPUAI_API_KEY", Provider: "zai", AccountID: "zhipuai-auto"},
	{EnvVar: "ZEN_API_KEY", Provider: "opencode", AccountID: "opencode"},
	{EnvVar: "OPENCODE_API_KEY", Provider: "opencode", AccountID: "opencode"},
	{EnvVar: "GEMINI_API_KEY", Provider: "gemini_api", AccountID: "gemini-api", AiderShortNames: []string{"gemini", "google"}},
	{EnvVar: "GOOGLE_API_KEY", Provider: "gemini_api", AccountID: "gemini-google"},
	{EnvVar: "OLLAMA_API_KEY", Provider: "ollama", AccountID: "ollama-cloud"},
	{EnvVar: "ALIBABA_CLOUD_API_KEY", Provider: "alibaba_cloud", AccountID: "alibaba_cloud", AiderShortNames: []string{"alibaba", "qwen"}},
}

// envKeyByVar indexes envKeyMapping by env-var name for O(1) lookup. Built
// once at init.
var envKeyByVar = func() map[string]envKeyMappingEntry {
	out := make(map[string]envKeyMappingEntry, len(envKeyMapping))
	for _, m := range envKeyMapping {
		out[m.EnvVar] = m
	}
	return out
}()

// envKeyByAiderShortName indexes envKeyMapping by Aider's per-provider short
// name (the left side of `<provider>=<key>` entries in `.aider.conf.yml`'s
// `api-key:` list). Multiple short names can map to the same entry.
var envKeyByAiderShortName = func() map[string]envKeyMappingEntry {
	out := make(map[string]envKeyMappingEntry)
	for _, m := range envKeyMapping {
		for _, name := range m.AiderShortNames {
			out[strings.ToLower(name)] = m
		}
	}
	return out
}()

// adoptAPIKey is the shared "register an api_key account from a known env-var
// mapping" path. Used by every file-based detector (shell rc, Aider .env /
// YAML, future Tier-1 detectors). Honours "process env wins" by short-
// circuiting when the env var is already set, defers to addAccount's
// id-dedupe for cross-detector precedence, and emits a uniform masked log
// line on success.
func adoptAPIKey(result *Result, mapping envKeyMappingEntry, value, source string) {
	if os.Getenv(mapping.EnvVar) != "" {
		return
	}
	acct := core.AccountConfig{
		ID:        mapping.AccountID,
		Provider:  mapping.Provider,
		Auth:      "api_key",
		APIKeyEnv: mapping.EnvVar,
		Token:     value,
	}
	acct.SetHint("credential_source", source)

	before := len(result.Accounts)
	addAccount(result, acct)
	if len(result.Accounts) > before {
		log.Printf("[detect] %s → %s/%s (%s=%s)",
			source, mapping.Provider, mapping.AccountID, mapping.EnvVar, MaskKey(value))
	}
}

func detectEnvKeys(result *Result) {
	for _, mapping := range envKeyMapping {
		val := os.Getenv(mapping.EnvVar)
		if val == "" {
			continue
		}

		log.Printf("[detect] Found %s=%s", mapping.EnvVar, MaskKey(val))

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

// providerForStoredCredential maps a stored credential's account ID to its
// provider. Linear scan over envKeyMapping; the table is small and this runs
// at most once per stored credential.
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
