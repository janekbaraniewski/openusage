# Startup App Update Check — Design Doc

Date: 2026-02-27
Status: Implemented
Author: OpenUsage

## 1. Problem Statement

Users had no built-in signal that their OpenUsage binary was outdated. This led to silent drift between installed and latest release versions, with no in-app upgrade guidance.

## 2. Goals

1. Check for newer OpenUsage releases automatically on dashboard startup.
2. Keep startup responsive (non-blocking, short timeout, graceful failure).
3. Show clear upgrade instructions in TUI when update is available.
4. Tailor upgrade instructions to likely install method (Homebrew, `go install`, install script, Scoop, Chocolatey).

## 3. Non-Goals

1. In-place self-update of the OpenUsage binary.
2. Background polling during runtime after startup.
3. Adding config knobs for update-check behavior in this iteration.
4. Checking pre-release channels (`-rc`, `-beta`, etc.).

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| TUI | minor | New splash + footer notice when app update is available. |
| CLI startup | minor | Triggers async check at dashboard startup. |
| version | none | Reuses existing `internal/version.Version` ldflag value. |
| providers/telemetry/daemon | none | No behavior or API changes. |

### Compatibility

- No config schema changes.
- No persistent state changes.
- No data model changes.

## 5. Detailed Design

### 5.1 New `appupdate` package

Add `internal/appupdate/checker.go` with:

- `Check(ctx, CheckOptions) (Result, error)`
- `detectInstallMethod(executablePath) InstallMethod`
- `fetchLatestReleaseVersion(...)` against GitHub Releases API:
  - `https://api.github.com/repos/janekbaraniewski/openusage/releases/latest`

`Result` includes:

- `CurrentVersion`
- `LatestVersion`
- `UpdateAvailable`
- `InstallMethod`
- `UpgradeHint`

### 5.2 Version policy

Only stable semver versions are eligible for comparison:

- Accepted: `vX.Y.Z`, `X.Y.Z` (normalized to `vX.Y.Z`)
- Ignored: `dev`, invalid semver, prerelease/build metadata (`vX.Y.Z-rc.1`, `+meta`)

If current version is not stable semver, update checking is skipped silently.

### 5.3 Startup integration

In `cmd/openusage/dashboard.go`, run `appupdate.Check` in a goroutine immediately after creating the Bubble Tea program:

- timeout: `1200ms`
- non-blocking startup
- on success + newer version detected: send `tui.AppUpdateMsg`
- on network/API error: ignore (no fatal path, no user disruption)

### 5.4 Install method detection

Install method inferred from executable path heuristics:

- Homebrew: `.../Cellar/openusage/...` and common links
- Go install: `GOBIN`, `GOPATH/bin`, `~/go/bin`
- Install script: `/usr/local/bin/openusage`, `~/.local/bin/openusage`, `~/bin`
- Scoop: `.../scoop/apps/openusage/...`
- Chocolatey: `.../chocolatey/...`
- Unknown: fallback behavior

### 5.5 Upgrade hint mapping

- Homebrew: `brew upgrade janekbaraniewski/tap/openusage`
- Go install: `go install github.com/janekbaraniewski/openusage/cmd/openusage@latest`
- Install script: `curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash`
- Scoop: `scoop update openusage`
- Chocolatey: `choco upgrade openusage -y`
- Unknown: same actionable install script command (`curl ... | bash`) across platforms.

### 5.6 TUI rendering behavior

If update is available, surface notice in:

1. Splash progress block (`internal/tui/help.go`):
   - headline: `OpenUsage update available: <current> -> <latest>`
   - action line: `Run: <upgrade command>`
2. Footer status line (`internal/tui/model.go`) when no higher-priority footer state is active.

This keeps upgrade info visible both during startup and once dashboard is loaded.

### 5.7 Debug behavior

- In normal mode: update-check failures remain silent (no user disruption).
- In debug mode (`OPENUSAGE_DEBUG=1`): startup logs one line when update check fails, for diagnosis.

## 6. Failure & Edge-Case Handling

1. GitHub API timeout / network error / non-200:
   - no crash
   - no user-facing error for update check
2. Rate-limits:
   - optional `OPENUSAGE_GITHUB_TOKEN` is forwarded as Bearer token
3. Dev builds (`Version=dev`):
   - no update check, no notice
4. Forced test binaries in unusual locations (for example `/tmp/openusage-old`):
   - install method may be `unknown`
   - still shows actionable install-script upgrade command (`curl ... | bash`)
5. Windows with unknown install method:
   - still uses same `curl ... | bash` fallback (explicit product decision for this iteration).

## 7. Security & Privacy Considerations

1. Single unauthenticated GET to GitHub Releases API (or token-authenticated if env var provided).
2. No local credential persistence related to update checks.
3. API token (if provided) only sent to GitHub API endpoint via HTTPS.
4. If a non-GitHub override URL is used (tests/internal tooling), token is not forwarded.

## 8. Implementation Tasks

### Task 1: Add update checker package

Files: `internal/appupdate/checker.go`, `internal/appupdate/checker_test.go`  
Status: COMPLETE  
Description: Implement version normalization, release fetch, install-method detection, and upgrade hint mapping.

### Task 2: Wire startup async check

Files: `cmd/openusage/dashboard.go`  
Status: COMPLETE  
Description: Trigger non-blocking check on startup and emit `tui.AppUpdateMsg` when update is available.

### Task 3: Add TUI message/state/rendering

Files: `internal/tui/model.go`, `internal/tui/help.go`  
Status: COMPLETE  
Description: Add update state fields, process `AppUpdateMsg`, and render update notice in splash and footer.

### Task 4: Improve unknown-method guidance

Files: `internal/appupdate/checker.go`, `internal/tui/help.go`, tests  
Status: COMPLETE  
Description: Use actionable fallback upgrade command for unknown install method; add clearer daemon recovery hint in splash error state.

### Task 5: Add observability + startup seam tests

Files: `cmd/openusage/dashboard.go`, `cmd/openusage/dashboard_update_test.go`, `internal/appupdate/checker_test.go`  
Status: COMPLETE  
Description: Add debug-only logging for update-check failures and unit tests for startup orchestration + scoped GitHub auth header forwarding.

## 9. Validation

Executed:

- `go test ./internal/appupdate ./internal/tui ./cmd/openusage`
- `go test ./...`
- `go vet ./...`

## 10. Future Enhancements

1. Add optional config toggle to disable update checks.
2. Add periodic re-check (for long-running sessions), rate-limited.
3. Add explicit source detector if install metadata becomes available (instead of path heuristics).
4. Add optional changelog link rendering (`releases/tag/<latest>`).
