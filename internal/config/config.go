package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// UIConfig holds TUI preferences.
type UIConfig struct {
	RefreshIntervalSeconds int     `toml:"refresh_interval_seconds"`
	WarnThreshold          float64 `toml:"warn_threshold"`
	CritThreshold          float64 `toml:"crit_threshold"`
}

// Config is the top-level configuration.
type Config struct {
	UI         UIConfig             `toml:"ui"`
	AutoDetect bool                 `toml:"auto_detect"` // if true, scan workstation for tools & keys
	Accounts   []core.AccountConfig `toml:"accounts"`
}

// DefaultConfig returns sane defaults.
func DefaultConfig() Config {
	return Config{
		AutoDetect: true, // auto-detect by default
		UI: UIConfig{
			RefreshIntervalSeconds: 30,
			WarnThreshold:          0.20,
			CritThreshold:          0.05,
		},
	}
}

// ConfigDir returns the OS-appropriate config directory.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "agentusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agentusage")
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// Load reads the config from the default path. If the file doesn't exist,
// it returns DefaultConfig with no error.
func Load() (Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom reads the config from a specific path.
func LoadFrom(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Apply defaults for missing values.
	if cfg.UI.RefreshIntervalSeconds <= 0 {
		cfg.UI.RefreshIntervalSeconds = 30
	}
	if cfg.UI.WarnThreshold <= 0 {
		cfg.UI.WarnThreshold = 0.20
	}
	if cfg.UI.CritThreshold <= 0 {
		cfg.UI.CritThreshold = 0.05
	}

	return cfg, nil
}
