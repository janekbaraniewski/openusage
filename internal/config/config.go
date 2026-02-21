package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type UIConfig struct {
	RefreshIntervalSeconds int     `json:"refresh_interval_seconds"`
	WarnThreshold          float64 `json:"warn_threshold"`
	CritThreshold          float64 `json:"crit_threshold"`
}

type ExperimentalConfig struct {
	Analytics bool `json:"analytics"`
}

type DashboardProviderConfig struct {
	AccountID string `json:"account_id"`
	Enabled   bool   `json:"enabled"`
}

func (p *DashboardProviderConfig) UnmarshalJSON(data []byte) error {
	type rawDashboardProviderConfig struct {
		AccountID string `json:"account_id"`
		Enabled   *bool  `json:"enabled"`
	}

	var raw rawDashboardProviderConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.AccountID = raw.AccountID
	p.Enabled = true
	if raw.Enabled != nil {
		p.Enabled = *raw.Enabled
	}
	return nil
}

type DashboardConfig struct {
	Providers []DashboardProviderConfig `json:"providers"`
}

type Config struct {
	UI                   UIConfig             `json:"ui"`
	Theme                string               `json:"theme"`
	Experimental         ExperimentalConfig   `json:"experimental"`
	Dashboard            DashboardConfig      `json:"dashboard"`
	AutoDetect           bool                 `json:"auto_detect"`
	Accounts             []core.AccountConfig `json:"accounts"`
	AutoDetectedAccounts []core.AccountConfig `json:"auto_detected_accounts"`
}

var legacyAccountIDAliases = map[string]string{
	"openai-auto":        "openai",
	"anthropic-auto":     "anthropic",
	"openrouter-auto":    "openrouter",
	"groq-auto":          "groq",
	"mistral-auto":       "mistral",
	"deepseek-auto":      "deepseek",
	"xai-auto":           "xai",
	"gemini-api-auto":    "gemini-api",
	"gemini-google-auto": "gemini-google",
	"copilot-auto":       "copilot",
	"gemini-cli-auto":    "gemini-cli",
}

func DefaultConfig() Config {
	return Config{
		AutoDetect: true,
		Theme:      "Gruvbox",
		UI: UIConfig{
			RefreshIntervalSeconds: 30,
			WarnThreshold:          0.20,
			CritThreshold:          0.05,
		},
		Experimental: ExperimentalConfig{Analytics: false},
	}
}

func ConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "openusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "openusage")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "settings.json")
}

func Load() (Config, error) {
	return LoadFrom(ConfigPath())
}

func LoadFrom(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.UI.RefreshIntervalSeconds <= 0 {
		cfg.UI.RefreshIntervalSeconds = 30
	}
	if cfg.UI.WarnThreshold <= 0 {
		cfg.UI.WarnThreshold = 0.20
	}
	if cfg.UI.CritThreshold <= 0 {
		cfg.UI.CritThreshold = 0.05
	}
	if cfg.Theme == "" {
		cfg.Theme = DefaultConfig().Theme
	}
	cfg.Accounts = normalizeAccounts(cfg.Accounts)
	cfg.AutoDetectedAccounts = normalizeAccounts(cfg.AutoDetectedAccounts)
	cfg.Dashboard.Providers = normalizeDashboardProviders(cfg.Dashboard.Providers)

	return cfg, nil
}

func normalizeAccountID(id string) string {
	trimmed := strings.TrimSpace(id)
	if canonical, ok := legacyAccountIDAliases[trimmed]; ok {
		return canonical
	}
	return trimmed
}

func normalizeAccounts(in []core.AccountConfig) []core.AccountConfig {
	if len(in) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(in))
	out := make([]core.AccountConfig, 0, len(in))
	for _, acct := range in {
		acct.ID = normalizeAccountID(acct.ID)
		if acct.ID == "" || seen[acct.ID] {
			continue
		}
		seen[acct.ID] = true
		out = append(out, acct)
	}

	return out
}

func normalizeDashboardProviders(in []DashboardProviderConfig) []DashboardProviderConfig {
	if len(in) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(in))
	out := make([]DashboardProviderConfig, 0, len(in))
	for _, entry := range in {
		id := normalizeAccountID(entry.AccountID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, DashboardProviderConfig{
			AccountID: id,
			Enabled:   entry.Enabled,
		})
	}
	return out
}

// saveMu guards read-modify-write cycles on the config file.
var saveMu sync.Mutex

func Save(cfg Config) error {
	return SaveTo(ConfigPath(), cfg)
}

func SaveTo(path string, cfg Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// SaveTheme persists a theme name into the config file (read-modify-write).
func SaveTheme(theme string) error {
	return SaveThemeTo(ConfigPath(), theme)
}

func SaveThemeTo(path string, theme string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		cfg = DefaultConfig()
	}
	cfg.Theme = theme
	return SaveTo(path, cfg)
}

// SaveDashboardProviders persists dashboard provider preferences into the config file (read-modify-write).
func SaveDashboardProviders(providers []DashboardProviderConfig) error {
	return SaveDashboardProvidersTo(ConfigPath(), providers)
}

func SaveDashboardProvidersTo(path string, providers []DashboardProviderConfig) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		cfg = DefaultConfig()
	}
	cfg.Dashboard.Providers = normalizeDashboardProviders(providers)
	return SaveTo(path, cfg)
}

// SaveAutoDetected persists auto-detected accounts into the config file (read-modify-write).
func SaveAutoDetected(accounts []core.AccountConfig) error {
	return SaveAutoDetectedTo(ConfigPath(), accounts)
}

func SaveAutoDetectedTo(path string, accounts []core.AccountConfig) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		cfg = DefaultConfig()
	}
	cfg.AutoDetectedAccounts = accounts
	return SaveTo(path, cfg)
}
