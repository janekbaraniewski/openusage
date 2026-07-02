---
title: Providers
description: Catalog of every AI tool and API platform OpenUsage tracks.
sidebar_label: Providers
---

# Providers

OpenUsage supports 35 providers spanning local coding agents and cloud API platforms. Most are auto-detected on first run; the rest need a single environment variable. Each tile on the dashboard maps to one provider page below.

## Coding agents

These providers read local files, OAuth credentials, or shell out to a CLI. No API key is required for most of them.

<div className="provider-grid">
  <a href="./claude-code/">
    <strong>Claude Code</strong>
    <span>Sessions, billing blocks, burn rate, per-model tokens</span>
  </a>
  <a href="./cursor/">
    <strong>Cursor IDE</strong>
    <span>Plan spend, billing cycle, composer sessions, AI code score</span>
  </a>
  <a href="./copilot/">
    <strong>GitHub Copilot</strong>
    <span>Chat/code/premium quotas, org seats, rate limits</span>
  </a>
  <a href="./codex/">
    <strong>Codex CLI</strong>
    <span>Sessions, rate-limit windows, credit balance, plan</span>
  </a>
  <a href="./gemini-cli/">
    <strong>Gemini CLI</strong>
    <span>OAuth status, session tokens, MCP config, user quota</span>
  </a>
  <a href="./opencode/">
    <strong>OpenCode</strong>
    <span>Zen models, spend via telemetry plugin</span>
  </a>
  <a href="./amp/">
    <strong>Amp</strong>
    <span>Threads, ledger-reconciled credits, per-model tokens</span>
  </a>
  <a href="./codebuff/">
    <strong>Codebuff</strong>
    <span>Multi-channel chat history, credits, three-tier usage extraction</span>
  </a>
  <a href="./crush/">
    <strong>Crush</strong>
    <span>Per-project SQLite walker, sessions and tokens</span>
  </a>
  <a href="./droid/">
    <strong>Droid (Factory)</strong>
    <span>Session activity from Factory's settings dir</span>
  </a>
  <a href="./goose/">
    <strong>Goose</strong>
    <span>Block's Goose agent, SQLite-backed session reader</span>
  </a>
  <a href="./hermes/">
    <strong>Hermes</strong>
    <span>Nous Hermes agent, per-profile SQLite state</span>
  </a>
  <a href="./kilocode/">
    <strong>Kilo Code</strong>
    <span>VS Code extension tasks, OSS coding agent</span>
  </a>
  <a href="./kimi-cli/">
    <strong>Kimi CLI</strong>
    <span>Local wire.jsonl session reader (distinct from the Moonshot API tile)</span>
  </a>
  <a href="./kiro/">
    <strong>Kiro</strong>
    <span>CLI sessions + SQLite, hybrid local reader</span>
  </a>
  <a href="./mux/">
    <strong>Mux</strong>
    <span>Per-workspace session-usage.json, sessions and per-model tokens</span>
  </a>
  <a href="./openclaw/">
    <strong>OpenClaw</strong>
    <span>Transcripts plus legacy clawdbot / moltbot / moldbot paths</span>
  </a>
  <a href="./pi/">
    <strong>Pi</strong>
    <span>Pi and Oh My Pi local agent sessions</span>
  </a>
  <a href="./qwen-cli/">
    <strong>Qwen CLI</strong>
    <span>Per-project chat JSONL, usageMetadata token shape</span>
  </a>
  <a href="./roocode/">
    <strong>Roo Code</strong>
    <span>VS Code extension event parser, per-task usage</span>
  </a>
  <a href="./zed/">
    <strong>Zed Agent</strong>
    <span>SQLite thread reader (hosted Zed models only)</span>
  </a>
</div>

## Local runtimes

Self-hosted model servers running on this machine.

<div className="provider-grid">
  <a href="./ollama/">
    <strong>Ollama</strong>
    <span>Local models, VRAM, request log analytics, cloud credits</span>
  </a>
</div>

## API platforms

These providers require an API key in an environment variable. Some return only rate-limit headers, others return full billing and usage data.

<div className="provider-grid">
  <a href="./openai/">
    <strong>OpenAI</strong>
    <span>RPM/TPM rate limits</span>
  </a>
  <a href="./anthropic/">
    <strong>Anthropic</strong>
    <span>RPM/TPM rate limits</span>
  </a>
  <a href="./azure-openai/">
    <strong>Azure OpenAI</strong>
    <span>RPM/TPM rate limits via Azure resource endpoint</span>
  </a>
  <a href="./openrouter/">
    <strong>OpenRouter</strong>
    <span>Credits, daily/weekly/monthly usage, generation analytics, BYOK</span>
  </a>
  <a href="./groq/">
    <strong>Groq</strong>
    <span>RPM/TPM/RPD/TPD rate limits</span>
  </a>
  <a href="./mistral/">
    <strong>Mistral AI</strong>
    <span>Monthly budget, credit balance, spend, tokens (EUR)</span>
  </a>
  <a href="./deepseek/">
    <strong>DeepSeek</strong>
    <span>Balance breakdown, rate limits (CNY)</span>
  </a>
  <a href="./moonshot/">
    <strong>Moonshot</strong>
    <span>Balance breakdown, quotas, peak usage (USD or CNY)</span>
  </a>
  <a href="./perplexity/">
    <strong>Perplexity</strong>
    <span>Pro / Max plan quotas via browser-session auth</span>
  </a>
  <a href="./xai/">
    <strong>xAI (Grok)</strong>
    <span>Credits, rate limits, allowed models (USD)</span>
  </a>
  <a href="./zai/">
    <strong>Z.AI</strong>
    <span>5h window, monthly usage, credit grants, tool usage</span>
  </a>
  <a href="./gemini-api/">
    <strong>Gemini API</strong>
    <span>Model catalog, per-model token limits</span>
  </a>
  <a href="./alibaba-cloud/">
    <strong>Alibaba Cloud Model Studios</strong>
    <span>Billing period, balance, spend, per-model quotas (USD)</span>
  </a>
</div>
