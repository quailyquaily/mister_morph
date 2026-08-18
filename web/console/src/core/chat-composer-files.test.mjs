import assert from "node:assert/strict";
import test from "node:test";

import {
  buildComposerSubmission,
  composerFileDraftKey,
  composerFileExtension,
  composerFileReferences,
} from "./chat-composer-files.js";

test("composer submission keeps the full task metadata contract", () => {
  const readyFile = {
    id: "ready",
    status: "ready",
    dirName: "file_cache_dir",
    path: "notes.txt",
  };
  const result = buildComposerSubmission({
    task: " Ask the agent ",
    llmProfile: " quality ",
    files: [readyFile, { id: "pending", status: "uploading" }],
    topicID: " topic-1 ",
    workspaceDir: " /workspace ",
  });

  assert.deepEqual(result.requestBody, {
    task: "Ask the agent",
    llm_profile: "quality",
    file_references: [{ dir_name: "file_cache_dir", path: "notes.txt" }],
    topic_id: "topic-1",
    workspace_dir: "/workspace",
  });
  assert.deepEqual(result.submittedFiles, [readyFile]);
  assert.deepEqual(result.fileReferences, [
    { dir_name: "file_cache_dir", path: "notes.txt" },
  ]);
});

test("composer file drafts are scoped by endpoint and topic", () => {
  assert.equal(
    composerFileDraftKey({ endpointRef: " ep_remote_a ", topicID: " topic-1 " }),
    "ep_remote_a\ntopic-1"
  );
  assert.equal(composerFileDraftKey({ endpointRef: "", topicID: "topic-1" }), "");
});

test("composer file references include only ready runtime files", () => {
  assert.deepEqual(
    composerFileReferences([
      { status: "ready", dirName: "file_cache_dir", path: " a.txt " },
      { status: "ready", dir_name: "workspace_dir", path: " docs/b.md " },
      { status: "uploading", dirName: "file_cache_dir", path: "pending.txt" },
      { status: "ready", dirName: "elsewhere", path: "ignored.txt" },
      { status: "ready", dirName: "file_cache_dir", path: "" },
    ]),
    [
      { dir_name: "file_cache_dir", path: "a.txt" },
      { dir_name: "workspace_dir", path: "docs/b.md" },
    ]
  );
});

test("composer file extension is normalized", () => {
  assert.equal(composerFileExtension(" Report.PDF "), ".pdf");
  assert.equal(composerFileExtension("README"), "");
});
