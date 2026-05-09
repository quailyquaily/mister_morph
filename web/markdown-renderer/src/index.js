import createDOMPurify from "dompurify";
import rehypeKatex from "rehype-katex";
import rehypeRaw from "rehype-raw";
import rehypeStringify from "rehype-stringify";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { createHighlighter, createJavaScriptRegexEngine } from "shiki";
import { ShikiStreamTokenizer } from "shiki-stream";
import { applyThemeToElement, resolveTheme, themeCatalog, themeNames, themes } from "./themes.js";
import {
  BLOCK_ANIMATION_FADE_MS,
  createBlockQueueState,
  createStreamProfilerState,
  createSmoothStreamState,
  advanceSmoothFrame,
  forceSmoothStreamDrain,
  makeStreamingBlock,
  recordStreamProfilerFrame,
  recordStreamProfilerRender,
  repairStreamingMarkdown,
  resetBlockQueueState,
  resetStreamProfilerState,
  revealAnimatingBlock,
  resolveBlockQueue,
  shouldAnimateStreamingBlock,
  smoothStateSnapshot,
  streamProfilerSnapshot,
  syncSmoothStreamState,
  updateSmoothTarget,
} from "./streaming-core.js";
import { unified } from "unified";

import "katex/dist/katex.min.css";
import "./styles.css";

const DIAGRAM_LANGUAGES = new Set([
  "mermaid",
  "graphviz",
  "infographic",
]);
const MATH_FENCE_LANGUAGES = new Set([
  "math",
  "latex",
  "tex",
  "katex",
]);

const markdownProcessor = unified()
  .use(remarkParse)
  .use(remarkGfm, { singleTilde: false })
  .use(remarkMath)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeRaw)
  .use(rehypeKatex)
  .use(rehypeStringify, { allowDangerousHtml: true });

const markdownBlockProcessor = unified()
  .use(remarkParse)
  .use(remarkGfm, { singleTilde: false })
  .use(remarkMath);

let mermaidInitialized = false;
let mermaidThemeID = "";
let mermaidSequence = 0;
let vizPromise = null;
let mermaidModulePromise = null;
let infographicClassPromise = null;
const COPY_FEEDBACK_DURATION_MS = 360;
const SHIKI_THEME = "github-dark";
const STREAM_FINAL_SETTLE_MS = 900;
let shikiHighlighterPromise = null;
const shikiLoadedLanguages = new Set();

function stringValue(value) {
  return typeof value === "string" ? value : String(value ?? "");
}

function normalizeSourceText(raw) {
  let text = stringValue(raw);
  const trimmed = text.trim();
  if (!trimmed) {
    return "";
  }
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    try {
      const parsed = JSON.parse(trimmed);
      if (typeof parsed === "string") {
        text = parsed;
      }
    } catch {
      // Keep original text when it is not a valid JSON string literal.
    }
  }
  return text;
}

function escapeHtml(raw) {
  return stringValue(raw)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function normalizeDiagramLanguage(raw) {
  const value = stringValue(raw).trim().toLowerCase();
  if (value === "mmd") {
    return "mermaid";
  }
  if (value === "dot" || value === "gv") {
    return "graphviz";
  }
  return value;
}

function inferFormat(raw, explicit = "auto") {
  const requested = normalizeDiagramLanguage(explicit);
  if (requested && requested !== "auto") {
    return requested;
  }
  const text = normalizeSourceText(raw).trim();
  if (!text) {
    return "markdown";
  }
  if (text.startsWith("```") || text.includes("\n```")) {
    return "markdown";
  }
  if (/^(graph|flowchart|sequenceDiagram|classDiagram|stateDiagram|erDiagram|journey|pie|mindmap|timeline|gantt|quadrantChart|gitGraph)\b/.test(text)) {
    return "mermaid";
  }
  if (/^(graph|digraph)\b/.test(text)) {
    return "graphviz";
  }
  if (/^infographic\b/.test(text)) {
    return "infographic";
  }
  return "markdown";
}

function detectCodeLanguage(codeNode) {
  if (!codeNode) {
    return "";
  }
  for (const className of Array.from(codeNode.classList)) {
    if (className.startsWith("language-")) {
      return normalizeDiagramLanguage(className.slice("language-".length));
    }
  }
  return normalizeDiagramLanguage(codeNode.dataset?.language || "");
}

function normalizedFenceSource(raw) {
  return stringValue(raw).replaceAll("\r\n", "\n").replaceAll("\r", "\n").trim();
}

function fenceMathSource(language, rawSource) {
  const source = normalizedFenceSource(rawSource);
  if (!source) {
    return "";
  }
  if (MATH_FENCE_LANGUAGES.has(language)) {
    if (source.startsWith("$$") && source.endsWith("$$")) {
      return source;
    }
    return `$$\n${source}\n$$`;
  }
  if (!language && source.startsWith("$$") && source.endsWith("$$")) {
    return source;
  }
  return "";
}

function documentView(doc) {
  return doc?.defaultView || window;
}

function createPurifier(doc) {
  return createDOMPurify(documentView(doc));
}

async function loadMermaid() {
  mermaidModulePromise ||= import("mermaid").then((module) => module.default || module);
  return mermaidModulePromise;
}

async function loadViz() {
  vizPromise ||= import("@viz-js/viz").then(({ instance }) => instance());
  return vizPromise;
}

async function loadInfographicClass() {
  infographicClassPromise ||= import("@antv/infographic").then(({ Infographic }) => Infographic);
  return infographicClassPromise;
}

function sanitizeMarkup(markup, doc) {
  const purifier = createPurifier(doc);
  const clean = purifier.sanitize(stringValue(markup), {
    USE_PROFILES: {
      html: true,
      svg: true,
      svgFilters: true,
      mathMl: true,
    },
    ADD_TAGS: ["foreignObject"],
    ADD_ATTR: [
      "target",
      "rel",
      "xmlns",
      "xmlns:xlink",
      "viewBox",
      "preserveAspectRatio",
      "transform-origin",
    ],
  });
  const template = doc.createElement("template");
  template.innerHTML = clean;
  for (const anchor of template.content.querySelectorAll("a[href]")) {
    anchor.setAttribute("target", "_blank");
    anchor.setAttribute("rel", "noreferrer noopener");
  }
  return template.innerHTML;
}

function markdownToHtml(source, doc) {
  const normalized = normalizeSourceText(source);
  try {
    const rendered = markdownProcessor.processSync(normalized);
    return sanitizeMarkup(String(rendered), doc);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Markdown render failed.";
    return `<pre>${escapeHtml(`${message}\n\n${normalized}`)}</pre>`;
  }
}

function markdownToHtmlStrict(source, doc) {
  const normalized = normalizeSourceText(source);
  const rendered = markdownProcessor.processSync(normalized);
  const markup = sanitizeMarkup(String(rendered), doc);
  const template = doc.createElement("template");
  template.innerHTML = markup;
  if (template.content.querySelector(".katex-error")) {
    throw new Error("KaTeX render failed.");
  }
  return template.innerHTML;
}

function nowMs() {
  if (typeof performance !== "undefined" && typeof performance.now === "function") {
    return performance.now();
  }
  return Date.now();
}

function splitStreamingBlocks(source) {
  const text = stringValue(source);
  if (!text) {
    return [];
  }
  try {
    const tree = markdownBlockProcessor.parse(text);
    const nodes = Array.isArray(tree?.children) ? tree.children : [];
    const blocks = [];
    let cursor = 0;
    for (const node of nodes) {
      const start = Number(node?.position?.start?.offset);
      const end = Number(node?.position?.end?.offset);
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= cursor) {
        continue;
      }
      const blockStart = Math.max(0, Math.min(cursor, start));
      const raw = text.slice(blockStart, end);
      blocks.push(makeStreamingBlock(raw, blockStart, blocks.length));
      cursor = end;
    }
    if (cursor < text.length) {
      const tail = text.slice(cursor);
      if (tail.trim()) {
        blocks.push(makeStreamingBlock(tail, cursor, blocks.length));
      } else if (blocks.length > 0) {
        const previous = blocks[blocks.length - 1];
        previous.raw += tail;
        previous.endOffset = text.length;
        previous.charCount = [...previous.raw].length;
      }
    }
    if (blocks.length > 0) {
      return blocks;
    }
  } catch {
    // Fall back to one block below.
  }
  return [makeStreamingBlock(text, 0, 0)];
}

