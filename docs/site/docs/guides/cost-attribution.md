---
title: Cost attribution
description: Practical recipes for figuring out which tool, model, or project is burning your AI budget.
---

"Where did the spend go?" is the question OpenUsage is built around. This guide collects the recipes that actually work, ordered roughly from cheapest to richest.

## Recipe 1: glance at the dashboard

For a quick gut check, the tile grid sorts itself by status (worst first by default) and shows each provider's total spend or remaining quota at a glance. Cycle the time window with `w` to see today vs the last week vs the last month.

This is enough when:

- One provider is obviously dominant.
- You only need to confirm that nothing is in `LIMIT` or `WARN`.

When the answer is "spend is up but I don't know why", move on.

## Recipe 2: per-provider detail view

Press Enter on any tile to open the detail panel. It splits per-provider data into sections (use `[` / `]` to flip tabs):

- **Plan / Credits** — current balance, included quota, hard limits.
- **Models** — per-model breakdown of input/output/cache tokens and cost.
- **Sessions / Turns** — for agents, recent activity rows.
- **Rate limits** — rpm / tpm / rpd / tpd windows.

The Models tab is the workhorse for the question "which model is responsible?" Sort by cost (`s` in Analytics; the detail tables already sort by it) and the answer is usually obvious.

Press `Ctrl+O` from any provider tile to expand the model breakdown inline without leaving the dashboard.

## Recipe 3: Analytics screen

Tab over to Analytics for a cross-provider view:

- Per-day spend bars — useful for spotting spikes.
- Per-provider totals in the active window.
- Sub-tabs for **Models**, **Tools**, **Projects** (where data is available).

Sort with `s`, filter with `/`. The tabs only populate from providers that ship the relevant detail (mostly `claude_code`, `cursor`, `opencode`, `openrouter`, `zai`).

## Recipe 4: install agent integrations

Polling sees totals; it does not see individual messages. To get **per-turn**, **per-tool**, and **per-project** breakdowns you need to install the matching integration hook:

```bash
openusage telemetry daemon install        # one-time
openusage integrations install claude_code
openusage integrations install codex
openusage integrations install opencode
```

Each hook ships per-turn events to the daemon as they happen. Once installed:

- Claude Code: per-conversation cost rolls up into 5-hour billing blocks; burn-rate is visible on the detail panel.
- Codex: per-session token totals match the actual conversation timeline, not the 30s poll cadence.
- OpenCode: per-project breakdown becomes available in the Analytics screen.

This is the single biggest data-quality upgrade for cost attribution.

## Recipe 5: combine OpenCode with OpenRouter

If you use OpenCode as the agent and OpenRouter as the API gateway, you get the richest breakdown of any combination:

- **OpenCode telemetry plugin** records per-project, per-tool, per-turn metadata.
- **OpenRouter** records the underlying model, hosting provider, and exact cost per generation.

The two streams are deduped on `message_id`. Open the OpenCode detail panel and you'll see project rows; cross-reference against the OpenRouter detail panel for model and cost.

## Recipe 6: per-account breakdown

Configure one account per scope and the dashboard does the work for you:

- One key per project (each gets its own `api_key_env`, its own row).
- One key per environment (`-personal`, `-work`).
- One key per side project.

See [multi-account](multi-account.md). Provider APIs report by key, so this gets you per-key attribution without running anything custom.

## Recipe 7: Claude Code billing blocks

`claude_code` computes 5-hour rolling billing blocks (the same concept as Anthropic's subscription quotas) using local stats files. The detail panel shows:

- Current block start and time remaining.
- Cumulative tokens and cost in this block.
- Burn rate (tokens/min, cost/hr) extrapolating to block end.

If your monthly bill has a spike, find the block where it happened, then look at the Models breakdown for that period.

:::note
Claude Code costs are **API-equivalent estimates** computed from local pricing tables. They are not subscription charges. Useful for relative attribution and trend tracking; not exact for invoice reconciliation.
:::

## Recipe 8: long-running daemon + 30-day windows

For "where did spend go this month?" you need 30 days of history. The daemon's default `data.retention_days` is 30; if you want longer:

```json
{
  "data": { "retention_days": 90 }
}
```

Set it before the data ages out. Then use `w` to cycle to `30d` (or `all`) and the per-day chart in Analytics covers the full period.

## Anti-patterns

- **Trusting raw 1d totals against a fresh daemon install**, when the daemon has only been running for a few hours. The window can never reach further back than the data the daemon has actually stored.
- **Comparing Claude Code dollars to your Anthropic invoice**, when you're on a subscription plan. Use Claude Code numbers for relative attribution, not invoice math.
- **Counting OpenRouter cost twice** by adding it to the per-tool numbers from OpenCode. They're the same dollars, dedup'd on the daemon side.

## See also

- [Telemetry pipeline](../concepts/telemetry.md) — how events get deduped.
- [Time windows](../concepts/time-windows.md) — the semantics of `1d` vs `7d`.
- [Multi-account](multi-account.md)
- [Daemon overview](/daemon) — install hooks and integrations.
