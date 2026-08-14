import assert from "node:assert/strict";
import test from "node:test";

import {
  isTerminalStatus,
  normalizeTaskStatus,
  taskAgentText,
  taskListHistoryItems,
} from "./chat-task-history.js";

function t(key, vars = {}) {
  const messages = {
    chat_approval_waiting_text: "Waiting for approval.",
    chat_approval_denied: "Denied.",
    chat_approval_expired: "Expired.",
    chat_result_empty: "No output.",
    chat_polling_hint: `${vars.name || "Agent"} is working...`,
    chat_agent_name_fallback: "Agent",
    chat_duration_hour: `${vars.value} hour`,
    chat_duration_hours: `${vars.value} hours`,
    chat_duration_minute: `${vars.value} min`,
    chat_duration_second: `${vars.value} sec`,
    chat_task_duration_thought: `Thought for ${vars.duration}`,
  };
  return messages[key] || key;
}

test("taskListHistoryItems builds reusable endpoint-scoped chat items", () => {
  const items = taskListHistoryItems(
    [
      {
        id: "task_2",
        topic_id: "topic_b",
        task: "Second",
        status: "done",
        created_at: "2026-08-14T02:00:00Z",
        started_at: "2026-08-14T02:00:01Z",
        finished_at: "2026-08-14T02:00:03Z",
        result: {
          final: { output: "Done" },
          reasoning: "Checked both sources.",
          plan: { steps: [{ step: "Check", status: "completed" }] },
          activity: {
            history: [{ id: "tool_1", kind: "tool", name: "read_file", status: "done" }],
          },
        },
      },
      {
        id: "task_1",
        topic_id: "topic_b",
        task: "First",
        status: "done",
        created_at: "2026-08-14T01:00:00Z",
        result: { final: { output: "Earlier" } },
      },
    ],
    t,
    {
      endpointRef: "ep_remote",
      agentName: "Momo",
      locale: "en-US",
      now: new Date("2026-08-14T03:00:00Z"),
    }
  );

  assert.deepEqual(
    items.map((item) => item.id),
    ["task_1:user", "task_1:agent", "task_2:user", "task_2:agent"]
  );
  assert.equal(items[2].endpointRef, "ep_remote");
  assert.equal(items[2].topicID, "topic_b");
  assert.equal(items[3].text, "Done");
  assert.equal(items[3].reasoning, "Checked both sources.");
  assert.deepEqual(items[3].plan, {
    steps: [{ step: "Check", status: "completed" }],
  });
  assert.equal(items[3].activity.current.name, "read_file");
});

test("task status helpers and taskAgentText keep runtime semantics", () => {
  assert.equal(normalizeTaskStatus("RUNNING"), "running");
  assert.equal(normalizeTaskStatus("unknown"), "queued");
  assert.equal(isTerminalStatus("done"), true);
  assert.equal(isTerminalStatus("pending"), false);
  assert.equal(
    taskAgentText(
      {
        id: "task_pending",
        status: "running",
      },
      t,
      { agentName: "Momo", pendingText: "Keep the streamed text." }
    ),
    "Keep the streamed text."
  );
});
