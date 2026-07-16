package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// HideCosts overrides the dashboard-level setting for this account.
	// nil means "fall through to DashboardConfig.HideCosts (and then to the
	// plan-aware auto policy)".
	HideCosts *bool `json:"hide_costs,omitempty"`
}

type DashboardWidgetSection struct {
	ID      core.DashboardStandardSection `json:"id"`
	Enabled bool                          `json:"enabled"`
}

const (
	DashboardViewGrid    = "grid"
	DashboardViewStacked = "stacked"
	DashboardViewList    = "list"
	DashboardViewTabs    = "tabs"
	DashboardViewSplit   = "split"
	DashboardViewCompare = "compare"
)

func (p *DashboardProviderConfig) UnmarshalJSON(data []byte) error {
	type rawDashboardProviderConfig struct {
		AccountID string `json:"account_id"`
		Enabled   *bool  `json:"enabled"`
		HideCosts *bool  `json:"hide_costs"`
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
	p.HideCosts = raw.HideCosts
	return nil
}

func (s *DashboardWidgetSection) UnmarshalJSON(data []byte) error {
	type rawDashboardWidgetSection struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}

	var raw rawDashboardWidgetSection
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	s.ID = core.DashboardStandardSection(raw.ID)
	s.Enabled = true
	if raw.Enabled != nil {
		s.Enabled = *raw.Enabled
	}
	return nil
}

type DetailWidgetSection struct {
	ID      core.DetailStandardSection `json:"id"`
	Enabled bool                       `json:"enabled"`
}

func (s *DetailWidgetSection) UnmarshalJSON(data []byte) error {
	type rawDetailWidgetSection struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}

	var raw rawDetailWidgetSection
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	s.ID = core.DetailStandardSection(raw.ID)
	s.Enabled = true
	if raw.Enabled != nil {
		s.Enabled = *raw.Enabled
	}
	return nil
}

type DashboardConfig struct {
	Providers              []DashboardProviderConfig `json:"providers"`
	View                   string                    `json:"view"`
	WidgetSections         []DashboardWidgetSection  `json:"widget_sections,omitempty"`
	DetailSections         []DetailWidgetSection     `json:"detail_sections,omitempty"`
	HideSectionsWithNoData bool                      `json:"hide_sections_with_no_data,omitempty"`
	// HideCosts is the global default for suppressing monetary metrics.
	// nil means "fall through to the plan-aware auto policy" (see
	// core.ResolveHideCosts).
	HideCosts *bool `json:"hide_costs,omitempty"`
}

type ExportConfig struct {
	Target          string `json:"target"`           // HTTP URL of hub; empty disables export
	IntervalSeconds int    `json:"interval_seconds"` // push interval; default 60
	MachineName     string `json:"machine_name"`     // override hostname; empty uses os.Hostname()
	// AuthToken is the optional Bearer token sent with each push. It must NOT
	// be persisted to settings.json — supply it via OPENUSAGE_HUB_TOKEN env var
	// at runtime. Mirrors AccountConfig.Token's `json:"-"` posture.
	AuthToken string `json:"-"`
}

type HubConfig struct {
	ListenAddr          string `json:"listen_addr"`           // TCP address to listen on; default ":9190"
	StaleTimeoutSeconds int    `json:"stale_timeout_seconds"` // seconds before a machine entry is pruned; default 300
	// AuthToken is the optional Bearer token required on /v1/push and
	// /v1/snapshots. It must NOT be persisted to settings.json — supply it via
	// OPENUSAGE_HUB_TOKEN env var at runtime. Empty disables auth.
	AuthToken string `json:"-"`
}

type IntegrationState struct {
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
	Declined    bool   `json:"declined,omitempty"`
}

