# Integration Lifecycle Design

Date: 2026-02-24
Status: Proposed
Author: OpenUsage

## 1. Problem Statement

The plugin/integration system for external tool telemetry (Claude Code, Codex, OpenCode) exists as embedded templates and a Go manager, but there is no CLI command to install, upgrade, or manage integrations — users must run standalone shell scripts from the `plugins/` directory, there is no auto-install when tools are detected, and adding new integrations requires duplicating boilerplate across templates, manager methods, and install scripts.

## 2. Goals

1. Provide a single `openusage integrations` CLI command to list, install, upgrade, and uninstall integrations.
2. Auto-prompt for integration install when `openusage telemetry daemon` starts and detects uninstalled tools.
3. Embed all integration definitions in the binary so installs work without the source repo.
4. Make adding a new integration a data-driven process (add a definition + template, not new methods).
5. Remove redundant shell install scripts in `plugins/` — the Go manager becomes the single source of truth.

## 3. Non-Goals

1. Changing the telemetry pipeline or data model (covered by `UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md`).
2. Third-party/external plugin SDK — integrations remain built-in only.
3. Remote plugin delivery or auto-update from a registry.
4. Full TUI integrations redesign — the existing settings modal integrations tab (`internal/tui/settings_modal.go`) will be updated to use the new registry, but a major TUI revamp is out of scope for this design.
5. Adding new integrations beyond the existing three (Claude Code, Codex, OpenCode) — but the system must make future additions trivial.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | No changes to `UsageProvider`, `UsageSnapshot`, or `AccountConfig`. |
| providers | none | Provider implementations unchanged. `TelemetrySource` interface unchanged. |
| TUI | minor | Existing settings modal integrations tab (`settings_modal.go`, `model.go`) updated to use new registry instead of calling `Manager` directly. |
| config | minor | New `integrations` section in settings.json for tracking install state. |
| detect | minor | `AutoDetect` result gains a helper to match detected tools to available integrations. |
| daemon | minor | Daemon startup checks for uninstalled integrations and logs/prompts. |
| telemetry | none | Pipeline, store, and spool unchanged. |
| CLI | major | New `integrations` subcommand with list/install/upgrade/uninstall/status. |
| integrations | major | Refactor manager from per-integration methods to data-driven registry + installer. |

### Existing Design Doc Overlap

- **`UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md`**: Covers the telemetry data pipeline (ingestion, dedup, normalization, reconciliation). This design is complementary — it handles the *lifecycle* of getting hooks installed, not the data flow after hooks fire. Section 10 of that doc ("Agent Integration Design") describes per-agent adapter behavior; this design references but does not duplicate that.
- **`TELEMETRY_INTEGRATIONS.md`**: Documents current manual install procedures. This design **supersedes** that doc — once the CLI command exists, `TELEMETRY_INTEGRATIONS.md` should be updated to point users to `openusage integrations install`.

## 5. Detailed Design

### 5.1 Integration Registry (data-driven definitions)

Replace the current per-integration methods in `manager.go` (`detectOpenCode()`, `detectCodex()`, `detectClaudeCode()`, `installOpenCode()`, etc.) with a data-driven registry. Each integration is a struct with all the metadata the installer needs.

