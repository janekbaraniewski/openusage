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
		ConfigFileFunc: openCodeConfigFile,
		ConfigFormat:   ConfigJSON,
		ConfigPatcher:  patchOpenCodeConfig,
		Detector:       detectOpenCodeStatus,

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

// openCodeConfigFile resolves the config file to inspect or edit. OpenCode
// accepts both opencode.json and opencode.jsonc; when the user keeps an
// authoritative .jsonc we must read and edit that one rather than create a
// competing strict-JSON file alongside it.
func openCodeConfigFile(dirs Dirs) string {
	root := filepath.Join(dirs.ConfigRoot, "opencode")
	jsonc := filepath.Join(root, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err == nil {
		return jsonc
	}
	return filepath.Join(root, "opencode.json")
}

// patchOpenCodeConfig unregisters the plugin on uninstall and leaves the config
// untouched on install.
//
// The plugin is written into OpenCode's global plugin directory, and OpenCode
// loads every local file there automatically
// (https://opencode.ai/docs/plugins/#from-local-files). An explicit `plugin`
// entry is therefore redundant on install, and writing one costs real things:
// it would create an opencode.json for users who have none, and re-serializing
// a .jsonc through encoding/json would silently strip the comments that make it
// a .jsonc in the first place.
//
// Uninstall still has to run, because earlier openusage versions did write an
// explicit registration and a stale entry would point at a deleted file.
// Returning nil means "no config change required" — see Definition.ConfigPatcher.
func patchOpenCodeConfig(configData []byte, targetFile string, install bool) ([]byte, error) {
	if install {
		return nil, nil
	}
	if len(bytes.TrimSpace(configData)) == 0 {
		return nil, nil
	}

	cfg := map[string]any{}
	if err := decodeJSONC(configData, &cfg); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}

	raw, ok := cfg["plugin"].([]any)
	if !ok {
		return nil, nil
	}

	plugins := []string{}
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			plugins = append(plugins, text)
		}
	}

	pluginURL := "file://" + targetFile
	remaining := slices.DeleteFunc(slices.Clone(plugins), func(s string) bool {
		return s == pluginURL || strings.Contains(s, "openusage-telemetry.ts")
	})
	if len(remaining) == len(plugins) {
		// Nothing of ours in there — leave the file alone rather than
		// reformat a config we have no edit to make to.
		return nil, nil
	}
	cfg["plugin"] = remaining

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

	// A versioned plugin sitting in OpenCode's global plugin directory is
	// already active: OpenCode loads every local file there automatically
	// (https://opencode.ai/docs/plugins/#from-local-files). Requiring an
	// explicit `plugin` entry on top of that reported a working install as
	// PARTIAL. An explicit registration still counts, so configs written by
	// older openusage versions keep reporting READY.
	configured := st.Installed && st.InstalledVersion != ""
	if !configured {
		configFile := def.ConfigFileFunc(dirs)
		if configData, err := os.ReadFile(configFile); err == nil {
			var cfg map[string]any
			if decodeJSONC(configData, &cfg) == nil {
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
