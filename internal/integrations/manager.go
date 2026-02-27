package integrations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/version"
)

var IntegrationVersion = version.Version

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
	dirs Dirs
}

var integrationVersionRe = regexp.MustCompile(`openusage-integration-version:\s*([^\s]+)`)

func NewDefaultManager() Manager {
	return Manager{dirs: NewDefaultDirs()}
}

func (m Manager) ListStatuses() []Status {
	var statuses []Status
	for _, def := range AllDefinitions() {
		statuses = append(statuses, def.Detector(m.dirs))
	}
	return statuses
}

func (m Manager) Install(id ID) error {
	def, ok := DefinitionByID(id)
	if !ok {
		return fmt.Errorf("unknown integration id %q", id)
	}
	_, err := Install(def, m.dirs)
	return err
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
