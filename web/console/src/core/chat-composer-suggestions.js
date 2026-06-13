const COMPOSER_SUGGESTION_LIMIT = 8;

const COMPOSER_COMMAND_SUGGESTIONS = [
  {
    key: "command:help",
    type: "command",
    value: "/help",
    title: "/help",
    description: "Show available runtime commands.",
    insertText: "/help ",
  },
  {
    key: "command:stop",
    type: "command",
    value: "/stop",
    title: "/stop",
    description: "Stop the active task in this conversation.",
    insertText: "/stop ",
  },
  {
    key: "command:models",
    type: "command",
    value: "/models",
    title: "/models",
    description: "Show the current model profile.",
    insertText: "/models ",
  },
  {
    key: "command:skills",
    type: "command",
    value: "/skills",
    title: "/skills",
    description: "Show the currently loaded skills.",
    insertText: "/skills ",
  },
  {
    key: "command:ctx",
    type: "command",
    value: "/ctx",
    title: "/ctx",
    description: "Show context window usage for this conversation.",
    insertText: "/ctx ",
  },
  {
    key: "command:workspace",
    type: "command",
    value: "/workspace",
    title: "/workspace",
    description: "Show the current workspace directory.",
    insertText: "/workspace ",
  },
  {
    key: "command:think",
    type: "command",
    value: "/think",
    title: "/think <task>",
    description: "Run a task through the think LLM route.",
    insertText: "/think ",
  },
  {
    key: "command:models:list",
    type: "command",
    value: "/models list",
    title: "/models list",
    description: "List configured model profiles.",
    insertText: "/models list ",
  },
  {
    key: "command:models:set",
    type: "command",
    value: "/models set",
    title: "/models set <profile>",
    description: "Switch the current model profile.",
    insertText: "/models set ",
  },
  {
    key: "command:models:reset",
    type: "command",
    value: "/models reset",
    title: "/models reset",
    description: "Return model selection to automatic mode.",
    insertText: "/models reset ",
  },
  {
    key: "command:workspace:attach",
    type: "command",
    value: "/workspace attach",
    title: "/workspace attach <dir>",
    description: "Attach or replace the workspace directory.",
    insertText: "/workspace attach ",
  },
  {
    key: "command:workspace:detach",
    type: "command",
    value: "/workspace detach",
    title: "/workspace detach",
    description: "Detach the current workspace directory.",
    insertText: "/workspace detach ",
  },
];

function trimText(value) {
  return String(value || "").trim();
}

function composerTriggerContext(value, selectionStart) {
  const text = String(value || "");
  const cursor = Math.max(0, Math.min(Number(selectionStart) || 0, text.length));
  const beforeCursor = text.slice(0, cursor);
  const tokenStart = Math.max(
    beforeCursor.lastIndexOf(" "),
    beforeCursor.lastIndexOf("\n"),
    beforeCursor.lastIndexOf("\t")
  ) + 1;
  const token = beforeCursor.slice(tokenStart);
  if (token.length === 0) {
    return null;
  }
  const trigger = token.charAt(0);
  if (trigger !== "/" && trigger !== "$") {
    return null;
  }
  const query = token.slice(1);
  if (/\s/u.test(query)) {
    return null;
  }
  return {
    type: trigger === "/" ? "command" : "skill",
    trigger,
    query,
    start: tokenStart,
    end: cursor,
  };
}

function normalizeComposerSkillItems(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  const seen = new Set();
  const out = [];
  for (const item of values) {
    const id = trimText(item?.id);
    const name = trimText(item?.name);
    const value = id || name;
    if (!value) {
      continue;
    }
    const key = value.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push({
      key: `skill:${key}`,
      type: "skill",
      value,
      title: `$${value}`,
      description: trimText(item?.description),
      insertText: `$${value} `,
    });
  }
  return out;
}

function commandKey(value) {
  return String(value || "").trim().toLowerCase().replace(/\s+/gu, ":").replace(/^\/+/u, "");
}

function ensureTrailingSpace(value) {
  const text = String(value || "").trim();
  return text ? `${text} ` : "";
}

function normalizeComposerCommandItems(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  const seen = new Set();
  const out = [];
  for (const item of values) {
    const value = trimText(item?.value);
    if (!value.startsWith("/")) {
      continue;
    }
    const key = value.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    const insertText = ensureTrailingSpace(item?.insertText || item?.insert_text || value);
    if (!insertText) {
      continue;
    }
    out.push({
      key: `command:${commandKey(value)}`,
      type: "command",
      value,
      title: trimText(item?.title) || value,
      description: trimText(item?.description),
      insertText,
    });
  }
  return out;
}

function itemMatchesQuery(item, query) {
  const needle = trimText(query).toLowerCase();
  if (!needle) {
    return true;
  }
  return [item?.value, item?.title, item?.description]
    .some((value) => trimText(value).toLowerCase().includes(needle));
}

function buildComposerSuggestionItems({
  context,
  commands = COMPOSER_COMMAND_SUGGESTIONS,
  skills = [],
  limit = COMPOSER_SUGGESTION_LIMIT,
} = {}) {
  const type = String(context?.type || "");
  if (type !== "command" && type !== "skill") {
    return [];
  }
  const source = type === "command" && Array.isArray(commands) && commands.length > 0
    ? commands
    : type === "command"
      ? COMPOSER_COMMAND_SUGGESTIONS
      : skills;
  return source
    .filter((item) => itemMatchesQuery(item, context?.query))
    .slice(0, Math.max(0, Number(limit) || COMPOSER_SUGGESTION_LIMIT));
}

function replaceComposerSuggestionToken(value, range, insertText) {
  const text = String(value || "");
  const start = Math.max(0, Math.min(Number(range?.start) || 0, text.length));
  const end = Math.max(start, Math.min(Number(range?.end) || start, text.length));
  let before = text.slice(0, start);
  let after = text.slice(end);
  const insertion = String(insertText || "");
  if (insertion.endsWith(" ") && after.startsWith(" ")) {
    after = after.slice(1);
  }
  if (before.endsWith(" ") && insertion.startsWith(" ")) {
    before = before.slice(0, -1);
  }
  return `${before}${insertion}${after}`;
}

export {
  COMPOSER_COMMAND_SUGGESTIONS,
  buildComposerSuggestionItems,
  composerTriggerContext,
  normalizeComposerCommandItems,
  normalizeComposerSkillItems,
  replaceComposerSuggestionToken,
};