function markIncompleteStreamingBlocks(blocks, displayedSource, repairedSource) {
  const displayedLength = stringValue(displayedSource).length;
  const repairedLength = stringValue(repairedSource).length;
  const repairedTail = repairedLength > displayedLength;
  for (const block of blocks) {
    block.incomplete = repairedTail && block.endOffset > displayedLength;
  }
  return blocks;
}

function prefersReducedMotion(doc) {
  try {
    return documentView(doc).matchMedia?.("(prefers-reduced-motion: reduce)")?.matches === true;
  } catch {
    return false;
  }
}

function resetElementClass(element, className) {
  element.className = className;
}

function blockClass(block, state, animate) {
  const classes = [
    "mmr-markdown-block",
    `is-${state}`,
    `is-${block.kind}`,
  ];
  if (block.heavy) {
    classes.push("is-heavy");
  }
  if (animate) {
    classes.push("has-char-animation");
  }
  return classes.join(" ");
}

function isSkippableStreamElement(element) {
  const tag = element.tagName.toLowerCase();
  if (tag === "pre" || tag === "code" || tag === "table" || tag === "svg") {
    return true;
  }
  if (
    element.classList.contains("katex") ||
    element.classList.contains("mmr-diagram") ||
    element.classList.contains("mermaid")
  ) {
    return true;
  }
  return false;
}

function canAnimateTextContainer(element) {
  const tag = element.tagName.toLowerCase();
  return tag === "p" || /^h[1-6]$/.test(tag) || tag === "li";
}

function assignStreamBirths(entry, block, charDelay, renderNow) {
  const existing = Array.isArray(entry.charBirths) ? entry.charBirths : [];
  const nextLength = block.charCount;
  if (existing.length > nextLength) {
    entry.charBirths = existing.slice(0, nextLength);
    return entry.charBirths;
  }
  const births = existing.slice();
  const cap = renderNow + BLOCK_ANIMATION_FADE_MS;
  for (let i = births.length; i < nextLength; i += 1) {
    const previousBirth = i > 0 ? births[i - 1] : renderNow - charDelay;
    const chained = previousBirth + charDelay;
    births.push(Math.min(cap, Math.max(chained, renderNow)));
  }
  entry.charBirths = births;
  return births;
}

function wrapTextNodeForStream(textNode, context) {
  const value = textNode.nodeValue || "";
  if (!value) {
    return;
  }
  const doc = textNode.ownerDocument;
  const fragment = doc.createDocumentFragment();
  for (const char of value) {
    const span = doc.createElement("span");
    const birth = context.births[context.index];
    let className = "mmr-stream-char";
    if (context.revealed || context.index < context.revealedUntil || birth === undefined) {
      className = "mmr-stream-char mmr-stream-char-revealed";
    } else {
      const elapsed = context.now - birth;
      if (elapsed >= BLOCK_ANIMATION_FADE_MS) {
        className = "mmr-stream-char mmr-stream-char-revealed";
      } else if (elapsed !== 0) {
        span.style.animationDelay = `${-elapsed}ms`;
      }
    }
    span.className = className;
    span.textContent = char;
    fragment.append(span);
    context.index += 1;
  }
  textNode.replaceWith(fragment);
}

function wrapElementTextForStream(element, context) {
  if (isSkippableStreamElement(element)) {
    return;
  }
  if (canAnimateTextContainer(element)) {
    for (const child of Array.from(element.childNodes)) {
      if (child.nodeType === Node.TEXT_NODE) {
        wrapTextNodeForStream(child, context);
      } else if (child.nodeType === Node.ELEMENT_NODE) {
        wrapElementTextForStream(child, context);
      }
    }
    return;
  }
  for (const child of Array.from(element.children)) {
    wrapElementTextForStream(child, context);
  }
}

function applyStreamTextAnimation(element, entry, block, state, charDelay, renderNow, reducedMotion) {
  const shouldAnimate = shouldAnimateStreamingBlock(block, reducedMotion);
  if (!shouldAnimate) {
    element.classList.toggle("has-char-animation", false);
    return false;
  }
  const births = assignStreamBirths(entry, block, charDelay, renderNow);
  const context = {
    births,
    index: 0,
    now: renderNow,
    revealed: state === "revealed",
    revealedUntil: entry.revealedCharCount || 0,
  };
  wrapElementTextForStream(element, context);
  element.classList.toggle("has-char-animation", true);
  return true;
}

