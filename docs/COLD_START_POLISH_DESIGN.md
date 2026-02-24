# Cold Start Polish — Design Doc

## Problem Statement

The cold-start experience is buggy and confusing — the loading screen flickers between layout modes on state transitions, the daemon install prompt briefly re-appears after successful install, the word "daemon" is jargon that newcomers don't understand, and there's no progress feedback about what the app is doing during startup.

## Goals

1. **Fix bugs**: Eliminate the visual jump when transitioning between daemon states, fix the post-install flash where the "not installed" prompt reappears briefly.
2. **Friendly language**: Replace "daemon" with "background helper" in all user-facing splash text. Keep internal variable names unchanged.
3. **Progress visibility**: Show step-based progress so users know what's happening (config loaded, providers detected, helper connecting, data loading).
4. **Unified layout**: One rendering path for the splash screen — always banner on top, progress steps below, action hints at bottom. No layout shifts between states.
5. **Polish the install prompt**: Make the "set up background helper" prompt welcoming and clear.

## Non-Goals

- Changing the actual startup/connection logic (timeouts, retries, warm-up loop).
- Changing daemon internals, service management, or the broadcaster architecture.
- Adding new config options or changing the config schema.
- Modifying provider behavior or detection logic.

## Who Benefits

End users, especially newcomers running OpenUsage for the first time.

## Subsystems Affected

- **TUI** (`internal/tui/help.go`, `internal/tui/model.go`) — primary changes
- **Daemon** (`internal/daemon/process.go`) — terminology in ClassifyEnsureError messages

## Impact Analysis

### Bug 1: Layout Jump on State Transition

`renderSplash()` has two rendering paths controlled by `useBrandedSplashLoader()`:
- **Branded path** (DaemonConnecting, DaemonRunning): banner + single spinner line
- **Non-branded path** (NotInstalled, Outdated, Error, Starting): banner + multi-line status

When the state transitions from DaemonConnecting → DaemonNotInstalled, the layout jumps from a compact single-line display to a multi-line prompt. The centering recalculates, causing a visible flicker.

**Fix**: Unify into one rendering path that always shows banner + progress steps. The step content changes, but the layout structure stays stable.

### Bug 2: Post-Install Flash

In `model.go` `Update()`, `daemonInstallResultMsg` with `err == nil` sets `daemonInstalling = false` but doesn't update `daemonStatus`. The status remains `DaemonNotInstalled`, so the "not installed" prompt briefly reappears until the broadcaster detects the running daemon and emits `DaemonRunning`.

**Fix**: On successful install, set `daemonStatus = DaemonStarting` to show "Starting background helper..." while waiting for broadcaster confirmation.

### Bug 3: Dead Code in splashStatusLines

The `default` case in `splashStatusLines()` (help.go:348-360) is unreachable — `useBrandedSplashLoader()` routes DaemonConnecting and DaemonRunning to the branded path, so they never reach `splashStatusLines()`. The remaining statuses all have explicit cases.

**Fix**: Remove dead code during the splash rewrite.

## Design

### Unified Splash Layout

Replace the two-path rendering with a single layout:

```
     █▀█ █▀█ █▀▀ █▄░█   █░█ █▀ ▄▀█ █▀▀ █▀▀
     █▄█ █▀▀ ██▄ █░▀█   █▄█ ▄█ █▀█ █▄█ ██▄

     ✓ Configuration loaded
     ✓ 4 providers detected
     ⠋ Connecting to background helper...

     Press q to quit
```

When background helper is not set up:

```
     █▀█ █▀█ █▀▀ █▄░█   █░█ █▀ ▄▀█ █▀▀ █▀▀
     █▄█ █▀▀ ██▄ █░▀█   █▄█ ▄█ █▀█ █▄█ ██▄

     ✓ Configuration loaded
     ✓ 4 providers detected

     OpenUsage uses a small background helper to
     collect and cache usage data from your providers.

     ▸ Press Enter to set it up
       or run: openusage telemetry daemon install

     Press q to quit
```

After pressing Enter (installing):

```
     ✓ Configuration loaded
     ✓ 4 providers detected
     ⠋ Setting up background helper...

     Press q to quit
```

After install succeeds, waiting for data:

```
     ✓ Configuration loaded
     ✓ 4 providers detected
     ✓ Background helper running
     ⠋ Fetching usage data...

     Press q to quit
```

Error state:

```
     ✓ Configuration loaded
     ✓ 4 providers detected
     ✗ Could not connect to background helper
       Try: openusage telemetry daemon status

     Press q to quit
```

### Progress Steps

Progress is derived from existing model state — no new tracking needed:

| Step | Source | Display |
|------|--------|---------|
| Config loaded | Always true (TUI only runs after config.Load) | `✓ Configuration loaded` |
| Providers detected | `len(m.providerOrder)` | `✓ N providers detected` or `· No providers detected` |
| Helper status | `m.daemonStatus` + `m.daemonInstalling` | Varies by state (see above) |
| Data loading | `m.hasData` transitions to true | `⠋ Fetching usage data...` (shown when helper is running but no data yet) |

### Terminology Mapping

| Current (user-facing) | New |
|----------------------|-----|
| "Daemon service is not installed." | "Background helper is not set up." |
| "Installing daemon service..." | "Setting up background helper..." |
| "Starting daemon..." | "Starting background helper..." |
| "Connecting to telemetry daemon..." | "Connecting to background helper..." |
| "Could not connect to daemon." | "Could not connect to background helper." |
| "Daemon is outdated." | "Background helper needs an update." |
| "Upgrading daemon service..." | "Updating background helper..." |
| "Press Enter to install automatically" | "Press Enter to set it up" |
| "Press Enter to upgrade" | "Press Enter to update" |

