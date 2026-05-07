# Remote Exporter Design

Date: 2026-05-07
Status: Proposed
Author: kevinlin

## 1. Problem Statement

OpenUsage runs independently on each machine; there is no way to view aggregated AI tool usage and spend across multiple machines from a single dashboard.

## 2. Goals

1. Any OpenUsage instance can push its live snapshots to a central hub over plain HTTP.
2. A hub instance (`openusage hub`) receives snapshot batches from multiple machines and presents them in a unified TUI view.
3. Machine identity is derived from hostname by default, optionally overridden in config.
4. The feature is entirely opt-in: no config means no change in existing behavior.

## 3. Non-Goals

1. Multi-user ACL or per-user authentication.
2. TLS / encrypted transport (plain HTTP only for now).
3. Historical aggregation on the hub — hub holds only the latest snapshot batch per machine (in-memory).
4. Web UI — hub is a TUI like the rest of OpenUsage.
5. Push from the hub back to worker machines.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem   | Impact | Summary |
|-------------|--------|---------|
| core types  | minor  | New `RemoteEnvelope` type in `internal/core/remote.go` |
| providers   | none   | — |
| TUI         | none   | Existing tile rendering works unchanged via `snap.ProviderID`; machine appears in `snap.AccountID` prefix |
| config      | minor  | Two new optional sub-structs: `ExportConfig`, `HubConfig` |
| detect      | none   | — |
| daemon      | none   | — |
| telemetry   | none   | — |
| CLI         | minor  | New `openusage hub` cobra subcommand; dashboard wires exporter when `export.target` set |

New packages:
- `internal/exporter/` — background push goroutine
- `internal/hub/` — TCP HTTP server + in-memory store

### Existing Design Doc Overlap

None of the existing design docs in `docs/` cover remote aggregation. This is a new standalone feature.

## 5. Detailed Design

### 5.1 Core Type: `RemoteEnvelope`

New file `internal/core/remote.go`:

```go
// RemoteEnvelope is the wire format for snapshot batches pushed from a worker to the hub.
type RemoteEnvelope struct {
    Machine   string          `json:"machine"`    // hostname or configured machine_name
    SentAt    time.Time       `json:"sent_at"`
    Snapshots []UsageSnapshot `json:"snapshots"`
}
```

`UsageSnapshot` already carries full JSON tags — no changes needed there.

### 5.2 Config: `ExportConfig` and `HubConfig`

Added to `internal/config/config.go` and wired into the top-level `Config` struct:

```go
type ExportConfig struct {
    Target          string `json:"target"`            // e.g. "http://hub-host:9190" — empty disables
    IntervalSeconds int    `json:"interval_seconds"`  // default 60
    MachineName     string `json:"machine_name"`      // default: os.Hostname()
}

type HubConfig struct {
    ListenAddr           string `json:"listen_addr"`            // default ":9190"
    StaleTimeoutSeconds  int    `json:"stale_timeout_seconds"`  // default 300 (5 min)
}
```

Example `settings.json` additions:

```json
{
  "export": {
    "target": "http://192.168.1.10:9190",
    "interval_seconds": 60
  }
}
```

```json
{
  "hub": {
    "listen_addr": ":9190"
  }
}
```

### 5.3 Exporter (`internal/exporter/`)

**Files:** `exporter.go`, `exporter_test.go`

```go
type Exporter struct {
    target      string
    machine     string        // resolved machine name (hostname or config override)
    interval    time.Duration
    http        *http.Client
    mu          sync.RWMutex
    latest      []core.UsageSnapshot  // updated via Ingest(), pushed on each tick
}

func New(cfg config.ExportConfig) (*Exporter, error)
func (e *Exporter) Ingest(snaps map[string]core.UsageSnapshot)  // called by dashboard broadcaster
func (e *Exporter) Start(ctx context.Context)                   // runs push loop; blocks until ctx done
```

`Ingest()` snapshots the values from the map (clones them) and replaces `e.latest`. The push loop fires every `e.interval`, serialises a `RemoteEnvelope`, and POSTs it to `{target}/v1/push`. HTTP errors are logged and skipped — the exporter is best-effort.

### 5.4 Hub Server (`internal/hub/`)

