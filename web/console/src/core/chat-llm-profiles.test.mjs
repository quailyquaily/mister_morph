import assert from "node:assert/strict";
import test from "node:test";

import { normalizeChatLLMProfiles } from "./chat-llm-profiles.js";

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