```go
// internal/integrations/registry.go

type IntegrationType string

const (
    TypeHookScript IntegrationType = "hook_script"   // Bash script invoked by tool
    TypePlugin     IntegrationType = "plugin"         // TypeScript/JS plugin loaded by tool
)

type ConfigFormat string

const (
    ConfigJSON ConfigFormat = "json"
    ConfigTOML ConfigFormat = "toml"
)

// Definition is the complete, self-contained description of one integration.
type Definition struct {
    ID          ID              // "claude_code", "codex", "opencode"
    Name        string          // "Claude Code Hooks"
    Description string          // one-line for CLI help
    Type        IntegrationType // hook_script or plugin
    Template    string          // embedded template content (from go:embed)

    // Where to write the rendered template
    TargetFileFunc func(dirs Dirs) string // returns absolute path

    // Target tool's config file to patch.
    // Implementations must check tool-specific env var overrides internally:
    //   - Codex: CODEX_CONFIG_DIR (defaults to ~/.codex)
    //   - Claude Code: CLAUDE_SETTINGS_FILE (defaults to ~/.claude/settings.json)
    ConfigFileFunc func(dirs Dirs) string
    ConfigFormat   ConfigFormat
    ConfigPatcher  ConfigPatchFunc // patches the tool's config to register the hook/plugin

    // Detection: how to check if installed + configured
    Detector DetectFunc

    // Matching: how to correlate with auto-detection results.
    // Match against detect.Result.Accounts[].Provider (e.g., "claude_code", "codex", "opencode").
    // This is the stable identifier — DetectedTool.Name varies ("Claude Code CLI", "OpenAI Codex CLI")
    // and some tools (OpenCode) have no DetectedTool entry, only account entries via env keys.
    MatchProviderIDs []string // e.g., ["claude_code"] or ["opencode"]
}

// Dirs holds resolved filesystem paths used by all integrations.
type Dirs struct {
    Home          string
    ConfigRoot    string // XDG_CONFIG_HOME or ~/.config
    HooksDir      string // ~/.config/openusage/hooks
    OpenusageBin  string // resolved binary path
}

// NewDefaultDirs resolves Dirs from environment variables and platform defaults.
// Extracts the shared path resolution logic currently in NewDefaultManager().
func NewDefaultDirs() Dirs

// ConfigPatchFunc patches a tool's config file to register/unregister the integration.
// install=true adds the hook, install=false removes it.
type ConfigPatchFunc func(configData []byte, targetFile string, install bool) ([]byte, error)

// DetectFunc checks whether the integration is installed and configured.
type DetectFunc func(dirs Dirs) Status

// AllDefinitions returns the built-in integration definitions.
func AllDefinitions() []Definition {
    return []Definition{
        claudeCodeDef(),
        codexDef(),
        opencodeDef(),
    }
}
```

Each definition is constructed by a factory function (e.g. `claudeCodeDef()`) that wires the embedded template, path functions, and config patcher together. The existing `installClaudeCode()`, `installCodex()`, `installOpenCode()` logic moves into `ConfigPatchFunc` implementations — same logic, just structured as data rather than methods.

### 5.2 Installer (generic, definition-driven)

A single `Install` function operates on any `Definition`:

```go
// internal/integrations/installer.go

type InstallResult struct {
    ID             ID
    Action         string // "installed", "upgraded", "already_current", "uninstalled"
    TemplateFile   string // path to written template
    ConfigFile     string // path to patched config
    PreviousVer    string
    InstalledVer   string
}

// Install renders the template, writes the target file, patches the tool config.
func Install(def Definition, dirs Dirs) (InstallResult, error)

// Uninstall removes the target file and un-patches the tool config.
func Uninstall(def Definition, dirs Dirs) error

// Upgrade is Install when already installed (same flow, result.Action = "upgraded").
func Upgrade(def Definition, dirs Dirs) (InstallResult, error)
```

The `Install` flow:
1. Resolve paths via `def.TargetFileFunc(dirs)` and `def.ConfigFileFunc(dirs)`.
2. Render template: replace `__OPENUSAGE_INTEGRATION_VERSION__` and `__OPENUSAGE_BIN_DEFAULT__`.
3. `backupIfExists()` on both target file and config file (existing behavior).
4. Write rendered template to target path.
5. Read tool config, call `def.ConfigPatcher(configData, targetFile, true)`, write back.
6. Return `InstallResult`.

This eliminates the `switch id` dispatch in the current `Manager.Install()`.

### 5.3 Config Persistence (integration state)

Add an `integrations` section to the config file to track what the user has installed:

```go
// Added to internal/config/config.go Config struct

type IntegrationState struct {
    Installed   bool   `json:"installed"`
    Version     string `json:"version,omitempty"`
    InstalledAt string `json:"installed_at,omitempty"` // RFC3339
    Declined    bool   `json:"declined,omitempty"`     // user said "no" to auto-install
}

// In Config:
// Integrations map[string]IntegrationState `json:"integrations,omitempty"`
```

Example in settings.json:
```json
{
  "integrations": {
    "claude_code": {
      "installed": true,
      "version": "2026-02-24.1",
      "installed_at": "2026-02-24T12:00:00Z"
    },
    "codex": {
      "declined": true
    }
  }
}
```

New config methods:
- `SaveIntegrationState(id string, state IntegrationState) error`

### 5.4 Detection Bridge

Add a helper that matches detected tools/accounts to available integration definitions:

