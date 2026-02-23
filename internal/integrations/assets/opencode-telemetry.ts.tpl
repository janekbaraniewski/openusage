import type { Plugin } from "@opencode-ai/plugin"
// openusage-integration-version: __OPENUSAGE_INTEGRATION_VERSION__

type RuntimeConfig = {
  enabled: boolean
  openusageBin: string
  accountID?: string
  dbPath?: string
  spoolDir?: string
  spoolOnly: boolean
  verbose: boolean
}

type AnyRecord = Record<string, unknown>

const encoder = new TextEncoder()
const defaultOpenusageBin = "__OPENUSAGE_BIN_DEFAULT__"
let lastIngestErrorSignature = ""

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

function supportsTelemetryHook(binPath: string): boolean {
  try {
    const result = Bun.spawnSync([binPath, "telemetry", "hook", "--help"], {
      stdin: "ignore",
      stdout: "pipe",
      stderr: "pipe",
      env: process.env,
    })
    return result.exitCode === 0
  } catch {
    return false
  }
}

function resolveOpenusageBin(configValue: string | undefined): string {
  const candidates = [
    configValue?.trim(),
    process.env.OPENUSAGE_BIN?.trim(),
    defaultOpenusageBin,
    "openusage",
    "/opt/homebrew/bin/openusage",
    "/usr/local/bin/openusage",
    `${process.env.HOME || ""}/.local/bin/openusage`,
  ]

  const seen = new Set<string>()
  for (const candidate of candidates) {
    if (!candidate || seen.has(candidate)) {
      continue
    }
    seen.add(candidate)
    if (supportsTelemetryHook(candidate)) {
      return candidate
    }
  }

  return pickString(configValue, process.env.OPENUSAGE_BIN, "openusage")
}

function loadConfig(): RuntimeConfig {
  const accountID = process.env.OPENUSAGE_TELEMETRY_ACCOUNT_ID?.trim()
  const configuredBin = (process.env.OPENUSAGE_BIN || "").trim()
  return {
    enabled: parseBool(process.env.OPENUSAGE_TELEMETRY_ENABLED, true),
    openusageBin: resolveOpenusageBin(configuredBin),
    accountID: accountID && accountID !== "" ? accountID : undefined,
    dbPath: process.env.OPENUSAGE_TELEMETRY_DB_PATH?.trim() || undefined,
    spoolDir: process.env.OPENUSAGE_TELEMETRY_SPOOL_DIR?.trim() || undefined,
    spoolOnly: parseBool(process.env.OPENUSAGE_TELEMETRY_SPOOL_ONLY, false),
    verbose: parseBool(process.env.OPENUSAGE_TELEMETRY_VERBOSE, false),
  }
}

function buildArgs(cfg: RuntimeConfig): string[] {
  const args = [cfg.openusageBin, "telemetry", "hook", "opencode"]
  if (cfg.accountID) {
    args.push("--account-id", cfg.accountID)
  }
  if (cfg.dbPath) {
    args.push("--db-path", cfg.dbPath)
  }
  if (cfg.spoolDir) {
    args.push("--spool-dir", cfg.spoolDir)
  }
  if (cfg.spoolOnly) {
    args.push("--spool-only")
  }
  if (cfg.verbose) {
    args.push("--verbose")
  }
  return args
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

async function sendPayload(cfg: RuntimeConfig, payload: unknown): Promise<void> {
  const payloadJSON = safeJSONStringify(payload)
  if (!payloadJSON) {
    if (cfg.verbose) {
      console.error("[openusage-telemetry] payload serialization failed")
    }
    return
  }

  const proc = Bun.spawn(buildArgs(cfg), {
    stdin: "pipe",
    stdout: "ignore",
    stderr: "pipe",
    env: process.env,
  })

  try {
    const writer = proc.stdin.getWriter()
    await writer.write(encoder.encode(payloadJSON))
    await writer.close()
  } catch {
    proc.kill()
    return
  }

  const exitCode = await proc.exited
  if (exitCode === 0) {
    lastIngestErrorSignature = ""
    return
  }

  const stderrText = await new Response(proc.stderr).text()
  const detail = stderrText.trim()
  const signature = `${exitCode}:${detail}`
  if (!cfg.verbose && signature === lastIngestErrorSignature) {
    return
  }
  lastIngestErrorSignature = signature

  if (detail !== "") {
    console.error(`[openusage-telemetry] hook ingestion failed (exit ${exitCode}): ${detail}`)
  } else {
    console.error(`[openusage-telemetry] hook ingestion failed (exit ${exitCode})`)
  }
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
      await enqueue({
        event: input.event,
      })
    },

    async "tool.execute.after"(input, output) {
      const normalized = normalizeToolPayload(input, output)
      await enqueue({
        hook: "tool.execute.after",
        timestamp: Date.now(),
        input: normalized.input,
        output: normalized.output,
      })
    },

    async "chat.message"(input, output) {
      const normalized = normalizeChatPayload(input, output)
      await enqueue({
        hook: "chat.message",
        timestamp: Date.now(),
        input: normalized.input,
        output: normalized.output,
      })
    },
  }
}

export default OpenUsageTelemetry
