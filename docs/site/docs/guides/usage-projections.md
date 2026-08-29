---
title: Usage gauge projections
description: How OpenUsage computes the "resets in X · projected 100% in Y" annotation on windowed usage gauges, and when to trust it.
---

Usage gauges for windowed metrics (Claude Code 5h blocks, daily / weekly / monthly plan caps, etc.) carry a small dim annotation under the bar. It estimates when you will hit the cap at your current pace, alongside the time remaining in the window. This page documents the math, the eligibility rules, and the limits of the heuristic.

## What the annotation looks like

The detail panel is verbose; the dashboard tile is compressed. The projection half has two wordings depending on whether the pace would actually reach 100% before the window resets:

| Surface | Pace fits in window | Pace overshoots window |
|---|---|---|
| Detail view | `resets in 1h 23m · projected 100% in 42m` | `resets in 3h 42m · projected ~85% by reset` |
| Tile view | `resets 1h 23m · 100% in 42m` | `resets 3h 42m · ~85% by reset` |

Either half can render independently:

- Only `resets in 1h 23m` when the window has a known reset timestamp but no usable pace yet (zero usage so far, or the window just opened).
- Only `projected 100% in 42m` (or `projected ~85% by reset`) when pace is known but the metric has no `Resets[]` entry (rare; you only get this for tools that report a reset clock).

The annotation always renders in the dim style; it never blocks the headline percentage.

## How the projection is computed

The pace is a flat linear extrapolation of the current window:

```
elapsed         = window_duration − time_until_reset
pace            = (used% / 100) / elapsed_minutes    # fraction of window per minute
remaining%      = 100 − used%
minutes_to_100  = remaining% / (pace × 100)
```

OpenUsage then picks one of two wordings:

**Branch A — pace fits in the window** (`minutes_to_100 ≤ time_until_reset`).
The annotation reads `projected 100% in <duration>` because the window will actually reset *after* the linear projection hits the cap. This is the case the original heuristic always assumed.

**Branch B — pace overshoots the window** (`minutes_to_100 > time_until_reset`).
A naive "100% in 4h 35m" claim when the window resets in 3h 42m is misleading: you will never see those last 53 minutes — the window will roll over and `used%` will drop back to 0. Instead, OpenUsage shows the linear projection's value *at reset*:

```
projected_pct_at_reset = used% + pace × 100 × minutes_until_reset
N = round(projected_pct_at_reset)            # clamped to [0, 99]
```

The cap is `99`, not `100`. If the linear extrapolation rounds up to `100`, the wording would contradict the branch (we picked this wording precisely because we expect *not* to hit 100); printing `~99% by reset` is honest about that.

**Worked example.** A 5-hour window starts at 16:00 and resets at 21:00. At 17:18 (1h 18m elapsed, 3h 42m to reset) you have used 22%.

- `pace = 0.22 / 78 ≈ 0.00282` per minute.
- `minutes_to_100 = 78 / 0.282 ≈ 277` minutes ≈ 4h 37m.
- `time_until_reset = 222` minutes (3h 42m). The pace overshoots → Branch B.
- `projected_pct_at_reset = 22 + 0.282 × 222 ≈ 85`.
- Annotation prints `resets 3h 42m · ~85% by reset`.

The reset half is just `resetAt − now`, formatted compactly (`1h 23m`, `42m`, `45s`).

The same formula drives both the detail-view and the tile-view annotation; the wording differs but the numbers do not.

## Which metrics get a projection

Projection requires three things:

1. **A recognized window string** on the metric. Currently: `5h`, `1d`, `24h`, `today`, `7d`, `30d`. Other windows (e.g. `1m`, `1h`, `all-time`) render the gauge with no annotation.
2. **A reset timestamp** in `snap.Resets[key]`. Without it, OpenUsage cannot compute elapsed time and skips the annotation entirely.
3. **A gauge-eligible metric** — one with a meaningful `used%` (i.e. it already renders as a usage bar).

In practice, the annotation shows up on:

