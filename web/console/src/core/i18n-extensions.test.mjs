import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { i18nExtensions } from "../ext/i18n/index.js";

const i18nSource = new URL("../i18n/index.js", import.meta.url);

test("default i18n extension dictionary is empty", () => {
  assert.deepEqual(i18nExtensions, {});
});

test("translate falls back to extension messages after core messages", async () => {
  const source = await readFile(i18nSource, "utf8");

  assert.match(source, /import\s+\{\s*i18nExtensions\s*\}\s+from "\.\.\/ext\/i18n";/);
  assert.match(source, /function messageFrom\(messages, lang, key\)/);
  assert.match(source, /let text = messageFrom\(I18N, localeState\.lang, key\);/);
  assert.match(source, /text = messageFrom\(i18nExtensions, localeState\.lang, key\);/);
  assert.match(source, /text = key;/);

  const coreLookup = source.indexOf("messageFrom(I18N, localeState.lang, key)");
  const extensionLookup = source.indexOf("messageFrom(i18nExtensions, localeState.lang, key)");
  assert.ok(coreLookup >= 0, "core i18n lookup not found");
  assert.ok(extensionLookup > coreLookup, "extension i18n lookup must happen after core lookup");
});
