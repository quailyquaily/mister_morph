import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const settingsViewSource = new URL("../views/SettingsView.js", import.meta.url);

test("LLM save success uses toast instead of an inline success fence", async () => {
  const source = await readFile(settingsViewSource, "utf8");

  assert.match(source, /const saveMessage = t\("msg_save_success"\);/);
  assert.match(source, /if \(normalizedTarget === "llm"\) \{\s*toast\.success\(saveMessage\);\s*agentOk\.value = "";/);
  assert.match(source, /agentOk && agentNoticeTarget !== 'llm'/);
});