```go
// internal/integrations/match.go

type Match struct {
    Definition   Definition
    Tool         *detect.DetectedTool   // the detected tool, if found (nil for env-key-only like OpenCode)
    Account      *core.AccountConfig    // the detected account, if found
    Status       Status                 // current install/config status
    Actionable   bool                   // true if not installed and tool/account is detected
}

// MatchDetected takes detection results and returns integration matches.
// Matching strategy: each Definition has MatchProviderIDs (e.g., ["claude_code"]).
// These are matched against detect.Result.Accounts[].Provider — the stable identifier.
// Additionally, if a DetectedTool exists for that provider, it's included in the Match.
// This handles all cases:
//   - Claude Code: detected as tool ("Claude Code CLI") + account (provider="claude_code")
//   - Codex: detected as tool ("OpenAI Codex CLI") + account (provider="codex")
//   - OpenCode: detected via env keys only, account (provider="opencode"), no DetectedTool
func MatchDetected(defs []Definition, detected detect.Result, dirs Dirs) []Match
```

This does not change the detect package itself. The matching is done in the integrations package, which imports detect and core types. The provider ID is the stable join key — `DetectedTool.Name` is display-only and varies across tools.

### 5.5 CLI Commands

New cobra command group registered on root:

```
openusage integrations list               # list all integrations + status
openusage integrations install <id|--all> # install one or all detected
openusage integrations upgrade <id|--all> # upgrade outdated integrations
openusage integrations uninstall <id>     # remove integration
openusage integrations status [id]        # detailed status of one or all
```

```go
// cmd/openusage/integrations.go

func newIntegrationsCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "integrations",
        Short: "Manage telemetry integrations with coding tools",
    }
    cmd.AddCommand(
        newIntegrationsListCommand(),
        newIntegrationsInstallCommand(),
        newIntegrationsUpgradeCommand(),
        newIntegrationsUninstallCommand(),
        newIntegrationsStatusCommand(),
    )
    return cmd
}
```

**`integrations list`** output example:
```
Integration       Status      Version       Tool Detected
─────────────────────────────────────────────────────────
Claude Code       installed   2026-02-24.1  yes (claude @ /usr/local/bin/claude)
Codex             missing     -             yes (codex @ ~/.local/bin/codex)
OpenCode          outdated    2026-02-20.1  yes (opencode @ /usr/local/bin/opencode)
```

**`integrations install`** flow:
1. Run `detect.AutoDetect()` to find tools.
2. Run `MatchDetected()` to find actionable integrations.
3. If `--all`: install all actionable. If `<id>`: install that one (even if tool not detected, with a warning).
4. For each: call `Install(def, dirs)`, then `config.SaveIntegrationState(id, state)`.
5. Print results.

### 5.6 Daemon Auto-Prompt

When `openusage telemetry daemon` starts, check for uninstalled integrations:

```go
// In daemon startup (cmd/openusage/telemetry.go or internal/daemon/server.go)

func checkIntegrations(cfg config.Config, dirs integrations.Dirs) {
    defs := integrations.AllDefinitions()
    detected := detect.AutoDetect()
    matches := integrations.MatchDetected(defs, detected, dirs)

    for _, m := range matches {
        if !m.Actionable {
            continue
        }
        // Check if user already declined
        if state, ok := cfg.Integrations[string(m.Definition.ID)]; ok && state.Declined {
            continue
        }
        log.Printf("integration: %s detected but not installed — run: openusage integrations install %s", m.Definition.Name, m.Definition.ID)
    }
}
```

Non-interactive: just logs a message. No interactive prompts in daemon mode. The TUI can show a banner with the same info.

### 5.7 Remove `plugins/` Directory

The `plugins/` directory contains shell install scripts and source file copies that duplicate the embedded templates and Go manager logic. Remove it entirely:

1. Delete `plugins/` directory (9 files: 3 install.sh, 3 hook/plugin source copies, 3 READMEs).
2. Update `TELEMETRY_INTEGRATIONS.md` to reference `openusage integrations install` as the sole install method.
3. Update `.gitignore` if it references `plugins/`.

### 5.8 Backward Compatibility

- **Existing manually-installed hooks continue to work.** The version marker (`openusage-integration-version: ...`) is already embedded in templates. The `Detector` function checks both file existence and config registration — it will correctly detect manually-installed hooks as "installed".
- **Config file gains a new `integrations` key.** This is additive — existing configs without this key work fine (treated as empty map, all integrations unknown).
- **No changes to the telemetry pipeline.** Hook payloads, spool format, and SQLite schema are unchanged.
- **No changes to provider interfaces.** `UsageProvider`, `TelemetrySource`, and `UsageSnapshot` are untouched.

