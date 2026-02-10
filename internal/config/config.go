package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

type UIConfig struct {
	RefreshIntervalSeconds int     `toml:"refresh_interval_seconds"`
	WarnThreshold          float64 `toml:"warn_threshold"`
	CritThreshold          float64 `toml:"crit_threshold"`
}

type Config struct {
	UI         UIConfig             `toml:"ui"`
	AutoDetect bool                 `toml:"auto_detect"` // if true, scan workstation for tools & keys
	Accounts   []core.AccountConfig `toml:"accounts"`
}

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

func ConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "agentusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agentusage")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
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

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config %s: %w", path, err)
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

	return cfg, nil
}
