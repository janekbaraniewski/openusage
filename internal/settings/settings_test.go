package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if s.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme 'Catppuccin Mocha', got %q", s.Theme)
	}
	if s.Experimental.Analytics {
		t.Error("expected experimental analytics to be false by default")
	}
}

func TestLoadFrom_MissingFile(t *testing.T) {
	s, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme, got %q", s.Theme)
	}
	if s.Experimental.Analytics {
		t.Error("expected default analytics=false")
	}
}

func TestLoadFrom_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	data := []byte(`{"theme":"Nord","experimental":{"analytics":true}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Theme != "Nord" {
		t.Errorf("expected theme 'Nord', got %q", s.Theme)
	}
	if !s.Experimental.Analytics {
		t.Error("expected analytics=true")
	}
}

func TestLoadFrom_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if s.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme on error, got %q", s.Theme)
	}
}

func TestLoadFrom_EmptyThemeFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	data := []byte(`{"theme":"","experimental":{"analytics":true}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Theme != "Catppuccin Mocha" {
		t.Errorf("expected default theme for empty string, got %q", s.Theme)
	}
}

func TestSaveTo_CreatesFileAndDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "settings.json")

	s := Settings{
		Theme:        "Dracula",
		Experimental: ExperimentalConfig{Analytics: true},
	}

	if err := SaveTo(path, s); err != nil {
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

	original := Settings{
		Theme:        "Synthwave '84",
		Experimental: ExperimentalConfig{Analytics: false},
	}

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
