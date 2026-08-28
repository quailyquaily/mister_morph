import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const settingsViewSource = new URL("../views/SettingsView.js", import.meta.url);
const i18nSource = new URL("../i18n/index.js", import.meta.url);

test("console channel settings expose line, lark, and mixin alongside telegram and slack", async () => {
  const source = await readFile(settingsViewSource, "utf8");
  const i18n = await readFile(i18nSource, "utf8");

  assert.match(source, /id:\s*"lark",\s*titleKey:\s*"settings_console_runtime_lark"/);
  assert.match(source, /id:\s*"mixin",\s*titleKey:\s*"settings_console_runtime_mixin"/);
  assert.match(source, /line:\s*buildEmptyLineConsoleState\(\)/);
  assert.match(source, /lark:\s*buildEmptyLarkConsoleState\(\)/);
  assert.match(source, /mixin:\s*buildEmptyMixinConsoleState\(\)/);
  assert.match(source, /buildConsoleLineSnapshot\(state\)/);
  assert.match(source, /buildConsoleLarkSnapshot\(state\)/);
  assert.match(source, /buildConsoleMixinSnapshot\(state\)/);
  assert.match(source, /data\?\.line && typeof data\.line === "object"/);
  assert.match(source, /data\?\.lark && typeof data\.lark === "object"/);
  assert.match(source, /data\?\.mixin && typeof data\.mixin === "object"/);
  assert.match(source, /if \(target === "line"\)/);
  assert.match(source, /if \(target === "lark"\)/);
  assert.match(source, /if \(target === "mixin"\)/);
  assert.match(source, /saveConsoleSettings\('line'\)/);
  assert.match(source, /saveConsoleSettings\('lark'\)/);
  assert.match(source, /saveConsoleSettings\('mixin'\)/);
	assert.match(source, /\["all", "runtimes", "telegram", "slack", "line", "lark", "mixin", "guard"\]/);
	assert.match(source, /normalizedTarget === "mixin" && mixinSaveDisabled\.value/);
  assert.match(source, /consoleFieldEnvManaged\('line', 'channel_access_token'\)/);
  assert.match(source, /consoleFieldEnvManaged\('lark', 'app_secret'\)/);
  assert.match(source, /consoleFieldEnvManaged\('mixin', 'keystore_file'\)/);

  assert.match(i18n, /settings_console_line_title:/);
  assert.match(i18n, /settings_console_lark_title:/);
  assert.match(i18n, /settings_console_line_channel_access_token_label:/);
  assert.match(i18n, /settings_console_lark_app_secret_label:/);
  assert.match(i18n, /settings_console_runtime_lark:/);
  assert.match(i18n, /settings_console_mixin_keystore_file_label:/);
  assert.match(i18n, /settings_console_runtime_mixin:/);
});