// TmuxConfig holds user-tunable settings for the `openusage tmux` subcommand.
// All fields are optional; unset values fall through to preset defaults and
// then to hardcoded defaults inside internal/tmux.
type TmuxConfig struct {
	Preset         string               `json:"preset,omitempty"`
	Format         string               `json:"format,omitempty"`          // overrides preset
	Provider       string               `json:"provider,omitempty"`        // pin a provider; skips auto-detection
	ActiveStrategy string               `json:"active_strategy,omitempty"` // comma-separated list; default "recency,priority"
	PriorityOrder  []string             `json:"priority_order,omitempty"`
	RecencyWindow  string               `json:"recency_window,omitempty"` // Go duration string, default "4h"
	ColorMode      string               `json:"color_mode,omitempty"`     // truecolor|256|ansi|none; default truecolor
	Glyphs         string               `json:"glyphs,omitempty"`         // ascii|unicode|nerdfont
	Theme          string               `json:"theme,omitempty"`          // empty = inherit Config.Theme
	Source         string               `json:"source,omitempty"`         // auto|daemon|direct
	Interval       int                  `json:"interval,omitempty"`       // suggested tmux status-interval; default 5
	MaxRuntimeMS   int                  `json:"max_runtime_ms,omitempty"` // self-kill budget; default 800
	Variables      map[string]string    `json:"variables,omitempty"`      // user-defined template variables
	Segments       map[string]string    `json:"segments,omitempty"`       // user-defined named segments
	ColorRules     map[string]ColorRule `json:"color_rules,omitempty"`    // threshold-based coloring keyed by variable name
	Alerts         TmuxAlerts           `json:"alerts,omitempty"`
}

// ColorRule defines a threshold-based color mapping for the `:color` modifier.
// Color fields accept theme refs (e.g. "$accent") or hex (e.g. "#FF6600").
type ColorRule struct {
	LowAt       float64 `json:"low_at,omitempty"`
	MediumAt    float64 `json:"medium_at,omitempty"`
	HighAt      float64 `json:"high_at,omitempty"`
	LowColor    string  `json:"low_color,omitempty"`
	MediumColor string  `json:"medium_color,omitempty"`
	HighColor   string  `json:"high_color,omitempty"`
}

// TmuxAlerts configures push-alert thresholds for `openusage tmux watch`.
type TmuxAlerts struct {
	BurnRatePerHour       float64 `json:"burn_rate_per_hour,omitempty"`
	BlockMinutesRemaining int     `json:"block_minutes_remaining,omitempty"`
	CooldownMinutes       int     `json:"cooldown_minutes,omitempty"`
	Mode                  string  `json:"mode,omitempty"` // message|bell|both|none
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
	Export               ExportConfig                  `json:"export,omitempty"`
	Hub                  HubConfig                     `json:"hub,omitempty"`
	Tmux                 TmuxConfig                    `json:"tmux,omitempty"`
}

// DefaultProviderLinks returns built-in telemetry provider-id to dashboard provider-id mappings.
//
// Telemetry sources (e.g. the OpenCode plugin) tag events with whatever provider id the
// source tool uses internally. Those names don't always match openusage's internal provider
// ids — e.g. OpenCode says "google" for the Gemini API, "github-copilot" for Copilot,
// and "azure" for Azure OpenAI.
// These defaults paper over the rename mismatches so users don't see "Unmapped" for
// providers they have configured under a different name.
//
// Identity links (e.g. openai→openai) are intentionally omitted: the read-time matcher
// already handles direct id matches, so identity entries would be noise.
func DefaultProviderLinks() map[string]string {
	return map[string]string{
		"anthropic":      "claude_code",
		"google":         "gemini_api",
		"github-copilot": "copilot",
		"azure":          "azure_openai",
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
		Data:               DataConfig{TimeWindow: "30d", RetentionDays: defaultRetentionDays},
		Experimental:       ExperimentalConfig{Analytics: false},
		Telemetry:          TelemetryConfig{ProviderLinks: map[string]string{}},
		Dashboard:          DashboardConfig{View: DashboardViewGrid},
		ModelNormalization: core.DefaultModelNormalizationConfig(),
	}
}

// ConfigDir returns the directory that holds settings.json and other OpenUsage
// config. The per-OS base is provided by osConfigDir() in the platform-specific
// config_dir_*.go files.
func ConfigDir() string {
	return osConfigDir()
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

	cfg.UI = normalizeUIConfig(cfg.UI)
	if cfg.Theme == "" {
		cfg.Theme = DefaultConfig().Theme
	}
	cfg.Data = normalizeDataConfig(cfg.Data)
	cfg.ModelNormalization = core.NormalizeModelNormalizationConfig(cfg.ModelNormalization)
	cfg.Telemetry = normalizeTelemetryConfig(cfg.Telemetry)
	cfg.Accounts = normalizeAccounts(cfg.Accounts)
	cfg.AutoDetectedAccounts = normalizeAccounts(cfg.AutoDetectedAccounts)
	cfg.Dashboard.Providers = normalizeDashboardProviders(cfg.Dashboard.Providers)
	cfg.Dashboard.View = normalizeDashboardView(cfg.Dashboard.View)
	cfg.Dashboard.WidgetSections = normalizeDashboardWidgetSections(cfg.Dashboard.WidgetSections)
	cfg.Dashboard.DetailSections = normalizeDetailWidgetSections(cfg.Dashboard.DetailSections)

	return cfg, nil
}

