import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const contextSource = new URL("./context.js", import.meta.url);

test("remote endpoint 401 does not clear the Console session", async () => {
  const source = await readFile(contextSource, "utf8");
  const guardedUnauthorizedChecks = source.match(
    /resp\.status === 401 && !options\.noAuth && resp\.headers\.get\("X-MisterMorph-Proxy-Upstream"\) !== "1"/g,
  );

  assert.equal(guardedUnauthorizedChecks?.length, 2);
});
