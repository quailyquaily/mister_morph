import { spawn } from "node:child_process";
import crypto from "node:crypto";
import readline from "node:readline";
import process from "node:process";

export const WRAPPER_VERSION = "0.1.0";
export const ACP_PROTOCOL_VERSION = 1;
export const SUPPORTED_CONFIG_OPTIONS = [
  "model",
  "service_tier",
  "approval_policy",
  "reasoning_effort",
];

const JSONRPC_VERSION = "2.0";
const RPC_METHOD_NOT_FOUND = -32601;
const RPC_INVALID_PARAMS = -32602;
const RPC_INTERNAL_ERROR = -32603;
const DEFAULT_APPROVAL_POLICY = "never";
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
    serviceTier: pickString(source, "service_tier", "serviceTier"),
    approvalPolicy:
      pickValue(source, "approval_policy", "approvalPolicy") ??
      DEFAULT_APPROVAL_POLICY,
    reasoningEffort: pickString(
      source,
      "reasoning_effort",
      "reasoningEffort",
    ),
    sandbox: pickValue(source, "sandbox"),
    baseInstructions: pickString(
      source,
      "base_instructions",
      "baseInstructions",
    ),
    developerInstructions: pickString(
      source,
      "developer_instructions",
      "developerInstructions",
    ),
    ephemeral: pickBoolean(source, "ephemeral", true),
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
    if (item.type !== "text") {
      continue;
    }
    if (typeof item.text !== "string") {
      continue;
    }
    const text = item.text;
    if (text !== "") {
      parts.push(text);
    }
  }
  return parts.join("\n");
}

export function mapTurnOutcome(turn) {
  if (!isRecord(turn)) {
    throw new Error("codex turn response missing turn payload");
  }
  const status = normalizeString(turn.status).toLowerCase();
  switch (status) {
    case "completed":
      return { stopReason: "end_turn" };
    case "interrupted":
      return { stopReason: "cancelled" };
    case "failed": {
      const error = isRecord(turn.error) ? turn.error : {};
      const message =
        normalizeString(error.message) ||
        normalizeString(error.additionalDetails) ||
        "codex turn failed";
      throw new Error(message);
    }
    case "inprogress":
      return null;
    default:
      throw new Error(`unsupported codex turn status: ${turn.status}`);
  }
}

export function buildToolStartUpdate(item) {
  if (!isRecord(item) || typeof item.id !== "string") {
    return null;
  }
  if (item.type === "commandExecution") {
    return {
      sessionUpdate: "tool_call",
      toolCallId: item.id,
      title: typeof item.command === "string" ? item.command : "command",
      kind: "command_execution",
      status: mapItemStatus(item.status),
      content: textContent(typeof item.command === "string" ? item.command : ""),
    };
  }
  if (item.type === "fileChange") {
    return {
      sessionUpdate: "tool_call",
      toolCallId: item.id,
      title: "file change",
      kind: "file_change",
      status: mapItemStatus(item.status),
      content: textContent(summarizeFileChanges(item.changes)),
    };
  }
  return null;
}

export function buildToolProgressUpdate(method, params) {
  if (!isRecord(params) || typeof params.itemId !== "string") {
    return null;
  }
  if (
    method === "item/commandExecution/outputDelta" ||
    method === "command/exec/outputDelta"
  ) {
    return {
      sessionUpdate: "tool_call_update",
      toolCallId: params.itemId,
      kind: "command_execution",
      status: "in_progress",
      content: textContent(typeof params.delta === "string" ? params.delta : ""),
    };
  }
  if (method === "item/fileChange/outputDelta") {
    return {
      sessionUpdate: "tool_call_update",
      toolCallId: params.itemId,
      kind: "file_change",
      status: "in_progress",
      content: textContent(typeof params.delta === "string" ? params.delta : ""),
    };
  }
  return null;
}

export function buildToolDoneUpdate(item) {
  const started = buildToolStartUpdate(item);
  if (!started) {
    return null;
  }
  return {
    ...started,
    sessionUpdate: "tool_call_update",
    status: mapItemStatus(item.status),
    content: textContent(extractToolOutput(item) || summarizeFileChanges(item.changes)),
  };
}

export function shouldEmitAgentMessagePhase(phase) {
  const normalized = normalizeString(phase).toLowerCase();
  if (normalized === "commentary") {
    return false;
  }
  return true;
}

