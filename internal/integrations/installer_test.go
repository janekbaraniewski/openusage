package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDirs(root string) Dirs {
	return Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}
}

// --- Install tests ---

func TestInstallClaudeCode(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, ok := DefinitionByID(ClaudeCodeID)
	if !ok {
		t.Fatal("ClaudeCodeID definition not found")
	}

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Action != "installed" {
		t.Fatalf("result.Action = %q, want %q", result.Action, "installed")
	}
	if result.InstalledVer != IntegrationVersion {
		t.Fatalf("result.InstalledVer = %q, want %q", result.InstalledVer, IntegrationVersion)
	}

	// Verify template file was created with version marker.
	templateData, err := os.ReadFile(result.TemplateFile)
	if err != nil {
		t.Fatalf("read template file: %v", err)
	}
	templateStr := string(templateData)
	if !strings.Contains(templateStr, "openusage-integration-version: "+IntegrationVersion) {
		t.Fatalf("template missing version marker, got:\n%s", templateStr[:200])
	}
	if strings.Contains(templateStr, "__OPENUSAGE_INTEGRATION_VERSION__") {
		t.Fatal("template still contains unreplaced version placeholder")
	}
	if strings.Contains(templateStr, "__OPENUSAGE_BIN_DEFAULT__") {
		t.Fatal("template still contains unreplaced bin placeholder")
	}
	if !strings.Contains(templateStr, "/usr/local/bin/openusage") {
		t.Fatal("template missing openusage bin path")
	}

	// Verify config was patched correctly.
	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		t.Fatal("config missing hooks key")
	}
	for _, event := range []string{"Stop", "SubagentStop", "PostToolUse"} {
		entries, ok := hooks[event].([]any)
		if !ok || len(entries) == 0 {
			t.Fatalf("config missing hook entries for %s", event)
		}
	}
}

func TestInstallCodex(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, ok := DefinitionByID(CodexID)
	if !ok {
		t.Fatal("CodexID definition not found")
	}

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Action != "installed" {
		t.Fatalf("result.Action = %q, want %q", result.Action, "installed")
	}

	// Verify template.
	templateData, err := os.ReadFile(result.TemplateFile)
	if err != nil {
		t.Fatalf("read template file: %v", err)
	}
	if !strings.Contains(string(templateData), "openusage-integration-version: "+IntegrationVersion) {
		t.Fatal("template missing version marker")
	}

	// Verify config has notify line.
	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	configStr := string(configData)
	if !strings.Contains(configStr, "notify") {
		t.Fatal("config missing notify line")
	}
	if !strings.Contains(configStr, "codex-notify.sh") {
		t.Fatal("config missing hook file reference")
	}
}

func TestInstallOpenCode(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, ok := DefinitionByID(OpenCodeID)
	if !ok {
		t.Fatal("OpenCodeID definition not found")
	}

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Action != "installed" {
		t.Fatalf("result.Action = %q, want %q", result.Action, "installed")
	}

	// Verify template.
	templateData, err := os.ReadFile(result.TemplateFile)
	if err != nil {
		t.Fatalf("read template file: %v", err)
	}
	if !strings.Contains(string(templateData), "openusage-integration-version: "+IntegrationVersion) {
		t.Fatal("template missing version marker")
	}
	if !strings.Contains(string(templateData), "/usr/local/bin/openusage") {
		t.Fatal("template missing openusage bin path")
	}

	// Verify config has plugin entry.
	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	list, ok := cfg["plugin"].([]any)
	if !ok || len(list) == 0 {
		t.Fatal("config missing plugin list")
	}
	wantURL := "file://" + result.TemplateFile
	found := false
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
}

// --- Uninstall tests ---

func TestUninstallClaudeCode(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, _ := DefinitionByID(ClaudeCodeID)

	// Install first.
	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	// Uninstall.
	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	// Template file should be gone.
	if _, err := os.Stat(result.TemplateFile); !os.IsNotExist(err) {
		t.Fatal("template file still exists after uninstall")
	}

	// Config should have hooks removed.
	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	for _, event := range []string{"Stop", "SubagentStop", "PostToolUse"} {
		entries, _ := hooks[event].([]any)
		if len(entries) > 0 {
			t.Fatalf("config still has hook entries for %s after uninstall", event)
		}
	}
}

