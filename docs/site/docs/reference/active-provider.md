---
title: Active provider reference
description: Select the currently active AI provider and expose its quota facts for status bars and scripts.
keywords:
  - openusage active
  - active provider
  - AI status bar
  - provider quota
---

# Active provider

`openusage active` answers two questions for a status bar or script:

1. Which configured provider-account was used most recently?
2. What is its current quota position, in structured facts and a compact label?

The daemon is the authoritative path. It combines telemetry events, the current
read model, and the persisted pin. If the daemon cannot answer within two
seconds, the CLI falls back to the existing local-file activity detector.

## Commands

```bash
openusage active                 # human-readable label
openusage active --json          # selection and facts
openusage active detail --json   # selected provider's metric rows
openusage active list --json     # all selectable provider accounts
openusage active --explain       # decision trace
openusage active pin codex:default
openusage active unpin
```

Use `--socket-path` when the daemon is running on a non-default Unix socket:

```bash
openusage active --socket-path /path/to/openusage.sock --json
```

`active pin` persists the provider-account key in the daemon database. A live
pin wins over telemetry selection. It auto-releases when another provider
records an event after the pin was set, or when the pinned provider is removed
from the configured accounts. Activity from the pinned provider itself does not
release the pin.

## Selection order

The selector has a deliberate tier boundary:

1. A live pin wins outright.
2. If any candidate has a qualifying telemetry event, the candidate with the
   newest event wins.
3. If no candidate has an event, configured candidates are eligible and the
   default provider priority order chooses the winner.

The event tier uses `turn_completed`, `message_usage`, and `tool_usage` events.
Quota polling events are not treated as user activity; otherwise the provider
poller would make the status bar follow the poll interval. A sensible trap,
which is why it has been disarmed.

When the daemon is unavailable, the CLI uses local file/process recency and
sets `source` to `local`. That degraded path cannot identify API-key-only
providers. With no configured or detected candidates, the status is
`no_data`.

## JSON contract

`openusage active --json` emits one `Selection` object:

```json
{
  "selected": "claude_code:default",
  "display": "claude",
  "pinned": false,
  "severity": "good",
  "label": "37% left/reset 2h",
  "facts": {
    "pct_used": 63,
    "pct_remaining": 37,
    "reset_at": "2026-08-15T14:00:00Z"
  },
  "source": "events",
  "status": "ok"
}
```

Top-level fields:

| Field | Meaning |
| --- | --- |
| `selected` | Stable `provider_id:account_id` key. Empty when there is no selection. |
| `display` | Short lowercase name for a bar, such as `claude` or `codex`. |
| `pinned` | `true` when the live pin selected the winner. |
| `severity` | `good`, `warn`, `bad`, or `unknown`. |
| `label` | Ready-to-render quota text. It may be `quota unavailable`. |
| `facts` | Structured quota values. It is omitted when all facts are unknown. |
| `source` | `events`, `local`, or `pinned`; empty when there is no candidate. |
| `status` | `ok` for a selection or `no_data` when there are no candidates. |

`facts` fields are optional and use RFC 3339 timestamps:

| Field | Meaning |
| --- | --- |
| `at_cap` | The selected quota is exhausted. |
| `pct_used` | Percentage of the selected quota used. |
| `pct_remaining` | Percentage of the selected quota remaining. |
| `runout_at` | Forecast time at which the quota will run out. |
| `reset_at` | Time at which the selected quota resets. |
| `runout_before_reset` | Whether the forecast reaches zero before reset. |
| `forecast_source` | Metric that supplied the forecast, currently `quota_runout_hours`. |
| `requests_today` | Requests observed for the current day. |

Consumers should use `facts` for their own rendering and treat `label` as the
compact default wording. Do not parse the label to recover numbers. That way
lies brittle status bars and a very small, very angry Doctor.

## Label grammar

The normal quota forms are:

| Condition | Label form | Example |
| --- | --- | --- |
| Runout and reset known | `<runout>/<reset>` | `2d 0h/6d 21h` |
| At cap and reset known | `cap/<reset>` | `cap/6d 21h` |
| Runout, no reset | `out ~<runout>` | `out ~2d 0h` |
| No runout, quota and reset | `<pct> left/reset <reset>` | `37% left/reset 2h` |
| No runout, quota, no reset | `<pct> left` | `37% left` |
| No quota, request count available | `<n> req today` | `1,204 req today` |

Durations round up to the nearest minute. A day always includes its hours,
including `0h`. When there is no quota or request count at all, the label is
`quota unavailable` and severity is `warn`.

## Troubleshooting

### “My bar is stuck”

Most often, it is not stuck. It is pinned.

```bash
openusage active --json
openusage active --explain
openusage active unpin
```

The explanation will say `pinned: ... wins outright` when that is the cause.

### `source` says `local`

The daemon was unreachable or exceeded the two-second timeout. Check the
daemon and socket first:

```bash
openusage telemetry daemon status
openusage tmux doctor
```

Local detection is intentionally less precise. Start the daemon and install
the relevant integration hooks if you need event-backed selection.

### See the decision trace

Enable debug tracing for a single command:

```bash
OPENUSAGE_DEBUG=1 openusage active --json 2>&1
```

Look for a line beginning with `[active]`; it names the winner, source, total
candidate count, and number of candidates with events.
