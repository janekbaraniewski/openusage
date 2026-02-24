package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
)

func TestMatchDetected_AccountsByProviderID(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := Dirs{
		Home:         tmpDir,
		ConfigRoot:   filepath.Join(tmpDir, ".config"),
		HooksDir:     filepath.Join(tmpDir, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}

	detected := detect.Result{
		Tools: []detect.DetectedTool{
			{Name: "Claude Code CLI", BinaryPath: "/usr/bin/claude", Type: "cli"},
			{Name: "OpenAI Codex CLI", BinaryPath: "/usr/bin/codex", Type: "cli"},
		},
		Accounts: []core.AccountConfig{
			{ID: "claude_code", Provider: "claude_code"},
			{ID: "codex", Provider: "codex"},
			{ID: "opencode", Provider: "opencode"},
		},
	}

	defs := AllDefinitions()
	matches := MatchDetected(defs, detected, dirs)

	if len(matches) != len(defs) {
		t.Fatalf("expected %d matches (one per definition), got %d", len(defs), len(matches))
	}

	for _, m := range matches {
		switch m.Definition.ID {
		case ClaudeCodeID:
			if m.Account == nil || m.Account.Provider != "claude_code" {
				t.Errorf("claude_code: expected account with Provider=claude_code, got %v", m.Account)
			}
		case CodexID:
			if m.Account == nil || m.Account.Provider != "codex" {
				t.Errorf("codex: expected account with Provider=codex, got %v", m.Account)
			}
		case OpenCodeID:
			if m.Account == nil || m.Account.Provider != "opencode" {
				t.Errorf("opencode: expected account with Provider=opencode, got %v", m.Account)
			}
		}
	}
}

func TestMatchDetected_OpenCodeNoTool(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := Dirs{
		Home:         tmpDir,
		ConfigRoot:   filepath.Join(tmpDir, ".config"),
		HooksDir:     filepath.Join(tmpDir, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}

	detected := detect.Result{
		Accounts: []core.AccountConfig{
			{ID: "opencode", Provider: "opencode"},
		},
	}

	defs := AllDefinitions()
	matches := MatchDetected(defs, detected, dirs)

	var ocMatch *Match
	for i := range matches {
		if matches[i].Definition.ID == OpenCodeID {
			ocMatch = &matches[i]
			break
		}
	}
	if ocMatch == nil {
		t.Fatal("expected a match for OpenCode definition")
	}

	if ocMatch.Tool != nil {
		t.Errorf("expected Tool to be nil for OpenCode (no DetectedTool), got %+v", ocMatch.Tool)
	}
	if ocMatch.Account == nil {
		t.Fatal("expected Account to be set for OpenCode")
	}
	if ocMatch.Account.Provider != "opencode" {
		t.Errorf("expected Account.Provider=opencode, got %s", ocMatch.Account.Provider)
	}
	// Not installed, so should be actionable.
	if !ocMatch.Actionable {
		t.Error("expected Actionable=true for OpenCode with account detected and integration missing")
	}
}

func TestMatchDetected_UnmatchedAccountNoExtraMatch(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := Dirs{
		Home:         tmpDir,
		ConfigRoot:   filepath.Join(tmpDir, ".config"),
		HooksDir:     filepath.Join(tmpDir, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}

	detected := detect.Result{
		Accounts: []core.AccountConfig{
			{ID: "openai", Provider: "openai"},
		},
	}

	defs := AllDefinitions()
	matches := MatchDetected(defs, detected, dirs)

	if len(matches) != len(defs) {
		t.Fatalf("expected exactly %d matches (one per definition), got %d", len(defs), len(matches))
	}

	for _, m := range matches {
		if m.Account != nil {
			t.Errorf("definition %s: expected no account match for Provider=openai, got %+v", m.Definition.ID, m.Account)
		}
		if m.Actionable {
			t.Errorf("definition %s: expected Actionable=false when no account or tool matched", m.Definition.ID)
		}
	}
}

func TestMatchDetected_InstalledIntegrationNotActionable(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := Dirs{
		Home:         tmpDir,
		ConfigRoot:   filepath.Join(tmpDir, ".config"),
		HooksDir:     filepath.Join(tmpDir, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}

	// Create the claude hook file with correct version to make it "installed".
	hookDir := filepath.Join(dirs.HooksDir)
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookContent := "#!/bin/bash\n# OPENUSAGE_INTEGRATION_VERSION=" + IntegrationVersion + "\n"
	hookFile := filepath.Join(hookDir, "claude-hook.sh")
	if err := os.WriteFile(hookFile, []byte(hookContent), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create the claude settings file with hooks configured.
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": hookFile},
					},
				},
			},
			"SubagentStop": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": hookFile},
					},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": hookFile},
					},
				},
			},
		},
	}

	// We need to use the correct settings file path. The claude detector uses
	// ConfigFileFunc which checks CLAUDE_SETTINGS_FILE env, then falls back to
	// ~/.claude/settings.json
	settingsFile := filepath.Join(claudeDir, "settings.json")
	settingsData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, settingsData, 0o644); err != nil {
		t.Fatal(err)
	}

	detected := detect.Result{
		Tools: []detect.DetectedTool{
			{Name: "Claude Code CLI", BinaryPath: "/usr/bin/claude", Type: "cli"},
		},
		Accounts: []core.AccountConfig{
			{ID: "claude_code", Provider: "claude_code"},
		},
	}

	defs := AllDefinitions()
	matches := MatchDetected(defs, detected, dirs)

	var claudeMatch *Match
	for i := range matches {
		if matches[i].Definition.ID == ClaudeCodeID {
			claudeMatch = &matches[i]
			break
		}
	}
	if claudeMatch == nil {
		t.Fatal("expected a match for Claude Code definition")
	}

	if claudeMatch.Status.State != "ready" {
		t.Errorf("expected status State=ready, got %q", claudeMatch.Status.State)
	}
	if claudeMatch.Actionable {
		t.Error("expected Actionable=false when integration is already installed (state=ready)")
	}
	if claudeMatch.Account == nil {
		t.Error("expected Account to still be matched even when installed")
	}
	if claudeMatch.Tool == nil {
		t.Error("expected Tool to still be matched even when installed")
	}
}