func TestUninstallCodex(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, _ := DefinitionByID(CodexID)

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	if _, err := os.Stat(result.TemplateFile); !os.IsNotExist(err) {
		t.Fatal("template file still exists after uninstall")
	}

	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(configData), "notify") {
		t.Fatal("config still has notify line after uninstall")
	}
}

func TestUninstallOpenCode(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, _ := DefinitionByID(OpenCodeID)

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	if _, err := os.Stat(result.TemplateFile); !os.IsNotExist(err) {
		t.Fatal("template file still exists after uninstall")
	}

	configData, err := os.ReadFile(result.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	list, _ := cfg["plugin"].([]any)
	if len(list) > 0 {
		t.Fatalf("config still has plugins after uninstall: %#v", list)
	}
}

// --- Idempotency tests ---

func TestInstallIdempotent(t *testing.T) {
	for _, id := range []ID{ClaudeCodeID, CodexID, OpenCodeID} {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			dirs := testDirs(root)
			def, ok := DefinitionByID(id)
			if !ok {
				t.Fatalf("definition not found for %s", id)
			}

			// Install twice.
			result1, err := Install(def, dirs)
			if err != nil {
				t.Fatalf("first Install() error = %v", err)
			}
			result2, err := Install(def, dirs)
			if err != nil {
				t.Fatalf("second Install() error = %v", err)
			}

			// Second install sees existing version, so action is "upgraded".
			if result2.Action != "upgraded" {
				t.Fatalf("second result.Action = %q, want %q", result2.Action, "upgraded")
			}

			// Config should not have duplicate entries.
			configData, err := os.ReadFile(result1.ConfigFile)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}

			switch id {
			case ClaudeCodeID:
				var cfg map[string]any
				if err := json.Unmarshal(configData, &cfg); err != nil {
					t.Fatalf("parse config: %v", err)
				}
				hooks, _ := cfg["hooks"].(map[string]any)
				for _, event := range []string{"Stop", "SubagentStop", "PostToolUse"} {
					entries, _ := hooks[event].([]any)
					if len(entries) != 1 {
						t.Fatalf("expected 1 entry for %s, got %d", event, len(entries))
					}
				}
			case CodexID:
				// Count lines that start with "notify" key assignment.
				notifyCount := 0
				for _, line := range strings.Split(string(configData), "\n") {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "notify") && strings.Contains(trimmed, "=") {
						notifyCount++
					}
				}
				if notifyCount != 1 {
					t.Fatalf("expected 1 notify key line, found %d in:\n%s", notifyCount, string(configData))
				}
			case OpenCodeID:
				var cfg map[string]any
				if err := json.Unmarshal(configData, &cfg); err != nil {
					t.Fatalf("parse config: %v", err)
				}
				list, _ := cfg["plugin"].([]any)
				if len(list) != 1 {
					t.Fatalf("expected 1 plugin entry, got %d", len(list))
				}
			}
		})
	}
}

// --- E2E lifecycle test ---