func normalizeUIConfig(in UIConfig) UIConfig {
	defaults := DefaultConfig().UI

	if in.RefreshIntervalSeconds <= 0 {
		core.Tracef("config: refresh_interval_seconds=%d is invalid, using default %d",
			in.RefreshIntervalSeconds, defaults.RefreshIntervalSeconds)
		in.RefreshIntervalSeconds = defaults.RefreshIntervalSeconds
	}

	if in.WarnThreshold <= 0 {
		core.Tracef("config: warn_threshold=%f is invalid, using default %f",
			in.WarnThreshold, defaults.WarnThreshold)
		in.WarnThreshold = defaults.WarnThreshold
	} else if in.WarnThreshold > 1 {
		core.Tracef("config: warn_threshold=%f exceeds 1.0, clamping to 1.0",
			in.WarnThreshold)
		in.WarnThreshold = 1.0
	}

	if in.CritThreshold <= 0 {
		core.Tracef("config: crit_threshold=%f is invalid, using default %f",
			in.CritThreshold, defaults.CritThreshold)
		in.CritThreshold = defaults.CritThreshold
	} else if in.CritThreshold > 1 {
		core.Tracef("config: crit_threshold=%f exceeds 1.0, clamping to 1.0",
			in.CritThreshold)
		in.CritThreshold = 1.0
	}

	return in
}

// defaultRetentionDays is the hot window: how long full per-event detail is
// kept before being downsampled into the daily rollup. 90d matches the
// local-file collectors' re-import lookback so re-imported history lands inside
// the window (no ingest churn).
const defaultRetentionDays = 90

func normalizeDataConfig(in DataConfig) DataConfig {
	tw := core.ParseTimeWindow(in.TimeWindow)
	retention := in.RetentionDays
	if retention <= 0 {
		core.Tracef("config: retention_days=%d is invalid, using default %d", retention, defaultRetentionDays)
		retention = defaultRetentionDays
	}
	// Upper bound only guards against absurd values; it is deliberately large so
	// users who want to keep long history can (downsampling, not a hard 90-day
	// wall, is the intended way to manage size). The previous 90-day cap meant
	// retention silently discarded everything older than a quarter.
	const maxRetentionDays = 3650 // ~10 years
	if retention > maxRetentionDays {
		core.Tracef("config: retention_days=%d exceeds maximum %d, clamping", retention, maxRetentionDays)
		retention = maxRetentionDays
	}
	if tw.Days() > retention {
		newTW := core.LargestWindowFitting(retention)
		core.Tracef("config: time_window %q (%d days) exceeds retention_days=%d, reducing to %q",
			tw, tw.Days(), retention, newTW)
		tw = newTW
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
		if len(acct.ProviderPaths) == 0 && len(acct.Paths) > 0 {
			acct.ProviderPaths = make(map[string]string, len(acct.Paths))
			for key, value := range acct.Paths {
				trimmedKey := strings.TrimSpace(key)
				trimmedValue := strings.TrimSpace(value)
				if trimmedKey == "" || trimmedValue == "" {
					continue
				}
				acct.ProviderPaths[trimmedKey] = trimmedValue
			}
		}
		acct.Paths = nil
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
			HideCosts: entry.HideCosts,
		}
	})
	filtered := lo.Filter(normalized, func(entry DashboardProviderConfig, _ int) bool { return entry.AccountID != "" })
	return lo.UniqBy(filtered, func(entry DashboardProviderConfig) string { return entry.AccountID })
}

func normalizeDashboardView(view string) string {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case DashboardViewGrid, DashboardViewStacked, DashboardViewTabs, DashboardViewSplit, DashboardViewCompare:
		return strings.ToLower(strings.TrimSpace(view))
	case DashboardViewList:
		// Legacy view id: map to split navigator/detail layout.
		return DashboardViewSplit
	default:
		return DashboardViewGrid
	}
}