func TestMatchDetected_ToolNameMatching(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := Dirs{
		Home:         tmpDir,
		ConfigRoot:   filepath.Join(tmpDir, ".config"),
		HooksDir:     filepath.Join(tmpDir, ".config", "openusage", "hooks"),
		OpenusageBin: "/usr/local/bin/openusage",
	}

	detected := detect.Result{
		Tools: []detect.DetectedTool{
			{Name: "Claude Code CLI", BinaryPath: "/usr/bin/claude", Type: "cli"},
			{Name: "OpenAI Codex CLI", BinaryPath: "/usr/bin/codex", Type: "cli"},
			{Name: "Unrelated Tool", BinaryPath: "/usr/bin/other", Type: "cli"},
		},
	}

	defs := AllDefinitions()
	matches := MatchDetected(defs, detected, dirs)

	for _, m := range matches {
		switch m.Definition.ID {
		case ClaudeCodeID:
			// MatchToolNameHint is "Claude Code", tool Name is "Claude Code CLI"
			if m.Tool == nil {
				t.Error("claude_code: expected tool match for 'Claude Code CLI' with hint 'Claude Code'")
			} else if !strings.Contains(m.Tool.Name, "Claude Code") {
				t.Errorf("claude_code: expected tool name containing 'Claude Code', got %q", m.Tool.Name)
			}
		case CodexID:
			// MatchToolNameHint is "Codex", tool Name is "OpenAI Codex CLI"
			if m.Tool == nil {
				t.Error("codex: expected tool match for 'OpenAI Codex CLI' with hint 'Codex'")
			} else if !strings.Contains(m.Tool.Name, "Codex") {
				t.Errorf("codex: expected tool name containing 'Codex', got %q", m.Tool.Name)
			}
		case OpenCodeID:
			// MatchToolNameHint is "", so no tool match expected
			if m.Tool != nil {
				t.Errorf("opencode: expected no tool match (empty hint), got %+v", m.Tool)
			}
		}
	}
}
