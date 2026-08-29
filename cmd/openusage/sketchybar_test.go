package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSketchybarCommandHasIntegrationControls(t *testing.T) {
	cmd := newSketchybarCommand()
	want := map[string]bool{"install": false, "uninstall": false, "doctor": false, "presets": false}
	for _, child := range cmd.Commands() {
		if _, ok := want[child.Name()]; ok {
			want[child.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing sketchybar %s subcommand", name)
		}
	}
}

// TestSketchybarInstallRequiresWrite pins the behaviour that `install` without
// --write is a preview. The subcommand used to force Write=true, so running it
// to inspect the block silently rewrote the user's sketchybarrc -- a file
// people hand-edit -- while --write was registered, documented, and ignored.
func TestSketchybarInstallRequiresWrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sketchybarrc")
	dataDir := filepath.Join(dir, "scripts")
	original := "# user config\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cmd := newSketchybarCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--config", configPath, "--data-dir", dataDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install without --write: %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != original {
		t.Fatalf("install without --write modified sketchybarrc:\n%s", got)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("install without --write created the script directory: err=%v", err)
	}
}
