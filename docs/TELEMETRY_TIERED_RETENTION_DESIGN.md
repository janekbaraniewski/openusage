# Telemetry Tiered Retention (Downsample-and-Keep) Design

Date: 2026-06-11
Status: Proposed
Author: janekbaraniewski

## 1. Problem Statement

The daemon stores every telemetry event at full per-event granularity in
`usage_events` and is expected to delete anything older than `data.retention_days`
(default 30). In practice this is wrong on both ends:

- **Retention deletes history users want to keep.** The product goal is to keep
  long-term history and *downsample* it for size, not discard it. A hard delete
  past the window destroys the data needed for month-over-month and all-time
  views.
- **A hidden 90-day ceiling.** `config.go` clamped `retention_days` to a maximum
  of 90, so even a user who asked for a year silently got a quarter. (Raised to
  ~10y as an interim fix; see §5.7.)
- **Retention vs. re-import churn.** Local-file telemetry sources (codex,
  opencode, claude_code, …) re-derive events from their own session logs every
  collect cycle. When retention deleted events past the window, the next collect
  re-ingested them (their dedup keys were deleted too), so the pruner and the
  collectors fought continuously — the DB never settled and retention never
  "won".
- **Re-aggregation on every read.** With no rollups, every dashboard read
  re-runs a dedup CTE + multi-dimension aggregates over the full raw window. Cost
  grows with history depth.

The net effect observed in the field: ~7 months of raw events accumulating
(because retention was effectively broken), a read model that timed out under
contention, and — when retention was "fixed" naively — destructive deletion of
data the user explicitly wanted to keep.

This design replaces *delete-first* retention with *downsample-and-keep*:
full detail for a recent window, compact aggregates retained long-term, and
size bounded without losing the shape of history.

## 2. Goals

1. **Keep history as aggregates forever** (or for a very long window) while
   bounding database size. Charts, totals, and trends for any past period stay
   correct.
2. **Full per-event detail for a recent "hot" window** (drill-down, dedup,
   per-turn inspection).
3. **Constant-ish read cost** regardless of how far back a query reaches, by
   reading pre-aggregated rollups for older windows.
4. **Eliminate the retention/re-import churn** without an ingest-time floor.
5. **No data loss on rollout** — backfill rollups from existing raw before any
   raw is pruned.
6. Preserve the already-landed stability fixes (read-only read model,
   corrupt-backup cleanup, batched prune mechanics, launchd plist hardening,
   raised retention ceiling, tmux 5h-quota cache).

## 3. Non-Goals

1. Changing how events are *collected* or *normalized* (sources, dedup keys,
   provider links are unchanged).
2. A time-series database or external store — this stays single-file SQLite.
3. Per-event retention policies per provider (one global tiering policy).
4. Downsampling the `balance_observations` series — it is already tiered
   (48h full → hourly → daily, 35d floor) and stays as-is.
