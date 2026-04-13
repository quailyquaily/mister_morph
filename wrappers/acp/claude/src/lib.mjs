import { spawn } from "node:child_process";
import crypto from "node:crypto";
import process from "node:process";
import readline from "node:readline";

export const WRAPPER_VERSION = "0.1.0";
export const ACP_PROTOCOL_VERSION = 1;
export const SUPPORTED_CONFIG_OPTIONS = [
  "model",
  "permission_mode",
  "allowed_tools",
  "append_system_prompt",
  "max_turns",
  "bare"
];

const JSONRPC_VERSION = "2.0";
const RPC_METHOD_NOT_FOUND = -32601;
const RPC_INVALID_PARAMS = -32602;
const RPC_INTERNAL_ERROR = -32603;
const DEFAULT_PERMISSION_MODE = "dontAsk";
const MAX_STDERR_BYTES = 64 * 1024;
const ACP_METHOD_INITIALIZE = "initialize";
const ACP_METHOD_AUTHENTICATE = "authenticate";
const ACP_METHOD_SESSION_NEW = "session/new";
const ACP_METHOD_SESSION_SET_CONFIG = "session/set_config_option";
const ACP_METHOD_SESSION_PROMPT = "session/prompt";
const ACP_METHOD_SESSION_CANCEL = "session/cancel";
const ACP_METHOD_SESSION_UPDATE = "session/update";

export function normalizeSessionOptions(raw = {}) {
  const source = isRecord(raw) ? raw : {};
  return {
    model: pickString(source, "model"),
    permissionMode:
      pickString(source, "permission_mode", "permissionMode") ??
      DEFAULT_PERMISSION_MODE,
    allowedTools: normalizeToolList(
      pickValue(source, "allowed_tools", "allowedTools")
    ),
    appendSystemPrompt: pickString(
      source,
      "append_system_prompt",
      "appendSystemPrompt"
    ),
    maxTurns: pickPositiveInt(source, "max_turns", "maxTurns"),
    bare: pickBoolean(source, "bare", false)
  };
}

export function collectACPText(content) {
  if (!Array.isArray(content)) {
    return "";
  }
  const parts = [];
  for (const item of content) {
    if (!isRecord(item)) {
      continue;
    }
    if (item.type !== "text" || typeof item.text !== "string") {
      continue;
    }
    const text = item.text;
    if (text !== "") {
      parts.push(text);
    }
  }
  return parts.join("\n");
}

export function buildBackendArgs() {
  return normalizeString(process.env.MISTERMORPH_CLAUDE_ARGS)
    .split(/\s+/)
    .filter(Boolean);
}

export function buildClaudePromptFlags(prompt, options = {}) {
  const args = [];
  if (options.bare) {
    args.push("--bare");
  }
  args.push(
    "-p",
    prompt,
    "--output-format",
    "stream-json",
    "--verbose",
    "--include-partial-messages",
    "--no-session-persistence"
  );
  if (options.permissionMode) {
    args.push("--permission-mode", options.permissionMode);
  }
  if (Array.isArray(options.allowedTools) && options.allowedTools.length > 0) {
    args.push("--allowedTools", ...options.allowedTools);
  }
  if (options.model) {
    args.push("--model", options.model);
  }
  if (options.appendSystemPrompt) {
    args.push("--append-system-prompt", options.appendSystemPrompt);
  }
  if (Number.isInteger(options.maxTurns) && options.maxTurns > 0) {
    args.push("--max-turns", String(options.maxTurns));
  }
  return args;
}

export function buildClaudeArgs(prompt, options = {}) {
  return [...buildBackendArgs(), ...buildClaudePromptFlags(prompt, options)];
}

export function createPromptState() {
  return { emittedText: "" };
}

export function processClaudeEvent(rawEvent, state) {
  const event = isRecord(rawEvent) ? rawEvent : {};
  const updates = [];

  if (normalizeString(event.type) === "stream_event") {
    const delta = extractStreamTextDelta(event.event);
    if (delta !== "") {
      state.emittedText += delta;
      updates.push(agentMessageChunk(delta));
    }
    return { updates };
  }

  if (normalizeString(event.type) === "assistant") {
    const assistantText = extractAssistantText(event.message);
    const delta = computeTextDelta(state.emittedText, assistantText);
    if (delta !== "") {
      state.emittedText += delta;
      updates.push(agentMessageChunk(delta));
    }
    return { updates };
  }

  if (normalizeString(event.type) === "result") {
    const resultText = stringOrEmpty(event.result);
    const delta = computeTextDelta(state.emittedText, resultText);
    if (delta !== "") {
      state.emittedText += delta;
      updates.push(agentMessageChunk(delta));
    }
    if (event.is_error === true) {
      return {
        updates,
        error: new Error(resultText || "claude print mode failed")
      };
    }
    return {
      updates,
      final: {
        stopReason: mapClaudeStopReason(event.stop_reason, event.subtype)
      }
    };
  }

  return { updates };
}

