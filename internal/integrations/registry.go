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

	// TargetFileFunc returns the absolute path where the rendered template is
	// written. When WritesArtifact reports false, this path is NOT written to;
	// it is still passed to the ConfigPatcher as the registration target (e.g.
	// the openusage binary path on platforms that register it directly).
	TargetFileFunc func(dirs Dirs) string

	// WritesArtifact reports whether Install should render and write the
	// template to TargetFileFunc. A nil value means true (the default: an
	// artifact file is always written). When it returns false, Install skips
	// writing/backing-up the target file and Uninstall skips removing it; only
	// the tool config is patched. This supports platforms where an integration
	// registers the openusage binary directly instead of a hook script.
	WritesArtifact func(dirs Dirs) bool

	// ConfigFileFunc returns the absolute path to the target tool's config file.
	// Implementations must check tool-specific env var overrides internally
	// (e.g., CODEX_CONFIG_DIR, CLAUDE_SETTINGS_FILE).
	ConfigFileFunc func(dirs Dirs) string
	ConfigFormat   ConfigFormat
	// ConfigPatcher returns the new config file contents. Returning nil
	// bytes with a nil error means "no config change required": the config
	// file is then left exactly as it is, and is not created if absent.
	// Used by tools that discover the integration without an explicit
	// registration entry.
	ConfigPatcher ConfigPatchFunc

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

// artifactSpec bundles the platform-specific pieces of a rendered integration
// artifact (hook script / batch file). It lets a definition pick its template,
// target basename, file mode, and binary-path escaper per OS via build-tagged
// helpers (see claude_artifact_*.go, codex_artifact_*.go) instead of inline
// runtime.GOOS branching.
type artifactSpec struct {
	// Template is the embedded template content. An empty Template means the
	// integration writes no artifact file on this platform (the installer
	// skips writing in that case).
	Template string
	// Basename is the file name written into the hooks dir. Empty when there
	// is no artifact file.
	Basename string
	// FileMode is the permission applied to the rendered artifact file.
	FileMode os.FileMode
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
		Home:       home,
		ConfigRoot: configRoot,
		// HooksDir is OpenUsage's OWN directory. configRoot stays the XDG-style
		// base because it also locates third-party tool dirs (e.g. OpenCode,
		// which resolves opencode.json/plugins via xdg-basedir and so uses
		// %USERPROFILE%\.config\opencode even on Windows), whereas HooksDir tracks
		// settings.json — see platformHooksDir() in hooks_dir_*.go.
		HooksDir:     platformHooksDir(configRoot),
		OpenusageBin: openusageBin,
	}
}
