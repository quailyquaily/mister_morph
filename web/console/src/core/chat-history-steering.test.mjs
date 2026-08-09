import assert from "node:assert/strict";
import test from "node:test";

import { placeSteeredAgentsAfterUsers } from "./chat-history-steering.js";

test("places a target agent after its steering user across history pages", () => {
  const items = [
    { id: "target:user", role: "user" },
    { id: "target:agent", role: "agent", taskId: "target" },
    {
      id: "steer:user",
      role: "user",
      steerTargetTaskID: "target",
    },
  ];

  assert.deepEqual(
    placeSteeredAgentsAfterUsers(items).map((item) => item.id),
    ["target:user", "steer:user", "target:agent"]
  );
});
