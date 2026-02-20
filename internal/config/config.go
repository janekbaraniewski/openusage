package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

type UIConfig struct {
	RefreshIntervalSeconds int     `json:"refresh_interval_seconds"`
	WarnThreshold          float64 `json:"warn_threshold"`
	CritThreshold          float64 `json:"crit_threshold"`
}

type ExperimentalConfig struct {
	Analytics bool `json:"analytics"`
}

type Config struct {
	UI                   UIConfig             `json:"ui"`
	Theme                string               `json:"theme"`
	Experimental         ExperimentalConfig   `json:"experimental"`
	AutoDetect           bool                 `json:"auto_detect"`
	Accounts             []core.AccountConfig `json:"accounts"`
	AutoDetectedAccounts []core.AccountConfig `json:"auto_detected_accounts"`
}

func DefaultConfig() Config {
	return Config{
		AutoDetect: true,
		Theme:      "Catppuccin Mocha",
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
		return filepath.Join(os.Getenv("APPDATA"), "agentusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agentusage")
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

	return cfg, nil
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
