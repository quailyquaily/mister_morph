import assert from "node:assert/strict";
import test from "node:test";

import { approvalDetailsByID, approvalParameterEntries } from "./chat-approvals.js";

test("approvalDetailsByID preserves complete tool parameters", () => {
  const command = "printf 'approval details'; ".repeat(24);
  const details = approvalDetailsByID({
    items: [
      {
        approval_request_id: "apr_1",
        tool_name: "bash",
        reasons: ["bash_requires_approval"],
        tool_params: {
          cmd: command,
          cwd: "/srv/morph",
          timeout_seconds: 180,
        },
      },
    ],
  });

  assert.deepEqual(details.get("apr_1"), {
    approvalRequestID: "apr_1",
    toolName: "bash",
    reasons: ["bash_requires_approval"],
    toolParams: {
      cmd: command,
      cwd: "/srv/morph",
      timeout_seconds: 180,
    },
  });
});

test("approvalParameterEntries keeps command first and formats all values", () => {
  assert.deepEqual(
    approvalParameterEntries({
      cwd: "/srv/morph",
      run_in_subtask: true,
      cmd: "echo one\necho two",
    }),
    [
      { name: "cmd", value: "echo one\necho two", command: true, multiline: true },
      { name: "cwd", value: "/srv/morph", command: false, multiline: false },
      { name: "run_in_subtask", value: "true", command: false, multiline: false },
    ]
  );
});
