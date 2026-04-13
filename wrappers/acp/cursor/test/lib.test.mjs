import test from "node:test";
import assert from "node:assert/strict";

import { buildBackendArgs, resolveAgentCommand } from "../src/lib.mjs";

test("buildBackendArgs appends acp and respects MISTERMORPH_CURSOR_ARGS", async (t) => {
  t.afterEach(() => {
    delete process.env.MISTERMORPH_CURSOR_ARGS;
  });

  process.env.MISTERMORPH_CURSOR_ARGS = "";
  assert.deepEqual(buildBackendArgs(), ["acp"]);

  process.env.MISTERMORPH_CURSOR_ARGS = "-e https://api.example.test";
  assert.deepEqual(buildBackendArgs(), ["-e", "https://api.example.test", "acp"]);
});

test("resolveAgentCommand respects env and options", async (t) => {
  t.afterEach(() => {
    delete process.env.MISTERMORPH_CURSOR_COMMAND;
  });

  delete process.env.MISTERMORPH_CURSOR_COMMAND;
  assert.equal(resolveAgentCommand(), "agent");
  assert.equal(resolveAgentCommand({ command: "/opt/cursor/bin/agent" }), "/opt/cursor/bin/agent");

  process.env.MISTERMORPH_CURSOR_COMMAND = "my-agent";
  assert.equal(resolveAgentCommand(), "my-agent");
});
