package integrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIntegrationVersion(t *testing.T) {
	data := []byte("# openusage-integration-version: 2026-02-23.1\n")
	got := parseIntegrationVersion(data)
	if got != "2026-02-23.1" {
		t.Fatalf("parseIntegrationVersion() = %q, want 2026-02-23.1", got)
	}
}

func TestManagerInstallAndListStatuses(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}
	m := Manager{dirs: dirs}

	for _, id := range []ID{OpenCodeID, CodexID, ClaudeCodeID} {
		if err := m.Install(id); err != nil {
			t.Fatalf("Install(%s) error = %v", id, err)
		}
	}

	statuses := m.ListStatuses()
	if len(statuses) != len(AllDefinitions()) {
		t.Fatalf("ListStatuses() returned %d statuses, want %d", len(statuses), len(AllDefinitions()))
	}

	for _, st := range statuses {
		if st.State != "ready" {
			t.Errorf("status for %s: State = %q, want ready", st.ID, st.State)
		}
		if st.InstalledVersion != IntegrationVersion {
			t.Errorf("status for %s: InstalledVersion = %q, want %q", st.ID, st.InstalledVersion, IntegrationVersion)
		}
	}
}

func TestManagerInstallUnknownID(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}
	m := Manager{dirs: dirs}

	err := m.Install("nonexistent")
	if err == nil {
		t.Fatal("Install(nonexistent) should return an error")
	}
}

func TestManagerListStatusesMissing(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}
	m := Manager{dirs: dirs}

	statuses := m.ListStatuses()
	for _, st := range statuses {
		if st.State != "missing" {
			t.Errorf("status for %s: State = %q, want missing", st.ID, st.State)
		}
	}
}

func TestManagerDetectOutdated(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}

	// Create an old-version hook file for codex.
	def, _ := DefinitionByID(CodexID)
	hookFile := def.TargetFileFunc(dirs)
	configFile := def.ConfigFileFunc(dirs)
	if err := os.MkdirAll(filepath.Dir(hookFile), 0o755); err != nil {
		t.Fatalf("mkdir hook dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(hookFile, []byte("# openusage-integration-version: 2025-01-01\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if err := os.WriteFile(configFile, []byte("notify = [\""+hookFile+"\"]\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := Manager{dirs: dirs}
	statuses := m.ListStatuses()

	var codexStatus Status
	for _, st := range statuses {
		if st.ID == CodexID {
			codexStatus = st
			break
		}
	}
	if !codexStatus.NeedsUpgrade {
		t.Fatalf("codex status.NeedsUpgrade = false, want true")
	}
	if codexStatus.State != "outdated" {
		t.Fatalf("codex status.State = %q, want outdated", codexStatus.State)
	}
}