export class CodexAppServerClient {
  constructor(options = {}) {
    this.command =
      normalizeString(options.command) ||
      normalizeString(process.env.MISTERMORPH_CODEX_COMMAND) ||
      "codex";
    this.args = buildBackendArgs(options.args);
    this.cwd = normalizeString(options.cwd) || process.cwd();
    this.env = { ...process.env, ...(isRecord(options.env) ? options.env : {}) };
    this.proc = null;
    this.rl = null;
    this.nextID = 1;
    this.pending = new Map();
    this.ready = null;
    this.notificationHandler = null;
    this.requestHandler = null;
    this.closed = false;
    this.starting = false;
  }

  async ensureStarted() {
    if (this.ready) {
      return this.ready;
    }
    this.ready = this.#start();
    return this.ready;
  }

  async #start() {
    this.starting = true;
    this.proc = spawn(this.command, this.args, {
      cwd: this.cwd,
      env: this.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.proc.stderr.on("data", (chunk) => {
      process.stderr.write(chunk);
    });
    this.proc.on("exit", (code, signal) => {
      const suffix = signal ? `signal ${signal}` : `code ${code ?? "unknown"}`;
      const error = new Error(`codex app-server exited with ${suffix}`);
      for (const pending of this.pending.values()) {
        pending.reject(error);
      }
      this.pending.clear();
    });

    this.rl = readline.createInterface({ input: this.proc.stdout });
    this.rl.on("line", (line) => {
      void this.#handleLine(line);
    });

    const initializeResult = await this.#sendRequest("initialize", {
      clientInfo: {
        name: "mistermorph-acp-codex",
        version: WRAPPER_VERSION,
      },
      capabilities: {
        experimentalApi: false,
      },
    });
    await this.#sendNotification("initialized", undefined);
    this.starting = false;
    return initializeResult;
  }

  async close() {
    this.closed = true;
    if (this.rl) {
      this.rl.close();
    }
    if (this.proc?.stdin && !this.proc.stdin.destroyed) {
      this.proc.stdin.end();
    }
    if (this.proc && !this.proc.killed) {
      this.proc.kill();
    }
  }

  async call(method, params) {
    await this.ensureStarted();
    return this.#sendRequest(method, params);
  }

  async notify(method, params) {
    await this.ensureStarted();
    this.#sendNotification(method, params);
  }

  #send(message) {
    if (!this.proc?.stdin || this.proc.stdin.destroyed) {
      throw new Error("codex app-server stdin is not available");
    }
    this.proc.stdin.write(`${JSON.stringify(message)}\n`);
  }

  async #sendRequest(method, params) {
    const id = this.nextID++;
    const response = new Promise((resolve, reject) => {
      this.pending.set(String(id), { resolve, reject });
    });
    this.#send({
      jsonrpc: JSONRPC_VERSION,
      id,
      method,
      params,
    });
    return response;
  }

  #sendNotification(method, params) {
    this.#send({
      jsonrpc: JSONRPC_VERSION,
      method,
      params,
    });
  }

  async #handleLine(line) {
    if (line.trim() === "") {
      return;
    }
    let message;
    try {
      message = JSON.parse(line);
    } catch (error) {
      process.stderr.write(`invalid codex app-server json: ${String(error)}\n`);
      return;
    }
    if (!isRecord(message)) {
      return;
    }
    if (Object.prototype.hasOwnProperty.call(message, "id") && !message.method) {
      const key = String(message.id);
      const pending = this.pending.get(key);
      if (!pending) {
        return;
      }
      this.pending.delete(key);
      if (message.error) {
        pending.reject(new Error(message.error.message || "codex app-server error"));
        return;
      }
      pending.resolve(message.result);
      return;
    }
    if (Object.prototype.hasOwnProperty.call(message, "id") && message.method) {
      try {
        const result = await this.#handleServerRequest(message);
        this.#send({
          jsonrpc: JSONRPC_VERSION,
          id: message.id,
          result,
        });
      } catch (error) {
        this.#send({
          jsonrpc: JSONRPC_VERSION,
          id: message.id,
          error: {
            code: RPC_INTERNAL_ERROR,
            message: error instanceof Error ? error.message : String(error),
          },
        });
      }
      return;
    }
    if (message.method && this.notificationHandler) {
      await this.notificationHandler(message);
    }
  }

  async #handleServerRequest(message) {
    if (!this.requestHandler) {
      throw new Error(`unhandled codex server request: ${message.method}`);
    }
    return this.requestHandler(message);
  }
}

