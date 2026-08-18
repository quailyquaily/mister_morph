import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function source(path) {
  return readFile(new URL(path, import.meta.url), "utf8");
}

test("AgentChatPane wires the complete ChatComposer contract", async () => {
  const pane = await source("../components/AgentChatPane.js");

  assert.equal(pane.includes(':showAddActions="false"'), false);
  assert.equal(pane.includes(':maxRows="8"'), false);
  assert.match(pane, /:attach-active="composerAttachActive"/u);
  assert.match(pane, /:attach-disabled="composerAddDisabled"/u);
  assert.match(pane, /:file-items="composerFiles"/u);
  assert.match(pane, /:commands="composerCommands"/u);
  assert.match(pane, /:skills="composerSkills"/u);
  assert.match(pane, /v-model:llm-profile-value="composerLLMProfile"/u);
  assert.match(pane, /@request-commands="ensureComposerCommandsLoaded"/u);
  assert.match(pane, /@request-skills="ensureComposerSkillsLoaded"/u);
  assert.match(pane, /@attach="openComposerWorkspaceBrowser"/u);
  assert.match(pane, /@upload="openComposerFilePicker"/u);
  assert.match(pane, /ref="composerFileInput"/u);
});

test("AgentChatPane submits through the shared full composer contract", async () => {
  const pane = await source("../components/AgentChatPane.js");

  assert.match(pane, /buildComposerSubmission\(\{/u);
  assert.match(pane, /llmProfile,/u);
  assert.match(pane, /files: composerFiles\.value,/u);
  assert.match(pane, /workspaceDir: topicsSupported\.value \? pendingWorkspace : "",/u);
});

test("AgentChatPane does not restyle ChatComposer as a compact variant", async () => {
  const styles = await source("../components/AgentChatPane.css");

  assert.equal(styles.includes(".agent-chat-pane > .chat-composer"), false);
  assert.equal(styles.includes(".chat-composer-gradient-blur"), false);
  assert.equal(styles.includes(".chat-composer-grid"), false);
});