func TestLifecycle_InstallDetectUpgradeUninstall(t *testing.T) {
	for _, id := range []ID{ClaudeCodeID, CodexID, OpenCodeID} {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			dirs := testDirs(root)
			def, ok := DefinitionByID(id)
			if !ok {
				t.Fatalf("definition not found for %s", id)
			}

			// Phase 1: Before install, status should be "missing".
			st := def.Detector(dirs)
			if st.State != "missing" {
				t.Fatalf("before install: state = %q, want %q", st.State, "missing")
			}
			if st.Installed {
				t.Fatal("before install: Installed should be false")
			}

			// Phase 2: Install.
			result, err := Install(def, dirs)
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if result.Action != "installed" {
				t.Fatalf("Install action = %q, want %q", result.Action, "installed")
			}

			// Phase 3: After install, status should be "ready".
			st = def.Detector(dirs)
			if st.State != "ready" {
				t.Fatalf("after install: state = %q, want %q", st.State, "ready")
			}
			if !st.Installed || !st.Configured {
				t.Fatalf("after install: Installed=%v, Configured=%v", st.Installed, st.Configured)
			}
			if st.InstalledVersion != IntegrationVersion {
				t.Fatalf("after install: version = %q, want %q", st.InstalledVersion, IntegrationVersion)
			}
			if st.NeedsUpgrade {
				t.Fatal("after install: NeedsUpgrade should be false")
			}

			// Phase 4: Simulate old version → detect as outdated.
			targetFile := def.TargetFileFunc(dirs)
			oldContent, err := os.ReadFile(targetFile)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			// Replace version marker with an old one.
			oldStr := strings.ReplaceAll(string(oldContent),
				"openusage-integration-version: "+IntegrationVersion,
				"openusage-integration-version: 2020-01-01.0")
			if err := os.WriteFile(targetFile, []byte(oldStr), def.TemplateFileMode); err != nil {
				t.Fatalf("write old version: %v", err)
			}
			st = def.Detector(dirs)
			if st.State != "outdated" {
				t.Fatalf("after downgrade: state = %q, want %q", st.State, "outdated")
			}
			if !st.NeedsUpgrade {
				t.Fatal("after downgrade: NeedsUpgrade should be true")
			}

			// Phase 5: Upgrade.
			upgradeResult, err := Upgrade(def, dirs)
			if err != nil {
				t.Fatalf("Upgrade() error = %v", err)
			}
			if upgradeResult.Action != "upgraded" {
				t.Fatalf("Upgrade action = %q, want %q", upgradeResult.Action, "upgraded")
			}
			if upgradeResult.PreviousVer != "2020-01-01.0" {
				t.Fatalf("Upgrade PreviousVer = %q, want %q", upgradeResult.PreviousVer, "2020-01-01.0")
			}

			// Phase 6: After upgrade, status should be "ready" again.
			st = def.Detector(dirs)
			if st.State != "ready" {
				t.Fatalf("after upgrade: state = %q, want %q", st.State, "ready")
			}
			if st.NeedsUpgrade {
				t.Fatal("after upgrade: NeedsUpgrade should be false")
			}

			// Phase 7: Uninstall.
			if err := Uninstall(def, dirs); err != nil {
				t.Fatalf("Uninstall() error = %v", err)
			}

			// Phase 8: After uninstall, status should be "missing".
			st = def.Detector(dirs)
			if st.Installed {
				t.Fatal("after uninstall: Installed should be false")
			}
			// Config file still exists (just patched), but template is gone.
			if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
				t.Fatal("after uninstall: template file should not exist")
			}
		})
	}
}

// --- Upgrade tests ---

func TestUpgrade(t *testing.T) {
	for _, id := range []ID{ClaudeCodeID, CodexID, OpenCodeID} {
		t.Run(string(id), func(t *testing.T) {
			root := t.TempDir()
			dirs := testDirs(root)
			def, ok := DefinitionByID(id)
			if !ok {
				t.Fatalf("definition not found for %s", id)
			}

			targetFile := def.TargetFileFunc(dirs)

			// Seed an old version template file.
			if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			oldContent := "# openusage-integration-version: 2025-01-01.0\nold content\n"
			if err := os.WriteFile(targetFile, []byte(oldContent), def.TemplateFileMode); err != nil {
				t.Fatalf("write old template: %v", err)
			}

			result, err := Upgrade(def, dirs)
			if err != nil {
				t.Fatalf("Upgrade() error = %v", err)
			}
			if result.Action != "upgraded" {
				t.Fatalf("result.Action = %q, want %q", result.Action, "upgraded")
			}
			if result.PreviousVer != "2025-01-01.0" {
				t.Fatalf("result.PreviousVer = %q, want %q", result.PreviousVer, "2025-01-01.0")
			}
			if result.InstalledVer != IntegrationVersion {
				t.Fatalf("result.InstalledVer = %q, want %q", result.InstalledVer, IntegrationVersion)
			}

			// Verify new version is in the template.
			templateData, err := os.ReadFile(targetFile)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			ver := parseIntegrationVersion(templateData)
			if ver != IntegrationVersion {
				t.Fatalf("template version = %q, want %q", ver, IntegrationVersion)
			}
		})
	}
}
