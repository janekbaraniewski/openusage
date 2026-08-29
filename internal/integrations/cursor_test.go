package integrations

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPatchCursorConfig(t *testing.T) {
	input := []byte(`{"version":1,"statusLine":{"type":"command","command":"some-old-cmd"}}`)
	// Refuses unrelated command
	if _, err := patchCursorConfig(input, "/tmp/openusage", true); err == nil {
		t.Fatal("patchCursorConfig() should refuse to overwrite unrelated custom command")
	}

	cleanInput := []byte(`{"version":1}`)
	patched, err := patchCursorConfig(cleanInput, "/tmp/open usage", true)
	if err != nil {
		t.Fatalf("patchCursorConfig(install) error = %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(patched, &cfg); err != nil {
		t.Fatalf("parse patched config: %v", err)
	}
	status, ok := cfg["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing: %#v", cfg)
	}
	command, _ := status["command"].(string)
	if want := `"/tmp/open usage" cursor statusline`; command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}

	uninstalled, err := patchCursorConfig(patched, "/tmp/open usage", false)
	if err != nil {
		t.Fatalf("patchCursorConfig(uninstall) error = %v", err)
	}
	var uninstalledCfg map[string]any
	if err := json.Unmarshal(uninstalled, &uninstalledCfg); err != nil {
		t.Fatalf("parse uninstalled config: %v", err)
	}
	if _, exists := uninstalledCfg["statusLine"]; exists {
		t.Fatalf("statusLine survived uninstall: %#v", uninstalledCfg)
	}
}

func TestCursorInstallLifecycle(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: filepath.Join(root, "bin", "openusage"),
	}
	def, ok := DefinitionByID(CursorID)
	if !ok {
		t.Fatal("cursor definition not found")
	}
	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.ConfigFile != filepath.Join(root, ".cursor", "cli-config.json") {
		t.Fatalf("config file = %q", result.ConfigFile)
	}
	status := def.Detector(dirs)
	if status.State != "ready" || !status.Installed || !status.Configured {
		t.Fatalf("installed status = %+v, want ready", status)
	}
	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	status = def.Detector(dirs)
	if status.State != "missing" {
		t.Fatalf("uninstalled status = %+v, want missing", status)
	}
}