export class ClaudePromptRun {
  constructor(options = {}) {
    this.command =
      normalizeString(options.command) ||
      normalizeString(process.env.MISTERMORPH_CLAUDE_COMMAND) ||
      "claude";
    this.baseArgs = Array.isArray(options.args) ? options.args : buildBackendArgs();
    this.cwd = normalizeString(options.cwd) || process.cwd();
    this.env = { ...process.env, ...(isRecord(options.env) ? options.env : {}) };
    this.onUpdate =
      typeof options.onUpdate === "function" ? options.onUpdate : () => {};
    this.proc = null;
    this.cancelRequested = false;
  }

  async run(prompt, sessionOptions) {
    const args = [...this.baseArgs, ...buildClaudePromptFlags(prompt, sessionOptions)];
    const state = createPromptState();
    return new Promise((resolve, reject) => {
      const proc = spawn(this.command, args, {
        cwd: this.cwd,
        env: this.env,
        stdio: ["ignore", "pipe", "pipe"]
      });
      this.proc = proc;
      const stdout = readline.createInterface({ input: proc.stdout });
      const stderrState = createCappedStderrState();
      let settled = false;

      const finish = (fn, value) => {
        if (settled) {
          return;
        }
        settled = true;
        stdout.close();
        fn(value);
      };

      proc.stderr.on("data", (chunk) => {
        appendStderrChunk(stderrState, chunk);
      });

      stdout.on("line", (line) => {
        if (settled || line.trim() === "") {
          return;
        }
        let event;
        try {
          event = JSON.parse(line);
        } catch (error) {
          this.cancel();
          finish(reject, new Error(`invalid claude stream-json line: ${String(error)}`));
          return;
        }

        const outcome = processClaudeEvent(event, state);
        for (const update of outcome.updates) {
          this.onUpdate(update);
        }
        if (outcome.error) {
          finish(reject, outcome.error);
          return;
        }
        if (outcome.final) {
          finish(resolve, outcome.final);
        }
      });

      proc.on("close", (code, signal) => {
        if (settled) {
          return;
        }
        if (this.cancelRequested) {
          finish(resolve, { stopReason: "cancelled" });
          return;
        }
        const detail = normalizeString(stderrDetail(stderrState));
        const suffix = signal ? `signal ${signal}` : `code ${code ?? "unknown"}`;
        finish(
          reject,
          new Error(detail || `claude print mode exited with ${suffix}`)
        );
      });
    });
  }

  cancel() {
    this.cancelRequested = true;
    if (this.proc && !this.proc.killed) {
      this.proc.kill("SIGTERM");
    }
  }
}

export class ClaudeACPServer {
  constructor(options = {}) {
    this.stdin = options.stdin ?? process.stdin;
    this.stdout = options.stdout ?? process.stdout;
    this.readline = readline.createInterface({ input: this.stdin });
    this.command =
      normalizeString(options.command) ||
      normalizeString(process.env.MISTERMORPH_CLAUDE_COMMAND) ||
      "claude";
    this.env = isRecord(options.env) ? options.env : {};
    this.sessions = new Map();
  }

  start() {
    this.readline.on("line", (line) => {
      void this.#handleACPLine(line);
    });
  }

  async #handleACPLine(line) {
    if (line.trim() === "") {
      return;
    }
    let message;
    try {
      message = JSON.parse(line);
    } catch (error) {
      this.#writeError(null, RPC_INVALID_PARAMS, `invalid json: ${String(error)}`);
      return;
    }
    if (!isRecord(message)) {
      this.#writeError(null, RPC_INVALID_PARAMS, "invalid request");
      return;
    }
    const method = normalizeString(message.method);
    if (method === "") {
      this.#writeError(message.id ?? null, RPC_INVALID_PARAMS, "missing method");
      return;
    }