## 6. Alternatives Considered

### Keep per-integration methods, just add CLI

We could keep the current `installClaudeCode()`, `installCodex()`, `installOpenCode()` methods and just wrap them in a CLI command. Rejected because: adding a 4th integration (e.g., Gemini CLI) would require adding another method to the manager, another `case` in `Install()`, another `detect*()` method — the switch/method fan-out grows linearly. The data-driven approach makes new integrations a single definition.

### External plugin system (load definitions from files)

We could allow users to drop integration definitions into a directory. Rejected because: this adds complexity (parsing, validation, security) for a use case that doesn't exist yet. All current and foreseeable integrations are built-in. If needed later, `AllDefinitions()` can be extended to load external definitions.

### Full TUI integrations redesign

The existing settings modal integrations tab already supports install/upgrade. A more ambitious redesign (dedicated screen, richer status display, auto-detect prompts) was considered but deferred — the existing tab will be updated to use the new registry, which is sufficient for now.

## 7. Implementation Tasks

### Task 1: Define integration registry types and move definitions

Files: `internal/integrations/registry.go` (new), `internal/integrations/definitions.go` (new)
Depends on: none
Description: Create the `Definition`, `Dirs`, `ConfigPatchFunc`, `DetectFunc` types and `NewDefaultDirs()` constructor in `registry.go`. `NewDefaultDirs()` extracts the shared path resolution logic currently in `NewDefaultManager()` (home dir, XDG_CONFIG_HOME, OPENUSAGE_BIN, binary discovery). Create `AllDefinitions()` in `definitions.go` with the three existing integrations as data-driven definitions. Each definition's `ConfigPatcher` and `Detector` reuses the existing logic from `manager.go` (extract into standalone functions). Wire in the existing `go:embed` templates. Each definition's `ConfigFileFunc` checks its tool-specific env var override (e.g., `CODEX_CONFIG_DIR`, `CLAUDE_SETTINGS_FILE`).
Tests: `internal/integrations/registry_test.go` — test that `AllDefinitions()` returns 3 definitions, each with non-empty ID/Name/Template. Test that `NewDefaultDirs()` resolves correctly from env vars and defaults.

### Task 2: Implement generic Install/Uninstall/Upgrade