Internal variable names (`DaemonStatus`, `daemonInstalling`, `DaemonConnecting`, etc.) stay unchanged.

### Shared Loading Component

`brandedLoaderLines()` (`help.go:405`) and `resolveLoadingMessage()` (`help.go:393`) are used by both the splash screen and dashboard tile loading states (`tiles.go:498`). These functions stay as a shared loading component. The splash rewrite replaces only the splash-specific rendering path while keeping the shared component intact for tiles.

### Functions to Rewrite/Remove

In `internal/tui/help.go`:
- **`renderSplash()`**: Replace two-path branching with single unified layout (banner + `splashProgressLines()` + hints). Use `brandedLoaderLines()` for the banner+spinner portion to stay in sync with tile loading.
- **`splashStatusLines()`** → rename to **`splashProgressLines()`**: Return all progress step lines (config, providers, helper status, data loading) as a single slice.
- **`loadingSplashMessage()`**: Remove (no longer needed — progress steps replace generic messages).
- **`useBrandedSplashLoader()`**: Remove (no longer needed — unified path).
- **Keep `brandedLoaderLines()`**: Shared with `tiles.go:498` for tile loading states.
- **Keep `resolveLoadingMessage()`**: Called by `brandedLoaderLines()`, tested in `loading_message_test.go`.

### Post-Install State Fix

In `internal/tui/model.go`, `Update()` case `daemonInstallResultMsg`:

```go
case daemonInstallResultMsg:
    m.daemonInstalling = false
    if msg.err != nil {
        m.daemonStatus = DaemonError
        m.daemonMessage = msg.err.Error()
    } else {
        m.daemonStatus = DaemonStarting  // <-- NEW: show "Starting..." instead of flashing back to "not installed"
    }
    return m, nil
```

## Backward Compatibility

No impact. Changes are purely visual:
- No config schema changes
- No stored data changes
- No public interface changes (`UsageProvider`, `UsageSnapshot`, `AccountConfig` unchanged)
- Internal `DaemonStatus` types/values unchanged
- CLI subcommand output (`openusage telemetry daemon install/status`) unchanged

## Implementation Tasks

### Task 1: Fix post-install flash bug and update terminology in model.go

Files: `internal/tui/model.go`
Depends on: none
Description: Two changes in model.go:
1. In the `daemonInstallResultMsg` handler (line 322), set `m.daemonStatus = DaemonStarting` on successful install (when `msg.err == nil`). This prevents the brief flash back to the "not installed" prompt while the broadcaster detects the running helper.
2. In `handleSplashKey()` (line 449), update `m.daemonMessage = "Installing daemon service..."` → `"Setting up background helper..."`.
Tests: Write new tests (none exist today) for the install result handler — verify that successful install sets status to DaemonStarting, and that failed install sets DaemonError with the error message.

### Task 2: Rewrite splash rendering with unified layout

Files: `internal/tui/help.go`
Depends on: none
Description: Replace `renderSplash()` (line 242) with a unified single-path layout: always render ASCII banner at top, then progress step lines from a new `splashProgressLines()` function, then a bottom hint line ("Press q to quit"). Remove `useBrandedSplashLoader()` (line 384), `loadingSplashMessage()` (line 363), and `splashStatusLines()` (line 289). Keep `brandedLoaderLines()` (line 405) and `resolveLoadingMessage()` (line 393) — they are shared with `tiles.go:498` for tile loading states.

The new `splashProgressLines()` returns step lines based on model state:
- Config loaded: always show checkmark
- Providers detected: show count from `len(m.providerOrder)` with checkmark, or dim dot if zero
- Helper status: varies by `m.daemonStatus` and `m.daemonInstalling` (spinner/checkmark/error/install prompt)
- Data loading: spinner when helper is running but `!m.hasData`

The install prompt for DaemonNotInstalled should show a welcoming explanation ("OpenUsage uses a small background helper...") followed by the action hint.
Tests: Write new tests for `splashProgressLines()` — verify correct lines for each daemon status (Connecting, NotInstalled, Starting, Running, Outdated, Error) and for the installing sub-state.

### Task 3: Update daemon error messages for friendly language

Files: `internal/daemon/process.go`
Depends on: none
Description: Update `ClassifyEnsureError()` (line 16) message for the "not installed" case (line 25): `"Daemon service is not installed."` → `"Background helper is not set up."`. This is the only hardcoded user-facing string in this function — the other cases pass through the raw error message. The `InstallHint` (line 26) stays unchanged (it's a CLI command).

Note: Most user-facing "daemon" strings (12 total) are hardcoded in `help.go` and `model.go`, not in `process.go`. Those are handled by Tasks 1 and 2. This task covers only the one message that flows through `DaemonState.Message` → `model.daemonMessage` → TUI.
Tests: Write new tests for `ClassifyEnsureError` — none exist today (`process_test.go` only tests `IsReleaseSemver` and `HealthCurrent`).

### Task 4: Integration verification

Files: `cmd/openusage/dashboard.go`, `internal/tui/model.go`, `internal/tui/help.go`
Depends on: Tasks 1, 2, 3
Description: Verify the full startup flow works end-to-end: build the binary, confirm the splash renders correctly for each state. Ensure the demo command still works (demo bypasses the daemon, so splash is not shown — verify it's unaffected). Check that `handleSplashKey()` still works for Enter (install) and q (quit).
Tests: Manual verification + ensure `make test` passes.

### Dependency Graph

- Tasks 1, 2, 3: parallel group (independent changes in different functions/files)
- Task 4: depends on all (integration verification)
