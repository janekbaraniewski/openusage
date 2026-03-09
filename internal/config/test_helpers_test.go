package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSettingsJSON(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	return path
}

func loadConfigJSON(t *testing.T, content string) Config {
	t.Helper()

	cfg, err := LoadFrom(writeSettingsJSON(t, content))
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}
	return cfg
}
