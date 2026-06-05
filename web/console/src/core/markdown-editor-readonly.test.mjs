import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownEditorSource = new URL("../components/MarkdownEditor.js", import.meta.url);

test("MarkdownEditor exposes readOnly separately from disabled", async () => {
  const source = await readFile(markdownEditorSource, "utf8");

  assert.match(source, /readOnly:\s*\{\s*type:\s*Boolean,\s*default:\s*false,\s*\}/);
  assert.match(source, /const readOnly = disabled \|\| props\.readOnly === true;/);
  assert.match(source, /textarea\.disabled = disabled;/);
  assert.match(source, /textarea\.readOnly = readOnly;/);
  assert.match(source, /readOnly:\s*props\.disabled \|\| props\.readOnly,/);
  assert.match(source, /\(\)\s*=>\s*props\.readOnly,\s*\(\)\s*=>\s*\{\s*applyTextareaState\(\);\s*\}/);
});