func normalizeDashboardWidgetSections(in []DashboardWidgetSection) []DashboardWidgetSection {
	if len(in) == 0 {
		return nil
	}

	normalized := make([]DashboardWidgetSection, 0, len(in))
	seenSections := make(map[core.DashboardStandardSection]bool, len(in))

	for _, section := range in {
		sectionID := core.DashboardStandardSection(strings.ToLower(strings.TrimSpace(string(section.ID))))
		sectionID = core.NormalizeDashboardStandardSection(sectionID)
		if sectionID == core.DashboardSectionHeader || !core.IsKnownDashboardStandardSection(sectionID) || seenSections[sectionID] {
			continue
		}
		normalized = append(normalized, DashboardWidgetSection{
			ID:      sectionID,
			Enabled: section.Enabled,
		})
		seenSections[sectionID] = true
	}

	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeDetailWidgetSections(in []DetailWidgetSection) []DetailWidgetSection {
	if len(in) == 0 {
		return nil
	}

	normalized := make([]DetailWidgetSection, 0, len(in))
	seenSections := make(map[core.DetailStandardSection]bool, len(in))

	for _, section := range in {
		sectionID := core.DetailStandardSection(strings.ToLower(strings.TrimSpace(string(section.ID))))
		if !core.IsKnownDetailStandardSection(sectionID) || seenSections[sectionID] {
			continue
		}
		normalized = append(normalized, DetailWidgetSection{
			ID:      sectionID,
			Enabled: section.Enabled,
		})
		seenSections[sectionID] = true
	}

	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// saveMu guards every code path that writes the config file. Both modifyConfig
// (read-modify-write helpers like SaveTheme) and direct Save/SaveTo callers
// must take it; otherwise a Save() can race a concurrent modifyConfig and
// roll back the modification.
var saveMu sync.Mutex

func Save(cfg Config) error {
	return SaveTo(ConfigPath(), cfg)
}

func SaveTo(path string, cfg Config) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	return saveLocked(path, cfg)
}

// saveLocked is the actual write path; callers MUST hold saveMu.
func saveLocked(path string, cfg Config) error {
	target := path
	if _, err := os.Lstat(path); err == nil {
		// Resolve the full symlink chain so the write lands on the real file
		// instead of replacing the symlink itself.
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolving config path %s: %w", path, err)
		}
		target = resolved
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat config: %w", err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')

	tmpPath := target + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing config tmp file: %w", err)
	}
	defer os.Remove(tmpPath) // no-op if rename succeeded; cleans up on rename failure
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("renaming config tmp file: %w", err)
	}
	return nil
}

// modifyConfig performs an atomic read-modify-write on the config file at path.
// If the file cannot be read, it returns an error without writing — this prevents
// a failed read from silently overwriting user-set values with zero defaults.
func modifyConfig(path string, mutate func(*Config)) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfg, err := LoadFrom(path)
	if err != nil {
		return fmt.Errorf("modifyConfig: reading current config: %w", err)
	}
	mutate(&cfg)
	return saveLocked(path, cfg)
}

// SaveTheme persists a theme name into the config file (read-modify-write).
func SaveTheme(theme string) error {
	return SaveThemeTo(ConfigPath(), theme)
}

func SaveThemeTo(path string, theme string) error {
	return modifyConfig(path, func(cfg *Config) { cfg.Theme = theme })
}

// SaveDashboardProviders persists dashboard provider preferences into the config file (read-modify-write).
func SaveDashboardProviders(providers []DashboardProviderConfig) error {
	return SaveDashboardProvidersTo(ConfigPath(), providers)
}

func SaveDashboardProvidersTo(path string, providers []DashboardProviderConfig) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.Providers = normalizeDashboardProviders(providers)
	})
}

// SaveDashboardView persists dashboard view preference into the config file (read-modify-write).
func SaveDashboardView(view string) error {
	return SaveDashboardViewTo(ConfigPath(), view)
}

func SaveDashboardViewTo(path string, view string) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.View = normalizeDashboardView(view)
	})
}

// SaveDashboardWidgetSections persists dashboard widget section preferences
// into the config file (read-modify-write).
func SaveDashboardWidgetSections(sections []DashboardWidgetSection) error {
	return SaveDashboardWidgetSectionsTo(ConfigPath(), sections)
}

func SaveDashboardWidgetSectionsTo(path string, sections []DashboardWidgetSection) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.WidgetSections = normalizeDashboardWidgetSections(sections)
	})
}

// SaveDetailWidgetSections persists detail view section preferences
// into the config file (read-modify-write).
func SaveDetailWidgetSections(sections []DetailWidgetSection) error {
	return SaveDetailWidgetSectionsTo(ConfigPath(), sections)
}

func SaveDetailWidgetSectionsTo(path string, sections []DetailWidgetSection) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.DetailSections = normalizeDetailWidgetSections(sections)
	})
}

