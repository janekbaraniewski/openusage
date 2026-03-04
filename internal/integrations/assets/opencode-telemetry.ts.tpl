import type { Plugin } from "@opencode-ai/plugin"
// openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__

import { createConnection as netCreateConnection } from "node:net"
import { existsSync, mkdirSync, writeFileSync, renameSync, readdirSync } from "node:fs"

type RuntimeConfig = {
  enabled: boolean
  accountID?: string
  verbose: boolean
}

type AnyRecord = Record<string, unknown>

function parseBool(value: string | undefined, defaultValue: boolean): boolean {
  if (value === undefined) {
    return defaultValue
  }
  const normalized = value.trim().toLowerCase()
  if (normalized === "" || normalized === "1" || normalized === "true" || normalized === "yes" || normalized === "on") {
    return true
  }
  if (normalized === "0" || normalized === "false" || normalized === "no" || normalized === "off") {
    return false
  }
  return defaultValue
}

function asRecord(value: unknown): AnyRecord | undefined {
  if (value && typeof value === "object") {
    return value as AnyRecord
  }
  return undefined
}

function pickString(...values: unknown[]): string {
  for (const value of values) {
    if (typeof value === "string") {
      const trimmed = value.trim()
      if (trimmed !== "") {
        return trimmed
      }
    }
  }
  return ""
}

function pickInt(...values: unknown[]): number {
  for (const value of values) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return Math.trunc(value)
    }
    if (typeof value === "string") {
      const parsed = Number.parseInt(value, 10)
      if (Number.isFinite(parsed)) {
        return parsed
      }
    }
  }
  return 0
}

function pickPathString(root: unknown, ...paths: string[][]): string {
  for (const path of paths) {
    let current: unknown = root
    let found = true
    for (const key of path) {
      const rec = asRecord(current)
      if (!rec || !(key in rec)) {
        found = false
        break
      }
      current = rec[key]
    }
    if (!found) {
      continue
    }
    const resolved = pickString(current)
    if (resolved !== "") {
      return resolved
    }
  }
  return ""
}

function sanitizeUpstreamProvider(value: string): string {
  const trimmed = value.trim()
  if (trimmed === "") {
    return ""
  }
  const normalized = trimmed.toLowerCase()
  if (normalized === "openrouter" || normalized === "openusage" || normalized === "opencode" || normalized === "unknown") {
    return ""
  }
  return trimmed
}

function normalizeAgentName(value: unknown): string {
  if (typeof value === "string" && value.trim() !== "") {
    return value.trim()
  }
  const rec = asRecord(value)
  if (!rec) {
    return ""
  }
  return pickString(rec.name, rec.id, rec.type)
}

function normalizeModel(value: unknown): { providerID?: string; modelID?: string } {
  const rec = asRecord(value)
  if (!rec) {
    return {}
  }
  const providerID = pickString(rec.providerID, rec.provider_id, rec.provider)
  const modelID = pickString(rec.modelID, rec.model_id, rec.id, rec.model)
  const out: { providerID?: string; modelID?: string } = {}
  if (providerID) {
    out.providerID = providerID
  }
  if (modelID) {
    out.modelID = modelID
  }
  return out
}

function loadConfig(): RuntimeConfig {
  const accountID = process.env.OPENUSAGE_TELEMETRY_ACCOUNT_ID?.trim()
  return {
    enabled: parseBool(process.env.OPENUSAGE_TELEMETRY_ENABLED, true),
    accountID: accountID && accountID !== "" ? accountID : undefined,
    verbose: parseBool(process.env.OPENUSAGE_TELEMETRY_VERBOSE, false),
  }
}

function summarizeParts(parts: unknown): Record<string, number> {
  if (!Array.isArray(parts)) {
    return {}
  }

  const summary: Record<string, number> = {}
  for (const part of parts) {
    const typeValue = (part as { type?: unknown })?.type
    const key = typeof typeValue === "string" && typeValue.trim() !== ""
      ? typeValue.trim()
      : "unknown"
    summary[key] = (summary[key] || 0) + 1
  }
  return summary
}

