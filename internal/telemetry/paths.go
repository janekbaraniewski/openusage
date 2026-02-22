package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultStateDir() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
		return filepath.Join(base, "openusage"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("telemetry: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "openusage"), nil
}

func DefaultDBPath() (string, error) {
	stateDir, err := DefaultStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "telemetry.db"), nil
}
