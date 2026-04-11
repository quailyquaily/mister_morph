#!/usr/bin/env node

import process from "node:process";

const args = process.argv.slice(2);
const promptIndex = args.findIndex((arg) => arg === "-p" || arg === "--print");
const prompt = promptIndex >= 0 ? args[promptIndex + 1] ?? "" : "";

let result = "Hello";
if (prompt.includes("exactly")) {
  const marker = prompt.split("exactly").pop()?.trim() ?? "";
  result = marker.replace(/^[:\s"]+/, "").replace(/[".\s]+$/, "") || result;
}

console.log(
  JSON.stringify({
    type: "system",
    subtype: "init",
    session_id: "fake-claude-session",
    tools: ["Read", "Edit", "Write", "Bash"],
    permissionMode: "dontAsk"
  })
);
console.log(
  JSON.stringify({
    type: "stream_event",
    event: {
      delta: {
        type: "text_delta",
        text: result.slice(0, Math.max(1, Math.floor(result.length / 2)))
      }
    }
  })
);
console.log(
  JSON.stringify({
    type: "assistant",
    message: {
      content: [{ type: "text", text: result }]
    }
  })
);
console.log(
  JSON.stringify({
    type: "result",
    subtype: "success",
    is_error: false,
    result,
    stop_reason: "stop_sequence",
    session_id: "fake-claude-session"
  })
);
