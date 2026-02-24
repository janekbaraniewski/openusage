package integrations

import (
	"os"
	"path/filepath"
	"strings"
)

// IntegrationType distinguishes hook scripts from plugins.
type IntegrationType string

const (
	TypeHookScript IntegrationType = "hook_script"
	TypePlugin     IntegrationType = "plugin"
)

// ConfigFormat describes the format of the target tool's config file.
type ConfigFormat string

const (
	ConfigJSON ConfigFormat = "json"
	ConfigTOML ConfigFormat = "toml"
)

// ConfigPatchFunc patches a tool's config file to register or unregister
// an integration. When install is true, the hook/plugin entry is added;
// when false, it is removed. configData is the raw file content,
// targetFile is the path to the installed hook/plugin file.
type ConfigPatchFunc func(configData []byte, targetFile string, install bool) ([]byte, error)

// DetectFunc checks whether the integration is installed and configured.
type DetectFunc func(dirs Dirs) Status

// Definition is the complete, self-contained description of one built-in integration.
type Definition struct {
	ID          ID
	Name        string
	Description string
	Type        IntegrationType
	Template    string // embedded template content

	// TargetFileFunc returns the absolute path where the rendered template is written.
	TargetFileFunc func(dirs Dirs) string

	// ConfigFileFunc returns the absolute path to the target tool's config file.
	// Implementations must check tool-specific env var overrides internally
	// (e.g., CODEX_CONFIG_DIR, CLAUDE_SETTINGS_FILE).
	ConfigFileFunc func(dirs Dirs) string
	ConfigFormat   ConfigFormat
	ConfigPatcher  ConfigPatchFunc

	Detector DetectFunc

	// MatchProviderIDs lists provider IDs from detect.Result.Accounts that
	// correspond to this integration. This is the stable join key for
	// matching auto-detected accounts to integration definitions.
	MatchProviderIDs []string

	// MatchToolNameHint is a substring to match against detect.DetectedTool.Name
	// for associating a detected tool entry with this integration. Empty means
	// no tool matching (env-key-only providers like OpenCode).
	MatchToolNameHint string

	// TemplateFileMode is the file permission for the rendered template file.
	TemplateFileMode os.FileMode

	// EscapeBin transforms the openusage binary path for template substitution.
	EscapeBin func(string) string
}

// Dirs holds resolved filesystem paths shared across all integrations.
type Dirs struct {
	Home         string
	ConfigRoot   string // XDG_CONFIG_HOME or ~/.config
	HooksDir     string // ~/.config/openusage/hooks
	OpenusageBin string // resolved binary path
}

// NewDefaultDirs resolves Dirs from environment variables and platform defaults.
func NewDefaultDirs() Dirs {
	home, _ := os.UserHomeDir()
	configRoot := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
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

	return Dirs{
		Home:         home,
		ConfigRoot:   configRoot,
		HooksDir:     filepath.Join(configRoot, "openusage", "hooks"),
		OpenusageBin: openusageBin,
	}
}
