package integrations

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	IntegrationVersion = "2026-02-23.1"
)

type ID string

const (
	OpenCodeID   ID = "opencode"
	CodexID      ID = "codex"
	ClaudeCodeID ID = "claude_code"
)

type Status struct {
	ID               ID
	Name             string
	Installed        bool
	Configured       bool
	InstalledVersion string
	DesiredVersion   string
	NeedsUpgrade     bool
	State            string
	Summary          string
}

type Manager struct {
	openCodeConfigFile string
	openCodePluginFile string
	codexConfigFile    string
	codexHookFile      string
	claudeSettingsFile string
	claudeHookFile     string
	openusageBin       string
}

var integrationVersionRe = regexp.MustCompile(`openusage-integration-version:\s*([^\s]+)`)

//go:embed assets/opencode-telemetry.ts.tpl
var opencodeTemplate string

//go:embed assets/codex-notify.sh.tpl
var codexTemplate string

//go:embed assets/claude-hook.sh.tpl
var claudeTemplate string

func NewDefaultManager() Manager {
	home, _ := os.UserHomeDir()
	configRoot := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}

	codexConfigDir := strings.TrimSpace(os.Getenv("CODEX_CONFIG_DIR"))
	if codexConfigDir == "" {
		codexConfigDir = filepath.Join(home, ".codex")
	}

	claudeSettingsFile := strings.TrimSpace(os.Getenv("CLAUDE_SETTINGS_FILE"))
	if claudeSettingsFile == "" {
		claudeSettingsFile = filepath.Join(home, ".claude", "settings.json")
	}

	openusageBin := strings.TrimSpace(os.Getenv("OPENUSAGE_BIN"))
	if openusageBin == "" {
		if exe, err := os.Executable(); err == nil {
			openusageBin = exe
		}
	}
	if openusageBin == "" {
		openusageBin = "openusage"
	}

	hooksDir := filepath.Join(configRoot, "openusage", "hooks")
	return Manager{
		openCodeConfigFile: filepath.Join(configRoot, "opencode", "opencode.json"),
		openCodePluginFile: filepath.Join(configRoot, "opencode", "plugins", "openusage-telemetry.ts"),
		codexConfigFile:    filepath.Join(codexConfigDir, "config.toml"),
		codexHookFile:      filepath.Join(hooksDir, "codex-notify.sh"),
		claudeSettingsFile: claudeSettingsFile,
		claudeHookFile:     filepath.Join(hooksDir, "claude-hook.sh"),
		openusageBin:       openusageBin,
	}
}

func (m Manager) ListStatuses() []Status {
	statuses := []Status{
		m.detectOpenCode(),
		m.detectCodex(),
		m.detectClaudeCode(),
	}
	return statuses
}

func (m Manager) Install(id ID) error {
	switch id {
	case OpenCodeID:
		return m.installOpenCode()
	case CodexID:
		return m.installCodex()
	case ClaudeCodeID:
		return m.installClaudeCode()
	default:
		return fmt.Errorf("unknown integration id %q", id)
	}
}

