package integrations

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed assets/opencode-telemetry.ts.tpl
var opencodeTemplate string

//go:embed assets/codex-notify.sh.tpl
var codexTemplate string

//go:embed assets/claude-hook.sh.tpl
var claudeTemplate string

// AllDefinitions returns the built-in integration definitions.
func AllDefinitions() []Definition {
	return []Definition{
		claudeCodeDef(),
		codexDef(),
		opencodeDef(),
	}
}

// DefinitionByID returns the definition with the given ID, or false if not found.
func DefinitionByID(id ID) (Definition, bool) {
	for _, def := range AllDefinitions() {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

func claudeCodeDef() Definition {
	return Definition{
		ID:          ClaudeCodeID,
		Name:        "Claude Code Hooks",
		Description: "Telemetry hooks for Claude Code (Stop, SubagentStop, PostToolUse)",
		Type:        TypeHookScript,
		Template:    claudeTemplate,

		TargetFileFunc: func(dirs Dirs) string {
			return filepath.Join(dirs.HooksDir, "claude-hook.sh")
		},
		ConfigFileFunc: func(dirs Dirs) string {
			if f := strings.TrimSpace(os.Getenv("CLAUDE_SETTINGS_FILE")); f != "" {
				return f
			}
			return filepath.Join(dirs.Home, ".claude", "settings.json")
		},
		ConfigFormat:  ConfigJSON,
		ConfigPatcher: patchClaudeCodeConfig,
		Detector:      detectClaudeCodeStatus,

		MatchProviderIDs:  []string{"claude_code"},
		MatchToolNameHint: "Claude Code",
		TemplateFileMode:  0o755,
		EscapeBin:         escapeForShellString,
	}
}

func codexDef() Definition {
	return Definition{
		ID:          CodexID,
		Name:        "Codex Notify Hook",
		Description: "Telemetry notify hook for OpenAI Codex CLI",
		Type:        TypeHookScript,
		Template:    codexTemplate,

		TargetFileFunc: func(dirs Dirs) string {
			return filepath.Join(dirs.HooksDir, "codex-notify.sh")
		},
		ConfigFileFunc: func(dirs Dirs) string {
			codexDir := strings.TrimSpace(os.Getenv("CODEX_CONFIG_DIR"))
			if codexDir == "" {
				codexDir = filepath.Join(dirs.Home, ".codex")
			}
			return filepath.Join(codexDir, "config.toml")
		},
		ConfigFormat:  ConfigTOML,
		ConfigPatcher: patchCodexConfig,
		Detector:      detectCodexStatus,

		MatchProviderIDs:  []string{"codex"},
		MatchToolNameHint: "Codex",
		TemplateFileMode:  0o755,
		EscapeBin:         escapeForShellString,
	}
}

func opencodeDef() Definition {
	return Definition{
		ID:          OpenCodeID,
		Name:        "OpenCode Plugin",
		Description: "Telemetry plugin for OpenCode IDE",
		Type:        TypePlugin,
		Template:    opencodeTemplate,

		TargetFileFunc: func(dirs Dirs) string {
			return filepath.Join(dirs.ConfigRoot, "opencode", "plugins", "openusage-telemetry.ts")
		},
		ConfigFileFunc: func(dirs Dirs) string {
			return filepath.Join(dirs.ConfigRoot, "opencode", "opencode.json")
		},
		ConfigFormat:  ConfigJSON,
		ConfigPatcher: patchOpenCodeConfig,
		Detector:      detectOpenCodeStatus,

		MatchProviderIDs:  []string{"opencode"},
		MatchToolNameHint: "",
		TemplateFileMode:  0o644,
		EscapeBin:         escapeForTSString,
	}
}

// --- Config patchers ---

func patchClaudeCodeConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	cfg := map[string]any{}
	if len(bytes.TrimSpace(configData)) > 0 {
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("parse claude settings: %w", err)
		}
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	cfg["hooks"] = hooks

	syncEvents := []string{"Stop", "SubagentStop"}
	asyncEvents := []string{"PostToolUse"}

	hookEntry := func(async bool) map[string]any {
		h := map[string]any{
			"type":    "command",
			"command": targetFile,
			"timeout": 30,
		}
		if async {
			h["async"] = true
		}
		return h
	}

	allEvents := append(syncEvents, asyncEvents...)

	if install {
		for _, event := range syncEvents {
			entries, _ := hooks[event].([]any)
			entries = removeCommandEntries(entries, targetFile)
			entries = append(entries, map[string]any{
				"matcher": "*",
				"hooks":   []any{hookEntry(false)},
			})
			hooks[event] = entries
		}
		for _, event := range asyncEvents {
			entries, _ := hooks[event].([]any)
			entries = removeCommandEntries(entries, targetFile)
			entries = append(entries, map[string]any{
				"matcher": "*",
				"hooks":   []any{hookEntry(true)},
			})
			hooks[event] = entries
		}
	} else {
		for _, event := range allEvents {
			entries, _ := hooks[event].([]any)
			hooks[event] = removeCommandEntries(entries, targetFile)
		}
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize claude settings: %w", err)
	}
	return append(payload, '\n'), nil
}

func patchCodexConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	notifyLine := fmt.Sprintf("notify = [\"%s\"]", targetFile)

	if install {
		out := notifyLine + "\n"
		if len(configData) > 0 {
			lines := strings.Split(string(configData), "\n")
			replaced := false
			for i, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "notify") && strings.Contains(line, "=") {
					lines[i] = notifyLine
					replaced = true
					break
				}
			}
			if !replaced {
				if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
					lines = append(lines, "")
				}
				lines = append(lines, notifyLine)
			}
			out = strings.Join(lines, "\n")
			if !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
		}
		return []byte(out), nil
	}

	// Uninstall: remove the notify line.
	if len(configData) == 0 {
		return configData, nil
	}
	lines := strings.Split(string(configData), "\n")
	var filtered []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "notify") && strings.Contains(line, "=") {
			continue
		}
		filtered = append(filtered, line)
	}
	out := strings.Join(filtered, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out), nil
}

func patchOpenCodeConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}
	if len(bytes.TrimSpace(configData)) > 0 {
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("parse opencode config: %w", err)
		}
	}

	plugins := []string{}
	if raw, ok := cfg["plugin"].([]any); ok {
		for _, item := range raw {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				plugins = append(plugins, text)
			}
		}
	}

	pluginURL := "file://" + targetFile

	if install {
		if !slices.Contains(plugins, pluginURL) {
			plugins = append(plugins, pluginURL)
		}
	} else {
		plugins = slices.DeleteFunc(plugins, func(s string) bool {
			return s == pluginURL || strings.Contains(s, "openusage-telemetry.ts")
		})
	}
	cfg["plugin"] = plugins

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize opencode config: %w", err)
	}
	return append(payload, '\n'), nil
}

// --- Detectors ---

func detectClaudeCodeStatus(dirs Dirs) Status {
	def := claudeCodeDef()
	st := Status{
		ID:             ClaudeCodeID,
		Name:           def.Name,
		DesiredVersion: IntegrationVersion,
	}

	hookFile := def.TargetFileFunc(dirs)
	hookData, hookErr := os.ReadFile(hookFile)
	st.Installed = hookErr == nil
	st.InstalledVersion = parseIntegrationVersion(hookData)

	configured := false
	configFile := def.ConfigFileFunc(dirs)
	if settingsData, err := os.ReadFile(configFile); err == nil {
		var cfg map[string]any
		if json.Unmarshal(settingsData, &cfg) == nil {
			configured = hasCommandHook(cfg, "Stop", "claude-hook.sh") &&
				hasCommandHook(cfg, "SubagentStop", "claude-hook.sh") &&
				hasCommandHook(cfg, "PostToolUse", "claude-hook.sh")
		}
	}
	st.Configured = configured
	deriveState(&st)
	return st
}

func detectCodexStatus(dirs Dirs) Status {
	def := codexDef()
	st := Status{
		ID:             CodexID,
		Name:           def.Name,
		DesiredVersion: IntegrationVersion,
	}

	hookFile := def.TargetFileFunc(dirs)
	hookData, hookErr := os.ReadFile(hookFile)
	st.Installed = hookErr == nil
	st.InstalledVersion = parseIntegrationVersion(hookData)

	configured := false
	configFile := def.ConfigFileFunc(dirs)
	if cfgData, err := os.ReadFile(configFile); err == nil {
		content := string(cfgData)
		if strings.Contains(content, "notify") && strings.Contains(content, "codex-notify.sh") {
			configured = true
		}
	}
	st.Configured = configured
	deriveState(&st)
	return st
}

func detectOpenCodeStatus(dirs Dirs) Status {
	def := opencodeDef()
	st := Status{
		ID:             OpenCodeID,
		Name:           def.Name,
		DesiredVersion: IntegrationVersion,
	}

	pluginFile := def.TargetFileFunc(dirs)
	pluginData, pluginErr := os.ReadFile(pluginFile)
	st.Installed = pluginErr == nil
	st.InstalledVersion = parseIntegrationVersion(pluginData)

	configured := false
	configFile := def.ConfigFileFunc(dirs)
	if configData, err := os.ReadFile(configFile); err == nil {
		var cfg map[string]any
		if json.Unmarshal(configData, &cfg) == nil {
			if list, ok := cfg["plugin"].([]any); ok {
				for _, item := range list {
					text, ok := item.(string)
					if !ok {
						continue
					}
					if text == "file://"+pluginFile || strings.Contains(text, "openusage-telemetry.ts") {
						configured = true
						break
					}
				}
			}
		}
	}
	st.Configured = configured
	deriveState(&st)
	return st
}

// --- Helpers (shared) ---

func removeCommandEntries(entries []any, command string) []any {
	var filtered []any
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		hooksList, ok := entryMap["hooks"].([]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		var remainingHooks []any
		for _, hook := range hooksList {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				remainingHooks = append(remainingHooks, hook)
				continue
			}
			if strings.TrimSpace(stringOrEmpty(hookMap["type"])) == "command" {
				cmd := strings.TrimSpace(stringOrEmpty(hookMap["command"]))
				if cmd == command || strings.Contains(cmd, filepath.Base(command)) {
					continue
				}
			}
			remainingHooks = append(remainingHooks, hook)
		}
		if len(remainingHooks) > 0 {
			entryMap["hooks"] = remainingHooks
			filtered = append(filtered, entryMap)
		}
	}
	return filtered
}