    try {
      const result = await this.#handleACPRequest(method, message.params);
      this.#write({
        jsonrpc: JSONRPC_VERSION,
        id: message.id ?? null,
        result
      });
    } catch (error) {
      const code = error instanceof JsonRpcFailure ? error.code : RPC_INTERNAL_ERROR;
      const messageText = error instanceof Error ? error.message : String(error);
      this.#writeError(message.id ?? null, code, messageText);
    }
  }

  async #handleACPRequest(method, params) {
    switch (method) {
      case ACP_METHOD_INITIALIZE:
        return {
          protocolVersion: ACP_PROTOCOL_VERSION,
          authMethods: []
        };
      case ACP_METHOD_AUTHENTICATE:
        return {};
      case ACP_METHOD_SESSION_NEW:
        return this.#createSession(params);
      case ACP_METHOD_SESSION_SET_CONFIG:
        return this.#setSessionConfig(params);
      case ACP_METHOD_SESSION_PROMPT:
        return this.#runPrompt(params);
      case ACP_METHOD_SESSION_CANCEL:
        return this.#cancelPrompt(params);
      default:
        throw new JsonRpcFailure(
          RPC_METHOD_NOT_FOUND,
          `unsupported ACP method: ${method}`
        );
    }
  }

  #createSession(params) {
    const payload = asObject(params, "session/new params");
    const cwd = normalizeString(payload.cwd) || process.cwd();
    const options = normalizeSessionOptions(
      isRecord(payload._meta) ? payload._meta : {}
    );
    const sessionId = crypto.randomUUID();
    this.sessions.set(sessionId, {
      sessionId,
      cwd,
      options,
      pendingRun: null
    });
    return {
      sessionId,
      configOptions: SUPPORTED_CONFIG_OPTIONS.map((id) => ({ id }))
    };
  }

  #setSessionConfig(params) {
    const payload = asObject(params, "session/set_config_option params");
    const session = this.#getSession(payload.sessionId);
    const configId = normalizeString(payload.configId);
    if (!SUPPORTED_CONFIG_OPTIONS.includes(configId)) {
      return {};
    }
    session.options = applyConfigOption(session.options, configId, payload.value);
    return {};
  }

  async #runPrompt(params) {
    const payload = asObject(params, "session/prompt params");
    const session = this.#getSession(payload.sessionId);
    if (session.pendingRun) {
      throw new JsonRpcFailure(
        RPC_INVALID_PARAMS,
        `session ${session.sessionId} already has an active prompt`
      );
    }
    const prompt = collectACPText(payload.prompt);
    if (prompt.trim() === "") {
      throw new JsonRpcFailure(
        RPC_INVALID_PARAMS,
        "session/prompt requires text content"
      );
    }
    const run = new ClaudePromptRun({
      command: this.command,
      cwd: session.cwd,
      env: this.env,
      onUpdate: (update) => {
        this.#notifySessionUpdate(session.sessionId, update);
      }
    });
    session.pendingRun = run;
    try {
      return await run.run(prompt, session.options);
    } finally {
      if (session.pendingRun === run) {
        session.pendingRun = null;
      }
    }
  }

  #cancelPrompt(params) {
    const payload = asObject(params, "session/cancel params");
    const session = this.#getSession(payload.sessionId);
    if (session.pendingRun) {
      session.pendingRun.cancel();
    }
    return {};
  }

  #notifySessionUpdate(sessionId, update) {
    this.#write({
      jsonrpc: JSONRPC_VERSION,
      method: ACP_METHOD_SESSION_UPDATE,
      params: {
        sessionId,
        update
      }
    });
  }

  #getSession(sessionId) {
    const key = normalizeString(sessionId);
    const session = this.sessions.get(key);
    if (!session) {
      throw new JsonRpcFailure(
        RPC_INVALID_PARAMS,
        `unknown sessionId: ${sessionId}`
      );
    }
    return session;
  }

  #write(message) {
    this.stdout.write(`${JSON.stringify(message)}\n`);
  }

  #writeError(id, code, message) {
    this.#write({
      jsonrpc: JSONRPC_VERSION,
      id,
      error: {
        code,
        message
      }
    });
  }
}

export function main(argv = process.argv.slice(2)) {
  if (argv.includes("--help") || argv.includes("-h")) {
    printHelp();
    return;
  }
  const server = new ClaudeACPServer();
  server.start();
}

export function printHelp(stream = process.stdout) {
  stream.write(
    [
      "MisterMorph ACP Claude Wrapper",
      "",
      "Starts an ACP agent over stdio and bridges it to Claude Code print mode.",
      "",
      "Usage:",
      "  node ./wrappers/acp/claude/src/index.mjs",
      "",
      "Environment:",
      "  MISTERMORPH_CLAUDE_COMMAND  backend executable, default: claude",
      "  MISTERMORPH_CLAUDE_ARGS     extra backend args inserted before print-mode flags",
      ""
    ].join("\n")
  );
}

function agentMessageChunk(text) {
  return {
    sessionUpdate: "agent_message_chunk",
    content: [{ type: "text", text }]
  };
}

function applyConfigOption(options, configId, value) {
  const next = { ...options };
  switch (configId) {
    case "model":
      next.model = typeof value === "string" ? value.trim() || null : null;
      break;
    case "permission_mode":
      next.permissionMode =
        typeof value === "string"
          ? value.trim() || DEFAULT_PERMISSION_MODE
          : DEFAULT_PERMISSION_MODE;
      break;
    case "allowed_tools":
      next.allowedTools = normalizeToolList(value);
      break;
    case "append_system_prompt":
      next.appendSystemPrompt =
        typeof value === "string" ? value.trim() || null : null;
      break;
    case "max_turns":
      next.maxTurns = normalizePositiveInt(value);
      break;
    case "bare":
      next.bare = normalizeBoolean(value, false);
      break;
    default:
      break;
  }
  return next;
}