func (m Manager) detectOpenCode() Status {
	st := Status{
		ID:             OpenCodeID,
		Name:           "OpenCode Plugin",
		DesiredVersion: IntegrationVersion,
	}

	pluginData, pluginErr := os.ReadFile(m.openCodePluginFile)
	st.Installed = pluginErr == nil
	st.InstalledVersion = parseIntegrationVersion(pluginData)

	configured := false
	if configData, err := os.ReadFile(m.openCodeConfigFile); err == nil {
		var cfg map[string]any
		if json.Unmarshal(configData, &cfg) == nil {
			if list, ok := cfg["plugin"].([]any); ok {
				for _, item := range list {
					text, ok := item.(string)
					if !ok {
						continue
					}
					if text == "file://"+m.openCodePluginFile || strings.Contains(text, "openusage-telemetry.ts") {
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

func (m Manager) detectCodex() Status {
	st := Status{
		ID:             CodexID,
		Name:           "Codex Notify Hook",
		DesiredVersion: IntegrationVersion,
	}

	hookData, hookErr := os.ReadFile(m.codexHookFile)
	st.Installed = hookErr == nil
	st.InstalledVersion = parseIntegrationVersion(hookData)

	configured := false
	if cfgData, err := os.ReadFile(m.codexConfigFile); err == nil {
		cfg := string(cfgData)
		if strings.Contains(cfg, "notify") && strings.Contains(cfg, "codex-notify.sh") {
			configured = true
		}
	}
	st.Configured = configured
	deriveState(&st)
	return st
}

func (m Manager) detectClaudeCode() Status {
	st := Status{
		ID:             ClaudeCodeID,
		Name:           "Claude Code Hooks",
		DesiredVersion: IntegrationVersion,
	}

	hookData, hookErr := os.ReadFile(m.claudeHookFile)
	st.Installed = hookErr == nil
	st.InstalledVersion = parseIntegrationVersion(hookData)

	configured := false
	if settingsData, err := os.ReadFile(m.claudeSettingsFile); err == nil {
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

func deriveState(st *Status) {
	if st == nil {
		return
	}
	if st.Installed && st.InstalledVersion != "" && st.InstalledVersion != st.DesiredVersion {
		st.NeedsUpgrade = true
		st.State = "outdated"
		st.Summary = "Upgrade available"
		return
	}
	if st.Installed && st.Configured {
		st.State = "ready"
		st.Summary = "Installed and active"
		return
	}
	if st.Installed && !st.Configured {
		st.State = "partial"
		st.Summary = "Installed but not configured"
		return
	}
	st.State = "missing"
	st.Summary = "Not installed"
}

func parseIntegrationVersion(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	match := integrationVersionRe.FindSubmatch(data)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}

func hasCommandHook(root map[string]any, eventName, commandNeedle string) bool {
	hooksRaw, ok := root["hooks"].(map[string]any)
	if !ok {
		return false
	}
	entries, ok := hooksRaw[eventName].([]any)
	if !ok {
		return false
	}

	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hooksList, ok := entryMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, hook := range hooksList {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(stringOrEmpty(hookMap["type"])) != "command" {
				continue
			}
			cmd := strings.TrimSpace(stringOrEmpty(hookMap["command"]))
			if cmd != "" && strings.Contains(cmd, commandNeedle) {
				return true
			}
		}
	}
	return false
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

func (m Manager) installOpenCode() error {
	if err := os.MkdirAll(filepath.Dir(m.openCodePluginFile), 0o755); err != nil {
		return fmt.Errorf("create opencode plugin dir: %w", err)
	}
	if err := m.writeVersionedTemplate(
		opencodeTemplate,
		m.openCodePluginFile,
		m.openusageBin,
		escapeForTSString,
		0o644,
	); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.openCodeConfigFile), 0o755); err != nil {
		return fmt.Errorf("create opencode config dir: %w", err)
	}

	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}
	if data, err := os.ReadFile(m.openCodeConfigFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse opencode config: %w", err)
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
	pluginURL := "file://" + m.openCodePluginFile
	if !slices.Contains(plugins, pluginURL) {
		plugins = append(plugins, pluginURL)
	}
	cfg["plugin"] = plugins

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize opencode config: %w", err)
	}
	payload = append(payload, '\n')
	if err := backupIfExists(m.openCodeConfigFile); err != nil {
		return err
	}
	if err := os.WriteFile(m.openCodeConfigFile, payload, 0o600); err != nil {
		return fmt.Errorf("write opencode config: %w", err)
	}
	return nil
}

func (m Manager) installCodex() error {
	if err := os.MkdirAll(filepath.Dir(m.codexHookFile), 0o755); err != nil {
		return fmt.Errorf("create codex hook dir: %w", err)
	}
	if err := m.writeVersionedTemplate(
		codexTemplate,
		m.codexHookFile,
		m.openusageBin,
		escapeForShellString,
		0o755,
	); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.codexConfigFile), 0o755); err != nil {
		return fmt.Errorf("create codex config dir: %w", err)
	}
	if err := backupIfExists(m.codexConfigFile); err != nil {
		return err
	}

	notifyLine := fmt.Sprintf("notify = [\"%s\"]", m.codexHookFile)
	out := notifyLine + "\n"
	if data, err := os.ReadFile(m.codexConfigFile); err == nil {
		lines := strings.Split(string(data), "\n")
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

	if err := os.WriteFile(m.codexConfigFile, []byte(out), 0o600); err != nil {
		return fmt.Errorf("write codex config: %w", err)
	}
	return nil
}

func (m Manager) installClaudeCode() error {
	if err := os.MkdirAll(filepath.Dir(m.claudeHookFile), 0o755); err != nil {
		return fmt.Errorf("create claude hook dir: %w", err)
	}
	if err := m.writeVersionedTemplate(
		claudeTemplate,
		m.claudeHookFile,
		m.openusageBin,
		escapeForShellString,
		0o755,
	); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.claudeSettingsFile), 0o755); err != nil {
		return fmt.Errorf("create claude settings dir: %w", err)
	}
	if err := backupIfExists(m.claudeSettingsFile); err != nil {
		return err
	}

	cfg := map[string]any{}
	if data, err := os.ReadFile(m.claudeSettingsFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse claude settings: %w", err)
		}
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	cfg["hooks"] = hooks

	for _, event := range []string{"Stop", "SubagentStop", "PostToolUse"} {
		entries, _ := hooks[event].([]any)
		if !entriesContainCommand(entries, m.claudeHookFile) {
			entries = append(entries, map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": m.claudeHookFile,
					},
				},
			})
		}
		hooks[event] = entries
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize claude settings: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(m.claudeSettingsFile, payload, 0o600); err != nil {
		return fmt.Errorf("write claude settings: %w", err)
	}
	return nil
}

func entriesContainCommand(entries []any, command string) bool {
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hooksList, ok := entryMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, hook := range hooksList {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(stringOrEmpty(hookMap["type"])) != "command" {
				continue
			}
			cmd := strings.TrimSpace(stringOrEmpty(hookMap["command"]))
			if cmd == command || strings.Contains(cmd, filepath.Base(command)) {
				return true
			}
		}
	}
	return false
}

func (m Manager) writeVersionedTemplate(
	templateBody, targetPath, openusageBin string,
	escapeBin func(string) string,
	mode os.FileMode,
) error {
	content := strings.ReplaceAll(templateBody, "__OPENUSAGE_INTEGRATION_VERSION__", IntegrationVersion)
	content = strings.ReplaceAll(content, "__OPENUSAGE_BIN_DEFAULT__", escapeBin(openusageBin))
	if err := os.WriteFile(targetPath, []byte(content), mode); err != nil {
		return fmt.Errorf("write integration file %s: %w", targetPath, err)
	}
	return nil
}

func escapeForShellString(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"$", "\\$",
	)
	return replacer.Replace(value)
}

func escapeForTSString(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
	)
	return replacer.Replace(value)
}

func backupIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read backup source %s: %w", path, err)
	}
	backupPath := path + ".bak"
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	return nil
}
