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
	"github.com/samber/lo"
)

type UIConfig struct {
	RefreshIntervalSeconds int     `json:"refresh_interval_seconds"`
	WarnThreshold          float64 `json:"warn_threshold"`
	CritThreshold          float64 `json:"crit_threshold"`
}

type ExperimentalConfig struct {
	Analytics bool `json:"analytics"`
}

type TelemetryConfig struct {
	// ProviderLinks maps source telemetry provider IDs to configured provider IDs.
	// Example: {"anthropic":"claude_code"}.
	ProviderLinks map[string]string `json:"provider_links"`
}

type DataConfig struct {
	TimeWindow    string `json:"time_window"`    // "1d", "3d", "7d", "30d"
	RetentionDays int    `json:"retention_days"` // max days to keep in SQLite
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

type IntegrationState struct {
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
	Declined    bool   `json:"declined,omitempty"`
}

type Config struct {
	UI                   UIConfig                      `json:"ui"`
	Theme                string                        `json:"theme"`
	Data                 DataConfig                    `json:"data"`
	Experimental         ExperimentalConfig            `json:"experimental"`
	Telemetry            TelemetryConfig               `json:"telemetry"`
	Dashboard            DashboardConfig               `json:"dashboard"`
	ModelNormalization   core.ModelNormalizationConfig `json:"model_normalization"`
	AutoDetect           bool                          `json:"auto_detect"`
	Accounts             []core.AccountConfig          `json:"accounts"`
	AutoDetectedAccounts []core.AccountConfig          `json:"auto_detected_accounts"`
	Integrations         map[string]IntegrationState   `json:"integrations,omitempty"`
}

// DefaultProviderLinks returns built-in telemetry provider-id to dashboard provider-id mappings.
func DefaultProviderLinks() map[string]string {
	return map[string]string{
		"anthropic": "claude_code",
	}
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
		Data:               DataConfig{TimeWindow: "30d", RetentionDays: 30},
		Experimental:       ExperimentalConfig{Analytics: false},
		Telemetry:          TelemetryConfig{ProviderLinks: map[string]string{}},
		ModelNormalization: core.DefaultModelNormalizationConfig(),
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
	cfg.Data = normalizeDataConfig(cfg.Data)
	cfg.ModelNormalization = core.NormalizeModelNormalizationConfig(cfg.ModelNormalization)
	cfg.Telemetry = normalizeTelemetryConfig(cfg.Telemetry)
	cfg.Accounts = normalizeAccounts(cfg.Accounts)
	cfg.AutoDetectedAccounts = normalizeAccounts(cfg.AutoDetectedAccounts)
	cfg.Dashboard.Providers = normalizeDashboardProviders(cfg.Dashboard.Providers)

	return cfg, nil
}

func normalizeDataConfig(in DataConfig) DataConfig {
	tw := core.ParseTimeWindow(in.TimeWindow)
	retention := in.RetentionDays
	if retention <= 0 {
		retention = 30
	}
	if retention > 90 {
		retention = 90
	}
	if tw.Days() > retention {
		tw = core.LargestWindowFitting(retention)
	}
	return DataConfig{
		TimeWindow:    string(tw),
		RetentionDays: retention,
	}
}

func normalizeAccountID(id string) string {
	return strings.TrimSpace(id)
}

func normalizeAccounts(in []core.AccountConfig) []core.AccountConfig {
	if len(in) == 0 {
		return nil
	}
	normalized := lo.Map(in, func(acct core.AccountConfig, _ int) core.AccountConfig {
		acct.ID = normalizeAccountID(acct.ID)
		return acct
	})
	filtered := lo.Filter(normalized, func(acct core.AccountConfig, _ int) bool { return acct.ID != "" })
	return lo.UniqBy(filtered, func(acct core.AccountConfig) string { return acct.ID })
}

func normalizeTelemetryConfig(in TelemetryConfig) TelemetryConfig {
	out := TelemetryConfig{
		ProviderLinks: DefaultProviderLinks(),
	}
	for source, target := range in.ProviderLinks {
		source = strings.ToLower(strings.TrimSpace(source))
		target = strings.ToLower(strings.TrimSpace(target))
		if source == "" || target == "" {
			continue
		}
		// user overrides win
		out.ProviderLinks[source] = target
	}
	return out
}

func normalizeDashboardProviders(in []DashboardProviderConfig) []DashboardProviderConfig {
	if len(in) == 0 {
		return nil
	}
	normalized := lo.Map(in, func(entry DashboardProviderConfig, _ int) DashboardProviderConfig {
		return DashboardProviderConfig{
			AccountID: normalizeAccountID(entry.AccountID),
			Enabled:   entry.Enabled,
		}
	})
	filtered := lo.Filter(normalized, func(entry DashboardProviderConfig, _ int) bool { return entry.AccountID != "" })
	return lo.UniqBy(filtered, func(entry DashboardProviderConfig) string { return entry.AccountID })
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

// SaveTimeWindow persists a time window into the config file (read-modify-write).
func SaveTimeWindow(window string) error {
	return SaveTimeWindowTo(ConfigPath(), window)
}

func SaveTimeWindowTo(path string, window string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		cfg = DefaultConfig()
	}
	cfg.Data.TimeWindow = string(core.ParseTimeWindow(window))
	return SaveTo(path, cfg)
}

// SaveIntegrationState persists an integration state into the config file (read-modify-write).
func SaveIntegrationState(id string, state IntegrationState) error {
	return SaveIntegrationStateTo(ConfigPath(), id, state)
}

func SaveIntegrationStateTo(path string, id string, state IntegrationState) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		cfg = DefaultConfig()
	}
	if cfg.Integrations == nil {
		cfg.Integrations = make(map[string]IntegrationState)
	}
	cfg.Integrations[id] = state
	return SaveTo(path, cfg)
}
