package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
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
	if cfg.Theme != "Catppuccin Mocha" {
		t.Errorf("default theme = %q, want 'Catppuccin Mocha'", cfg.Theme)
	}
	if cfg.Experimental.Analytics {
		t.Error("expected experimental analytics to be false by default")
	}
	if !cfg.AutoDetect {
		t.Error("expected auto_detect to be true by default")
	}
}

func TestLoadFrom_MissingFile(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UI.RefreshIntervalSeconds != 30 {
		t.Error("should return defaults for missing file")
	}
	if cfg.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme, got %q", cfg.Theme)
	}
}

func TestLoadFrom_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := `{
  "ui": {
    "refresh_interval_seconds": 10,
    "warn_threshold": 0.30,
    "crit_threshold": 0.10
  },
  "theme": "Nord",
  "experimental": { "analytics": true },
  "auto_detect": false,
  "accounts": [
    {
      "id": "openai-test",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    },
    {
      "id": "anthropic-test",
      "provider": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY"
    }
  ]
}`
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
	if cfg.Theme != "Nord" {
		t.Errorf("theme = %q, want 'Nord'", cfg.Theme)
	}
	if !cfg.Experimental.Analytics {
		t.Error("expected analytics=true")
	}
	if cfg.AutoDetect {
		t.Error("expected auto_detect=false")
	}
	if len(cfg.Accounts) != 2 {
		t.Errorf("accounts count = %d, want 2", len(cfg.Accounts))
	}
	if cfg.Accounts[0].ID != "openai-test" {
		t.Errorf("first account ID = %s, want openai-test", cfg.Accounts[0].ID)
	}
}

func TestLoadFrom_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if cfg.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme on error, got %q", cfg.Theme)
	}
}

func TestLoadFrom_EmptyThemeFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	data := []byte(`{"theme":"","experimental":{"analytics":true}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme for empty string, got %q", cfg.Theme)
	}
}

func TestLoadFrom_ZeroThresholdsGetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	data := []byte(`{"ui":{"refresh_interval_seconds":0,"warn_threshold":0,"crit_threshold":0}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UI.RefreshIntervalSeconds != 30 {
		t.Errorf("refresh = %d, want 30 (default for zero)", cfg.UI.RefreshIntervalSeconds)
	}
	if cfg.UI.WarnThreshold != 0.20 {
		t.Errorf("warn = %f, want 0.20", cfg.UI.WarnThreshold)
	}
	if cfg.UI.CritThreshold != 0.05 {
		t.Errorf("crit = %f, want 0.05", cfg.UI.CritThreshold)
	}
}

func TestSaveTo_CreatesFileAndDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "settings.json")

	cfg := DefaultConfig()
	cfg.Theme = "Dracula"
	cfg.Experimental.Analytics = true

	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error loading saved file: %v", err)
	}
	if loaded.Theme != "Dracula" {
		t.Errorf("expected 'Dracula', got %q", loaded.Theme)
	}
	if !loaded.Experimental.Analytics {
		t.Error("expected analytics=true after round-trip")
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	original := DefaultConfig()
	original.Theme = "Synthwave '84"
	original.Experimental.Analytics = false

	if err := SaveTo(path, original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.Theme != original.Theme {
		t.Errorf("theme mismatch: got %q, want %q", loaded.Theme, original.Theme)
	}
	if loaded.Experimental.Analytics != original.Experimental.Analytics {
		t.Errorf("analytics mismatch: got %v, want %v", loaded.Experimental.Analytics, original.Experimental.Analytics)
	}
}

func TestSaveThemeTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Start with a config
	cfg := DefaultConfig()
	cfg.Experimental.Analytics = true
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Save just the theme
	if err := SaveThemeTo(path, "Nord"); err != nil {
		t.Fatalf("SaveThemeTo error: %v", err)
	}

	// Verify theme changed but other fields preserved
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Theme != "Nord" {
		t.Errorf("theme = %q, want 'Nord'", loaded.Theme)
	}
	if !loaded.Experimental.Analytics {
		t.Error("analytics should be preserved after SaveThemeTo")
	}
}

func TestSaveAutoDetectedTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Start with a config that has theme and manual accounts
	cfg := DefaultConfig()
	cfg.Theme = "Dracula"
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Save auto-detected accounts
	accounts := []core.AccountConfig{
		{ID: "auto-1", Provider: "openai"},
		{ID: "auto-2", Provider: "anthropic"},
	}
	if err := SaveAutoDetectedTo(path, accounts); err != nil {
		t.Fatalf("SaveAutoDetectedTo error: %v", err)
	}

	// Verify auto-detected accounts saved but other fields preserved
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Theme != "Dracula" {
		t.Errorf("theme should be preserved, got %q", loaded.Theme)
	}
	if len(loaded.AutoDetectedAccounts) != 2 {
		t.Fatalf("auto_detected_accounts count = %d, want 2", len(loaded.AutoDetectedAccounts))
	}
	if loaded.AutoDetectedAccounts[0].ID != "auto-1" {
		t.Errorf("first auto-detected ID = %q, want 'auto-1'", loaded.AutoDetectedAccounts[0].ID)
	}
}

func TestSaveThemeTo_ThreadSafety(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	cfg := DefaultConfig()
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	themes := []string{"Nord", "Dracula", "Synthwave '84", "Catppuccin Mocha"}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = SaveThemeTo(path, themes[idx%len(themes)])
		}(i)
	}
	wg.Wait()

	// File should still be valid JSON
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("config corrupted after concurrent writes: %v", err)
	}
	// Theme should be one of the valid themes
	valid := false
	for _, th := range themes {
		if loaded.Theme == th {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("unexpected theme %q after concurrent writes", loaded.Theme)
	}
}

func TestLoadFrom_AutoDetectedAccountsPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := `{
  "auto_detect": true,
  "auto_detected_accounts": [
    {"id": "cached-openai", "provider": "openai", "api_key_env": "OPENAI_API_KEY"}
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AutoDetectedAccounts) != 1 {
		t.Fatalf("auto_detected_accounts count = %d, want 1", len(cfg.AutoDetectedAccounts))
	}
	if cfg.AutoDetectedAccounts[0].ID != "cached-openai" {
		t.Errorf("auto-detected ID = %q, want 'cached-openai'", cfg.AutoDetectedAccounts[0].ID)
	}
}

func TestLoadFrom_DashboardProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := `{
  "dashboard": {
    "providers": [
      {"account_id": "openai-personal"},
      {"account_id": "anthropic-work", "enabled": false},
      {"account_id": "openai-personal"},
      {"account_id": "   "}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Dashboard.Providers) != 2 {
		t.Fatalf("dashboard.providers count = %d, want 2", len(cfg.Dashboard.Providers))
	}

	first := cfg.Dashboard.Providers[0]
	if first.AccountID != "openai-personal" {
		t.Errorf("first account_id = %q, want openai-personal", first.AccountID)
	}
	if !first.Enabled {
		t.Error("missing enabled should default to true")
	}

	second := cfg.Dashboard.Providers[1]
	if second.AccountID != "anthropic-work" {
		t.Errorf("second account_id = %q, want anthropic-work", second.AccountID)
	}
	if second.Enabled {
		t.Error("expected anthropic-work enabled=false")
	}
}

func TestSaveDashboardProvidersTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	cfg := DefaultConfig()
	cfg.Theme = "Nord"
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	providers := []DashboardProviderConfig{
		{AccountID: "openai-personal", Enabled: true},
		{AccountID: "anthropic-work", Enabled: false},
		{AccountID: "openai-personal", Enabled: false},
	}
	if err := SaveDashboardProvidersTo(path, providers); err != nil {
		t.Fatalf("SaveDashboardProvidersTo error: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Theme != "Nord" {
		t.Errorf("theme should be preserved, got %q", loaded.Theme)
	}
	if len(loaded.Dashboard.Providers) != 2 {
		t.Fatalf("dashboard.providers count = %d, want 2", len(loaded.Dashboard.Providers))
	}
	if loaded.Dashboard.Providers[0].AccountID != "openai-personal" {
		t.Errorf("first provider = %q, want openai-personal", loaded.Dashboard.Providers[0].AccountID)
	}
	if !loaded.Dashboard.Providers[0].Enabled {
		t.Error("expected openai-personal enabled=true")
	}
	if loaded.Dashboard.Providers[1].AccountID != "anthropic-work" {
		t.Errorf("second provider = %q, want anthropic-work", loaded.Dashboard.Providers[1].AccountID)
	}
	if loaded.Dashboard.Providers[1].Enabled {
		t.Error("expected anthropic-work enabled=false")
	}
}
