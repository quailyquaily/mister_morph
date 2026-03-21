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

let mermaidInitialized = false;
let mermaidThemeID = "";
let mermaidSequence = 0;
let vizPromise = null;
let mermaidModulePromise = null;
let infographicClassPromise = null;
const COPY_FEEDBACK_DURATION_MS = 360;
const SHIKI_THEME = "github-dark";
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

async function highlightCodeWithShiki(codeNode, source, language, cacheEntry) {
  const resolvedLanguage = await ensureShikiLanguage(language);
  if (!resolvedLanguage) {
    codeNode.textContent = source;
    codeNode.classList.remove("mmr-code-content");
    return null;
  }

  const highlighter = await getShikiHighlighter();
  const normalizedSource = stringValue(source);
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

async function renderCodeBlock(pre, source, language = "", cacheEntry = null) {
  const doc = pre.ownerDocument;
  const wrapper = doc.createElement("div");
  wrapper.className = "mmr-code-block";

  const normalizedLanguage = stringValue(language).trim();
  const codeNode = pre.querySelector("code");
  let nextCacheEntry = null;
  if (codeNode) {
    nextCacheEntry = await highlightCodeWithShiki(codeNode, source, normalizedLanguage, cacheEntry);
  }
  if (normalizedLanguage) {
    const badge = doc.createElement("span");
    badge.className = "mmr-code-language";
    badge.textContent = normalizedLanguage;
    wrapper.append(badge);
  }

  const button = doc.createElement("button");
  button.type = "button";
  button.className = "mmr-code-copy";
  button.setAttribute("aria-label", "Copy code");
  button.setAttribute("title", "Copy code");
  button.append(createCopyIcon(doc));

  let resetTimerID = 0;
  button.addEventListener("click", async () => {
    if (button.disabled) {
      return;
    }
    button.disabled = true;
    try {
      await copyTextToClipboard(source, doc);
      button.dataset.copyState = "copied";
      button.setAttribute("title", "Copied");
      button.setAttribute("aria-label", "Code copied");
    } catch {
      button.dataset.copyState = "failed";
      button.setAttribute("title", "Copy failed");
      button.setAttribute("aria-label", "Copy failed");
    } finally {
      const view = documentView(doc);
      view.clearTimeout(resetTimerID);
      resetTimerID = view.setTimeout(() => {
        button.disabled = false;
        button.dataset.copyState = "";
        button.setAttribute("title", "Copy code");
        button.setAttribute("aria-label", "Copy code");
      }, COPY_FEEDBACK_DURATION_MS);
    }
  });

  pre.replaceWith(wrapper);
  wrapper.append(button, pre);
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
    const updateSignature = `${requestedTheme}\u0000${requestedFormat}\u0000${normalizedSource}`;
    if (updateSignature === this.lastUpdateSignature) {
      return;
    }
    this.lastUpdateSignature = updateSignature;
    const theme = applyThemeToElement(this.root, resolveTheme(this.options.theme));
    const format = inferFormat(normalizedSource, this.options.format);
    this.cleanup();
    this.root.replaceChildren();

    if (!normalizedSource.trim()) {
      this.codeBlockHighlightState.clear();
      return;
    }

    if (format !== "markdown") {
      this.codeBlockHighlightState.clear();
      await this.renderStandalone(format, normalizedSource, token, theme);
      return;
    }

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

  destroy() {
    this.cleanup();
    this.root.replaceChildren();
    this.root.classList.remove("mmr-root");
    this.lastUpdateSignature = "";
    this.codeBlockHighlightState.clear();
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

  async enhanceMarkdown(container, token, theme) {
    const codeBlocks = Array.from(container.querySelectorAll("pre > code"));
    const activeCodeBlockKeys = new Set();
    for (const [index, code] of codeBlocks.entries()) {
      if (token !== this.renderToken) {
        return;
      }
      const cacheKey = String(index);
      activeCodeBlockKeys.add(cacheKey);
      const language = detectCodeLanguage(code);
      const pre = code.parentElement;
      if (!pre?.parentElement) {
        this.codeBlockHighlightState.delete(cacheKey);
        continue;
      }
      const source = code.textContent || "";
      const mathSource = fenceMathSource(language, source);
      if (mathSource) {
        this.codeBlockHighlightState.delete(cacheKey);
        try {
          renderMathFence(pre, mathSource);
        } catch {
          const nextCacheEntry = await renderCodeBlock(pre, source, language);
          if (token !== this.renderToken) {
            return;
          }
          if (nextCacheEntry) {
            this.codeBlockHighlightState.set(cacheKey, nextCacheEntry);
          }
        }
        continue;
      }
      if (!DIAGRAM_LANGUAGES.has(language)) {
        const nextCacheEntry = await renderCodeBlock(
          pre,
          source,
          language,
          this.codeBlockHighlightState.get(cacheKey) || null
        );
        if (token !== this.renderToken) {
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
    if (token !== this.renderToken) {
      return;
    }
    for (const key of Array.from(this.codeBlockHighlightState.keys())) {
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