function asObject(value, label) {
  if (!isRecord(value)) {
    throw new JsonRpcFailure(RPC_INVALID_PARAMS, `invalid ${label}`);
  }
  return value;
}

function computeTextDelta(previousText, nextText) {
  const prev = typeof previousText === "string" ? previousText : "";
  const next = typeof nextText === "string" ? nextText : "";
  if (next === "" || next === prev) {
    return "";
  }
  if (next.startsWith(prev)) {
    return next.slice(prev.length);
  }
  if (prev.startsWith(next)) {
    return "";
  }
  return next;
}

function extractAssistantText(message) {
  if (!isRecord(message) || !Array.isArray(message.content)) {
    return "";
  }
  const parts = [];
  for (const item of message.content) {
    if (!isRecord(item)) {
      continue;
    }
    if (item.type !== "text" || typeof item.text !== "string") {
      continue;
    }
    const text = item.text;
    if (text !== "") {
      parts.push(text);
    }
  }
  return parts.join("\n");
}

function extractStreamTextDelta(event) {
  if (!isRecord(event)) {
    return "";
  }
  const delta = isRecord(event.delta) ? event.delta : null;
  if (delta && normalizeString(delta.type).toLowerCase() === "text_delta") {
    return stringOrEmpty(delta.text);
  }
  return "";
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

class JsonRpcFailure extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

function mapClaudeStopReason(stopReason, subtype) {
  const reason = normalizeString(stopReason).toLowerCase();
  if (reason === "" && normalizeString(subtype).toLowerCase() === "success") {
    return "end_turn";
  }
  switch (reason) {
    case "":
    case "stop_sequence":
    case "end_turn":
      return "end_turn";
    case "max_turns":
      return "max_turns";
    default:
      return reason;
  }
}

function normalizeBoolean(value, fallback) {
  if (typeof value === "boolean") {
    return value;
  }
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase();
    if (normalized === "true") {
      return true;
    }
    if (normalized === "false") {
      return false;
    }
  }
  return fallback;
}

function normalizePositiveInt(value) {
  if (typeof value === "number" && Number.isInteger(value) && value > 0) {
    return value;
  }
  if (typeof value === "string") {
    const parsed = Number.parseInt(value.trim(), 10);
    if (Number.isInteger(parsed) && parsed > 0) {
      return parsed;
    }
  }
  return null;
}

function normalizeString(value) {
  return typeof value === "string" ? value.trim() : "";
}

function stringOrEmpty(value) {
  return typeof value === "string" ? value : "";
}

export function createCappedStderrState() {
  return {
    buffers: [],
    size: 0,
    truncated: false
  };
}

export function appendStderrChunk(state, chunk) {
  if (!state) {
    return;
  }
  const buf = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk));
  if (buf.length === 0) {
    return;
  }
  state.buffers.push(buf);
  state.size += buf.length;

  while (state.size > MAX_STDERR_BYTES && state.buffers.length > 0) {
    const head = state.buffers[0];
    const overflow = state.size - MAX_STDERR_BYTES;
    state.truncated = true;
    if (head.length <= overflow) {
      state.buffers.shift();
      state.size -= head.length;
      continue;
    }
    state.buffers[0] = head.subarray(overflow);
    state.size -= overflow;
    break;
  }
}

export function stderrDetail(state) {
  if (!state || state.buffers.length === 0) {
    return "";
  }
  const text = Buffer.concat(state.buffers).toString("utf8");
  if (!state.truncated) {
    return text;
  }
  return `[stderr truncated]\n${text}`;
}

function normalizeToolList(value) {
  if (typeof value === "string") {
    return value
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  }
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .map((item) => (typeof item === "string" ? item.trim() : ""))
    .filter(Boolean);
}

function pickBoolean(source, key, fallback) {
  return normalizeBoolean(source[key], fallback);
}

function pickPositiveInt(source, ...keys) {
  for (const key of keys) {
    if (!Object.prototype.hasOwnProperty.call(source, key)) {
      continue;
    }
    return normalizePositiveInt(source[key]);
  }
  return null;
}

function pickString(source, ...keys) {
  for (const key of keys) {
    const value = source[key];
    if (typeof value !== "string") {
      continue;
    }
    const text = value.trim();
    if (text !== "") {
      return text;
    }
  }
  return null;
}

function pickValue(source, ...keys) {
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(source, key)) {
      return source[key];
    }
  }
  return null;
}