function reconcileStreamingChildren(parent, orderedElements) {
  const wanted = new Set(orderedElements);
  for (const child of Array.from(parent.children)) {
    if (!wanted.has(child)) {
      child.remove();
    }
  }
  orderedElements.forEach((element, index) => {
    const current = parent.children[index] || null;
    if (current !== element) {
      parent.insertBefore(element, current);
    }
  });
}

function streamingCodeCachePrefix(block) {
  return `stream:${block.key}:`;
}

function childWithClass(element, className) {
  return Array.from(element?.children || []).find((child) => child.classList?.contains(className)) || null;
}

function syncCodeCopyButton(button, source, doc) {
  button.type = "button";
  button.className = "mmr-code-copy";
  button.setAttribute("aria-label", "Copy code");
  button.setAttribute("title", "Copy code");
  button.__mmrCopySource = stringValue(source);
  if (!button.firstChild) {
    button.append(createCopyIcon(doc));
  }
  if (button.__mmrCopyReady) {
    return;
  }
  button.__mmrCopyReady = true;
  button.__mmrCopyResetTimerID = 0;
  button.addEventListener("click", async () => {
    if (button.disabled) {
      return;
    }
    button.disabled = true;
    try {
      await copyTextToClipboard(button.__mmrCopySource, doc);
      button.dataset.copyState = "copied";
      button.setAttribute("title", "Copied");
      button.setAttribute("aria-label", "Code copied");
    } catch {
      button.dataset.copyState = "failed";
      button.setAttribute("title", "Copy failed");
      button.setAttribute("aria-label", "Copy failed");
    } finally {
      const view = documentView(doc);
      view.clearTimeout(button.__mmrCopyResetTimerID);
      button.__mmrCopyResetTimerID = view.setTimeout(() => {
        button.disabled = false;
        button.dataset.copyState = "";
        button.setAttribute("title", "Copy code");
        button.setAttribute("aria-label", "Copy code");
      }, COPY_FEEDBACK_DURATION_MS);
    }
  });
}

function syncCodeLanguageBadge(wrapper, language, doc) {
  let badge = childWithClass(wrapper, "mmr-code-language");
  if (!language) {
    badge?.remove();
    return;
  }
  if (!badge) {
    badge = doc.createElement("span");
    badge.className = "mmr-code-language";
    wrapper.prepend(badge);
  }
  badge.textContent = language;
}

function syncCodeNodeLanguage(codeNode, language) {
  const highlighted = codeNode.classList.contains("mmr-code-content");
  if (language) {
    codeNode.className = `language-${language}`;
    codeNode.dataset.language = language;
  } else {
    codeNode.className = "";
    delete codeNode.dataset.language;
  }
  if (highlighted) {
    codeNode.classList.add("mmr-code-content");
  }
}

function ensureCodeBlockShell(pre, source, language = "") {
  const doc = pre.ownerDocument;
  let wrapper = pre.parentElement?.classList?.contains("mmr-code-block") ? pre.parentElement : null;
  if (!wrapper) {
    wrapper = doc.createElement("div");
    wrapper.className = "mmr-code-block";
    pre.replaceWith(wrapper);
    wrapper.append(pre);
  } else {
    wrapper.className = "mmr-code-block";
  }

  const normalizedLanguage = stringValue(language).trim();
  syncCodeLanguageBadge(wrapper, normalizedLanguage, doc);

  let button = childWithClass(wrapper, "mmr-code-copy");
  if (!button) {
    button = doc.createElement("button");
    const badge = childWithClass(wrapper, "mmr-code-language");
    wrapper.insertBefore(button, badge?.nextSibling || wrapper.firstChild);
  }
  syncCodeCopyButton(button, source, doc);

  if (pre.parentElement !== wrapper) {
    wrapper.append(pre);
  }
  if (wrapper.lastElementChild !== pre) {
    wrapper.append(pre);
  }

  return {
    codeNode: pre.querySelector("code"),
    wrapper,
  };
}