5. Backfilling raw detail that no longer exists in source logs (beyond the
   collectors' re-import lookback). Such periods keep aggregates only.

## 4. Impact Analysis

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Possibly a rollup record type for the read model; no wire/config-visible change |
| providers | none | Collection unchanged |
| TUI | minor | Read model returns the same `UsageSnapshot` shape; analytics may gain coarser granularity for old windows |
| config | moderate | `retention_days` repurposed as the hot (raw) window; new rollup-retention knobs |
| detect | none | — |
| daemon | moderate | New rollup loop; prune-after-rollup ordering; ingest floor removed |
| telemetry | major | New `usage_rollup_daily` table, rollup engine, tier-aware read model |
| CLI | minor | `daemon status` / a maintenance command may report rollup watermark |

## 5. Detailed Design

### 5.1 Tiers

Two tiers only. An hourly tier was considered and rejected (§6): nothing in the
UI needs sub-day resolution for data older than the hot window — the analytics
charts are daily, and anything sub-day (5h block, burn rate) is always inside the
hot window where raw is kept.

| Tier | Source | Granularity | Retention | Serves |
|------|--------|-------------|-----------|--------|
| Hot | `usage_events` (raw) | per-event | `data.retention_days` (default **90d**) | recent/detailed views, dedup, drill-down, 5h block, burn rate |
| Cold | `usage_rollup_daily` | day bucket × dims | `data.rollup_daily_days` (default **0 = forever**) | 30d, analytics, all-time, trends |

**Dimensions** carried in each daily rollup row (the GROUP BY key):
`day, provider_id, account_id, model_canonical, tool_name, project, language, interface, status`.
This preserves every breakdown the analytics screen slices by (you can still ask
"project X cost in March") — only the per-turn *timeline* within a day is lost.

**Measures** (summed): `input_tokens, output_tokens, reasoning_tokens,
cache_read_tokens, cache_write_tokens, total_tokens, cost_usd, requests,
event_count`.

`limit_snapshot` events are **not** rolled up — the latest one per provider is a
point-in-time quota state already hydrated by the read model; older ones are
prunable with raw.

### 5.2 Schema

```sql
CREATE TABLE IF NOT EXISTS usage_rollup_daily (
  day             TEXT NOT NULL,           -- 'YYYY-MM-DD' (UTC)
  provider_id     TEXT NOT NULL,
  account_id      TEXT NOT NULL DEFAULT '',
  model_canonical TEXT NOT NULL DEFAULT '',
  tool_name       TEXT NOT NULL DEFAULT '',
  project         TEXT NOT NULL DEFAULT '',
  language        TEXT NOT NULL DEFAULT '',
  interface       TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT '',
  input_tokens       INTEGER NOT NULL DEFAULT 0,
  output_tokens      INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens   INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens       INTEGER NOT NULL DEFAULT 0,
  cost_usd           REAL    NOT NULL DEFAULT 0,
  requests           INTEGER NOT NULL DEFAULT 0,
  event_count        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, provider_id, account_id, model_canonical, tool_name, project, language, interface, status)
);
CREATE INDEX IF NOT EXISTS idx_rollup_daily_window ON usage_rollup_daily (provider_id, account_id, day);
```

A `metadata` row tracks the rollup watermark: `rollup_daily_watermark = <latest
fully-rolled day>`.

### 5.3 Rollup engine (incremental, idempotent)

Runs in the daemon on a timer (e.g. every few minutes, and once at startup).

1. **Choose the dirty window.** From the watermark day to `today` (re-rolling the
   current and most recent day each pass so late-arriving events for those days
   are captured). Buckets are whole UTC days.
2. **Recompute, don't accumulate.** For each affected day, recompute the
   aggregate *from raw* and `INSERT … ON CONFLICT(PK) DO UPDATE SET …` the
   measures to the recomputed totals. Recompute-and-replace (vs. additive) makes
   it idempotent: re-running over a day yields the same row, so a crash/restart
   mid-rollup is safe and re-ingested (deduped) events self-correct.
3. **Advance the watermark** to the latest day that can no longer change (i.e.
   older than the hot window's tail / fully settled).

Dedup is already applied when rows enter `usage_events`, so the rollup sums the
canonical (deduped) facts directly — it reuses the existing deduped-usage view
to avoid double counting overlapping source channels.

### 5.4 Prune-after-rollup

The retention loop changes from *delete by age* to *delete only what is safely
rolled up*:

```
prune raw usage_events WHERE occurred_at < (now - hotWindow)
  AND date(occurred_at) <= rollup_daily_watermark   -- never delete un-rolled detail
```

This guarantees every deleted raw event is already represented in the daily
rollup. Batched deletion (already implemented) and the corrupt/orphan cleanup
remain. VACUUM only after large cleanups (already implemented).

### 5.5 Churn elimination (no ingest floor)

The collectors re-import roughly the last ~90 days of source history each cycle.
If the **hot window ≥ the collectors' re-import lookback**, every re-imported
event lands *inside* the hot window, so prune-after-rollup never deletes it and
the collectors never recreate a just-deleted row. The churn disappears with no
ingest-time floor (the floor from the abandoned approach is dropped). This is the
main reason to default the hot window to **90d**.

If the hot window is set shorter than the lookback, we either (a) keep a small
ingest floor at the lookback boundary, or (b) accept bounded churn. Recommended:
keep hot ≥ lookback and avoid the floor entirely.

### 5.6 Tier-aware read model

The read model selects a tier per query window:

- window entirely within the hot tier → aggregate from raw (current path).
- window reaches past the hot tier → read `usage_rollup_daily` for the older days
  and the raw path for the still-hot tail, unioned at the day boundary.

Rollup reads are plain indexed `SUM … GROUP BY day`, far cheaper than the raw
dedup CTE. The output is the same `UsageSnapshot` shape; only the granularity of
old data is coarser (per-day instead of per-event), which the daily analytics
charts already bucket anyway.

### 5.7 Config

Repurpose and extend `data.*`:

```jsonc
{
  "data": {
    "retention_days": 90,     // HOT window: how long full per-event detail is kept
    "rollup_daily_days": 0    // keep daily aggregates this long (0 = forever)
  }
}
```

- `retention_days` becomes the hot/raw window (back-compat: existing values keep
  working; the meaning shifts from "delete everything past this" to "keep full
  detail this long, aggregates beyond").
- The 90-day clamp is removed/raised (interim fix already applied: ceiling 3650).
- Migration: on upgrade, existing DBs get the rollup tables created and a
  one-time backfill (§5.8); no config change is required for defaults.

### 5.8 Rollout / backfill

1. Create the rollup table (idempotent `Init`).
2. **Backfill**: on first start after upgrade, roll all existing raw into the
   daily rollup, in bounded batches (by day), before the prune loop is allowed to
   delete anything. Advance the watermark as it progresses so it resumes after a
   restart.
3. Only after backfill reaches steady state does prune-after-rollup begin.
4. The interim safe state (`retention_days` high, nothing deleted) means no data
   is at risk during rollout.

## 6. Alternatives Considered

- **Additive rollups** (increment buckets as events arrive) — rejected: not
  idempotent, double-counts on replay/restart, and conflicts with dedup
  enrichment that can change an event after first insert.
- **Ingest-time retention floor** (the abandoned approach) — rejected: it
  prevents history from ever being captured and is unnecessary once hot ≥
  lookback. It also fought the user's intent to keep history.
- **Keep all raw forever, no rollups** — rejected: unbounded growth and
  ever-slower reads; the original problem.
- **Three tiers (hot raw + warm hourly + cold daily)** — rejected: the hourly
  tier serves no view (anything sub-day is inside the hot window; analytics is
  daily), so it is pure overhead. Two tiers (raw + daily) cover every need.
- **External TSDB / Parquet cold store** — rejected: adds a dependency and ops
  burden for a local single-binary tool.

## 7. Implementation Tasks

### Task 1: Schema + metadata watermark
Add `usage_rollup_daily` and the `rollup_daily_watermark` row to `Init`.

### Task 2: Rollup engine
Incremental, idempotent recompute-and-upsert of daily buckets from the deduped
raw view. Unit tests for idempotency (double-run = same totals), late-event
handling, and rollup-total == raw-total over a window.

### Task 3: Backfill
One-time bounded backfill of existing raw into the daily rollup,
watermark-resumable, gated before any prune.

### Task 4: Prune-after-rollup
Change the retention loop to delete raw only below the daily watermark and the
hot window. Drop the ingest floor.

### Task 5: Tier-aware read model
Window→tier selection (raw for the hot tail, daily rollup for older days) with
boundary union; same `UsageSnapshot` output. Perf trace per tier.

### Task 6: Config
Add `rollup_daily_days`; document `retention_days` as the hot window; keep the
raised ceiling.

### Task 7: Docs + verify
Update `docs/site/docs/daemon/storage.md`; build with `[SUCCESS]`. Verify on a
real DB: backfill correctness (rollup totals == raw totals over a window), size
bounded, reads fast, history preserved across a prune cycle.

### Dependency Graph
Task 1 → Task 2 → {Task 3, Task 4, Task 5} → Task 6 → Task 7.

## 8. Decisions

Resolved (2026-06-11):

1. **Hot window**: **90 days** (≥ collector re-import lookback, so no ingest
   floor; ~100MB of raw).
2. **Trade-off**: beyond the hot window, drop per-event rows; keep **daily**
   aggregates (forever) with full dimension breakdowns. Confirmed.
3. **`retention_days`**: repurposed as the hot window. Confirmed.
4. **Tiers**: two only — raw (hot) + daily (cold). Hourly tier dropped.

Remaining to confirm:

5. **Rollup dimensions**: §5.1 lists `provider, account, model, tool, project,
   language, interface, status`. Confirm this covers every analytics breakdown
   before coding (a missing dimension can't be recovered once raw is pruned).
