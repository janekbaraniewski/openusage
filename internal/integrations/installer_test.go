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

	writesArtifact := def.WritesArtifact == nil || def.WritesArtifact(dirs)
	if writesArtifact {
		// Verify template (Unix: a codex-notify.sh script is written).
		templateData, err := os.ReadFile(result.TemplateFile)
		if err != nil {
			t.Fatalf("read template file: %v", err)
		}
		if !strings.Contains(string(templateData), "openusage-integration-version: "+IntegrationVersion) {
			t.Fatal("template missing version marker")
		}
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
	if writesArtifact {
		if !strings.Contains(configStr, "codex-notify.sh") {
			t.Fatal("config missing hook file reference")
		}
	} else {
		// Windows: the openusage binary is registered directly with the
		// telemetry hook subcommand instead of a script file.
		if !strings.Contains(configStr, "telemetry") || !strings.Contains(configStr, "hook") || !strings.Contains(configStr, "codex") {
			t.Fatalf("config missing binary notify registration: %s", configStr)
		}
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

	// The plugin lands in OpenCode's global plugin directory, which OpenCode
	// loads automatically, so install must not write a config file — doing so
	// would create an opencode.json for users who deliberately have none.
	if _, err := os.Stat(result.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("install created a config file at %s (err = %v), want none", result.ConfigFile, err)
	}

	// And it must still report READY rather than PARTIAL.
	st := def.Detector(dirs)
	if !st.Configured {
		t.Errorf("Configured = false after install; auto-discovery should count as configured")
	}
	if st.State != "ready" {
		t.Errorf("State = %q, want %q", st.State, "ready")
	}
}

// An authoritative opencode.jsonc must be read as JSONC and left alone rather
// than shadowed by a competing strict-JSON opencode.json. Regression test for
// the PARTIAL false negative in #313.
func TestInstallOpenCodeLeavesJSONCConfigUntouched(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, ok := DefinitionByID(OpenCodeID)
	if !ok {
		t.Fatal("OpenCodeID definition not found")
	}

	configDir := filepath.Join(root, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsoncPath := filepath.Join(configDir, "opencode.jsonc")
	original := `{
  // the model I actually use
  "model": "anthropic/claude-sonnet-4-5",
}
`
	if err := os.WriteFile(jsoncPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if result.ConfigFile != jsoncPath {
		t.Errorf("ConfigFile = %q, want the existing jsonc at %q", result.ConfigFile, jsoncPath)
	}
	got, err := os.ReadFile(jsoncPath)
	if err != nil {
		t.Fatalf("read jsonc: %v", err)
	}
	if string(got) != original {
		t.Errorf("opencode.jsonc was rewritten:\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
	if _, err := os.Stat(filepath.Join(configDir, "opencode.json")); !os.IsNotExist(err) {
		t.Error("install created a competing opencode.json next to the authoritative .jsonc")
	}

	st := def.Detector(dirs)
	if st.State != "ready" {
		t.Errorf("State = %q, want %q with a JSONC config", st.State, "ready")
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

	if def.WritesArtifact == nil || def.WritesArtifact(dirs) {
		if _, err := os.Stat(result.TemplateFile); !os.IsNotExist(err) {
			t.Fatal("template file still exists after uninstall")
		}
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

	// Install wrote no config, so uninstall has none to clean up.
	if _, err := os.Stat(result.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("config file exists at %s (err = %v), want none", result.ConfigFile, err)
	}
}

// Configs written by older openusage versions carry an explicit file:// plugin
// entry pointing at the artifact uninstall deletes. That entry must still be
// removed, and unrelated plugins left in place.
func TestUninstallOpenCodeRemovesLegacyRegistration(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, _ := DefinitionByID(OpenCodeID)

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	// Marshal rather than interpolate: on Windows TemplateFile contains
	// backslashes, which are escape sequences inside a JSON string literal.
	configPath := filepath.Join(root, ".config", "opencode", "opencode.json")
	legacy, err := json.Marshal(map[string]any{
		"plugin": []string{"file://" + result.TemplateFile, "some-other-plugin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	list, _ := cfg["plugin"].([]any)
	if len(list) != 1 || list[0] != "some-other-plugin" {
		t.Fatalf("plugin list = %#v, want only [\"some-other-plugin\"]", list)
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

			// Second install sees the existing version embedded in the artifact
			// file, so the action is "upgraded". Integrations that write no
			// artifact file (e.g. Codex on Windows registers the binary
			// directly) have no on-disk version to detect, so they report
			// "installed" each time.
			writesArtifact := def.WritesArtifact == nil || def.WritesArtifact(dirs)
			wantAction := "upgraded"
			if !writesArtifact {
				wantAction = "installed"
			}
			if result2.Action != wantAction {
				t.Fatalf("second result.Action = %q, want %q", result2.Action, wantAction)
			}

			// OpenCode relies on plugin-directory auto-discovery and writes
			// no config, so there is nothing that could duplicate.
			if id == OpenCodeID {
				if _, err := os.Stat(result1.ConfigFile); !os.IsNotExist(err) {
					t.Fatalf("install created a config file at %s (err = %v), want none", result1.ConfigFile, err)
				}
				return
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

			targetFile := def.TargetFileFunc(dirs)
			writesArtifact := def.WritesArtifact == nil || def.WritesArtifact(dirs)

			// Phases 4-6 simulate an on-disk version downgrade then upgrade.
			// These only apply to integrations that write an artifact file;
			// integrations that register the openusage binary directly (e.g.
			// Codex on Windows) have no on-disk version to downgrade.
			if writesArtifact {
				// Phase 4: Simulate old version → detect as outdated.
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
			// Config file still exists (just patched), but the artifact file
			// (when one is written) is gone.
			if writesArtifact {
				if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
					t.Fatal("after uninstall: template file should not exist")
				}
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
			writesArtifact := def.WritesArtifact == nil || def.WritesArtifact(dirs)

			if writesArtifact {
				// Seed an old version template file.
				if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				oldContent := "# openusage-integration-version: 2025-01-01.0\nold content\n"
				if err := os.WriteFile(targetFile, []byte(oldContent), def.TemplateFileMode); err != nil {
					t.Fatalf("write old template: %v", err)
				}
			}

			result, err := Upgrade(def, dirs)
			if err != nil {
				t.Fatalf("Upgrade() error = %v", err)
			}
			if result.Action != "upgraded" {
				t.Fatalf("result.Action = %q, want %q", result.Action, "upgraded")
			}
			if result.InstalledVer != IntegrationVersion {
				t.Fatalf("result.InstalledVer = %q, want %q", result.InstalledVer, IntegrationVersion)
			}

			if writesArtifact {
				if result.PreviousVer != "2025-01-01.0" {
					t.Fatalf("result.PreviousVer = %q, want %q", result.PreviousVer, "2025-01-01.0")
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
			}
		})
	}
}

// Uninstalling from a JSONC config must excise only our registration and leave
// every comment intact. Re-serializing through encoding/json would strip them,
// which is the same harm as creating a competing strict-JSON file.
func TestUninstallOpenCodePreservesJSONCComments(t *testing.T) {
	root := t.TempDir()
	dirs := testDirs(root)
	def, _ := DefinitionByID(OpenCodeID)

	result, err := Install(def, dirs)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	configDir := filepath.Join(root, ".config", "opencode")
	jsoncPath := filepath.Join(configDir, "opencode.jsonc")
	original := `{
  // the model I actually use
  "model": "anthropic/claude-sonnet-4-5",
  "plugin": [
    "keep-me",
    // ours, added by an older openusage
    ` + string(mustJSONString(t, "file://"+result.TemplateFile)) + `
  ]
}
`
	if err := os.WriteFile(jsoncPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(def, dirs); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	got, err := os.ReadFile(jsoncPath)
	if err != nil {
		t.Fatalf("read jsonc: %v", err)
	}
	text := string(got)

	if strings.Contains(text, "openusage-telemetry.ts") {
		t.Errorf("our registration survived uninstall:\n%s", text)
	}
	if !strings.Contains(text, "// the model I actually use") {
		t.Errorf("uninstall stripped a comment:\n%s", text)
	}
	if !strings.Contains(text, `"keep-me"`) {
		t.Errorf("uninstall dropped an unrelated plugin:\n%s", text)
	}
	// The result must still parse, and the dangling comma left by removing the
	// last array element must be gone so it is valid strict JSON too.
	var cfg map[string]any
	if err := json.Unmarshal(stripJSONC(got), &cfg); err != nil {
		t.Fatalf("result does not parse after comment stripping: %v\n%s", err, text)
	}
	if list, _ := cfg["plugin"].([]any); len(list) != 1 || list[0] != "keep-me" {
		t.Errorf("plugin list = %#v, want [keep-me]", cfg["plugin"])
	}
}

func mustJSONString(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
