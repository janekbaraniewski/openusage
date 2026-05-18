package detect

import (
	"log"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// kiloCodeExtensionSubdir is the VS Code globalStorage subdirectory the
// Kilo Code extension writes to. Kept in sync with
// internal/providers/roocode.KiloExtensionSubdir; this detector lives
// upstream of providers so we can't import the constant directly.
const kiloCodeExtensionSubdir = "kilocode.kilo-code"

// detectKiloCode registers a local Kilo Code account when the extension's
// VS Code globalStorage subdirectory is present in any known VS Code
// variant. Like the Roo Code detector, "extension dir exists but no tasks
// yet" still counts — the provider's Fetch handles missing data
// gracefully.
func detectKiloCode(result *Result) {
	tasksRoot := firstExistingExtensionTasksRoot(kiloCodeExtensionSubdir)
	extensionDir := firstExistingExtensionDir(kiloCodeExtensionSubdir)
	if tasksRoot == "" && extensionDir == "" {
		return
	}

	log.Printf("[detect] Found Kilo Code extension at %s", firstNonEmpty(extensionDir, tasksRoot))

	if extensionDir != "" {
		result.Tools = append(result.Tools, DetectedTool{
			Name:      "Kilo Code",
			ConfigDir: extensionDir,
			Type:      "ide",
		})
	}

	acct := core.AccountConfig{
		ID:           "kilo_code",
		Provider:     "kilo_code",
		Auth:         "local",
		RuntimeHints: make(map[string]string),
	}
	if tasksRoot != "" {
		acct.SetPath("tasks_dir", tasksRoot)
		acct.SetHint("tasks_dir", tasksRoot)
	}
	if extensionDir != "" {
		acct.SetHint("extension_dir", extensionDir)
	}
	acct.SetHint("credential_source", "vscode_global_storage")

	addAccount(result, acct)
}
