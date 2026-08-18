import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function source(path) {
  return readFile(new URL(path, import.meta.url), "utf8");
}

test("ChatView uses the shared ChatComposer for both placeholder and chat input", async () => {
  const view = await source("../views/ChatView.js");

  assert.match(view, /import ChatComposer from "\.\.\/components\/ChatComposer";/u);
  assert.match(view, /components:\s*\{[^}]*ChatComposer/us);
  assert.equal((view.match(/<ChatComposer\b/gu) || []).length, 2);
  assert.equal(view.includes('ref="composerField"'), false);
  assert.equal(view.includes("<QTextarea"), false);
});

test("ChatComposer owns textarea behavior and exposes imperative hooks", async () => {
  const component = await source("../components/ChatComposer.js");
  const styles = await source("../components/ChatComposer.css");

  assert.match(component, /from "\.\.\/core\/chat-composer-suggestions"/u);
  assert.match(component, /composerHighlightSegments/u);
  assert.match(component, /expose\(\{\s*focus,\s*insertText,\s*syncHeight,/us);
  assert.match(component, /commands:\s*\{/u);
  assert.match(component, /emit\("requestCommands"\)/u);
  assert.match(component, /emit\("requestSkills"\)/u);
  assert.match(component, /emit\("submit"\)/u);
  assert.match(component, /class="chat-composer-grid"/u);
  assert.match(component, /class="chat-composer-input-shell"/u);
  assert.match(component, /class="chat-composer-highlight"/u);
  assert.match(component, /<textarea/u);
  assert.match(component, /@input="handleInput"/u);
  assert.match(component, /@scroll="handleInputScroll"/u);
  assert.match(component, /:value="inputValue"/u);
  assert.equal(component.includes("<QTextarea"), false);
  assert.equal(component.includes("chat-composer-suggestions-head"), false);
  assert.equal(styles.includes("q-textarea"), false);
  assert.equal(styles.includes("chat-composer-suggestions-head"), false);
});

test("ChatComposer resyncs wrapping when its rendered width changes", async () => {
  const component = await source("../components/ChatComposer.js");

  assert.match(component, /new ResizeObserver/u);
  assert.match(component, /onUnmounted\(/u);
  assert.match(component, /ref="composerRoot"/u);
  assert.match(component, /syncHeight\(\)/u);
});

test("ChatComposer measures empty and one-line content from the single-row baseline", async () => {
  const component = await source("../components/ChatComposer.js");
  const syncHeightStart = component.indexOf("async function syncHeight(rawValue = props.modelValue)");
  const firstNextTick = component.indexOf("await nextTick();", syncHeightStart);
  const textNormalization = component.indexOf("const text = normalizedText(rawValue);", syncHeightStart);
  const baselineMode = component.indexOf('const baselineSingleLine = text === "" || !text.includes("\\n");', syncHeightStart);

  assert.match(component, /async function syncHeight\(rawValue = props\.modelValue\)/u);
  assert.match(component, /const DEFAULT_MAX_ROWS = 24;/u);
  assert.match(component, /function singleLineTextareaMetrics\(field\)/u);
  assert.match(component, /const text = normalizedText\(rawValue\)/u);
  assert.ok(textNormalization > syncHeightStart && textNormalization < firstNextTick);
  assert.ok(baselineMode > textNormalization && baselineMode < firstNextTick);
  assert.match(component, /if \(text === ""\)/u);
  assert.match(component, /const metrics = singleLineTextareaMetrics\(field\);/u);
  assert.match(component, /syncHeight\(nextValue\)/u);
  assert.equal(component.includes("requestAnimationFrame(syncHeight)"), false);
});

test("ChatComposer keeps the multi-row action rail on a fixed control row", async () => {
  const styles = await source("../components/ChatComposer.css");

  assert.match(styles, /--chat-composer-control-size:\s*38px;/u);
  assert.match(styles, /grid-template-rows:\s*minmax\(0,\s*1fr\)\s*var\(--chat-composer-control-size\);/u);
  assert.match(styles, /\.chat-composer\.is-multi-row \.chat-composer-grid\s*\{[^}]*padding:\s*12px 10px 8px;/us);
  assert.match(styles, /\.chat-composer\.is-multi-row \.chat-composer-input-shell\s*\{[^}]*align-self:\s*start;/us);
  assert.match(styles, /\.chat-composer\.is-multi-row \.chat-composer-toolbar-start,\s*\n\.chat-composer\.is-multi-row \.chat-composer-actions\s*\{[^}]*min-height:\s*var\(--chat-composer-control-size\);/us);
});

test("ChatComposer renders a passive mirror layer for token highlights", async () => {
  const styles = await source("../components/ChatComposer.css");

  assert.match(styles, /\.chat-composer-input-shell\s*\{[^}]*position:\s*relative;/us);
  assert.match(styles, /\.chat-composer-highlight\s*\{[^}]*position:\s*absolute;[^}]*pointer-events:\s*none;/us);
  assert.match(styles, /\.chat-composer-highlight-token\.is-command\s*\{/u);
  assert.match(styles, /\.chat-composer-highlight-token\.is-skill\s*\{/u);
});

test("ChatView reserves the measured composer height for the desktop overlay", async () => {
  const view = await source("../views/ChatView.js");

  assert.match(view, /const composerHeight = ref\(/u);
  assert.match(view, /--chat-overlay-compose-h/u);
  assert.match(view, /function updateComposerHeight/u);
  assert.match(view, /:style="chatMainStyle"/u);
  assert.equal((view.match(/@height-change="updateComposerHeight"/gu) || []).length, 2);
});

test("ChatView loads backend command suggestions for both composers", async () => {
  const view = await source("../views/ChatView.js");

  assert.match(view, /normalizeComposerCommandItems/u);
  assert.match(view, /const composerCommands = shallowRef\(\[\]\);/u);
  assert.match(view, /async function ensureComposerCommandsLoaded\(\)/u);
  assert.match(view, /"\/commands"/u);
  assert.equal((view.match(/:commands="composerCommands"/gu) || []).length, 2);
  assert.equal((view.match(/@request-commands="ensureComposerCommandsLoaded"/gu) || []).length, 2);
});

test("Chat composer exposes a per-task LLM profile picker only when named profiles exist", async () => {
  const component = await source("../components/ChatComposer.js");
  const view = await source("../views/ChatView.js");

  assert.match(component, /llmProfileItems:\s*\{/u);
  assert.match(component, /llmProfileValue:\s*\{/u);
  assert.match(component, /"update:llmProfileValue"/u);
  assert.match(component, /<QDropdownMenu/u);
  assert.match(component, /v-if="llmProfileItems\.length > 1"/u);
  assert.match(component, /emit\("update:llmProfileValue"/u);

  assert.match(view, /normalizeChatLLMProfiles/u);
  assert.match(view, /const composerLLMProfile = ref\(""\);/u);
  assert.match(view, /await runtimeApiFetchForEndpoint\(endpointRef, "\/llm\/profiles"\);/u);
  assert.equal((view.match(/v-model:llm-profile-value="composerLLMProfile"/gu) || []).length, 2);
  assert.equal((view.match(/:llm-profile-items="composerLLMProfileItems"/gu) || []).length, 2);
  assert.match(view, /buildComposerSubmission\(\{/u);
  assert.match(view, /llmProfile,/u);
});

test("ChatView restores the latest available LLM profile for each topic", async () => {
  const view = await source("../views/ChatView.js");

  assert.match(view, /lastUsedChatLLMProfile/u);
  assert.match(view, /resolveAvailableChatLLMProfile/u);
  assert.match(view, /const composerTopicLLMProfile = ref\(""\);/u);
  assert.match(view, /applyComposerTopicLLMProfile\(lastUsedChatLLMProfile\(tasks\)\);/u);
  assert.match(
    view,
    /composerLLMProfiles\.value = profiles;\s*syncComposerLLMProfile\(\);/u
  );
});

test("ChatView defaults the last agent status to duration without overriding manual toggles", async () => {
  const view = await source("../views/ChatView.js");

  assert.match(view, /function applyDefaultHistoryDurationVisibility\(items\)/u);
  assert.match(view, /lastAgentIndex/u);
  assert.match(view, /durationVisibleManual/u);
  assert.match(view, /if \(item\?\.durationVisibleManual === true\)/u);
  assert.match(view, /durationVisible:\s*item\?\.durationVisible === true/u);
  assert.match(view, /durationVisible:\s*defaultDurationVisible/u);
});
