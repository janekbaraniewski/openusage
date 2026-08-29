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
		antigravityDef(),
		cursorDef(),
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
	art := claudeArtifact()
	return Definition{
		ID:          ClaudeCodeID,
		Name:        "Claude Code Hooks",
		Description: "Telemetry hooks for Claude Code (Stop, SubagentStop, PostToolUse)",
		Type:        TypeHookScript,
		Template:    art.Template,

		TargetFileFunc: func(dirs Dirs) string {
			return filepath.Join(dirs.HooksDir, art.Basename)
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
		TemplateFileMode:  art.FileMode,
		EscapeBin:         art.EscapeBin,
	}
}

func codexDef() Definition {
	art := codexArtifact()
	return Definition{
		ID:          CodexID,
		Name:        "Codex Notify Hook",
		Description: "Telemetry notify hook for OpenAI Codex CLI",
		Type:        TypeHookScript,
		Template:    art.Template,

		TargetFileFunc: func(dirs Dirs) string {
			path, _ := codexTargetFile(dirs)
			return path
		},
		WritesArtifact: func(dirs Dirs) bool {
			_, writes := codexTargetFile(dirs)
			return writes
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
		TemplateFileMode:  art.FileMode,
		EscapeBin:         art.EscapeBin,
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

func antigravityDef() Definition {
	return Definition{
		ID:          AntigravityID,
		Name:        "Antigravity Status Line",
		Description: "Status-line bridge for Antigravity CLI",
		Type:        TypeHookScript,
		// Antigravity can invoke the OpenUsage binary directly. There is no
		// separate artifact to render or remove.
		WritesArtifact: func(Dirs) bool { return false },
		TargetFileFunc: func(dirs Dirs) string {
			return dirs.OpenusageBin
		},
		ConfigFileFunc: func(dirs Dirs) string {
			if f := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SETTINGS_FILE")); f != "" {
				return f
			}
			configDir := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CONFIG_DIR"))
			if configDir == "" {
				configDir = filepath.Join(dirs.Home, ".gemini", "antigravity-cli")
			}
			return filepath.Join(configDir, "settings.json")
		},
		ConfigFormat:  ConfigJSON,
		ConfigPatcher: patchAntigravityConfig,
		Detector:      detectAntigravityStatus,

		MatchProviderIDs:  []string{"antigravity"},
		MatchToolNameHint: "Antigravity CLI",
		TemplateFileMode:  0o755,
		EscapeBin:         escapeForShellString,
	}
}

func cursorDef() Definition {
	return Definition{
		ID:          CursorID,
		Name:        "Cursor Status Line",
		Description: "Status-line bridge for Cursor CLI",
		Type:        TypeHookScript,
		WritesArtifact: func(Dirs) bool { return false },
		TargetFileFunc: func(dirs Dirs) string {
			return dirs.OpenusageBin
		},
		ConfigFileFunc: func(dirs Dirs) string {
			if f := strings.TrimSpace(os.Getenv("CURSOR_SETTINGS_FILE")); f != "" {
				return f
			}
			configDir := strings.TrimSpace(os.Getenv("CURSOR_CONFIG_DIR"))
			if configDir == "" {
				configDir = filepath.Join(dirs.Home, ".cursor")
			}
			return filepath.Join(configDir, "cli-config.json")
		},
		ConfigFormat:  ConfigJSON,
		ConfigPatcher: patchCursorConfig,
		Detector:      detectCursorStatus,

		MatchProviderIDs:  []string{"cursor"},
		MatchToolNameHint: "Cursor",
		TemplateFileMode:  0o755,
		EscapeBin:         escapeForShellString,
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
	// targetFile is the script path on Unix or the openusage binary path on
	// Windows; codexNotifyTOML renders the platform-correct notify assignment
	// (basic string array on Unix, literal string array invoking the binary on
	// Windows so backslashes survive).
	notifyLine := codexNotifyTOML(targetFile)

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

func patchAntigravityConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	if !install && len(bytes.TrimSpace(configData)) == 0 {
		return configData, nil
	}

	cfg := map[string]any{}
	if len(bytes.TrimSpace(configData)) > 0 {
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("parse antigravity settings: %w", err)
		}
	}

	statusLine, _ := cfg["statusLine"].(map[string]any)
	existingCommand := ""
	if statusLine != nil {
		existingCommand = strings.TrimSpace(stringOrEmpty(statusLine["command"]))
	}

	if install {
		if existingCommand != "" && !isAntigravityOpenUsageCommand(existingCommand) {
			return nil, fmt.Errorf("antigravity statusLine.command is already configured with another command")
		}
		if statusLine == nil {
			statusLine = map[string]any{}
		}
		statusLine["type"] = "command"
		statusLine["command"] = antigravityStatuslineCommand(targetFile)
		statusLine["enabled"] = true
		statusLine["stack_with_default"] = true
		cfg["statusLine"] = statusLine
	} else if isAntigravityOpenUsageCommand(existingCommand) {
		// Leave the built-in status-line feature enabled, but remove only the
		// command owned by OpenUsage. Unrelated custom commands are untouched.
		cfg["statusLine"] = map[string]any{"enabled": true}
	} else {
		return configData, nil
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize antigravity settings: %w", err)
	}
	return append(payload, '\n'), nil
}

func antigravityStatuslineCommand(targetFile string) string {
	targetFile = strings.TrimSpace(targetFile)
	if targetFile == "" {
		targetFile = "openusage"
	}
	return fmt.Sprintf("\"%s\" antigravity statusline", escapeForShellString(targetFile))
}

func isAntigravityOpenUsageCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	return strings.Contains(command, "antigravity statusline")
}

func patchCursorConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	if !install && len(bytes.TrimSpace(configData)) == 0 {
		return configData, nil
	}

	cfg := map[string]any{}
	if len(bytes.TrimSpace(configData)) > 0 {
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("parse cursor config: %w", err)
		}
	}

	statusLine, _ := cfg["statusLine"].(map[string]any)
	existingCommand := ""
	if statusLine != nil {
		existingCommand = strings.TrimSpace(stringOrEmpty(statusLine["command"]))
	}

	if install {
		if existingCommand != "" && !isCursorOpenUsageCommand(existingCommand) {
			return nil, fmt.Errorf("cursor statusLine.command is already configured with another command")
		}
		if statusLine == nil {
			statusLine = map[string]any{}
		}
		statusLine["type"] = "command"
		statusLine["command"] = cursorStatuslineCommand(targetFile)
		cfg["statusLine"] = statusLine
	} else if isCursorOpenUsageCommand(existingCommand) {
		delete(cfg, "statusLine")
	} else {
		return configData, nil
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize cursor config: %w", err)
	}
	return append(payload, '\n'), nil
}

