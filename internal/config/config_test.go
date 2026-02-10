package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.UI.RefreshIntervalSeconds != 30 {
		t.Errorf("default refresh = %d, want 30", cfg.UI.RefreshIntervalSeconds)
	}
	if cfg.UI.WarnThreshold != 0.20 {
		t.Errorf("default warn = %f, want 0.20", cfg.UI.WarnThreshold)
	}
	if cfg.UI.CritThreshold != 0.05 {
		t.Errorf("default crit = %f, want 0.05", cfg.UI.CritThreshold)
	}
}

func TestLoadFrom_MissingFile(t *testing.T) {
	cfg, err := LoadFrom("/tmp/nonexistent_agentusage_test.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UI.RefreshIntervalSeconds != 30 {
		t.Error("should return defaults for missing file")
	}
}

func TestLoadFrom_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[ui]
refresh_interval_seconds = 10
warn_threshold = 0.30
crit_threshold = 0.10

[[accounts]]
id = "openai-test"
provider = "openai"
api_key_env = "OPENAI_API_KEY"
probe_model = "gpt-4.1-mini"

[[accounts]]
id = "anthropic-test"
provider = "anthropic"
api_key_env = "ANTHROPIC_API_KEY"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if cfg.UI.RefreshIntervalSeconds != 10 {
		t.Errorf("refresh = %d, want 10", cfg.UI.RefreshIntervalSeconds)
	}
	if cfg.UI.WarnThreshold != 0.30 {
		t.Errorf("warn = %f, want 0.30", cfg.UI.WarnThreshold)
	}
	if len(cfg.Accounts) != 2 {
		t.Errorf("accounts count = %d, want 2", len(cfg.Accounts))
	}
	if cfg.Accounts[0].ID != "openai-test" {
		t.Errorf("first account ID = %s, want openai-test", cfg.Accounts[0].ID)
	}
}