- Claude Code 5-hour billing blocks.
- Codex CLI primary and secondary rate-limit windows. The live usage endpoint reports `window_minutes` and a `resets_at` timestamp; primary windows are `300m → 5h` (recognized) and secondary windows are `10080m → 7d` (recognized). Both render with a projection. Other plans may emit non-standard `window_minutes` values (e.g. `60`, `1440`); those format as `1h` or `1d`, of which only `1d` is recognized — see the window-string allowlist above.
- Z.AI coding-plan 5h and 24h usage gauges.
- Cursor monthly / 14-day plan caps.
- Ollama request quotas where the upstream reports a `5h` window.
- OpenRouter and Perplexity windowed plan caps where reset times are exposed.
- Alibaba Cloud daily and monthly quotas.

If a provider doesn't supply a reset timestamp or uses an unrecognized window, the gauge still works — you just won't see the projection.

### Provider-neutral export metrics

Snapshot normalization exposes the same projection data to scripts and
integrations when a provider supplies the required inputs:

- `quota_burn_rate` — average current-period consumption per hour.
- `quota_runout_hours` — hours until the quota would reach its limit at that pace.
- `quota_forecast_metric`, `quota_forecast_period_start`, and
  `quota_forecast_reset_at` — source and timing metadata.

The values are estimates, not vendor promises. Integrations should show the
reset countdown alongside the run-out estimate and say `not before reset` when
the estimate exceeds the remaining quota period.

## When the projection is suppressed

The pace half is omitted (only `resets in …` renders, or nothing at all) when:

| Condition | Reason |
|---|---|
| `used% == 0` | No data to extrapolate from. |
| `used% >= 100` | Already at the cap — a forecast is meaningless. |
| `elapsed <= 0` | Window has not started yet, or the reset clock is in the future by more than the window. |
| `pace` is NaN, ±Inf, or `<= 0` | Numerically degenerate; refuse rather than print a misleading number. |
| Computed `minutes_to_100 <= 0` | Same — degenerate. Render the reset half alone. |

These rules apply to both the tile and the detail annotation.

## Limitations: it is a heuristic, not a forecast

The pace is the **average** rate over the whole elapsed window. It assumes you will keep spending at exactly that rate until reset. In practice:

- A burst of activity in the first 10 minutes of a 5-hour block makes the projection look catastrophic. Wait a few minutes of idle time and the prediction relaxes.
- A long idle stretch followed by a spike makes the projection look reassuring right up until you hit the wall.
- Approaching the end of a window, even a fraction of a percent per minute can dominate the projection because `remaining%` is small.
- Branch B (`~N% by reset`) is just as linear as Branch A. It assumes the next `time_until_reset` minutes look exactly like the elapsed slice. A quiet stretch ahead of you will land below `N`; a burst will overshoot it. The number is a sanity check, not a guarantee.

Treat the annotation as a *"if today keeps looking like today"* signal — useful for catching pace problems early, not a substitute for hard rate-limit math. If you need a non-linear forecast (recent-weighted, EWMA, etc.) the analytics screen is the place to build that yourself; the gauge will not do it for you.

## Interaction with `hide_costs`

The projection is a **usage** annotation, not a cost annotation. It is computed from percentages and never references dollars. That means:

- `hide_costs = true` (per-account or global) hides cost columns and the dollar projections in the cost section, but it **does not** hide gauge projections.
- The `c` keystroke that cycles cost visibility (auto → hide → show → auto) has no effect on this annotation.

If you want the gauge projection without the dollar noise on a subscription plan, that's already how the default plan-aware `hide_costs` policy behaves: costs hidden, usage projection visible. See [`dashboard.hide_costs`](../reference/configuration.md#dashboardhide_costs) for the precedence rules.

## Related

- [Claude Code provider](../providers/claude-code.md) — 5-hour billing blocks, the highest-traffic user of this annotation.
- [Time windows](../concepts/time-windows.md) — separate concept; `w` changes the **aggregation** window for totals, not the **gauge** window which is fixed per-metric by the provider.
- [Cost attribution](cost-attribution.md) — for the dollar side of "where is the burn coming from?"
