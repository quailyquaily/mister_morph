import assert from "node:assert/strict";
import test from "node:test";

import {
  clearResourceStateForTests,
  invalidateResource,
  loadResource,
  resourceKey,
} from "./resources.js";

test("loadResource deduplicates concurrent loads for the same key", async () => {
  clearResourceStateForTests();
  let calls = 0;
  const key = resourceKey("tasks", "list", "endpoint-a");

  const loader = async () => {
    calls += 1;
    await new Promise((resolve) => setTimeout(resolve, 10));
    return { ok: true };
  };

  const [left, right] = await Promise.all([loadResource(key, loader), loadResource(key, loader)]);

  assert.equal(calls, 1);
  assert.equal(left, right);
  assert.deepEqual(left, { ok: true });
});

test("loadResource can cache in memory until forced or invalidated", async () => {
  clearResourceStateForTests();
  let calls = 0;
  const key = resourceKey("setup", "integrity");
  const loader = async () => {
    calls += 1;
    return { calls };
  };

  assert.deepEqual(await loadResource(key, loader, { cache: true }), { calls: 1 });
  assert.deepEqual(await loadResource(key, loader, { cache: true }), { calls: 1 });
  assert.deepEqual(await loadResource(key, loader, { force: true, cache: true }), { calls: 2 });

  invalidateResource(resourceKey("setup"));
  assert.deepEqual(await loadResource(key, loader, { cache: true }), { calls: 3 });
});

test("loadResource does not cache stale values after invalidation", async () => {
  clearResourceStateForTests();
  const key = resourceKey("setup", "integrity");
  let resolveFirst = null;

  const firstLoad = loadResource(
    key,
    () =>
      new Promise((resolve) => {
        resolveFirst = resolve;
      }),
    { cache: true }
  );

  invalidateResource(resourceKey("setup"));

  let calls = 0;
  const fresh = await loadResource(
    key,
    async () => {
      calls += 1;
      return { stale: false };
    },
    { cache: true }
  );

  resolveFirst({ stale: true });
  assert.deepEqual(await firstLoad, { stale: true });

  assert.equal(calls, 1);
  assert.deepEqual(fresh, { stale: false });
  assert.deepEqual(await loadResource(key, () => ({ missedCache: true }), { cache: true }), {
    stale: false,
  });
});
