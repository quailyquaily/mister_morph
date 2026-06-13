import assert from "node:assert/strict";
import test from "node:test";

import {
  COMPOSER_COMMAND_SUGGESTIONS,
  buildComposerSuggestionItems,
  composerHighlightSegments,
  composerSuggestionInsertText,
  composerTriggerContext,
  normalizeComposerCommandItems,
  normalizeComposerSkillItems,
  replaceComposerSuggestionToken,
} from "./chat-composer-suggestions.js";

test("composerTriggerContext detects slash and skill tokens at the cursor", () => {
  assert.deepEqual(composerTriggerContext("/", 1), {
    type: "command",
    trigger: "/",
    query: "",
    start: 0,
    end: 1,
  });
  assert.deepEqual(composerTriggerContext("ask /pla", 8), {
    type: "command",
    trigger: "/",
    query: "pla",
    start: 4,
    end: 8,
  });
  assert.deepEqual(composerTriggerContext("use $imagegen", 13), {
    type: "skill",
    trigger: "$",
    query: "imagegen",
    start: 4,
    end: 13,
  });

  assert.equal(composerTriggerContext("plain/", 6), null);
  assert.equal(composerTriggerContext("/plan now", 9), null);
});

test("normalizeComposerSkillItems dedupes skills and prefers ids for insertion", () => {
  const items = normalizeComposerSkillItems([
    { id: "imagegen", name: "Image Gen", description: "Create images" },
    { id: "imagegen", name: "Duplicate", description: "ignored" },
    { id: "", name: "openai-docs", description: "Docs" },
    null,
  ]);

  assert.deepEqual(items, [
    {
      key: "skill:imagegen",
      type: "skill",
      value: "imagegen",
      title: "$imagegen",
      description: "Create images",
      insertText: "$imagegen ",
    },
    {
      key: "skill:openai-docs",
      type: "skill",
      value: "openai-docs",
      title: "$openai-docs",
      description: "Docs",
      insertText: "$openai-docs ",
    },
  ]);
});

test("normalizeComposerCommandItems accepts backend command payloads", () => {
  const items = normalizeComposerCommandItems([
    {
      value: "/models list",
      title: "/models list",
      description: "List model profiles",
      insert_text: "/models list ",
    },
    {
      value: "/models list",
      title: "Duplicate",
      description: "ignored",
      insert_text: "/models list ",
    },
    {
      value: "/workspace attach",
      title: "/workspace attach <dir>",
      description: "Attach workspace",
    },
    {
      value: "review",
      title: "not a slash command",
    },
    null,
  ]);

  assert.deepEqual(items, [
    {
      key: "command:models:list",
      type: "command",
      value: "/models list",
      title: "/models list",
      description: "List model profiles",
      insertText: "/models list ",
    },
    {
      key: "command:workspace:attach",
      type: "command",
      value: "/workspace attach",
      title: "/workspace attach <dir>",
      description: "Attach workspace",
      insertText: "/workspace attach ",
    },
  ]);
});

test("buildComposerSuggestionItems filters command and skill candidates", () => {
  const skills = normalizeComposerSkillItems([
    { id: "imagegen", name: "Image Gen", description: "Create images" },
    { id: "openai-docs", name: "OpenAI Docs", description: "API reference" },
  ]);

  const commandItems = buildComposerSuggestionItems({
    context: { type: "command", query: "mod" },
    skills,
  });
  assert.equal(commandItems[0].insertText, "/models ");
  assert.ok(commandItems.every((item) => item.type === "command"));

  const skillItems = buildComposerSuggestionItems({
    context: { type: "skill", query: "imag" },
    skills,
  });
  assert.deepEqual(skillItems.map((item) => item.value), ["imagegen"]);
});

test("buildComposerSuggestionItems can use commands loaded from the backend", () => {
  const commands = normalizeComposerCommandItems([
    { value: "/custom", title: "/custom", description: "Backend command", insert_text: "/custom " },
  ]);

  const commandItems = buildComposerSuggestionItems({
    context: { type: "command", query: "cus" },
    commands,
  });

  assert.deepEqual(commandItems.map((item) => item.value), ["/custom"]);
});

test("composer command suggestions are backed by runtime commands", () => {
  const values = COMPOSER_COMMAND_SUGGESTIONS.map((item) => item.value);

  assert.deepEqual(values.slice(0, 7), [
    "/help",
    "/stop",
    "/models",
    "/skills",
    "/ctx",
    "/workspace",
    "/think",
  ]);
  assert.ok(values.includes("/models list"));
  assert.ok(values.includes("/workspace attach"));
  assert.equal(values.includes("/plan"), false);
  assert.equal(values.includes("/review"), false);
  assert.equal(values.includes("/fix"), false);
  assert.ok(COMPOSER_COMMAND_SUGGESTIONS.every((item) => item.insertText.endsWith(" ")));
});

test("replaceComposerSuggestionToken replaces the active token without doubling spaces", () => {
  const suggestion = COMPOSER_COMMAND_SUGGESTIONS.find((item) => item.value === "/think");
  assert.ok(suggestion);

  assert.equal(
    replaceComposerSuggestionToken("ask /thi later", { start: 4, end: 8 }, suggestion.insertText),
    "ask /think later"
  );
  assert.equal(
    replaceComposerSuggestionToken("$img", { start: 0, end: 4 }, "$imagegen "),
    "$imagegen "
  );
});

test("composer suggestion insertion always keeps one trailing space", () => {
  assert.equal(composerSuggestionInsertText("/think"), "/think ");
  assert.equal(composerSuggestionInsertText("$imagegen "), "$imagegen ");
  assert.equal(composerSuggestionInsertText(""), "");

  assert.equal(
    replaceComposerSuggestionToken("ask /thi later", { start: 4, end: 8 }, "/think"),
    "ask /think later"
  );
  assert.equal(
    replaceComposerSuggestionToken("$img", { start: 0, end: 4 }, "$imagegen"),
    "$imagegen "
  );
});

test("composerHighlightSegments marks slash commands and skill tokens", () => {
  const commands = normalizeComposerCommandItems([
    { value: "/models list", title: "/models list", insert_text: "/models list " },
    { value: "/think", title: "/think", insert_text: "/think " },
  ]);
  const skills = normalizeComposerSkillItems([
    { id: "imagegen", name: "Image Gen" },
  ]);

  assert.deepEqual(
    composerHighlightSegments({
      text: "/models list with $imagegen and plain/",
      commands,
      skills,
    }),
    [
      { type: "command", text: "/models list" },
      { type: "text", text: " with " },
      { type: "skill", text: "$imagegen" },
      { type: "text", text: " and plain/" },
    ]
  );

  assert.deepEqual(
    composerHighlightSegments({ text: "ask /thi then $ope" }),
    [
      { type: "text", text: "ask " },
      { type: "command", text: "/thi" },
      { type: "text", text: " then " },
      { type: "skill", text: "$ope" },
    ]
  );
});