**Files:** `server.go`, `store.go`, `types.go`, `server_test.go`, `store_test.go`

#### Store

```go
type Store struct {
    mu           sync.RWMutex
    machines     map[string]machineEntry  // keyed by machine name
    staleTimeout time.Duration
}

type machineEntry struct {
    envelope    core.RemoteEnvelope
    receivedAt  time.Time
}

func NewStore(staleTimeout time.Duration) *Store
func (s *Store) Ingest(env core.RemoteEnvelope)
// Snapshots returns a flat map[string]core.UsageSnapshot with account keys prefixed
// "{machine}:{provider_id}:{account_id}". Stale entries (older than staleTimeout) are pruned.
func (s *Store) Snapshots() map[string]core.UsageSnapshot
func (s *Store) MachineNames() []string
```

Key insight: `Snapshots()` rewrites the AccountID to `"{machine}:{originalAccountID}"` so the TUI's tile grid naturally shows one row per machine+provider combination. The original `ProviderID` is untouched, so TUI widget rendering (colors, gauge styles) works without any changes.

#### Server

```go
type Server struct {
    cfg   HubServerConfig
    store *Store
}

func NewServer(addr string, store *Store) *Server
func (s *Server) ListenAndServe(ctx context.Context) error  // starts net/http on TCP addr
```

HTTP routes:
- `POST /v1/push` — decodes `RemoteEnvelope`, calls `store.Ingest()`; responds `{"ok": true}`
- `GET  /healthz` — returns `{"status":"ok","machines":["work-mac","home-linux"]}`

No authentication. Intended for trusted LAN use.

### 5.5 Hub CLI Command (`cmd/openusage/hub.go`)

New file registered in `main.go` via `root.AddCommand(newHubCommand())`.

```go
func newHubCommand() *cobra.Command
func runHub(cfg config.Config)
```

`runHub`:
1. Resolves `cfg.Hub.ListenAddr` (default `:9190`).
2. Creates `hub.Store` with stale timeout from config.
3. Starts `hub.Server.ListenAndServe()` in a goroutine.
4. Launches a feed goroutine that calls `store.Snapshots()` every 5 seconds and calls `dispatcher.dispatch()` with the result.
5. Starts the normal Bubble Tea TUI (same `tui.NewModel` call as `runDashboard`, but with no accounts/daemon — all data comes from the hub store feed).

No daemon, no providers, no auto-detect in hub mode.

### 5.6 Dashboard Exporter Integration (`cmd/openusage/dashboard.go`)

When `cfg.Export.Target != ""`, create an `*exporter.Exporter` and wire it:

```go
// in runDashboard, after existing broadcaster setup:
var exp *exporter.Exporter
if strings.TrimSpace(cfg.Export.Target) != "" {
    exp, err = exporter.New(cfg.Export)
    // log and skip on error
    if err == nil {
        go exp.Start(ctx)
    }
}

// Extend the broadcaster callback:
func(frame daemon.SnapshotFrame) {
    dispatcher.dispatch(frame)
    if exp != nil {
        exp.Ingest(frame.Snapshots)
    }
},
```

### 5.7 Backward Compatibility

- All new config fields are optional with zero values that disable the feature.
- No changes to existing CLI commands, daemon behavior, TUI, or provider interfaces.
- `UsageSnapshot` gains no new fields.
- Existing `settings.json` files without `export` or `hub` keys continue to work exactly as before.

## 6. Alternatives Considered

### Extend Daemon with TCP Listen

Add a `TCPAddr` field to `daemon.Config` and make the existing daemon HTTP mux also listen on TCP.

**Rejected**: The daemon carries significant complexity (SQLite, spool, telemetry pipeline, service install). The hub use-case only needs an in-memory store and a simple push endpoint. Mixing them would force the hub to manage a SQLite database it doesn't need, and would complicate the daemon's existing service lifecycle.

### Pull Instead of Push

The hub periodically GETs snapshots from each worker's daemon (already has `/v1/read-model`).

**Rejected**: Workers often run behind NAT or firewalls. Push from workers to hub is more universally deployable. The user confirmed push as the preferred direction.

### Prometheus Pushgateway-style Separate Binary

