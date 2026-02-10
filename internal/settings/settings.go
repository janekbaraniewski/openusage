package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/janekbaraniewski/agentusage/internal/config"
)

type ExperimentalConfig struct {
	Analytics bool `json:"analytics"`
}

type Settings struct {
	Theme        string             `json:"theme"`
	Experimental ExperimentalConfig `json:"experimental"`
}

func DefaultSettings() Settings {
	return Settings{
		Theme:        "Catppuccin Mocha",
		Experimental: ExperimentalConfig{Analytics: false},
	}
}

func Path() string {
	return filepath.Join(config.ConfigDir(), "settings.json")
}

func Load() (Settings, error) {
	return LoadFrom(Path())
}

func LoadFrom(path string) (Settings, error) {
	s := DefaultSettings()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("reading settings: %w", err)
	}

	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), fmt.Errorf("parsing settings %s: %w", path, err)
	}

	if s.Theme == "" {
		s.Theme = DefaultSettings().Theme
	}

	return s, nil
}

func Save(s Settings) error {
	return SaveTo(Path(), s)
}

func SaveTo(path string, s Settings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating settings dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing settings: %w", err)
	}

	return nil
}
