import test from "node:test";
import assert from "node:assert/strict";

import {
  appendStderrChunk,
  buildClaudePromptFlags,
  collectACPText,
  createCappedStderrState,
  createPromptState,
  normalizeSessionOptions,
  processClaudeEvent,
  stderrDetail
} from "../src/lib.mjs";

test("normalizeSessionOptions supports defaults and arrays", () => {
  const options = normalizeSessionOptions({
    model: "claude-opus-4-6",
    permission_mode: "dontAsk",
    allowed_tools: ["Read", "Edit"],
    max_turns: 3
  });

  assert.equal(options.model, "claude-opus-4-6");
  assert.equal(options.permissionMode, "dontAsk");
  assert.deepEqual(options.allowedTools, ["Read", "Edit"]);
  assert.equal(options.maxTurns, 3);
  assert.equal(options.bare, false);
});

test("collectACPText joins ACP text items", () => {
  const text = collectACPText([
    { type: "text", text: "hello" },
    { type: "image", url: "ignored" },
    { type: "text", text: "world" }
  ]);

  assert.equal(text, "hello\nworld");
});

test("collectACPText preserves surrounding whitespace", () => {
  const text = collectACPText([{ type: "text", text: " hello \n" }]);

  assert.equal(text, " hello \n");
});

test("buildClaudePromptFlags includes streaming and permission flags", () => {
  const args = buildClaudePromptFlags("Say Hello", {
    permissionMode: "dontAsk",
    allowedTools: ["Read", "Edit"],
    model: "opus",
    appendSystemPrompt: "Be precise.",
    maxTurns: 2,
    bare: true
  });

  assert.deepEqual(args.slice(0, 9), [
    "--bare",
    "-p",
    "Say Hello",
    "--output-format",
    "stream-json",
    "--verbose",
    "--include-partial-messages",
    "--no-session-persistence",
    "--permission-mode"
  ]);
  assert.ok(args.includes("--allowedTools"));
  assert.ok(args.includes("Read"));
  assert.ok(args.includes("Edit"));
  assert.ok(args.includes("--append-system-prompt"));
  assert.ok(args.includes("--max-turns"));
});

test("processClaudeEvent emits stream deltas and final stop reason", () => {
  const state = createPromptState();

  const partial = processClaudeEvent(
    {
      type: "stream_event",
      event: {
        delta: { type: "text_delta", text: "Hel" }
      }
    },
    state
  );
  assert.equal(partial.updates.length, 1);
  assert.equal(partial.updates[0].content[0].text, "Hel");

  const assistant = processClaudeEvent(
    {
      type: "assistant",
      message: {
        content: [{ type: "text", text: "Hello" }]
      }
    },
    state
  );
  assert.equal(assistant.updates.length, 1);
  assert.equal(assistant.updates[0].content[0].text, "lo");

  const result = processClaudeEvent(
    {
      type: "result",
      subtype: "success",
      is_error: false,
      result: "Hello",
      stop_reason: "stop_sequence"
    },
    state
  );
  assert.equal(result.updates.length, 0);
  assert.deepEqual(result.final, { stopReason: "end_turn" });
});

test("processClaudeEvent turns result errors into failures", () => {
  const state = createPromptState();
  const outcome = processClaudeEvent(
    {
      type: "result",
      subtype: "success",
      is_error: true,
      result: "authentication failed",
      stop_reason: "stop_sequence"
    },
    state
  );

  assert.equal(outcome.updates.length, 1);
  assert.match(outcome.error.message, /authentication failed/);
});

test("processClaudeEvent preserves whitespace deltas", () => {
  const state = createPromptState();

  const partial = processClaudeEvent(
    {
      type: "stream_event",
      event: {
        delta: { type: "text_delta", text: " " }
      }
    },
    state
  );
  assert.equal(partial.updates[0].content[0].text, " ");

  const assistant = processClaudeEvent(
    {
      type: "assistant",
      message: {
        content: [{ type: "text", text: " world\n" }]
      }
    },
    state
  );
  assert.equal(assistant.updates[0].content[0].text, "world\n");
});

test("stderrDetail caps backend stderr", () => {
  const state = createCappedStderrState();
  appendStderrChunk(state, Buffer.from("x".repeat(70000)));

  const detail = stderrDetail(state);
  assert.match(detail, /\[stderr truncated\]/);
  assert.ok(detail.length < 70000);
});