Files: `internal/integrations/installer.go` (new), `internal/integrations/installer_test.go` (new)
Depends on: Task 1
Description: Implement `Install()`, `Uninstall()`, and `Upgrade()` that operate on any `Definition`. Template rendering (version/bin substitution), file writing with backup, and config patching via `ConfigPatchFunc`. The uninstall path calls `ConfigPatchFunc` with `install=false` to remove the hook entry. Use `t.TempDir()` in tests to simulate the full install/uninstall cycle.
Tests: For each of the 3 integrations: test install creates expected files + patches config correctly. Test uninstall removes hook from config. Test upgrade replaces template and updates version marker. Test install is idempotent (running twice doesn't duplicate hook entries).

### Task 3: Add integration state to config

Files: `internal/config/config.go`, `configs/example_settings.json`
Depends on: none
Description: Add `IntegrationState` struct and `Integrations map[string]IntegrationState` field to `Config`. Add `SaveIntegrationState(id string, state IntegrationState) error` method following the existing RMW pattern. Update example config with a sample `integrations` section.
Tests: `internal/config/config_test.go` — test round-trip: save integration state, reload config, verify state preserved. Test that missing `integrations` key in existing config loads as empty map (backward compat).

### Task 4: Detection bridge (match tools/accounts to integrations)

Files: `internal/integrations/match.go` (new), `internal/integrations/match_test.go` (new)
Depends on: Task 1
Description: Implement `MatchDetected()` that takes `AllDefinitions()`, a `detect.Result`, and `Dirs`, returning `[]Match`. Matching uses `Definition.MatchProviderIDs` against `detect.Result.Accounts[].Provider` as the stable join key. If a `DetectedTool` exists for the same provider, it's included in the `Match` for display (binary path, config dir). Handles OpenCode (env-key-only, no DetectedTool) correctly.
Tests: Test with mock accounts matching by provider ID. Test OpenCode case (account with provider="opencode", no DetectedTool). Test that an account with no matching integration is ignored. Test that an installed integration shows as not-actionable.

### Task 5: CLI `integrations` command group

Files: `cmd/openusage/integrations.go` (new), `cmd/openusage/main.go` (register command)
Depends on: Task 1, 2, 3, 4
Description: Implement `integrations list`, `integrations install <id|--all>`, `integrations upgrade <id|--all>`, `integrations uninstall <id>`, `integrations status [id]`. The `list` command runs auto-detect + match and prints a table. The `install` command calls `Install()` + `SaveIntegrationState()`. The `upgrade` command re-installs outdated integrations. Register the command group on root in `main.go`.
Tests: Since CLI commands do I/O, test the core logic (list formatting, install orchestration) as exported functions called from the command handlers. Use `t.TempDir()` for filesystem operations.

### Task 6: Daemon startup integration check

Files: `cmd/openusage/telemetry.go`
Depends on: Task 1, 3, 4
Description: Add `checkIntegrations()` call during daemon startup. Runs detection, matches to definitions, logs suggestions for uninstalled integrations (skipping declined ones). Non-interactive — log only.
Tests: Test `checkIntegrations()` with a config that has one declined integration — verify it's not logged. Test with a detected-but-uninstalled integration — verify the log message includes the install command.

### Task 7: Refactor manager.go to use registry and update TUI callers

Files: `internal/integrations/manager.go`, `internal/tui/model.go`, `internal/tui/settings_modal.go`
Depends on: Task 1, 2
Description: Refactor `Manager` to delegate to the registry + installer instead of per-integration methods. `ListStatuses()` iterates `AllDefinitions()` and calls each `Detector`. `Install(id)` looks up the definition and calls `Install()`. Keep the `Manager` struct for `Dirs` resolution (it still knows about env vars and default paths). Remove `installOpenCode()`, `installCodex()`, `installClaudeCode()`, `detectOpenCode()`, `detectCodex()`, `detectClaudeCode()` — their logic now lives in definitions. Update TUI callers: `model.go:290` (`installIntegrationCmd`) and `model.go:1902` (`refreshIntegrationStatuses`) both call `integrations.NewDefaultManager()` directly — update these to use the refactored Manager API.
Tests: Rewrite `manager_test.go` — existing tests directly call removed methods (`m.detectOpenCode()`, `m.detectCodex()`, `m.detectClaudeCode()`). New tests should exercise `Manager.ListStatuses()` and `Manager.Install(id)` through the registry-backed implementation. Add test that `Install()` with unknown ID returns error.

### Task 8: Update docs and deprecate shell scripts

Files: `docs/TELEMETRY_INTEGRATIONS.md`, `plugins/` (delete entire directory)
Depends on: Task 5
Description: Delete `plugins/` directory entirely (redundant shell scripts and source copies). Update `TELEMETRY_INTEGRATIONS.md` to document `openusage integrations install` as the sole install method. Update any other docs that reference `plugins/`.
Tests: None (documentation + deletion only).

### Task 9: Integration verification (end-to-end)

Files: `internal/integrations/installer_test.go` (extend)
Depends on: Task 1, 2, 3, 4, 5, 7
Description: End-to-end test: create a temp dir structure simulating a workstation with Claude Code and Codex config dirs. Run detection, match, install all, verify files created, verify config files patched correctly, verify config state saved. Then run upgrade (bump version), verify template updated. Then uninstall, verify hooks removed from config. This validates the full lifecycle.
Tests: Single comprehensive test function covering install → verify → upgrade → uninstall cycle for all 3 integrations.

### Dependency Graph

```
Sequential: Task 1 (registry types)
Parallel group: Tasks 2, 3, 4 (all depend on Task 1 only, independent of each other)
Sequential: Task 5 (CLI commands, depends on 1-4)
Parallel group: Tasks 6, 7 (depend on 1+3+4 and 1+2 respectively, independent of each other)
Sequential: Task 8 (docs, depends on 5)
Sequential: Task 9 (end-to-end verification, depends on all above)
```

```
        ┌──── Task 2 (installer) ──────┐
        │                               │
Task 1 ─┼──── Task 3 (config) ─────────┼── Task 5 (CLI) ── Task 8 (docs)
        │                               │       │
        └──── Task 4 (match) ──────────┘       │
                │                               │
                └── Task 6 (daemon check) ──────┤
                                                │
                    Task 7 (refactor manager) ──┤
                                                │
                                          Task 9 (e2e)
```