export class CodexACPServer {
  constructor(options = {}) {
    this.stdin = options.stdin ?? process.stdin;
    this.stdout = options.stdout ?? process.stdout;
    this.stderr = options.stderr ?? process.stderr;
    this.readline = readline.createInterface({ input: this.stdin });
    this.codex = new CodexAppServerClient({
      command: options.codexCommand,
      args: options.codexArgs,
      cwd: options.cwd,
      env: options.env,
    });
    this.sessions = new Map();
    this.codex.notificationHandler = async (message) => {
      await this.#handleCodexNotification(message);
    };
    this.codex.requestHandler = async (message) => {
      return this.#handleCodexRequest(message);
    };
  }

  start() {
    this.readline.on("line", (line) => {
      void this.#handleACPLine(line);
    });
    this.readline.on("close", () => {
      void this.codex.close();
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
        result,
      });
    } catch (error) {
      if (error instanceof JsonRpcFailure) {
        this.#writeError(message.id ?? null, error.code, error.message);
        return;
      }
      this.#writeError(
        message.id ?? null,
        RPC_INTERNAL_ERROR,
        error instanceof Error ? error.message : String(error),
      );
    }
  }

  async #handleACPRequest(method, params) {
    switch (method) {
      case ACP_METHOD_INITIALIZE:
        return {
          protocolVersion: ACP_PROTOCOL_VERSION,
          authMethods: [],
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
        throw new JsonRpcFailure(RPC_METHOD_NOT_FOUND, `unsupported ACP method: ${method}`);
    }
  }

  async #createSession(params) {
    const payload = asObject(params, "session/new params");
    const cwd = normalizeString(payload.cwd) || process.cwd();
    const meta = isRecord(payload._meta) ? payload._meta : {};
    const options = normalizeSessionOptions(meta);

    const result = await this.codex.call("thread/start", cleanObject({
      cwd,
      model: options.model ?? null,
      serviceTier: options.serviceTier ?? null,
      approvalPolicy: options.approvalPolicy,
      sandbox: options.sandbox ?? null,
      baseInstructions: options.baseInstructions ?? null,
      developerInstructions: options.developerInstructions ?? null,
      ephemeral: options.ephemeral,
      experimentalRawEvents: false,
      persistExtendedHistory: false,
    }));

    const thread = asObject(result?.thread, "codex thread/start result.thread");
    const sessionId = normalizeString(thread.id) || crypto.randomUUID();
    this.sessions.set(sessionId, {
      sessionId,
      threadId: normalizeString(thread.id) || sessionId,
      cwd,
      options,
      pendingTurn: null,
      itemPhases: new Map(),
    });

    return {
      sessionId,
      configOptions: SUPPORTED_CONFIG_OPTIONS.map((id) => ({ id })),
    };
  }

  async #setSessionConfig(params) {
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
    if (session.pendingTurn) {
      throw new JsonRpcFailure(
        RPC_INVALID_PARAMS,
        `session ${session.sessionId} already has an active turn`,
      );
    }
    const prompt = collectACPText(payload.prompt);
    if (prompt.trim() === "") {
      throw new JsonRpcFailure(RPC_INVALID_PARAMS, "session/prompt requires text content");
    }

    const deferred = createDeferred();
    session.pendingTurn = {
      turnId: "",
      resolve: deferred.resolve,
      reject: deferred.reject,
    };

    try {
      const result = await this.codex.call("turn/start", cleanObject({
        threadId: session.threadId,
        input: [
          {
            type: "text",
            text: prompt,
            text_elements: [],
          },
        ],
        cwd: session.cwd,
        model: session.options.model ?? null,
        serviceTier: session.options.serviceTier ?? null,
        approvalPolicy: session.options.approvalPolicy ?? null,
        effort: session.options.reasoningEffort ?? null,
      }));

      const turn = asObject(result?.turn, "codex turn/start result.turn");
      session.pendingTurn.turnId = normalizeString(turn.id);
      const immediate = mapTurnOutcome(turn);
      if (!immediate) {
        return deferred.promise;
      }
      session.pendingTurn = null;
      return immediate;
    } catch (error) {
      session.pendingTurn = null;
      throw error;
    }
  }

  async #cancelPrompt(params) {
    const payload = asObject(params, "session/cancel params");
    const session = this.#getSession(payload.sessionId);
    if (!session.pendingTurn || normalizeString(session.pendingTurn.turnId) === "") {
      return {};
    }
    await this.codex.call("turn/interrupt", {
      threadId: session.threadId,
      turnId: session.pendingTurn.turnId,
    });
    return {};
  }

  async #handleCodexNotification(message) {
    const method = normalizeString(message.method);
    const params = isRecord(message.params) ? message.params : {};
    const session = this.#findSessionByThreadId(params.threadId);
    if (!session) {
      return;
    }

    if (method === "item/agentMessage/delta") {
      const phase = session.itemPhases.get(normalizeString(params.itemId));
      if (!shouldEmitAgentMessagePhase(phase)) {
        return;
      }
      const delta = stringOrEmpty(params.delta);
      if (delta !== "") {
        this.#notifySessionUpdate(session.sessionId, {
          sessionUpdate: "agent_message_chunk",
          content: textContent(delta),
        });
      }
      return;
    }

    if (method === "item/started") {
      this.#rememberItemPhase(session, params.item);
      const update = buildToolStartUpdate(params.item);
      if (update) {
        this.#notifySessionUpdate(session.sessionId, update);
      }
      return;
    }

    if (
      method === "item/commandExecution/outputDelta" ||
      method === "command/exec/outputDelta" ||
      method === "item/fileChange/outputDelta"
    ) {
      const update = buildToolProgressUpdate(method, params);
      if (update) {
        this.#notifySessionUpdate(session.sessionId, update);
      }
      return;
    }

    if (method === "item/completed") {
      this.#rememberItemPhase(session, params.item);
      const update = buildToolDoneUpdate(params.item);
      if (update) {
        this.#notifySessionUpdate(session.sessionId, update);
      }
      return;
    }

    if (method === "turn/completed") {
      if (!session.pendingTurn) {
        return;
      }
      if (!turnMatches(session.pendingTurn.turnId, params.turn?.id)) {
        return;
      }
      const pending = session.pendingTurn;
      session.pendingTurn = null;
      try {
        pending.resolve(mapTurnOutcome(params.turn));
      } catch (error) {
        pending.reject(error);
      }
      return;
    }

    if (method === "error") {
      if (!session.pendingTurn) {
        return;
      }
      if (!turnMatches(session.pendingTurn.turnId, params.turnId)) {
        return;
      }
      const pending = session.pendingTurn;
      session.pendingTurn = null;
      const error = isRecord(params.error) ? params.error : {};
      const messageText =
        normalizeString(error.message) ||
        normalizeString(error.additionalDetails) ||
        "codex turn failed";
      pending.reject(new Error(messageText));
    }
  }

  async #handleCodexRequest(message) {
    const method = normalizeString(message.method);
    const params = isRecord(message.params) ? message.params : {};
    const session = this.#findSessionByThreadId(params.threadId);
    const autoApprove =
      session?.options?.approvalPolicy === "never"
        ? false
        : normalizeString(process.env.MISTERMORPH_CODEX_AUTO_APPROVE) === "1";

    if (method === "item/commandExecution/requestApproval") {
      return {
        decision: autoApprove ? "acceptForSession" : "decline",
      };
    }
    if (method === "item/fileChange/requestApproval") {
      return {
        decision: autoApprove ? "acceptForSession" : "decline",
      };
    }
    throw new Error(`unsupported codex server request: ${method}`);
  }

  #notifySessionUpdate(sessionId, update) {
    this.#write({
      jsonrpc: JSONRPC_VERSION,
      method: ACP_METHOD_SESSION_UPDATE,
      params: {
        sessionId,
        update,
      },
    });
  }

  #rememberItemPhase(session, item) {
    if (!session || !isRecord(item)) {
      return;
    }
    if (normalizeString(item.type) !== "agentMessage") {
      return;
    }
    const itemID = normalizeString(item.id);
    if (itemID === "") {
      return;
    }
    session.itemPhases.set(itemID, normalizeString(item.phase));
  }

  #getSession(sessionId) {
    const key = normalizeString(sessionId);
    const session = this.sessions.get(key);
    if (!session) {
      throw new JsonRpcFailure(RPC_INVALID_PARAMS, `unknown sessionId: ${sessionId}`);
    }
    return session;
  }

  #findSessionByThreadId(threadId) {
    const key = normalizeString(threadId);
    if (key === "") {
      return null;
    }
    for (const session of this.sessions.values()) {
      if (session.threadId === key) {
        return session;
      }
    }
    return null;
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
        message,
      },
    });
  }
}

