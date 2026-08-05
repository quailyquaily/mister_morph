import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

for (const view of ["SettingsView.js", "SetupView.js"]) {
  test(`${view} keeps Codex login separate from LLM settings`, async () => {
    const source = await readFile(new URL(`../views/${view}`, import.meta.url), "utf8");

    assert.match(source, /body: \{ session_id: sessionID, set_default: false \}/);
    assert.doesNotMatch(source, /body: \{ session_id: sessionID, set_default: true \}/);
  });
}
