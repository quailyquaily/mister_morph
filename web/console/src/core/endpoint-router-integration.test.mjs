import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const routerSource = new URL("../router/index.js", import.meta.url);
const appShellSource = new URL("../composables/useAppShell.js", import.meta.url);
const appLayoutSource = new URL("../layouts/AppLayout.js", import.meta.url);

test("endpoint-owned pages are mounted under the endpoint route scope", async () => {
  const source = await readFile(routerSource, "utf8");

  for (const suffix of [
    "/chat",
    "/chat/:topic_id",
    "/contacts",
    "/todo",
    "/stats",
    "/audit",
    "/logs",
    "/settings",
    "/settings/:section",
    "/setup",
    "/setup/llm",
    "/setup/persona",
    "/setup/soul",
    "/setup/done",
    "/setup/repair",
  ]) {
    assert.ok(
      source.includes(`\`\${ENDPOINT_SCOPE_PATH}${suffix}\``),
      `missing scoped route ${suffix}`,
    );
  }
  assert.match(source, /endpointRefFromRouteParam\(to\.params\.endpoint_ref\)/);
  assert.match(source, /endpointState\.setSelectedEndpointRef\(requestedEndpointRef\)/);
});

test("old endpoint-owned paths redirect to the local console scope", async () => {
  const source = await readFile(routerSource, "utf8");

  assert.match(source, /legacyEndpointRedirect\("\/todo"\)/);
  assert.match(source, /legacyEndpointRedirect\("\/chat\/:topic_id"\)/);
});

test("agent switching navigates through the route and endpoint changes remount the page", async () => {
  const [shell, layout] = await Promise.all([
    readFile(appShellSource, "utf8"),
    readFile(appLayoutSource, "utf8"),
  ]);

  assert.match(shell, /endpointSwitchPath\(/);
  assert.match(shell, /router\.push\(nextPath\)/);
  assert.doesNotMatch(
    shell,
    /function onEndpointChange[\s\S]*?endpointState\.setSelectedEndpointRef[\s\S]*?\n  }/,
  );
  assert.match(layout, /<RouterView :key="endpointViewKey" \/>/);
});