export function buildBackendArgs(rawArgs) {
  const extraArgs = Array.isArray(rawArgs)
    ? rawArgs
    : normalizeString(process.env.MISTERMORPH_CODEX_ARGS)
        .split(/\s+/)
        .filter(Boolean);
  return ["app-server", ...extraArgs];
}

export function printHelp(stream = process.stdout) {
  stream.write(
    [
      "MisterMorph ACP Codex Wrapper",
      "",
      "Starts an ACP agent over stdio and bridges it to codex app-server.",
      "",
      "Usage:",
      "  node ./wrappers/acp/codex/src/index.mjs",
      "",
      "Environment:",
      "  MISTERMORPH_CODEX_COMMAND       backend executable, default: codex",
      "  MISTERMORPH_CODEX_ARGS          extra backend args appended after app-server",
      "  MISTERMORPH_CODEX_AUTO_APPROVE  set to 1 to auto-accept command/file approvals",
      "",
    ].join("\n"),
  );
}

export function main(argv = process.argv.slice(2)) {
  if (argv.includes("--help") || argv.includes("-h")) {
    printHelp();
    return;
  }
  const server = new CodexACPServer();
  server.start();
}

class JsonRpcFailure extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

function createDeferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function asObject(value, label) {
  if (!isRecord(value)) {
    throw new JsonRpcFailure(RPC_INVALID_PARAMS, `invalid ${label}`);
  }
  return value;
}

