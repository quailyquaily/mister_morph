import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const settingsViewSource = new URL("../views/SettingsView.js", import.meta.url);

test("Settings uses one selected endpoint ref for endpoint-owned settings", async () => {
  const source = await readFile(settingsViewSource, "utf8");

  assert.match(source, /const settingsEndpointRef = computed\(/);
  assert.doesNotMatch(source, /agentSettingsEndpointRef|personaSettingsEndpointRef/);
  assert.match(source, /const consoleEndpointRef = computed\(/);
  assert.match(source, /watch\(consoleEndpointRef,/);
  assert.equal(source.match(/getEndpointRef: \(\) => settingsEndpointRef\.value/g)?.length, 2);
  assert.equal(source.match(/return endpointApiFetch\(endpointRef, path, options\)/g)?.length, 2);
});

for (const provider of ["XAI", "Pro"]) {
  test(`${provider} auth keeps login polling on its starting endpoint`, async () => {
    const source = await readFile(new URL(`../composables/use${provider}AuthFlow.js`, import.meta.url), "utf8");
    const prefix = provider === "XAI" ? "xai" : "pro";

    assert.match(source, /const getEndpointRef =/);
    assert.match(source, new RegExp(`let ${prefix}LoginEndpointRef = ""`));
    assert.match(source, new RegExp(`request\\(\\s*"/auth/${prefix}/login/start",[\\s\\S]*?, targetEndpointRef\\)`));
    assert.match(source, new RegExp(`request\\(\\s*"/auth/${prefix}/login/poll",[\\s\\S]*?,\\s*targetEndpointRef\\s*\\)`));
    assert.match(source, /targetEndpointRef !== currentEndpointRef\(\)/);
  });
}
