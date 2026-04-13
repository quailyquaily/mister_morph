import test from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";

import {
  CodexACPServer,
  buildToolDoneUpdate,
  buildToolProgressUpdate,
  buildToolStartUpdate,
  collectACPText,
  mapTurnOutcome,
  normalizeSessionOptions,
  shouldEmitAgentMessagePhase,
} from "../src/lib.mjs";

test("normalizeSessionOptions supports snake_case keys and defaults", () => {
  const options = normalizeSessionOptions({
    model: "gpt-5-codex",
    service_tier: "flex",
    reasoning_effort: "low",
  });

  assert.equal(options.model, "gpt-5-codex");
  assert.equal(options.serviceTier, "flex");
  assert.equal(options.reasoningEffort, "low");
  assert.equal(options.approvalPolicy, "never");
  assert.equal(options.ephemeral, true);
});

test("collectACPText joins ACP text items", () => {
  const text = collectACPText([
    { type: "text", text: "hello" },
    { type: "image", url: "ignored" },
    { type: "text", text: "world" },
  ]);

  assert.equal(text, "hello\nworld");
});

test("collectACPText preserves surrounding whitespace", () => {
  const text = collectACPText([{ type: "text", text: " hello \n" }]);

  assert.equal(text, " hello \n");
});

test("mapTurnOutcome maps completed and interrupted turns", () => {
  assert.deepEqual(mapTurnOutcome({ status: "completed" }), {
    stopReason: "end_turn",
  });
  assert.deepEqual(mapTurnOutcome({ status: "interrupted" }), {
    stopReason: "cancelled",
  });
});

test("mapTurnOutcome throws on failed turns", () => {
  assert.throws(
    () =>
      mapTurnOutcome({
        status: "failed",
        error: { message: "boom" },
      }),
    /boom/,
  );
});

test("tool update builders map command execution events", () => {
  const started = buildToolStartUpdate({
    type: "commandExecution",
    id: "cmd-1",
    command: "go test ./...",
    status: "inProgress",
  });
  assert.equal(started.sessionUpdate, "tool_call");
  assert.equal(started.toolCallId, "cmd-1");
  assert.equal(started.kind, "command_execution");
  assert.equal(started.status, "in_progress");

  const progress = buildToolProgressUpdate("item/commandExecution/outputDelta", {
    itemId: "cmd-1",
    delta: "ok",
  });
  assert.equal(progress.sessionUpdate, "tool_call_update");
  assert.equal(progress.toolCallId, "cmd-1");

  const done = buildToolDoneUpdate({
    type: "commandExecution",
    id: "cmd-1",
    command: "go test ./...",
    status: "completed",
    aggregatedOutput: "ok",
  });
  assert.equal(done.sessionUpdate, "tool_call_update");
  assert.equal(done.status, "completed");
});

test("tool update builders preserve whitespace deltas", () => {
  const update = buildToolProgressUpdate("item/commandExecution/outputDelta", {
    itemId: "cmd-1",
    delta: " hello \n",
  });

  assert.equal(update.content[0].text, " hello \n");
});

test("CodexACPServer preserves whitespace-only agent deltas", async () => {
  const stdout = new PassThrough();
  let raw = "";
  stdout.on("data", (chunk) => {
    raw += chunk.toString();
  });

  const server = new CodexACPServer({
    stdin: new PassThrough(),
    stdout,
  });
  server.sessions.set("sess-1", {
    sessionId: "sess-1",
    threadId: "thread-1",
    options: {},
    pendingTurn: null,
    itemPhases: new Map([["item-1", "final_answer"]]),
  });

  await server.codex.notificationHandler({
    method: "item/agentMessage/delta",
    params: {
      threadId: "thread-1",
      itemId: "item-1",
      delta: " ",
    },
  });

  const lines = raw.trimEnd().split("\n");
  assert.equal(lines.length, 1);
  const message = JSON.parse(lines[0]);
  assert.equal(message.params.update.content[0].text, " ");
});

test("shouldEmitAgentMessagePhase suppresses commentary", () => {
  assert.equal(shouldEmitAgentMessagePhase("commentary"), false);
  assert.equal(shouldEmitAgentMessagePhase("final_answer"), true);
  assert.equal(shouldEmitAgentMessagePhase(""), true);
});