function applyConfigOption(options, configId, value) {
  const next = { ...options };
  switch (configId) {
    case "model":
      next.model = typeof value === "string" ? value.trim() || null : null;
      break;
    case "service_tier":
      next.serviceTier = typeof value === "string" ? value.trim() || null : null;
      break;
    case "approval_policy":
      next.approvalPolicy = value ?? DEFAULT_APPROVAL_POLICY;
      break;
    case "reasoning_effort":
      next.reasoningEffort =
        typeof value === "string" ? value.trim() || null : null;
      break;
    default:
      break;
  }
  return next;
}

function cleanObject(value) {
  const out = {};
  for (const [key, item] of Object.entries(value)) {
    if (item === undefined) {
      continue;
    }
    out[key] = item;
  }
  return out;
}

function extractToolOutput(item) {
  if (!isRecord(item)) {
    return "";
  }
  if (item.type === "commandExecution") {
    return typeof item.aggregatedOutput === "string" ? item.aggregatedOutput : "";
  }
  return "";
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function mapItemStatus(value) {
  switch (normalizeString(value).toLowerCase()) {
    case "inprogress":
      return "in_progress";
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    default:
      return normalizeString(value) || "unknown";
  }
}

function normalizeString(value) {
  return typeof value === "string" ? value.trim() : "";
}

function stringOrEmpty(value) {
  return typeof value === "string" ? value : "";
}

function pickBoolean(source, key, fallback) {
  const value = source[key];
  return typeof value === "boolean" ? value : fallback;
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

function summarizeFileChanges(changes) {
  if (!Array.isArray(changes)) {
    return "";
  }
  const paths = [];
  for (const change of changes) {
    if (!isRecord(change)) {
      continue;
    }
    const path =
      pickString(change, "path", "filePath", "relativePath") ||
      pickString(change.target ?? {}, "path");
    if (path) {
      paths.push(path);
    }
  }
  return paths.join("\n");
}

function textContent(text) {
  if (typeof text !== "string" || text === "") {
    return [];
  }
  return [{ type: "text", text }];
}

function turnMatches(expected, actual) {
  const want = normalizeString(expected);
  const got = normalizeString(actual);
  if (want === "") {
    return got !== "";
  }
  return want === got;
}
