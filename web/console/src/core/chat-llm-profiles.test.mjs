import assert from "node:assert/strict";
import test from "node:test";

import {
  lastUsedChatLLMProfile,
  normalizeChatLLMProfiles,
  resolveAvailableChatLLMProfile,
} from "./chat-llm-profiles.js";

test("normalizeChatLLMProfiles keeps valid named profiles and normalizes metadata", () => {
  assert.deepEqual(
    normalizeChatLLMProfiles([
      { name: " local ", inference_provider: " ollama ", model: " qwen3:8b " },
      { name: "cheap", inferenceProvider: "openai", modelName: "gpt-4.1-mini" },
      { name: "cheap", model: "duplicate" },
      { name: "  ", model: "ignored" },
      null,
    ]),
    [
      { name: "cheap", inferenceProvider: "openai", modelName: "gpt-4.1-mini" },
      { name: "local", inferenceProvider: "ollama", modelName: "qwen3:8b" },
    ]
  );
});

test("normalizeChatLLMProfiles returns no items when only the default route exists", () => {
  assert.deepEqual(normalizeChatLLMProfiles([]), []);
  assert.deepEqual(normalizeChatLLMProfiles(undefined), []);
});

test("lastUsedChatLLMProfile follows the newest model task in a topic", () => {
  assert.equal(
    lastUsedChatLLMProfile([
      { created_at: "2026-08-02T09:00:00Z", llm_profile: "cheap" },
      {
        created_at: "2026-08-02T11:00:00Z",
        llm_profile: "ignored-steer",
        steer_target_task_id: "task-running",
      },
      { created_at: "2026-08-02T10:00:00Z", llm_profile: "quality" },
    ]),
    "quality"
  );
});

test("lastUsedChatLLMProfile preserves an explicit return to the default route", () => {
  assert.equal(
    lastUsedChatLLMProfile([
      { created_at: "2026-08-02T09:00:00Z", llm_profile: "cheap" },
      { created_at: "2026-08-02T10:00:00Z" },
    ]),
    ""
  );
});

test("resolveAvailableChatLLMProfile falls back only when a named profile is unavailable", () => {
  const profiles = normalizeChatLLMProfiles([
    { name: "cheap", model: "gpt-5-mini" },
    { name: "quality", model: "gpt-5.5" },
  ]);

  assert.equal(resolveAvailableChatLLMProfile(" quality ", profiles), "quality");
  assert.equal(resolveAvailableChatLLMProfile("deleted", profiles), "");
  assert.equal(resolveAvailableChatLLMProfile("", profiles), "");
});
