# markdown-renderer

`web/markdown-renderer` is a standalone browser-side markdown renderer used by
the console frontend. It does not depend on Vue. The console consumes its built
artifacts from `web/console/src/vendor/markdown-renderer/` and wraps them with a
thin Vue component.

## Scope

- Markdown + GFM
- Inline HTML
- Syntax highlighting
- KaTeX math rendering
- Mermaid
- Graphviz
- Infographic
- Theme system shared across markdown, diagrams, and console embedding

## Supported Syntax

### Markdown

- Paragraphs, headings, lists, tables, task lists
- Inline code and fenced code blocks
- Inline HTML

### Math

- Inline math: `$E=mc^2$`
- Display math:

  ```md
  $$
  E=mc^2
  $$
  ```

- Fenced math blocks are also normalized into display math:

  ````md
  ```
  $$
  E=mc^2
  $$
  ```
  ````

  ````md
  ```math
  E=mc^2
  ```
  ````

Supported math fence languages: `math`, `latex`, `tex`, `katex`.

### Diagrams

- `mermaid`
- `mmd`
- `graphviz`
- `dot`
- `gv`
- `infographic`

Pure-source auto detection currently supports:

- Mermaid source
- Graphviz DOT
- Infographic source

## Themes

Built-in themes:

- `paper`
- `console`
- `folio`
- `blueprint`

Each theme drives:

- CSS variables for markdown content
- Mermaid theme variables
- Graphviz render attributes
- Infographic theme config

The renderer exports `supportedThemes`, `themeCatalog`, `themes`, and
`resolveTheme`.

## Public API

Main exports from `src/index.js`:

- `MarkdownRenderer`
- `mountMarkdownRenderer(root, source, options)`
- `supportedFenceLanguages`
- `supportedThemes`
- `themeCatalog`
- `themes`
- `resolveTheme`

Renderer options:

- `format`: default `auto`
- `theme`: default `paper`
- `streaming`: default `false`
- `streamMode`: `realtime`, `balanced`, or `silky`; default `balanced`
- `streamProfiler`: default `false`

## Streaming Rendering

Streaming is owned by `MarkdownRenderer`. Callers keep sending complete Markdown
source snapshots and set `streaming: true` while the answer is still growing.
The renderer decides how much of the latest snapshot should be visible on each
animation frame.

### Data Flow

1. `update(source, { streaming: true })` normalizes the complete source snapshot.
2. Append-only snapshots update the smooth stream target.
3. Non-append replacements synchronize immediately, because the old displayed
   text can no longer be safely animated toward the new source.
4. A `requestAnimationFrame` loop advances `displayedContent` toward
   `targetContent`.
5. When `streaming` becomes `false`, the renderer drains the remaining buffered
   text, waits briefly for the tail animation to settle, then rerenders through
   the full non-streaming Markdown path.

### Smoothing

The smoothing state tracks target chars, displayed chars, backlog, upstream
arrival rate, chunk size, release speed, and resume state.

`streamMode` changes the preset:

- `realtime`: faster release, smaller buffer.
- `balanced`: default tradeoff.
- `silky`: slower release, larger buffer.

The release speed is dynamic. It rises when backlog grows, drains faster after
upstream input settles, and uses a short warmup when new content arrives after
an empty buffer. The internal `reserveChars` metric is only a release pacing
buffer; it never adds DOM height.

### Markdown Blocks

Every displayed snapshot is repaired, split into top-level Markdown blocks, and
reconciled against the previous streaming DOM.

Blocks move through these states:

- `revealed`: already stable content.
- `animating`: a newly released block that may still run text animation.
- `streaming`: the tail block that is still growing.
- `queued`: complete block content waiting behind the current animated block.

Only normal text blocks get character fade animation. Code, tables, math, SVG,
diagrams, and HTML-heavy blocks skip character animation because their DOM shape
can change substantially during enhancement.

### Heavy Enhancements

Streaming blocks still use the full Markdown HTML path, so inline KaTeX and
sanitized raw HTML are visible while content streams.

Fenced code blocks create the final wrapper synchronously: `<pre>`, `<code>`,
language badge, and copy button are present from the start. Shiki highlighting
only updates token spans inside the existing `<code>` element, so the block does
not switch between a plain shell and the final enhanced shell.

Complete math and diagram fences are enhanced during streaming. Repaired but
incomplete math and diagram fences stay as code until the fence closes.

### Layout And Scroll

Streaming DOM height comes only from rendered content. The renderer does not add
a height floor and does not insert a tail spacer for pending backlog.

The streaming document disables grid row stretching with `align-content: start`
and keeps streaming/final block margins aligned. This keeps the final
non-streaming rerender close to the streaming height.

The console wrapper observes the markdown host with `ResizeObserver` and emits
`rendered` when the content size changes. Chat scroll code uses that signal to
keep the list pinned to the bottom only when the user is already at the bottom.

### Profiler

Set `streamProfiler: true` to expose metrics on
`window.__MISTER_MORPH_MARKDOWN_STREAM_PROFILER__`.

The snapshot includes:

- render and frame history;
- backlog and target/displayed size;
- slow frame and slow render counts;
- queue length and block counts;
- release speed and internal `reserveChars`;
- resume state and reveal size.

## Build

Install dependencies:

```sh
pnpm install
```

Build the standalone bundle:

```sh
pnpm run build
```

Build and copy artifacts into the console vendor directory:

```sh
pnpm run build-console
```

## Console Integration

The console wrapper lives at:

- `web/console/src/components/MarkdownContent.js`

It lazy-loads:

- `web/console/src/vendor/markdown-renderer/index.js`
- `web/console/src/vendor/markdown-renderer/index.css`

Current console chat uses the `console` theme for agent output.

## Notes

- The renderer is intentionally framework-agnostic.
- Mermaid, Graphviz, and Infographic remain the heavy parts of the bundle.