A standalone `openusage-hub` binary with no TUI.

**Rejected**: The user specifically asked for an `openusage`-native solution. A TUI-bearing `openusage hub` subcommand is consistent with the project's design.

## 7. Implementation Tasks

### Task 1: Core envelope type
Files: `internal/core/remote.go`
Depends on: none
Description: Add `RemoteEnvelope` struct with `Machine`, `SentAt`, `Snapshots` fields and JSON tags. No changes to existing types.
Tests: `internal/core/remote_test.go` — JSON roundtrip for `RemoteEnvelope`.

### Task 2: Config additions
Files: `internal/config/config.go`, `configs/example_settings.json`
Depends on: none
Description: Add `ExportConfig` and `HubConfig` structs. Wire both into `Config` as `Export ExportConfig` and `Hub HubConfig`. Add defaults in `DefaultConfig()`: `ExportConfig.IntervalSeconds = 60`, `HubConfig.ListenAddr = ":9190"`, `HubConfig.StaleTimeoutSeconds = 300`.
Tests: `internal/config/config_test.go` — verify zero-value Export/Hub fields don't break existing config loading; verify defaults are applied.

### Task 3: Exporter package
Files: `internal/exporter/exporter.go`, `internal/exporter/exporter_test.go`
Depends on: Task 1, Task 2
Description: Implement `Exporter` with `New()`, `Ingest()`, `Start()`. The push loop serialises `RemoteEnvelope` and POSTs to `{target}/v1/push`. Resolve machine name: use `config.ExportConfig.MachineName` if set, otherwise `os.Hostname()`. Log push errors with throttling; never crash.
Tests: `httptest.NewServer` that records received envelopes; verify machine name resolution; verify interval timing; verify empty snapshot map skips POST.

### Task 4: Hub server and store
Files: `internal/hub/types.go`, `internal/hub/store.go`, `internal/hub/server.go`, `internal/hub/store_test.go`, `internal/hub/server_test.go`
Depends on: Task 1, Task 2
Description: Implement `Store` with `Ingest()` and `Snapshots()` (machine-prefixed AccountID rewriting + stale pruning). Implement `Server` with `/v1/push` and `/healthz` handlers. `ListenAndServe` uses `net/http` on TCP; respects context cancellation.
Tests: `store_test.go` — ingest two machines, verify prefixed keys, verify stale pruning. `server_test.go` — POST valid/invalid envelopes, verify 200/400 responses.

### Task 5: Hub CLI command
Files: `cmd/openusage/hub.go`, `cmd/openusage/main.go`
Depends on: Task 4
Description: Add `newHubCommand()` cobra command that calls `runHub(cfg)`. `runHub` starts the hub server, launches a 5-second feed ticker that calls `store.Snapshots()` and sends `tui.SnapshotsMsg` to the program, then starts the Bubble Tea TUI with `tui.NewModel` (no accounts, no daemon runtime). Register with `root.AddCommand(newHubCommand())` in `main.go`.
Tests: Integration smoke test — start hub, POST one envelope, verify TUI receives non-empty `SnapshotsMsg`.

### Task 6: Dashboard exporter wiring
Files: `cmd/openusage/dashboard.go`
Depends on: Task 3
Description: In `runDashboard`, if `cfg.Export.Target != ""`, construct `exporter.New(cfg.Export)` and launch `exp.Start(ctx)`. Extend the `StartBroadcaster` callback to call `exp.Ingest(frame.Snapshots)` after dispatching. Guard with nil check.
Tests: Extend existing `dashboard_test.go` (if present) or add a small unit test verifying the wiring path is nil-safe when `Export.Target` is empty.

### Dependency Graph

```
Task 1 (core type)     ──┐
                          ├──► Task 3 (exporter)  ──► Task 6 (dashboard wiring)
Task 2 (config)        ──┤
                          └──► Task 4 (hub)        ──► Task 5 (hub CLI)
```

- Tasks 1, 2: sequential (foundational — no dependencies)
- Tasks 3, 4: parallel group (both depend on 1+2, independent of each other)
- Task 5: depends on 4 only
- Task 6: depends on 3 only
- Tasks 5 and 6 can run in parallel once their respective dependencies finish