function normalizeToolPayload(input: unknown, output: unknown): { input: AnyRecord; output: AnyRecord } {
  const inputRec = asRecord(input) || {}
  const outputRec = asRecord(output) || {}
  const outputData = asRecord(outputRec.output) || {}
  const normalizedInput: AnyRecord = {
    tool: pickString(inputRec.tool, inputRec.name, outputRec.tool, outputData.tool),
    sessionID: pickString(inputRec.sessionID, inputRec.sessionId, outputRec.sessionID, outputRec.sessionId),
    callID: pickString(
      inputRec.callID,
      inputRec.callId,
      inputRec.toolCallID,
      inputRec.tool_call_id,
      outputRec.callID,
      outputRec.callId,
      outputData.callID,
      outputData.callId,
    ),
  }
  const normalizedOutput: AnyRecord = {
    title: pickString(outputRec.title, outputData.title, outputRec.name),
  }
  return { input: normalizedInput, output: normalizedOutput }
}

function normalizeChatPayload(input: unknown, output: unknown): { input: AnyRecord; output: AnyRecord } {
  const inputRec = asRecord(input) || {}
  const outputRec = asRecord(output) || {}
  const inputMessage = asRecord(inputRec.message)
  const outputMessage = asRecord(outputRec.message)

  const outputModel = normalizeModel(outputRec.model || outputMessage?.model)
  const inputModel = normalizeModel(inputRec.model || inputMessage?.model)

  const sessionID = pickString(
    inputRec.sessionID,
    inputRec.sessionId,
    inputMessage?.sessionID,
    inputMessage?.sessionId,
    outputMessage?.sessionID,
    outputMessage?.sessionId,
  )
  const messageID = pickString(
    inputRec.messageID,
    inputRec.messageId,
    inputMessage?.id,
    outputMessage?.id,
  )

  const normalizedInput: AnyRecord = {
    sessionID,
    agent: normalizeAgentName(inputRec.agent),
    messageID,
    variant: pickString(inputRec.variant, asRecord(inputRec.agent)?.variant),
    model: {
      providerID: pickString(outputModel.providerID, inputModel.providerID),
      modelID: pickString(outputModel.modelID, inputModel.modelID),
    },
  }

  const outputUsage = asRecord(outputRec.usage) || asRecord(outputMessage?.usage) || {}
  const partsCount = pickInt(outputRec.parts_count, Array.isArray(outputRec.parts) ? outputRec.parts.length : 0)
  const upstreamProvider = sanitizeUpstreamProvider(pickString(
    pickPathString(outputRec,
      ["upstream_provider"],
      ["upstreamProvider"],
      ["route", "provider_name"],
      ["route", "providerName"],
      ["route", "provider"],
      ["routing", "provider_name"],
      ["routing", "providerName"],
      ["routing", "provider"],
      ["router", "provider_name"],
      ["router", "providerName"],
      ["router", "provider"],
      ["endpoint", "provider_name"],
      ["endpoint", "providerName"],
      ["endpoint", "provider"],
      ["provider_name"],
      ["providerName"],
      ["provider"],
    ),
    pickPathString(outputMessage,
      ["upstream_provider"],
      ["upstreamProvider"],
      ["provider_name"],
      ["providerName"],
      ["provider"],
      ["info", "provider_name"],
      ["info", "providerName"],
      ["info", "provider"],
    ),
    pickPathString(outputRec,
      ["model", "provider"],
      ["model", "provider_name"],
      ["model", "providerName"],
    ),
  ))

  const normalizedOutput: AnyRecord = {
    message: {
      id: pickString(outputMessage?.id, messageID),
      sessionID,
      role: pickString(outputMessage?.role, "assistant"),
    },
    model: {
      providerID: pickString(outputModel.providerID, inputModel.providerID),
      modelID: pickString(outputModel.modelID, inputModel.modelID),
    },
    usage: outputUsage,
    context: {
      parts_total: Array.isArray(outputRec.parts) ? outputRec.parts.length : 0,
      parts_by_type: summarizeParts(outputRec.parts),
    },
    parts_count: partsCount,
  }
  if (upstreamProvider !== "") {
    normalizedOutput.upstream_provider = upstreamProvider
  }

  return { input: normalizedInput, output: normalizedOutput }
}

function safeJSONStringify(value: unknown): string | undefined {
  try {
    const seen = new WeakSet<object>()
    return JSON.stringify(value, (_key, current) => {
      if (typeof current === "bigint") {
        return Number(current)
      }
      if (typeof current === "object" && current !== null) {
        if (seen.has(current)) {
          return undefined
        }
        seen.add(current)
      }
      return current
    })
  } catch {
    return undefined
  }
}