function parseStreamingCodeFence(raw, fallbackLanguage = "", options = {}) {
  const text = stringValue(raw).replaceAll("\r\n", "\n").replaceAll("\r", "\n");
  const lines = text.split("\n");
  const openerIndex = lines.findIndex((line) => line.trimStart().match(/^(`{3,}|~{3,})([^`~]*)$/));
  if (openerIndex < 0) {
    return {
      language: normalizeDiagramLanguage(fallbackLanguage),
      source: text,
    };
  }

  const opener = lines[openerIndex].trimStart().match(/^(`{3,}|~{3,})([^`~]*)$/);
  const fence = opener?.[1] || "```";
  const info = stringValue(opener?.[2] || "").trim();
  const language = normalizeDiagramLanguage(info.split(/\s+/)[0] || fallbackLanguage);
  let closerIndex = -1;
  for (let index = lines.length - 1; index > openerIndex; index -= 1) {
    const closer = lines[index].trimStart().match(/^(`{3,}|~{3,})\s*$/);
    if (closer && closer[1][0] === fence[0] && closer[1].length >= fence.length) {
      closerIndex = index;
      break;
    }
  }
  const contentLines = lines.slice(openerIndex + 1, closerIndex >= 0 ? closerIndex : lines.length);
  let source = contentLines.join("\n");
  if (closerIndex >= 0 && options.preserveClosingNewline !== false && contentLines.length > 0) {
    source += "\n";
  }

  return {
    language,
    source,
  };
}

function renderStreamingCodeBlock(element, block) {
  const doc = element.ownerDocument;
  const parsed = parseStreamingCodeFence(block.raw, block.language, {
    preserveClosingNewline: !block.incomplete,
  });
  let wrapper = childWithClass(element, "mmr-code-block");
  let pre = wrapper?.querySelector("pre") || null;
  let code = pre?.querySelector("code") || null;
  if (!pre || !code) {
    element.replaceChildren();
    pre = doc.createElement("pre");
    code = doc.createElement("code");
    pre.append(code);
    element.append(pre);
  }

  syncCodeNodeLanguage(code, parsed.language);
  code.__mmrCodeSource = parsed.source;
  if (code.textContent !== parsed.source) {
    code.textContent = parsed.source;
    code.classList.remove("mmr-code-content");
  }
  ensureCodeBlockShell(pre, parsed.source, parsed.language);
}

function appendSvgMarkup(surface, markup) {
  const doc = surface.ownerDocument;
  const parser = new DOMParser();
  const parsed = parser.parseFromString(stringValue(markup), "image/svg+xml");
  const errorNode = parsed.querySelector("parsererror");
  if (errorNode) {
    throw new Error(errorNode.textContent || "Invalid SVG output.");
  }
  const svg = parsed.documentElement;
  if (!svg || svg.nodeName.toLowerCase() !== "svg") {
    throw new Error("Expected SVG output.");
  }
  const adopted = doc.importNode(svg, true);
  for (const anchor of adopted.querySelectorAll("a[href]")) {
    anchor.setAttribute("target", "_blank");
    anchor.setAttribute("rel", "noreferrer noopener");
  }
  surface.replaceChildren(adopted);
  return adopted;
}

function makeDiagramFrame(doc, language) {
  const figure = doc.createElement("figure");
  figure.className = `mmr-diagram mmr-diagram-${language}`;

  const header = doc.createElement("figcaption");
  header.className = "mmr-diagram-head";

  const badge = doc.createElement("span");
  badge.className = "mmr-diagram-badge";
  badge.textContent = language;

  const status = doc.createElement("span");
  status.className = "mmr-diagram-status";
  status.textContent = "rendered";

  header.append(badge, status);

  const surface = doc.createElement("div");
  surface.className = "mmr-diagram-surface";

  figure.append(header, surface);
  return { figure, surface, status };
}

function renderError(surface, source, error) {
  const doc = surface.ownerDocument;
  const message = doc.createElement("p");
  message.className = "mmr-diagram-error";
  message.textContent = error instanceof Error ? error.message : "Render failed.";

  const pre = doc.createElement("pre");
  pre.className = "mmr-diagram-fallback";
  pre.textContent = stringValue(source);

  surface.replaceChildren(message, pre);
}

function renderMathFence(pre, source) {
  const doc = pre.ownerDocument;
  const block = doc.createElement("div");
  block.className = "mmr-math-fence";
  block.innerHTML = markdownToHtmlStrict(source, doc);
  pre.replaceWith(block);
}

async function copyTextToClipboard(text, doc) {
  const value = stringValue(text);
  if (!value) {
    return;
  }
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return;
    }
  } catch {
    // Fall back to execCommand below.
  }

  const textarea = doc.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "readonly");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  doc.body.append(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  try {
    if (!doc.execCommand("copy")) {
      throw new Error("Copy command failed.");
    }
  } finally {
    textarea.remove();
  }
}

async function getShikiHighlighter() {
  shikiHighlighterPromise ||= createHighlighter({
    themes: [SHIKI_THEME],
    langs: [],
    engine: createJavaScriptRegexEngine(),
  });
  return shikiHighlighterPromise;
}

async function ensureShikiLanguage(raw) {
  const language = stringValue(raw).trim().toLowerCase();
  if (!language) {
    return "";
  }
  const highlighter = await getShikiHighlighter();
  const resolved = highlighter.resolveLangAlias(language) || language;
  if (shikiLoadedLanguages.has(resolved)) {
    return resolved;
  }
  try {
    await highlighter.loadLanguage(resolved);
    shikiLoadedLanguages.add(resolved);
    return resolved;
  } catch {
    return "";
  }
}

function applyShikiFontStyle(node, fontStyle) {
  if (!fontStyle) {
    return;
  }
  if (fontStyle & 1) {
    node.style.fontStyle = "italic";
  }
  if (fontStyle & 2) {
    node.style.fontWeight = "600";
  }
  if (fontStyle & 4) {
    node.style.textDecoration = "underline";
  }
}

function renderShikiTokens(codeNode, tokens) {
  const doc = codeNode.ownerDocument;
  const fragment = doc.createDocumentFragment();
  for (const token of tokens) {
    const content = stringValue(token?.content);
    if (!content) {
      continue;
    }
    if (!token?.color && !token?.fontStyle) {
      fragment.append(content);
      continue;
    }
    const span = doc.createElement("span");
    span.className = "mmr-code-token";
    span.textContent = content;
    if (token.color) {
      span.style.color = token.color;
    }
    applyShikiFontStyle(span, token.fontStyle || 0);
    fragment.append(span);
  }
  codeNode.replaceChildren(fragment);
  codeNode.classList.add("mmr-code-content");
}

function codeHighlightStillCurrent(codeNode, source, isCurrent) {
  return isCurrent() && codeNode.__mmrCodeSource === stringValue(source);
}

async function highlightCodeWithShiki(codeNode, source, language, cacheEntry, isCurrent = () => true) {
  const normalizedSource = stringValue(source);
  const resolvedLanguage = await ensureShikiLanguage(language);
  if (!codeHighlightStillCurrent(codeNode, normalizedSource, isCurrent)) {
    return cacheEntry || null;
  }
  if (!resolvedLanguage) {
    if (codeNode.textContent !== normalizedSource) {
      codeNode.textContent = normalizedSource;
    }
    codeNode.classList.remove("mmr-code-content");
    return null;
  }

  const highlighter = await getShikiHighlighter();
  if (!codeHighlightStillCurrent(codeNode, normalizedSource, isCurrent)) {
    return cacheEntry || null;
  }
  const isReusable =
    cacheEntry?.tokenizer &&
    cacheEntry.language === resolvedLanguage &&
    cacheEntry.theme === SHIKI_THEME;
  let tokenizer = isReusable
    ? cacheEntry.tokenizer
    : new ShikiStreamTokenizer({
        highlighter,
        lang: resolvedLanguage,
        theme: SHIKI_THEME,
      });
  const previousSource = isReusable ? stringValue(cacheEntry.source) : "";
  if (!normalizedSource.startsWith(previousSource)) {
    tokenizer.clear();
  }
  const stablePrefix = normalizedSource.startsWith(previousSource) ? previousSource : "";
  const nextChunk = normalizedSource.slice(stablePrefix.length);
  if (nextChunk) {
    await tokenizer.enqueue(nextChunk);
  }
  if (!codeHighlightStillCurrent(codeNode, normalizedSource, isCurrent)) {
    return cacheEntry || null;
  }
  renderShikiTokens(codeNode, [...tokenizer.tokensStable, ...tokenizer.tokensUnstable]);
  return {
    language: resolvedLanguage,
    source: normalizedSource,
    theme: SHIKI_THEME,
    tokenizer,
  };
}

function createCopyIcon(doc) {
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = doc.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");

  const back = doc.createElementNS(svgNS, "rect");
  back.setAttribute("x", "5");
  back.setAttribute("y", "3");
  back.setAttribute("width", "8");
  back.setAttribute("height", "10");
  back.setAttribute("rx", "1.5");
  back.setAttribute("fill", "none");
  back.setAttribute("stroke", "currentColor");
  back.setAttribute("stroke-width", "1.25");

  const front = doc.createElementNS(svgNS, "path");
  front.setAttribute("d", "M3.75 11.25V4.75c0-.83.67-1.5 1.5-1.5h5.5");
  front.setAttribute("fill", "none");
  front.setAttribute("stroke", "currentColor");
  front.setAttribute("stroke-width", "1.25");
  front.setAttribute("stroke-linecap", "round");
  front.setAttribute("stroke-linejoin", "round");

  svg.append(back, front);
  return svg;
}

async function renderCodeBlock(pre, source, language = "", cacheEntry = null, options = {}) {
  const isCurrent = typeof options.isCurrent === "function" ? options.isCurrent : () => true;
  const normalizedLanguage = stringValue(language).trim();
  const { codeNode } = ensureCodeBlockShell(pre, source, normalizedLanguage);
  let nextCacheEntry = null;
  if (codeNode) {
    syncCodeNodeLanguage(codeNode, normalizedLanguage);
    codeNode.__mmrCodeSource = stringValue(source);
    if (!isCurrent()) {
      return cacheEntry || null;
    }
    nextCacheEntry = await highlightCodeWithShiki(codeNode, source, normalizedLanguage, cacheEntry, isCurrent);
  }
  return nextCacheEntry;
}

function isInlineCopyCode(code) {
  if (!(code instanceof Element)) {
    return false;
  }
  if (code.closest("pre")) {
    return false;
  }
  if (
    code.classList.contains("math-inline") ||
    code.classList.contains("math-display") ||
    code.classList.contains("language-math") ||
    code.querySelector(".katex")
  ) {
    return false;
  }
  return Boolean(stringValue(code.textContent).trim());
}

function enhanceInlineCode(container) {
  const doc = container.ownerDocument;
  for (const code of Array.from(container.querySelectorAll("code"))) {
    if (!isInlineCopyCode(code)) {
      continue;
    }
    const source = stringValue(code.textContent);
    code.classList.add("mmr-inline-copy");
    code.setAttribute("role", "button");
    code.setAttribute("tabindex", "0");
    code.setAttribute("title", "Click to copy");
    code.setAttribute("aria-label", "Copy code");

    let resetTimerID = 0;
    const resetState = () => {
      code.dataset.copyBusy = "";
      code.dataset.copyState = "";
      code.setAttribute("title", "Click to copy");
      code.setAttribute("aria-label", "Copy code");
    };
    const triggerCopy = async (event) => {
      event.preventDefault();
      event.stopPropagation();
      if (code.dataset.copyBusy === "1") {
        return;
      }
      code.dataset.copyBusy = "1";
      try {
        await copyTextToClipboard(source, doc);
        code.dataset.copyState = "copied";
        code.setAttribute("title", "Copied");
        code.setAttribute("aria-label", "Code copied");
      } catch {
        code.dataset.copyState = "failed";
        code.setAttribute("title", "Copy failed");
        code.setAttribute("aria-label", "Copy failed");
      } finally {
        const view = documentView(doc);
        view.clearTimeout(resetTimerID);
        resetTimerID = view.setTimeout(resetState, COPY_FEEDBACK_DURATION_MS);
      }
    };

    code.addEventListener("click", triggerCopy);
    code.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        void triggerCopy(event);
      }
    });
  }
}

async function initializeMermaid(theme) {
  const mermaid = await loadMermaid();
  if (mermaidInitialized && mermaidThemeID === theme.id) {
    return mermaid;
  }
  mermaid.initialize(theme.mermaid);
  mermaidInitialized = true;
  mermaidThemeID = theme.id;
  return mermaid;
}

async function renderMermaid(surface, source, theme) {
  const mermaid = await initializeMermaid(theme);
  const id = `mmr-mermaid-${mermaidSequence += 1}`;
  const rendered = await mermaid.render(id, stringValue(source));
  appendSvgMarkup(surface, rendered.svg);
  rendered.bindFunctions?.(surface);
}

async function renderGraphviz(surface, source, theme) {
  const viz = await loadViz();
  const svgElement = viz.renderSVGElement(stringValue(source), theme.graphviz);
  surface.replaceChildren(svgElement);
}

async function renderInfographic(surface, source, theme) {
  const Infographic = await loadInfographicClass();
  const infographic = new Infographic({
    container: surface,
    editable: false,
    padding: 16,
    theme: theme.infographic.theme,
    themeConfig: theme.infographic.themeConfig,
  });
  infographic.render(stringValue(source));
  return () => {
    infographic.destroy();
  };
}

async function renderDiagram(surface, language, source, theme) {
  switch (language) {
    case "mermaid":
      await renderMermaid(surface, source, theme);
      return null;
    case "graphviz":
      await renderGraphviz(surface, source, theme);
      return null;
    case "infographic":
      return renderInfographic(surface, source, theme);
    default:
      throw new Error(`Unsupported diagram language: ${language}`);
  }
}

export class MarkdownRenderer {
  constructor(root, options = {}) {
    if (!(root instanceof Element)) {
      throw new Error("MarkdownRenderer requires a root Element.");
    }
    this.root = root;
    this.options = {
      format: "auto",
      theme: "paper",
      ...options,
    };
    this.cleanupFns = [];
    this.codeBlockHighlightState = new Map();
    this.renderToken = 0;
    this.lastUpdateSignature = "";
    this.streaming = {
      animatingTimer: 0,
      blockEntries: new Map(),
      documentNode: null,
      frameID: 0,
      frameTheme: null,
      frameToken: 0,
      lastOptionsSignature: "",
      profiler: createStreamProfilerState(),
      queue: createBlockQueueState(),
      smooth: createSmoothStreamState(""),
      wakeTimer: 0,
    };
    this.root.classList.add("mmr-root");
  }

  async update(source, nextOptions = {}) {
    this.options = {
      ...this.options,
      ...nextOptions,
    };
    const token = ++this.renderToken;
    const normalizedSource = normalizeSourceText(source);
    const requestedFormat = normalizeDiagramLanguage(this.options.format);
    const requestedTheme = stringValue(this.options.theme).trim().toLowerCase();
    const streaming = this.options.streaming === true;
    const streamMode = stringValue(this.options.streamMode || "balanced");
    const updateSignature = `${requestedTheme}\u0000${requestedFormat}\u0000${streaming ? "stream" : "full"}\u0000${streamMode}\u0000${normalizedSource}`;
    if (updateSignature === this.lastUpdateSignature) {
      return;
    }
    this.lastUpdateSignature = updateSignature;
    const theme = applyThemeToElement(this.root, resolveTheme(this.options.theme));
    const format = inferFormat(normalizedSource, this.options.format);

    if (!normalizedSource.trim()) {
      this.stopStreaming();
      this.cleanup();
      this.root.replaceChildren();
      this.codeBlockHighlightState.clear();
      return;
    }

    if (format !== "markdown") {
      this.stopStreaming();
      this.cleanup();
      this.root.replaceChildren();
      this.codeBlockHighlightState.clear();
      await this.renderStandalone(format, normalizedSource, token, theme);
      return;
    }

    if (streaming) {
      await this.updateStreamingMarkdown(normalizedSource, token, theme, {
        mode: streamMode,
        profiler: this.options.streamProfiler === true,
      });
      return;
    }

    if (this.streaming.documentNode) {
      await this.finishStreamingMarkdown(normalizedSource, token, theme);
      return;
    }

    this.stopStreaming();
    await this.renderFullMarkdown(normalizedSource, token, theme);
  }

  async renderFullMarkdown(normalizedSource, token, theme) {
    this.cleanup();
    this.root.replaceChildren();
    this.codeBlockHighlightState.clear();

    const documentNode = this.root.ownerDocument.createElement("article");
    documentNode.className = "mmr-document";
    documentNode.innerHTML = markdownToHtml(normalizedSource, this.root.ownerDocument);
    this.root.replaceChildren(documentNode);

    await this.enhanceMarkdown(documentNode, token, theme);
  }

  cleanup() {
    while (this.cleanupFns.length > 0) {
      const cleanup = this.cleanupFns.pop();
      try {
        cleanup?.();
      } catch {
        // ignore renderer cleanup failures
      }
    }
  }

  stopStreaming() {
    const view = documentView(this.root.ownerDocument);
    if (this.streaming.frameID) {
      view.cancelAnimationFrame(this.streaming.frameID);
      this.streaming.frameID = 0;
    }
    if (this.streaming.wakeTimer) {
      view.clearTimeout(this.streaming.wakeTimer);
      this.streaming.wakeTimer = 0;
    }
    if (this.streaming.animatingTimer) {
      view.clearTimeout(this.streaming.animatingTimer);
      this.streaming.animatingTimer = 0;
    }
  }

  resetStreamingState(content = "") {
    this.stopStreaming();
    syncSmoothStreamState(this.streaming.smooth, content, nowMs());
    this.cleanupStreamingEntries();
    this.streaming.documentNode = null;
    this.streaming.frameTheme = null;
    this.streaming.frameToken = 0;
    this.streaming.lastOptionsSignature = "";
    resetStreamProfilerState(this.streaming.profiler);
    resetBlockQueueState(this.streaming.queue);
  }

  destroy() {
    this.resetStreamingState();
    this.cleanup();
    this.root.replaceChildren();
    this.root.classList.remove("mmr-root");
    this.lastUpdateSignature = "";
    this.codeBlockHighlightState.clear();
  }

  ensureStreamingDocument() {
    const doc = this.root.ownerDocument;
    if (this.streaming.documentNode?.parentElement === this.root) {
      return this.streaming.documentNode;
    }
    this.cleanup();
    this.codeBlockHighlightState.clear();
    const documentNode = doc.createElement("article");
    documentNode.className = "mmr-document mmr-document-streaming";
    this.streaming.documentNode = documentNode;
    this.root.replaceChildren(documentNode);
    return documentNode;
  }

  async updateStreamingMarkdown(normalizedSource, token, theme, streamOptions) {
    const mode = stringValue(streamOptions.mode || "balanced");
    const optionsSignature = `${mode}\u0000${theme.id}`;
    if (this.streaming.lastOptionsSignature !== optionsSignature) {
      this.resetStreamingState(this.streaming.smooth.displayedContent || "");
      this.streaming.smooth = createSmoothStreamState("", { mode, now: nowMs() });
      this.streaming.lastOptionsSignature = optionsSignature;
    }
    const now = nowMs();
    const result = updateSmoothTarget(this.streaming.smooth, normalizedSource, now);
    if (result.immediate) {
      this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, token, theme);
      return;
    }
    this.scheduleStreamingFrame(token, theme);
  }

  scheduleStreamingFrame(token, theme) {
    const view = documentView(this.root.ownerDocument);
    this.streaming.frameToken = token;
    this.streaming.frameTheme = theme;
    if (this.streaming.wakeTimer) {
      view.clearTimeout(this.streaming.wakeTimer);
      this.streaming.wakeTimer = 0;
    }
    if (this.streaming.frameID) {
      return;
    }
    const tick = (ts) => {
      this.streaming.frameID = 0;
      const frameToken = this.streaming.frameToken || token;
      const frameTheme = this.streaming.frameTheme || theme;
      const previousFrameTs = this.streaming.smooth.lastFrameTs;
      const result = advanceSmoothFrame(this.streaming.smooth, ts);
      this.recordStreamFrameProfiler({
        at: ts,
        backlog: result.backlog,
        frameIntervalMs: previousFrameTs === null ? 0 : Math.max(0, ts - previousFrameTs),
        reserveChars: result.reserveChars || 0,
        resumeActive: result.resumeActive === true,
        revealChars: result.revealChars,
        waitingForLag: result.waitingForLag === true,
      });
      if (result.revealChars > 0) {
        this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, frameToken, frameTheme);
      }
      if (this.streaming.smooth.displayedCount < this.streaming.smooth.targetCount) {
        if (result.waitingForLag) {
          const delay = Math.min(48, Math.max(16, this.streaming.smooth.config.activeInputWindowMs / 4));
          this.streaming.wakeTimer = view.setTimeout(() => {
            this.streaming.wakeTimer = 0;
            this.scheduleStreamingFrame(this.streaming.frameToken || frameToken, this.streaming.frameTheme || frameTheme);
          }, delay);
        } else {
          this.streaming.frameID = view.requestAnimationFrame(tick);
        }
      }
    };
    this.streaming.frameID = view.requestAnimationFrame(tick);
  }

  renderStreamingMarkdown(source, token, theme) {
    if (token !== this.renderToken) {
      return;
    }
    const startedAt = nowMs();
    const doc = this.root.ownerDocument;
    const documentNode = this.ensureStreamingDocument();
    const repaired = repairStreamingMarkdown(source);
    const blocks = markIncompleteStreamingBlocks(splitStreamingBlocks(repaired), source, repaired);
    const queue = resolveBlockQueue(this.streaming.queue, blocks);
    const renderNow = nowMs();
    const reducedMotion = prefersReducedMotion(doc);
    const activeKeys = new Set();
    const orderedElements = [];

    this.scheduleAnimatingBlockReveal(queue, blocks, token, theme);

    for (const [index, block] of blocks.entries()) {
      const state = queue.states[index];
      if (state === "queued") {
        continue;
      }
      activeKeys.add(block.key);
      let entry = this.streaming.blockEntries.get(block.key);
      if (!entry) {
        const element = doc.createElement("div");
        entry = {
          charBirths: [],
          charCount: 0,
          cleanupFns: [],
          element,
          enhanceSignature: "",
          raw: "",
          revealedCharCount: 0,
          state: "",
        };
        this.streaming.blockEntries.set(block.key, entry);
      }
      const animate = shouldAnimateStreamingBlock(block, reducedMotion);
      const className = blockClass(block, state, animate);
      if (entry.raw !== block.raw || entry.state !== state || entry.className !== className) {
        if (entry.raw && entry.state === state && block.raw.startsWith(entry.raw)) {
          entry.revealedCharCount = Math.max(entry.revealedCharCount || 0, entry.charCount || 0);
        } else if (state === "revealed") {
          entry.revealedCharCount = block.charCount;
        } else {
          entry.revealedCharCount = 0;
        }
        entry.raw = block.raw;
        entry.state = state;
        entry.className = className;
        entry.charCount = block.charCount;
        resetElementClass(entry.element, className);
        this.cleanupStreamingEntry(entry);
        if (block.kind === "code") {
          renderStreamingCodeBlock(entry.element, block);
        } else {
          entry.element.innerHTML = markdownToHtml(block.raw, doc);
          applyStreamTextAnimation(
            entry.element,
            entry,
            block,
            state,
            queue.charDelay,
            renderNow,
            reducedMotion
          );
        }
        this.enhanceStreamingBlock(entry, block, state, token, theme);
      }
      orderedElements.push(entry.element);
    }

    for (const [key, entry] of Array.from(this.streaming.blockEntries.entries())) {
      if (!activeKeys.has(key)) {
        this.clearCodeBlockCache(streamingCodeCachePrefix({ key }));
        this.cleanupStreamingEntry(entry);
        entry.element.remove();
        this.streaming.blockEntries.delete(key);
      }
    }

    reconcileStreamingChildren(documentNode, orderedElements);
    const smooth = smoothStateSnapshot(this.streaming.smooth);
    this.recordStreamProfiler({
      blockCount: blocks.length,
      backlog: smooth.backlog,
      durationMs: nowMs() - startedAt,
      name: "stream-render",
      queueLength: queue.queueLength,
      smooth,
    });
  }

  clearCodeBlockCache(prefix) {
    for (const key of Array.from(this.codeBlockHighlightState.keys())) {
      if (key.startsWith(prefix)) {
        this.codeBlockHighlightState.delete(key);
      }
    }
  }

  cleanupStreamingEntry(entry) {
    const cleanupFns = Array.isArray(entry?.cleanupFns) ? entry.cleanupFns : [];
    while (cleanupFns.length > 0) {
      const cleanup = cleanupFns.pop();
      try {
        cleanup?.();
      } catch {
        // ignore renderer cleanup failures
      }
    }
  }

  cleanupStreamingEntries() {
    for (const entry of this.streaming.blockEntries.values()) {
      this.cleanupStreamingEntry(entry);
    }
    this.streaming.blockEntries.clear();
  }

  enhanceStreamingBlock(entry, block, state, token, theme) {
    const cacheKeyPrefix = streamingCodeCachePrefix(block);
    const renderHeavy = !block.incomplete;
    const signature = `${theme.id}\u0000${state}\u0000${renderHeavy ? "heavy" : "light"}\u0000${block.raw}`;
    if (entry.enhanceSignature === signature) {
      return;
    }
    entry.enhanceSignature = signature;
    void this.enhanceMarkdown(entry.element, token, theme, {
      cacheKeyPrefix,
      isCurrent: () => this.streaming.blockEntries.get(block.key) === entry && entry.enhanceSignature === signature,
      registerCleanup: (cleanup) => {
        if (this.streaming.blockEntries.get(block.key) === entry && entry.enhanceSignature === signature) {
          entry.cleanupFns.push(cleanup);
          return;
        }
        cleanup?.();
      },
      renderDiagrams: renderHeavy,
      renderMathFences: renderHeavy,
    });
  }

  scheduleAnimatingBlockReveal(queue, blocks, token, theme) {
    const view = documentView(this.root.ownerDocument);
    if (this.streaming.animatingTimer) {
      view.clearTimeout(this.streaming.animatingTimer);
      this.streaming.animatingTimer = 0;
    }
    if (queue.animatingIndex < 0) {
      return;
    }
    const block = blocks[queue.animatingIndex];
    const reducedMotion = prefersReducedMotion(this.root.ownerDocument);
    const totalTime = shouldAnimateStreamingBlock(block, reducedMotion)
      ? Math.max(0, (block.charCount - 1) * queue.charDelay) + BLOCK_ANIMATION_FADE_MS
      : 0;
    this.streaming.animatingTimer = view.setTimeout(() => {
      this.streaming.animatingTimer = 0;
      revealAnimatingBlock(this.streaming.queue, queue.animatingIndex);
      this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, token, theme);
    }, totalTime);
  }

  async finishStreamingMarkdown(normalizedSource, token, theme) {
    this.stopStreaming();
    const result = updateSmoothTarget(this.streaming.smooth, normalizedSource, nowMs());
    if (result.immediate) {
      this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, token, theme);
    }
    forceSmoothStreamDrain(this.streaming.smooth, nowMs());
    await this.drainStreamingMarkdown(token, theme);

    await new Promise((resolve) => {
      const view = documentView(this.root.ownerDocument);
      view.setTimeout(resolve, STREAM_FINAL_SETTLE_MS);
    });
    if (token !== this.renderToken || !this.streaming.documentNode) {
      return;
    }
    this.resetStreamingState(normalizedSource);
    await this.renderFullMarkdown(normalizedSource, token, theme);
  }

  async drainStreamingMarkdown(token, theme) {
    if (this.streaming.smooth.displayedCount >= this.streaming.smooth.targetCount) {
      this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, token, theme);
      return;
    }
    await new Promise((resolve) => {
      const view = documentView(this.root.ownerDocument);
      const tick = (ts) => {
        this.streaming.frameID = 0;
        if (token !== this.renderToken) {
          resolve();
          return;
        }
        const previousFrameTs = this.streaming.smooth.lastFrameTs;
        const result = advanceSmoothFrame(this.streaming.smooth, ts);
        this.recordStreamFrameProfiler({
          at: ts,
          backlog: result.backlog,
          frameIntervalMs: previousFrameTs === null ? 0 : Math.max(0, ts - previousFrameTs),
          reserveChars: result.reserveChars || 0,
          resumeActive: result.resumeActive === true,
          revealChars: result.revealChars,
          waitingForLag: result.waitingForLag === true,
        });
        if (result.revealChars > 0 || result.done) {
          this.renderStreamingMarkdown(this.streaming.smooth.displayedContent, token, theme);
        }
        if (this.streaming.smooth.displayedCount < this.streaming.smooth.targetCount) {
          this.streaming.frameID = view.requestAnimationFrame(tick);
          return;
        }
        resolve();
      };
      this.streaming.frameID = view.requestAnimationFrame(tick);
    });
  }

  recordStreamProfiler(sample) {
    if (this.options.streamProfiler !== true) {
      return;
    }
    const view = documentView(this.root.ownerDocument);
    const at = nowMs();
    recordStreamProfilerRender(this.streaming.profiler, {
      at,
      backlog: sample.backlog,
      blockCount: sample.blockCount,
      durationMs: sample.durationMs,
      queueLength: sample.queueLength,
    });
    const snapshot = streamProfilerSnapshot(this.streaming.profiler, sample.smooth, at);
    view.__MISTER_MORPH_MARKDOWN_STREAM_PROFILER__ = {
      ...snapshot,
      lastSample: {
        ...sample,
        at: Date.now(),
      },
    };
  }

  recordStreamFrameProfiler(sample) {
    if (this.options.streamProfiler !== true) {
      return;
    }
    recordStreamProfilerFrame(this.streaming.profiler, sample);
    const view = documentView(this.root.ownerDocument);
    const snapshot = streamProfilerSnapshot(
      this.streaming.profiler,
      smoothStateSnapshot(this.streaming.smooth),
      nowMs()
    );
    view.__MISTER_MORPH_MARKDOWN_STREAM_PROFILER__ = snapshot;
  }

  async renderStandalone(format, source, token, theme) {
    const documentNode = this.root.ownerDocument.createElement("article");
    documentNode.className = "mmr-document mmr-document-standalone";
    this.root.replaceChildren(documentNode);
    const frame = makeDiagramFrame(this.root.ownerDocument, format);
    documentNode.append(frame.figure);
    try {
      const cleanup = await renderDiagram(frame.surface, format, source, theme);
      if (token !== this.renderToken) {
        cleanup?.();
        return;
      }
      if (typeof cleanup === "function") {
        this.cleanupFns.push(cleanup);
      }
    } catch (error) {
      frame.status.textContent = "failed";
      frame.figure.classList.add("is-error");
      renderError(frame.surface, source, error);
    }
  }

  async enhanceMarkdown(container, token, theme, options = {}) {
    const codeBlocks = Array.from(container.querySelectorAll("pre > code"));
    const activeCodeBlockKeys = new Set();
    const cacheKeyPrefix = stringValue(options.cacheKeyPrefix || "");
    const registerCleanup =
      typeof options.registerCleanup === "function"
        ? options.registerCleanup
        : (cleanup) => {
            this.cleanupFns.push(cleanup);
          };
    const isCurrent = typeof options.isCurrent === "function" ? options.isCurrent : () => true;
    const renderDiagrams = options.renderDiagrams !== false;
    const renderMathFences = options.renderMathFences !== false;
    for (const [index, code] of codeBlocks.entries()) {
      if (token !== this.renderToken || !isCurrent()) {
        return;
      }
      const cacheKey = `${cacheKeyPrefix}${index}`;
      activeCodeBlockKeys.add(cacheKey);
      const language = detectCodeLanguage(code);
      const pre = code.parentElement;
      if (!pre?.parentElement) {
        this.codeBlockHighlightState.delete(cacheKey);
        continue;
      }
      const source = code.textContent || "";
      const mathSource = fenceMathSource(language, source);
      if (mathSource && renderMathFences) {
        this.codeBlockHighlightState.delete(cacheKey);
        try {
          renderMathFence(pre, mathSource);
        } catch {
          const nextCacheEntry = await renderCodeBlock(pre, source, language, null, { isCurrent });
          if (token !== this.renderToken || !isCurrent()) {
            return;
          }
          if (nextCacheEntry) {
            this.codeBlockHighlightState.set(cacheKey, nextCacheEntry);
          }
        }
        continue;
      }
      if (!DIAGRAM_LANGUAGES.has(language) || !renderDiagrams) {
        const nextCacheEntry = await renderCodeBlock(
          pre,
          source,
          language,
          this.codeBlockHighlightState.get(cacheKey) || null,
          { isCurrent }
        );
        if (token !== this.renderToken || !isCurrent()) {
          return;
        }
        if (nextCacheEntry) {
          this.codeBlockHighlightState.set(cacheKey, nextCacheEntry);
        } else {
          this.codeBlockHighlightState.delete(cacheKey);
        }
        continue;
      }
      this.codeBlockHighlightState.delete(cacheKey);
      const frame = makeDiagramFrame(this.root.ownerDocument, language);
      pre.replaceWith(frame.figure);

      try {
        const cleanup = await renderDiagram(frame.surface, language, source, theme);
        if (token !== this.renderToken || !isCurrent()) {
          cleanup?.();
          return;
        }
        if (typeof cleanup === "function") {
          registerCleanup(cleanup);
        }
      } catch (error) {
        frame.status.textContent = "failed";
        frame.figure.classList.add("is-error");
        renderError(frame.surface, source, error);
      }
    }
    if (token !== this.renderToken || !isCurrent()) {
      return;
    }
    for (const key of Array.from(this.codeBlockHighlightState.keys())) {
      if (cacheKeyPrefix && !key.startsWith(cacheKeyPrefix)) {
        continue;
      }
      if (!activeCodeBlockKeys.has(key)) {
        this.codeBlockHighlightState.delete(key);
      }
    }
    enhanceInlineCode(container);
  }
}

export function mountMarkdownRenderer(root, source = "", options = {}) {
  const renderer = new MarkdownRenderer(root, options);
  void renderer.update(source, options);
  return renderer;
}

export const supportedFenceLanguages = Array.from(DIAGRAM_LANGUAGES);
export const supportedThemes = themeNames;
export { themeCatalog, themes, resolveTheme };
