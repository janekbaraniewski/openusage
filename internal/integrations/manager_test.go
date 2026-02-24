package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIntegrationVersion(t *testing.T) {
	data := []byte("# openusage-integration-version: 2026-02-23.1\n")
	got := parseIntegrationVersion(data)
	if got != "2026-02-23.1" {
		t.Fatalf("parseIntegrationVersion() = %q, want 2026-02-23.1", got)
	}
}

func TestInstallOpenCodeAndDetectReady(t *testing.T) {
	root := t.TempDir()
	m := Manager{
		openCodeConfigFile: filepath.Join(root, "opencode", "opencode.json"),
		openCodePluginFile: filepath.Join(root, "opencode", "plugins", "openusage-telemetry.ts"),
		openusageBin:       "/tmp/openusage-bin",
	}

	if err := m.Install(OpenCodeID); err != nil {
		t.Fatalf("Install(OpenCodeID) error = %v", err)
	}

	pluginData, err := os.ReadFile(m.openCodePluginFile)
	if err != nil {
		t.Fatalf("read plugin file: %v", err)
	}
	plugin := string(pluginData)
	if !strings.Contains(plugin, "openusage-integration-version: "+IntegrationVersion) {
		t.Fatalf("plugin missing integration version marker: %q", plugin)
	}
	if !strings.Contains(plugin, "/tmp/openusage-bin") {
		t.Fatalf("plugin missing pinned openusage bin: %q", plugin)
	}

	cfgData, err := os.ReadFile(m.openCodeConfigFile)
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		t.Fatalf("parse opencode config: %v", err)
	}
	list, ok := cfg["plugin"].([]any)
	if !ok {
		t.Fatalf("opencode config missing plugin list: %#v", cfg)
	}
	found := false
	wantURL := "file://" + m.openCodePluginFile
	for _, item := range list {
		text, _ := item.(string)
		if text == wantURL {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("plugin list missing %q: %#v", wantURL, list)
	}

	status := m.detectOpenCode()
	if status.State != "ready" {
		t.Fatalf("status.State = %q, want ready", status.State)
	}
	if status.InstalledVersion != IntegrationVersion {
		t.Fatalf("status.InstalledVersion = %q, want %q", status.InstalledVersion, IntegrationVersion)
	}
}

func TestDetectCodexOutdated(t *testing.T) {
	root := t.TempDir()
	hookPath := filepath.Join(root, "hooks", "codex-notify.sh")
	configPath := filepath.Join(root, "codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatalf("mkdir hook dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte("# openusage-integration-version: 2025-01-01\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("notify = [\""+hookPath+"\"]\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := Manager{
		codexHookFile:   hookPath,
		codexConfigFile: configPath,
	}
	status := m.detectCodex()
	if !status.NeedsUpgrade {
		t.Fatalf("status.NeedsUpgrade = false, want true")
	}
	if status.State != "outdated" {
		t.Fatalf("status.State = %q, want outdated", status.State)
	}
}

func TestInstallClaudeCodeAndDetectReady(t *testing.T) {
	root := t.TempDir()
	m := Manager{
		claudeHookFile:     filepath.Join(root, "hooks", "claude-hook.sh"),
		claudeSettingsFile: filepath.Join(root, "claude", "settings.json"),
		openusageBin:       "/tmp/openusage-bin",
	}
	if err := m.Install(ClaudeCodeID); err != nil {
		t.Fatalf("Install(ClaudeCodeID) error = %v", err)
	}

	status := m.detectClaudeCode()
	if status.State != "ready" {
		t.Fatalf("status.State = %q, want ready", status.State)
	}
	if status.InstalledVersion != IntegrationVersion {
		t.Fatalf("status.InstalledVersion = %q, want %q", status.InstalledVersion, IntegrationVersion)
	}
}