function resolveSocketPath(): string {
  const explicit = (process.env.OPENUSAGE_SOCKET || "").trim()
  if (explicit !== "") {
    return explicit
  }
  const stateHome = (process.env.XDG_STATE_HOME || "").trim()
  const base = stateHome !== "" ? stateHome : `${process.env.HOME}/.local/state`
  return `${base}/openusage/telemetry.sock`
}

function resolveHookSpoolDir(): string {
  const explicit = (process.env.OPENUSAGE_HOOK_SPOOL || "").trim()
  if (explicit !== "") {
    return explicit
  }
  const stateHome = (process.env.XDG_STATE_HOME || "").trim()
  const base = stateHome !== "" ? stateHome : `${process.env.HOME}/.local/state`
  return `${base}/openusage/hook-spool`
}

async function postToSocket(socketPath: string, path: string, body: string): Promise<boolean> {
  return new Promise((resolve) => {
    const conn = netCreateConnection({ path: socketPath })
    let resolved = false
    const done = (ok: boolean) => { if (!resolved) { resolved = true; resolve(ok) } }
    conn.setTimeout(2000, () => { conn.destroy(); done(false) })
    conn.on("error", () => done(false))
    conn.on("connect", () => {
      const req = `POST ${path} HTTP/1.0\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: ${Buffer.byteLength(body)}\r\nConnection: close\r\n\r\n${body}`
      conn.write(req, () => { conn.destroy(); done(true) })
    })
  })
}

async function spoolToDisk(source: string, accountID: string, payloadJSON: string, verbose: boolean): Promise<void> {
  const dir = resolveHookSpoolDir()
  try {
    if (!existsSync(dir)) { mkdirSync(dir, { recursive: true }) }
    const files = readdirSync(dir).filter(f => f.endsWith(".json"))
    if (files.length >= 500) {
      if (verbose) { console.error("[openusage-telemetry] hook spool full (500 files)") }
      return
    }
    const ts = Math.floor(Date.now() / 1000)
    const rnd = Math.random().toString(16).slice(2, 10)
    const tmp = `${dir}/${ts}_${rnd}.json.tmp`
    const dst = `${dir}/${ts}_${rnd}.json`
    // Build record via interpolation — avoids JSON.parse round-trip on payload
    const record = `{"source":${JSON.stringify(source)},"account_id":${JSON.stringify(accountID)},"payload":${payloadJSON}}`
    writeFileSync(tmp, record + "\n")
    renameSync(tmp, dst)
  } catch (err) {
    if (verbose) { console.error(`[openusage-telemetry] spool write failed: ${err}`) }
  }
}

async function sendPayload(cfg: RuntimeConfig, payload: unknown): Promise<void> {
  const payloadJSON = safeJSONStringify(payload)
  if (!payloadJSON) {
    if (cfg.verbose) {
      console.error("[openusage-telemetry] payload serialization failed")
    }
    return
  }

  // Primary: POST to daemon unix socket (no process spawn).
  const socketPath = resolveSocketPath()
  let path = "/v1/hook/opencode"
  if (cfg.accountID) {
    path += `?account_id=${encodeURIComponent(cfg.accountID)}`
  }

  try {
    const ok = await postToSocket(socketPath, path, payloadJSON)
    if (ok) return
  } catch {
    // socket failed, fall through to spool
  }

  // Fallback: spool raw payload to disk for daemon pickup.
  await spoolToDisk("opencode", cfg.accountID || "", payloadJSON, cfg.verbose)
}

export const OpenUsageTelemetry: Plugin = async () => {
  const cfg = loadConfig()
  if (!cfg.enabled) {
    return {}
  }

  let queue: Promise<void> = Promise.resolve()
  const enqueue = (payload: unknown): Promise<void> => {
    queue = queue
      .catch(() => undefined)
      .then(() => sendPayload(cfg, payload))
    return queue
  }

  return {
    async event(input) {
      enqueue({
        event: input.event,
      })
    },

    async "tool.execute.after"(input, output) {
      const normalized = normalizeToolPayload(input, output)
      enqueue({
        hook: "tool.execute.after",
        timestamp: Date.now(),
        input: normalized.input,
        output: normalized.output,
      })
    },

    async "chat.message"(input, output) {
      const normalized = normalizeChatPayload(input, output)
      enqueue({
        hook: "chat.message",
        timestamp: Date.now(),
        input: normalized.input,
        output: normalized.output,
      })
    },
  }
}

export default OpenUsageTelemetry
