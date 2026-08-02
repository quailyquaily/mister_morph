import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function source(path) {
  return readFile(new URL(path, import.meta.url), "utf8");
}

test("reasoning expands into the existing Markdown chat bubble", async () => {
  const [item, statusCard] = await Promise.all([
    source("../components/ChatHistoryItem.js"),
    source("../components/ChatStatusCard.js"),
  ]);

  assert.equal(item.includes("ChatReasoningBubble"), false);
  assert.match(
    item,
    /<div\s+v-if="reasoningVisible && expandedPanel === 'reasoning'"\s+:class="surfaceClass"\s*>\s*<ChatRichContent\s+class="chat-history-markdown"\s+:source="item\.reasoning"[\s\S]*?theme="blueprint"/u
  );
  assert.equal((item.match(/<ChatRichContent\b/gu) || []).length, 2);

  assert.match(statusCard, /hasReasoning/u);
  assert.equal(statusCard.includes("chat-reasoning-text"), false);
  assert.equal(statusCard.includes("<MarkdownContent"), false);

  await assert.rejects(source("../components/ChatReasoningBubble.js"), { code: "ENOENT" });
  await assert.rejects(source("../components/ChatReasoningBubble.css"), { code: "ENOENT" });
});