// SaveDashboardHideSectionsWithNoData persists whether empty dashboard widget
// sections should be hidden in the config file (read-modify-write).
func SaveDashboardHideSectionsWithNoData(hide bool) error {
	return SaveDashboardHideSectionsWithNoDataTo(ConfigPath(), hide)
}

func SaveDashboardHideSectionsWithNoDataTo(path string, hide bool) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.HideSectionsWithNoData = hide
	})
}

// SaveDashboardHideCosts persists the global hide_costs toggle. Pass nil to clear
// the override (return to plan-aware auto behavior).
func SaveDashboardHideCosts(hide *bool) error {
	return SaveDashboardHideCostsTo(ConfigPath(), hide)
}

func SaveDashboardHideCostsTo(path string, hide *bool) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Dashboard.HideCosts = hide
	})
}

// SaveDashboardProviderHideCosts persists the per-account hide_costs override.
// Pass nil to clear the override (fall through to global / auto).
//
// If no DashboardProviderConfig exists for accountID yet, one is appended with
// Enabled=true so the override sticks.
func SaveDashboardProviderHideCosts(accountID string, hide *bool) error {
	return SaveDashboardProviderHideCostsTo(ConfigPath(), accountID, hide)
}

func SaveDashboardProviderHideCostsTo(path string, accountID string, hide *bool) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("save dashboard provider hide_costs: account_id must be non-empty")
	}
	return modifyConfig(path, func(cfg *Config) {
		found := false
		for i := range cfg.Dashboard.Providers {
			if cfg.Dashboard.Providers[i].AccountID == accountID {
				cfg.Dashboard.Providers[i].HideCosts = hide
				found = true
				break
			}
		}
		if !found {
			cfg.Dashboard.Providers = append(cfg.Dashboard.Providers, DashboardProviderConfig{
				AccountID: accountID,
				Enabled:   true,
				HideCosts: hide,
			})
		}
	})
}

// SaveAutoDetected persists auto-detected accounts into the config file (read-modify-write).
func SaveAutoDetected(accounts []core.AccountConfig) error {
	return SaveAutoDetectedTo(ConfigPath(), accounts)
}

func SaveAutoDetectedTo(path string, accounts []core.AccountConfig) error {
	return modifyConfig(path, func(cfg *Config) { cfg.AutoDetectedAccounts = accounts })
}

// SaveTimeWindow persists a time window into the config file (read-modify-write).
func SaveTimeWindow(window string) error {
	return SaveTimeWindowTo(ConfigPath(), window)
}

func SaveTimeWindowTo(path string, window string) error {
	return modifyConfig(path, func(cfg *Config) {
		cfg.Data.TimeWindow = string(core.ParseTimeWindow(window))
	})
}

// SaveProviderLink persists a single telemetry provider link into the config file
// (read-modify-write). Source and target are normalized (lowercased, trimmed). An empty
// source or target is rejected as an error.
func SaveProviderLink(source, target string) error {
	return SaveProviderLinkTo(ConfigPath(), source, target)
}

func SaveProviderLinkTo(path string, source, target string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	target = strings.ToLower(strings.TrimSpace(target))
	if source == "" || target == "" {
		return fmt.Errorf("save provider link: source and target must be non-empty")
	}
	return modifyConfig(path, func(cfg *Config) {
		if cfg.Telemetry.ProviderLinks == nil {
			cfg.Telemetry.ProviderLinks = map[string]string{}
		}
		cfg.Telemetry.ProviderLinks[source] = target
	})
}

// DeleteProviderLink removes a user-defined telemetry provider link. If the link only
// exists as a built-in default, this is a no-op (the default cannot be erased without
// adding a tombstone, and we don't model that today).
func DeleteProviderLink(source string) error {
	return DeleteProviderLinkTo(ConfigPath(), source)
}

func DeleteProviderLinkTo(path string, source string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return fmt.Errorf("delete provider link: source must be non-empty")
	}
	return modifyConfig(path, func(cfg *Config) {
		if cfg.Telemetry.ProviderLinks == nil {
			return
		}
		delete(cfg.Telemetry.ProviderLinks, source)
	})
}

// SaveIntegrationState persists an integration state into the config file (read-modify-write).
func SaveIntegrationState(id string, state IntegrationState) error {
	return SaveIntegrationStateTo(ConfigPath(), id, state)
}

func SaveIntegrationStateTo(path string, id string, state IntegrationState) error {
	return modifyConfig(path, func(cfg *Config) {
		if cfg.Integrations == nil {
			cfg.Integrations = make(map[string]IntegrationState)
		}
		cfg.Integrations[id] = state
	})
}
