package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchAntigravityConfig(t *testing.T) {
	input := []byte(`{"theme":"dark","statusLine":{"enabled":true,"padding":0}}`)
	patched, err := patchAntigravityConfig(input, "/tmp/open usage", true)
	if err != nil {
		t.Fatalf("patchAntigravityConfig(install) error = %v", err)
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
	if want := `"/tmp/open usage" antigravity statusline`; command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}
	if status["stack_with_default"] != true {
		t.Fatalf("stack_with_default = %#v, want true", status["stack_with_default"])
	}

	uninstalled, err := patchAntigravityConfig(patched, "/tmp/open usage", false)
	if err != nil {
		t.Fatalf("patchAntigravityConfig(uninstall) error = %v", err)
	}
	var uninstalledCfg map[string]any
	if err := json.Unmarshal(uninstalled, &uninstalledCfg); err != nil {
		t.Fatalf("parse uninstalled config: %v", err)
	}
	uninstalledStatus, _ := uninstalledCfg["statusLine"].(map[string]any)
	if _, exists := uninstalledStatus["command"]; exists {
		t.Fatalf("OpenUsage command survived uninstall: %#v", uninstalledStatus)
	}
}

func TestPatchAntigravityConfigRefusesCustomCommand(t *testing.T) {
	input := []byte(`{"statusLine":{"type":"command","command":"my-statusline"}}`)
	if _, err := patchAntigravityConfig(input, "/tmp/openusage", true); err == nil {
		t.Fatal("patchAntigravityConfig() replaced an unrelated custom command")
	}
	unchanged, err := patchAntigravityConfig(input, "/tmp/openusage", false)
	if err != nil {
		t.Fatalf("uninstall custom command error = %v", err)
	}
	if strings.TrimSpace(string(unchanged)) != strings.TrimSpace(string(input)) {
		t.Fatalf("uninstall changed unrelated config: %s", unchanged)
	}
}

func TestAntigravityCommandBinaryRoundTrip(t *testing.T) {
	for _, bin := range []string{"/tmp/open usage", `/tmp/a$b/openusage`, `/tmp/q"x\y/openusage`} {
		command := antigravityStatuslineCommand(bin)
		if got := antigravityCommandBinary(command); got != bin {
			t.Errorf("antigravityCommandBinary(%q) = %q, want %q", command, got, bin)
		}
		if got := antigravityCommandBinary(command + " --state-file /tmp/x.json"); got != bin {
			t.Errorf("with flags: got %q, want %q", got, bin)
		}
	}
	if got := antigravityCommandBinary("openusage antigravity statusline"); got != "openusage" {
		t.Errorf("bare command: got %q, want openusage", got)
	}
}

func TestAntigravityInstallLifecycle(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: filepath.Join(root, "bin", "openusage"),
	}
	def, ok := DefinitionByID(AntigravityID)
	if !ok {
		t.Fatal("antigravity definition not found")
	}
	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.ConfigFile != filepath.Join(root, ".gemini", "antigravity-cli", "settings.json") {
		t.Fatalf("config file = %q", result.ConfigFile)
	}
	if err := os.MkdirAll(filepath.Dir(dirs.OpenusageBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dirs.OpenusageBin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	status := def.Detector(dirs)
	if status.State != "ready" || !status.Installed || !status.Configured {
		t.Fatalf("installed status = %+v, want ready", status)
	}
	// A binary that moved away leaves Antigravity calling a dead path.
	if err := os.Remove(dirs.OpenusageBin); err != nil {
		t.Fatal(err)
	}
	status = def.Detector(dirs)
	if status.State != "outdated" || !status.NeedsUpgrade {
		t.Fatalf("missing-binary status = %+v, want outdated", status)
	}
	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	status = def.Detector(dirs)
	if status.State != "missing" {
		t.Fatalf("uninstalled status = %+v, want missing", status)
	}
}
