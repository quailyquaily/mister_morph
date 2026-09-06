import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const settingsViewSource = new URL("../views/SettingsView.js", import.meta.url);

test("Settings action notices use toast instead of inline fences", async () => {
  const source = await readFile(settingsViewSource, "utf8");

  assert.match(source, /const saveMessage = t\("msg_save_success"\);/);
  assert.match(source, /toast\.success\(saveMessage\);/);
  assert.match(source, /toast\.success\(settingsSavedMessage\(payload\)\);/);
  assert.match(source, /toast\.success\(t\("settings_desktop_update_checksum_copied"\)\);/);

  assert.doesNotMatch(source, /:text="agentOk"/);
  assert.doesNotMatch(source, /:text="agentErr"/);
  assert.doesNotMatch(source, /:text="consoleOk"/);
  assert.doesNotMatch(source, /:text="consoleErr"/);
  assert.doesNotMatch(source, /:text="desktopOk"/);
  assert.doesNotMatch(source, /:text="desktopErr"/);

  assert.match(source, /:text="agentValidationError"/);
  assert.match(source, /:text="skillsValidationError"/);
});
