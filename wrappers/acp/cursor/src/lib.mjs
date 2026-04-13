import { spawn } from "node:child_process";
import process from "node:process";
import readline from "node:readline";

export const WRAPPER_VERSION = "0.1.0";

export function buildBackendArgs() {
  const extra = normalizeString(process.env.MISTERMORPH_CURSOR_ARGS)
    .split(/\s+/)
    .filter(Boolean);
  return [...extra, "acp"];
}

export function resolveAgentCommand(options = {}) {
  return (
    normalizeString(options.command) ||
    normalizeString(process.env.MISTERMORPH_CURSOR_COMMAND) ||
    "agent"
  );
}

/**
 * Transparent stdio proxy: MisterMorph ACP client <-> Cursor CLI `agent acp`.
 * JSON-RPC lines pass through unchanged; stderr is forwarded for CLI logs.
 */
export class CursorACPProxy {
  constructor(options = {}) {
    this.command = resolveAgentCommand(options);
    this.args = Array.isArray(options.args) ? options.args : buildBackendArgs();
    this.cwd = normalizeString(options.cwd) || process.cwd();
    this.env = { ...process.env, ...(isRecord(options.env) ? options.env : {}) };
    this.stdin = options.stdin ?? process.stdin;
    this.stdout = options.stdout ?? process.stdout;
    this.stderr = options.stderr ?? process.stderr;
    this.proc = null;
    this.stdinRl = null;
    this.stdoutRl = null;
    this.closed = false;
  }

  start() {
    this.proc = spawn(this.command, this.args, {
      cwd: this.cwd,
      env: this.env,
      stdio: ["pipe", "pipe", "pipe"],
    });

    this.proc.stderr.on("data", (chunk) => {
      this.stderr.write(chunk);
    });

    this.proc.on("error", (err) => {
      if (this.closed) {
        return;
      }
      this.closed = true;
      const msg =
        err instanceof Error ? err.message : String(err);
      this.stderr.write(
        `\n[cursor-acp-proxy] failed to spawn ${this.command}: ${msg}\n`,
      );
      process.exit(1);
    });

    this.proc.on("exit", (code, signal) => {
      if (this.closed) {
        return;
      }
      this.closed = true;
      const suffix = signal ? `signal ${signal}` : `code ${code ?? "unknown"}`;
      this.stderr.write(`\n[cursor-acp-proxy] backend exited (${suffix})\n`);
      process.exit(code ?? 1);
    });

    this.stdinRl = readline.createInterface({ input: this.stdin });
    this.stdinRl.on("line", (line) => {
      if (this.closed || !this.proc?.stdin || this.proc.stdin.destroyed) {
        return;
      }
      this.proc.stdin.write(`${line}\n`);
    });
    this.stdinRl.on("close", () => {
      void this.close();
    });

    this.stdoutRl = readline.createInterface({ input: this.proc.stdout });
    this.stdoutRl.on("line", (line) => {
      if (this.closed) {
        return;
      }
      if (line.trim() === "") {
        return;
      }
      this.stdout.write(`${line}\n`);
    });
    this.stdoutRl.on("close", () => {
      if (!this.closed) {
        void this.close();
      }
    });
  }

  async close() {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.stdinRl) {
      this.stdinRl.close();
    }
    if (this.stdoutRl) {
      this.stdoutRl.close();
    }
    if (this.proc?.stdin && !this.proc.stdin.destroyed) {
      this.proc.stdin.end();
    }
    if (this.proc && !this.proc.killed) {
      this.proc.kill();
    }
  }
}

export function printHelp(stream = process.stdout) {
  stream.write(
    [
      "MisterMorph ACP Cursor proxy",
      "",
      "Transparently forwards stdio JSON-RPC between MisterMorph and Cursor CLI:",
      "  <your-command> ... <args> acp",
      "",
      "Default command is `agent` (Cursor CLI). Ensure `agent` is on PATH or set",
      "MISTERMORPH_CURSOR_COMMAND to the full path.",
      "",
      "Usage:",
      "  node ./wrappers/acp/cursor/src/index.mjs",
      "",
      "Environment:",
      "  MISTERMORPH_CURSOR_COMMAND  backend executable, default: agent",
      "  MISTERMORPH_CURSOR_ARGS     extra args inserted before the final `acp`",
      "                              (space-separated, e.g. \"--api-key $CURSOR_API_KEY\")",
      "",
      "Authenticate before use: `agent login`, or pass API key / auth token via",
      "MISTERMORPH_CURSOR_ARGS / Cursor CLI env vars (see Cursor ACP docs).",
      "",
    ].join("\n"),
  );
}

export function main(argv = process.argv.slice(2)) {
  if (argv.includes("--help") || argv.includes("-h")) {
    printHelp();
    return;
  }
  const proxy = new CursorACPProxy();
  proxy.start();
}

function normalizeString(value) {
  return typeof value === "string" ? value.trim() : "";
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
