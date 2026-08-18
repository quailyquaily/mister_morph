import assert from "node:assert/strict";
import test from "node:test";

import {
  endpointPagePath,
  endpointRefFromRouteParam,
  endpointRoutePath,
  endpointRouteRef,
  endpointSwitchPath,
} from "./endpoint-routes.js";

test("local console uses the reserved default route ref", () => {
  assert.equal(endpointRouteRef("ep_console_local"), "default");
  assert.equal(endpointRefFromRouteParam("default"), "ep_console_local");
  assert.equal(endpointRoutePath("ep_console_local", "/todo"), "/e/default/todo");
});

test("remote endpoints keep their endpoint ref in scoped paths", () => {
  assert.equal(endpointRouteRef("ep_remote_b"), "ep_remote_b");
  assert.equal(endpointRefFromRouteParam("ep_remote_b"), "ep_remote_b");
  assert.equal(endpointRoutePath("ep_remote_b", "/settings/channels"), "/e/ep_remote_b/settings/channels");
});

test("endpoint page paths can be retained while switching agents", () => {
  assert.equal(endpointPagePath("/e/default/memory"), "/memory");
  assert.equal(endpointPagePath("/e/ep_remote_b/chat/topic_123"), "/chat/topic_123");
  assert.equal(endpointPagePath("/overview"), "");
  assert.equal(endpointPagePath("/e/default/setup/llm"), "/setup/llm");
  assert.equal(endpointPagePath("/chat/desk"), "");
});

test("switching agents retains the page but uses the target agent's chat topic", () => {
  assert.equal(
    endpointSwitchPath("ep_remote_b", "/e/default/settings/channels"),
    "/e/ep_remote_b/settings/channels",
  );
  assert.equal(
    endpointSwitchPath("ep_remote_b", "/e/default/chat/topic_from_a", "topic_from_b"),
    "/e/ep_remote_b/chat/topic_from_b",
  );
  assert.equal(endpointSwitchPath("ep_remote_b", "/overview", "topic_from_b"), "/e/ep_remote_b/chat/topic_from_b");
});
