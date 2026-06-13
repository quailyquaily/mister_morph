import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const runtimeViewSource = new URL("../views/RuntimeView.js", import.meta.url);

test("runtime overview renders all supported channel statuses", async () => {
  const source = await readFile(runtimeViewSource, "utf8");

  assert.match(source, /channel_line_configured:\s*false/);
  assert.match(source, /channel_lark_configured:\s*false/);
  assert.match(source, /channel_running_line:\s*false/);
  assert.match(source, /channel_running_lark:\s*false/);
  assert.match(source, /key:\s*"line"[\s\S]*title:\s*t\("endpoint_channel_line"\)/);
  assert.match(source, /key:\s*"lark"[\s\S]*title:\s*t\("endpoint_channel_lark"\)/);
  assert.match(source, /const larkRunning = toBool\(channel\.lark_running, false\) \|\| runningChannel === "lark";/);
  assert.match(source, /overview\.channel_lark_configured = toBool\(channel\.lark_configured, false\) \|\| larkRunning;/);
});
