# Local Web Dashboard (`openusage serve`) Design

Date: 2026-08-28
Status: Proposed
Author: cursor-agent

## 1. Problem Statement

OpenUsage's live usage view is terminal-only; there is no way to open the same snapshot data in a browser on the local machine.

## 2. Goals

1. Add `openusage serve` to start a local HTTP server that serves a self-contained usage dashboard.
2. Reuse the existing headless snapshot path (`export.Collect`) so the web view matches `openusage export` / daemon read-model data.
3. Ship a polished dark dashboard (Catppuccin Mocha, with a light-theme toggle) inspired by [CPA Usage Keeper](https://github.com/Willxup/cpa-usage-keeper): KPI cards, spend chart, provider cards, and a detail pane.
4. Default to loopback-only bind (`127.0.0.1:8080`) with the same unsafe-default guard as `openusage hub`.

## 3. Non-Goals

1. A React/Node frontend toolchain or a separate `web/` package.json build.
2. Replacing the Bubble Tea TUI.
3. Multi-user accounts, TLS termination, or a hosted SaaS dashboard.
4. Mutating settings, installing the daemon, or writing credentials from the browser.
5. Historical analytics beyond what `UsageSnapshot.DailySeries` / `ModelUsage` already carry.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | Reuses `UsageSnapshot` as-is |
| providers | none | Catalog names read from `AllProviders().Describe()` |
| TUI | none | Terminal dashboard unchanged |
| config | minor | Optional `serve.listen_addr` |
| detect | none | — |
| daemon | none | Read via existing `export.Collect` / daemon socket |
| telemetry | none | — |
| CLI | major | New `openusage serve` cobra command |

New package: `internal/webserve/` — HTTP server, snapshot collector wrapper, embedded UI.

### Existing Design Doc Overlap

- `docs/REMOTE_EXPORTER_DESIGN.md` — hub is a TUI + JSON push API for multi-machine aggregation. This feature is a **browser UI for the local workstation**, not a replacement for hub. Non-goal 4 of that doc ("Web UI — hub is a TUI") stays true for hub.

## 5. Detailed Design

### 5.1 CLI

```
openusage serve [--listen ADDR] [--source auto|direct|daemon] [--demo] [--open|--no-open] [--allow-public]
```

| Flag | Default | Purpose |
|------|---------|---------|
| `--listen` | `127.0.0.1:8080` (or `serve.listen_addr`) | TCP bind address |
| `--source` | `auto` | Same collection sources as `openusage export` |
| `--demo` | off | Serve synthetic snapshots (no daemon / keys required) |
| `--open` | on when stdout is a TTY | Open the default browser |
| `--no-open` | off | Skip opening a browser |
| `--allow-public` | off | Allow a non-loopback bind without `OPENUSAGE_SERVE_TOKEN` |

Startup prints the URL and collection source, then blocks until SIGINT/SIGTERM.

### 5.2 HTTP surface

| Endpoint | Auth | Body |
|----------|------|------|
| `GET /` | no (static) | Embedded SPA |
| `GET /app.css`, `GET /app.js` | no | Embedded assets |
| `GET /healthz` | never | `{status, source}` |
| `GET /api/v1/snapshots` | Bearer when token set | Snapshot envelope |
| `GET /api/v1/meta` | Bearer when token set | Version, theme, window, source |

The SPA is vanilla HTML/CSS/JS embedded with `go:embed`. No npm step.

### 5.3 Envelope

```go
type Envelope struct {
    SchemaVersion           string               `json:"schema_version"`
    GeneratedAt             time.Time            `json:"generated_at"`
    OpenUsageVersion        string               `json:"openusage_version"`
    Source                  string               `json:"source"` // daemon | direct | demo
    TimeWindow              string               `json:"time_window"`
    Theme                   string               `json:"theme"`
    RefreshIntervalSeconds  int                  `json:"refresh_interval_seconds"`
    Catalog                 []CatalogEntry       `json:"catalog"`
    Snapshots               []core.UsageSnapshot `json:"snapshots"`
}
```

`Raw` maps are stripped before JSON (same defensive posture as `export`). Snapshots are cached in-process for the UI refresh interval so `--source direct` does not re-poll providers on every browser tick.

### 5.4 Auth and bind posture

- Default bind is loopback. `:8080` / `0.0.0.0:8080` without a token is refused unless `--allow-public`.
- Token comes from `OPENUSAGE_SERVE_TOKEN` (never persisted). Constant-time compare, same as hub.
- `/healthz` stays unauthenticated.
- When a token is required, the SPA prompts for it and stores it in `sessionStorage`.

### 5.5 UI

Keeper-inspired layout, OpenUsage colors:

- Dark Catppuccin Mocha default; warm paper light theme toggle (persisted in `localStorage`).
- Overview: KPI cards (spend, tokens, providers, healthy count), 14-day spend bars from `DailySeries`, model mix, provider card grid with status pills and remaining gauges.
- Click a card for a slide-over: message, metrics table, model usage, daily sparkline.
- Auto-refresh at `refresh_interval_seconds`. Empty / auth / error states are first-class.

### 5.6 Config

```go
type ServeConfig struct {
    ListenAddr string `json:"listen_addr"` // empty → 127.0.0.1:8080
    AuthToken  string `json:"-"`
}
```

Additive. Empty `serve` object means current defaults.

### 5.N Backward Compatibility

No public interface or stored-data changes. Existing configs ignore the new optional key.

## 6. Alternatives Considered

### Attach a web UI to `openusage hub`

Rejected: hub is a multi-machine aggregator with a different threat model (often bound on LAN). Local serve should work on one machine with no hub config.

### React + Vite frontend like Keeper

Rejected: adds a Node toolchain to a Go binary project. `go:embed` of a hand-written SPA keeps `make build` as the only build step.

### Convert TUI frames to HTML

Rejected: lipgloss output is terminal-specific. A native web layout is the value of this feature.

## 7. Implementation Tasks

### Task 1: Config + design types
Files: `internal/config/config.go`, `configs/example_settings.json`, `docs/WEB_SERVE_DESIGN.md`
Depends on: none
Description: Add optional `ServeConfig` with `listen_addr`. Do not persist auth tokens.
Tests: default config still has empty listen addr (runtime default applied later).

### Task 2: webserve package
Files: `internal/webserve/*.go`
Depends on: Task 1
Description: HTTP server, bind guard, snapshot collect/cache/sanitize, demo snapshots, provider catalog, embedded static handler.
Tests: httptest for health/snapshots/auth/static; bind-address table; Raw stripping; demo envelope shape.

### Task 3: CLI command
Files: `cmd/openusage/serve.go`, `cmd/openusage/serve_test.go`, `cmd/openusage/main.go`
Depends on: Task 2
Description: Cobra `serve` command, flag resolution, signal shutdown, optional browser open.
Tests: listen defaulting, exposure guard, token env resolution.

### Task 4: Embedded dashboard UI
Files: `internal/webserve/ui/index.html`, `app.css`, `app.js`
Depends on: Task 2
Description: Self-contained SPA with overview/detail, charts, theme toggle, token prompt.
Tests: served by the static handler test; visual check via `--demo`.

### Task 5: Docs
Files: `docs/site/docs/reference/cli.md`, `configuration.md`, `env-vars.md`, `getting-started/ways-to-use.md`, `guides/web-dashboard.md`, `guides/headless-servers.md`, `concepts/architecture.md`, `sidebars.ts`
Depends on: Task 3
Description: Document the command, flags, endpoints, and bind/auth posture. Add a guide page.
Tests: `DOCS_PREVIEW=1 npm run build` in `docs/site` succeeds.

### Dependency Graph

```
Task 1 → Task 2 → Task 3 → Task 5
              ↘ Task 4
```