func cursorStatuslineCommand(targetFile string) string {
	targetFile = strings.TrimSpace(targetFile)
	if targetFile == "" {
		targetFile = "openusage"
	}
	return fmt.Sprintf("\"%s\" cursor statusline", escapeForShellString(targetFile))
}

func isCursorOpenUsageCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	return strings.Contains(command, "cursor statusline")
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
	needle := filepath.Base(def.TargetFileFunc(dirs)) // claude-hook.sh / claude-hook.cmd
	if settingsData, err := os.ReadFile(configFile); err == nil {
		var cfg map[string]any
		if json.Unmarshal(settingsData, &cfg) == nil {
			configured = hasCommandHook(cfg, "Stop", needle) &&
				hasCommandHook(cfg, "SubagentStop", needle) &&
				hasCommandHook(cfg, "PostToolUse", needle)
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

	// On platforms where Codex registers the openusage binary directly (no
	// script artifact), "installed" tracks the config registration rather than
	// a file on disk; deriveState then keys off Configured.
	if def.WritesArtifact == nil || def.WritesArtifact(dirs) {
		hookFile := def.TargetFileFunc(dirs)
		hookData, hookErr := os.ReadFile(hookFile)
		st.Installed = hookErr == nil
		st.InstalledVersion = parseIntegrationVersion(hookData)
	}

	configured := false
	configFile := def.ConfigFileFunc(dirs)
	if cfgData, err := os.ReadFile(configFile); err == nil {
		if codexConfigured(string(cfgData)) {
			configured = true
		}
	}
	st.Configured = configured

	// When there is no artifact file, treat configuration as installation so
	// the derived state reflects "ready" once the notify hook is registered,
	// and the desired version comes from the running binary.
	if def.WritesArtifact != nil && !def.WritesArtifact(dirs) {
		st.Installed = configured
		if configured {
			st.InstalledVersion = IntegrationVersion
		}
	}

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

func detectAntigravityStatus(dirs Dirs) Status {
	def := antigravityDef()
	st := Status{
		ID:             AntigravityID,
		Name:           def.Name,
		DesiredVersion: IntegrationVersion,
	}

	configFile := def.ConfigFileFunc(dirs)
	data, err := os.ReadFile(configFile)
	if err != nil {
		deriveState(&st)
		return st
	}

	var cfg map[string]any
	if json.Unmarshal(data, &cfg) == nil {
		if statusLine, ok := cfg["statusLine"].(map[string]any); ok {
			st.Configured = isAntigravityOpenUsageCommand(stringOrEmpty(statusLine["command"]))
		}
	}
	// This integration registers the OpenUsage binary directly, so configured
	// is the equivalent of an installed artifact.
	st.Installed = st.Configured
	if st.Installed {
		st.InstalledVersion = IntegrationVersion
	}
	deriveState(&st)
	return st
}

func detectCursorStatus(dirs Dirs) Status {
	def := cursorDef()
	st := Status{
		ID:             CursorID,
		Name:           def.Name,
		DesiredVersion: IntegrationVersion,
	}

	configFile := def.ConfigFileFunc(dirs)
	data, err := os.ReadFile(configFile)
	if err != nil {
		deriveState(&st)
		return st
	}

	var cfg map[string]any
	if json.Unmarshal(data, &cfg) == nil {
		if statusLine, ok := cfg["statusLine"].(map[string]any); ok {
			st.Configured = isCursorOpenUsageCommand(stringOrEmpty(statusLine["command"]))
		}
	}
	st.Installed = st.Configured
	if st.Installed {
		st.InstalledVersion = IntegrationVersion
	}
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
