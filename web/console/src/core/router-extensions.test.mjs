import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const routerSource = new URL("../router/index.js", import.meta.url);
const routeExtensionsSource = new URL("./route-extensions.js", import.meta.url);

test("router mounts extension routes before the root redirect", async () => {
  const source = await readFile(routerSource, "utf8");

  assert.match(source, /import\s+\{\s*routeExtensions\s*\}\s+from "\.\.\/core\/route-extensions";/);
  assert.match(source, /const extensionRoutes = Array\.isArray\(routeExtensions\.routes\) \? routeExtensions\.routes : \[\];/);
  assert.match(
    source,
    /const extensionSetupFreePaths = Array\.isArray\(routeExtensions\.setupFreePaths\)\s*\?\s*routeExtensions\.setupFreePaths\s*:\s*\[\];/
  );

  const extensionIndex = source.indexOf("...extensionRoutes");
  const rootIndex = source.indexOf('{ path: "/", component: RootRedirectView');
  assert.notEqual(extensionIndex, -1, "extension routes spread not found");
  assert.notEqual(rootIndex, -1, "root redirect route not found");
  assert.ok(extensionIndex < rootIndex, "extension routes must be mounted before root redirect");
});

test("router includes extension setup-free paths in setup guard allowlist", async () => {
  const source = await readFile(routerSource, "utf8");

  const setupFreePathsStart = source.indexOf("const SETUP_FREE_PATHS = new Set([");
  assert.notEqual(setupFreePathsStart, -1, "SETUP_FREE_PATHS not found");
  const setupFreePathsEnd = source.indexOf("]);", setupFreePathsStart);
  assert.notEqual(setupFreePathsEnd, -1, "SETUP_FREE_PATHS end not found");
  const setupFreePathsBlock = source.slice(setupFreePathsStart, setupFreePathsEnd);

  assert.match(setupFreePathsBlock, /\.\.\.extensionSetupFreePaths/);
});

test("core route extensions forward to the legacy extension route entry", async () => {
  const source = await readFile(routeExtensionsSource, "utf8");

  assert.match(source, /export\s+\{\s*routeExtensions\s*\}\s+from "\.\.\/ext\/routes";/);
});

test("legacy extension route entry exports empty route and setup-free path arrays", async () => {
  const { routeExtensions } = await import("../ext/routes/index.js");

  assert.deepEqual(routeExtensions, {
    routes: [],
    setupFreePaths: [],
  });
});
