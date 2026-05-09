export const STREAM_MODES = ["realtime", "balanced", "silky"];

export const STREAM_PRESET_CONFIG = Object.freeze({
  balanced: Object.freeze({
    activeInputWindowMs: 220,
    defaultCps: 38,
    emaAlpha: 0.2,
    flushCps: 120,
    largeAppendChars: 120,
    maxActiveCps: 132,
    maxCps: 72,
    maxFlushCps: 280,
    minCps: 18,
    settleAfterMs: 360,
    settleDrainMaxMs: 520,
    settleDrainMinMs: 180,
    targetBufferMs: 120,
  }),
  realtime: Object.freeze({
    activeInputWindowMs: 140,
    defaultCps: 50,
    emaAlpha: 0.3,
    flushCps: 170,
    largeAppendChars: 180,
    maxActiveCps: 180,
    maxCps: 96,
    maxFlushCps: 360,
    minCps: 24,
    settleAfterMs: 260,
    settleDrainMaxMs: 360,
    settleDrainMinMs: 140,
    targetBufferMs: 40,
  }),
  silky: Object.freeze({
    activeInputWindowMs: 320,
    defaultCps: 28,
    emaAlpha: 0.14,
    flushCps: 96,
    largeAppendChars: 100,
    maxActiveCps: 102,
    maxCps: 56,
    maxFlushCps: 220,
    minCps: 14,
    settleAfterMs: 460,
    settleDrainMaxMs: 680,
    settleDrainMinMs: 240,
    targetBufferMs: 170,
  }),
});

export const BLOCK_ANIMATION_BASE_DELAY = 18;
export const BLOCK_ANIMATION_ACCELERATION = 0.3;
export const BLOCK_ANIMATION_FADE_MS = 280;
export const BLOCK_ANIMATION_MAX_DURATION_MS = 3000;
export const STREAM_CHAR_ANIMATION_LIMIT = 1200;
export const STREAM_FRAME_CHAR_CHUNK_LIMIT = 12;
export const STREAM_RELEASE_FALL_CPS_PER_SECOND = 240;
export const STREAM_RELEASE_RISE_CPS_PER_SECOND = 760;
export const STREAM_RESUME_MAX_REVEAL_CHARS = 2;
export const STREAM_RESUME_MIN_MS = 180;
export const STREAM_RESUME_MAX_MS = 420;
export const STREAM_PROFILER_HISTORY_LIMIT = 120;
export const STREAM_PROFILER_SLOW_FRAME_MS = 34;
export const STREAM_PROFILER_SLOW_RENDER_MS = 16;

const FENCE_RE = /^([ \t]*)(`{3,}|~{3,})([^`~\n]*)$/;
const DIAGRAM_LANGUAGES = new Set(["mermaid", "graphviz", "infographic"]);
const MATH_LANGUAGES = new Set(["math", "latex", "tex", "katex"]);

function stringValue(value) {
  return typeof value === "string" ? value : String(value ?? "");
}

export function normalizeStreamMode(raw) {
  const value = stringValue(raw).trim();
  return STREAM_MODES.includes(value) ? value : "balanced";
}

export function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

export function countChars(text) {
  return [...stringValue(text)].length;
}

export function charsOf(text) {
  return [...stringValue(text)];
}

function createSmoothMetrics() {
  return {
    appendCount: 0,
    displayedChars: 0,
    frameCount: 0,
    largeAppendCount: 0,
    largeAppendSyncs: 0,
    lastBacklog: 0,
    lastInputActive: false,
    lastRevealChars: 0,
    lastReleaseCps: 0,
    lastReserveChars: 0,
    lastResumeActive: false,
    lastTargetCps: 0,
    lastSettling: false,
    maxBacklog: 0,
    maxReleaseCps: 0,
    maxReleaseCpsDelta: 0,
    maxReserveChars: 0,
    maxResumeRevealChars: 0,
    maxRevealChars: 0,
    resetCount: 0,
    resumeCount: 0,
    revealFrameCount: 0,
    skippedFrameCount: 0,
    targetChars: 0,
    totalAppendedChars: 0,
  };
}

export function createSmoothStreamState(content = "", options = {}) {
  const mode = normalizeStreamMode(options.mode);
  const config = STREAM_PRESET_CONFIG[mode];
  const targetContent = stringValue(content);
  const targetChars = charsOf(targetContent);
  const now = Number.isFinite(options.now) ? options.now : 0;
  const metrics = createSmoothMetrics();
  metrics.displayedChars = targetChars.length;
  metrics.targetChars = targetChars.length;

  return {
    arrivalCpsEma: config.defaultCps,
    chunkSizeEma: 1,
    config,
    displayedContent: targetContent,
    displayedCount: targetChars.length,
    emaCps: config.defaultCps,
    lastFrameTs: null,
    lastInputCount: targetChars.length,
    lastInputTs: now,
    metrics,
    mode,
    inputIntervalEma: config.targetBufferMs,
    inputIntervalJitterEma: 0,
    releaseCarry: 0,
    releaseCps: config.defaultCps,
    resumeStartTs: 0,
    resumeUntilTs: 0,
    targetChars,
    targetContent,
    targetCount: targetChars.length,
  };
}

export function smoothStateSnapshot(state) {
  return {
    backlog: Math.max(0, state.targetCount - state.displayedCount),
    displayedChars: state.displayedCount,
    displayedContent: state.displayedContent,
    metrics: { ...state.metrics },
    mode: state.mode,
    targetChars: state.targetCount,
    targetContent: state.targetContent,
  };
}

export function syncSmoothStreamState(state, content, now = 0) {
  const nextContent = stringValue(content);
  const chars = charsOf(nextContent);
  state.targetContent = nextContent;
  state.targetChars = chars;
  state.targetCount = chars.length;
  state.displayedContent = nextContent;
  state.displayedCount = chars.length;
  state.emaCps = state.config.defaultCps;
  state.chunkSizeEma = 1;
  state.arrivalCpsEma = state.config.defaultCps;
  state.inputIntervalEma = state.config.targetBufferMs;
  state.inputIntervalJitterEma = 0;
  state.releaseCarry = 0;
  state.releaseCps = state.config.defaultCps;
  state.resumeStartTs = 0;
  state.resumeUntilTs = 0;
  state.lastInputTs = now;
  state.lastInputCount = chars.length;
  state.lastFrameTs = null;
  state.metrics.displayedChars = chars.length;
  state.metrics.targetChars = chars.length;
  state.metrics.lastBacklog = 0;
}

export function updateSmoothTarget(state, content, now = 0) {
  const wasEmpty = state.displayedCount >= state.targetCount;
  const lastActivityTs = Math.max(state.lastFrameTs || 0, state.lastInputTs || 0);
  const idleMs = Math.max(0, now - lastActivityTs);
  coolReleaseForEmptyBuffer(state, now);
  const nextContent = stringValue(content);
  if (nextContent === state.targetContent) {
    return { appendOnly: true, appendedChars: 0, immediate: false, reset: false };
  }

  const appendOnly = nextContent.startsWith(state.targetContent);
  if (!appendOnly) {
    syncSmoothStreamState(state, nextContent, now);
    state.metrics.resetCount += 1;
    return { appendOnly: false, appendedChars: state.targetCount, immediate: true, reset: true };
  }

  const appended = nextContent.slice(state.targetContent.length);
  const appendedChars = charsOf(appended);
  const appendedCount = appendedChars.length;
  state.metrics.appendCount += 1;
  state.metrics.totalAppendedChars += appendedCount;

  state.targetContent = nextContent;
  state.targetChars = [...state.targetChars, ...appendedChars];
  state.targetCount += appendedCount;
  if (appendedCount > state.config.largeAppendChars) {
    state.metrics.largeAppendCount += 1;
  }

  const deltaChars = state.targetCount - state.lastInputCount;
  const deltaMs = Math.max(1, now - state.lastInputTs);
  if (deltaChars > 0) {
    const instantCps = (deltaChars * 1000) / deltaMs;
    const normalizedInstantCps = clamp(instantCps, state.config.minCps, state.config.maxFlushCps * 2);
    const chunkAlpha = 0.35;
    const intervalAlpha = 0.22;
    const intervalMs = clamp(deltaMs, 16, 1200);
    const previousInterval = state.inputIntervalEma || intervalMs;
    state.inputIntervalEma = previousInterval * (1 - intervalAlpha) + intervalMs * intervalAlpha;
    state.inputIntervalJitterEma =
      state.inputIntervalJitterEma * (1 - intervalAlpha) +
      Math.abs(intervalMs - previousInterval) * intervalAlpha;
    state.chunkSizeEma = state.chunkSizeEma * (1 - chunkAlpha) + appendedCount * chunkAlpha;
    state.arrivalCpsEma =
      state.arrivalCpsEma * (1 - chunkAlpha) + normalizedInstantCps * chunkAlpha;

    const clampedCps = clamp(instantCps, state.config.minCps, state.config.maxActiveCps);
    state.emaCps = state.emaCps * (1 - state.config.emaAlpha) + clampedCps * state.config.emaAlpha;
    if (appendedCount > state.config.largeAppendChars) {
      state.emaCps = Math.max(state.emaCps, state.config.maxCps);
      state.arrivalCpsEma = Math.max(state.arrivalCpsEma, state.config.maxFlushCps);
    }
  }

  if (wasEmpty && appendedCount > 0) {
    startResumeWindow(state, now, idleMs, appendedCount);
  }

  state.lastInputTs = now;
  state.lastInputCount = state.targetCount;
  state.metrics.targetChars = state.targetCount;
  state.metrics.maxBacklog = Math.max(state.metrics.maxBacklog, state.targetCount - state.displayedCount);

  return { appendOnly: true, appendedChars: appendedCount, immediate: false, reset: false };
}

function coolReleaseForEmptyBuffer(state, now = 0) {
  if (state.displayedCount < state.targetCount) {
    return;
  }
  const lastActivityTs = Math.max(state.lastFrameTs || 0, state.lastInputTs || 0);
  const idleMs = Math.max(0, now - lastActivityTs);
  if (idleMs <= 0) {
    return;
  }
  const idleSeconds = Math.min(idleMs / 1000, 1.5);
  const targetCps = idleMs > state.config.activeInputWindowMs ? state.config.minCps : state.config.defaultCps;
  let nextCps = Math.max(targetCps, state.releaseCps - STREAM_RELEASE_FALL_CPS_PER_SECOND * idleSeconds);
  if (idleMs >= state.config.settleAfterMs) {
    nextCps = Math.min(nextCps, state.config.minCps);
  } else if (idleMs >= state.config.activeInputWindowMs) {
    nextCps = Math.min(nextCps, state.config.defaultCps);
  }
  state.releaseCps = clamp(nextCps, state.config.minCps, state.config.maxFlushCps);
  state.releaseCarry = 0;
}

function startResumeWindow(state, now, idleMs, appendedCount) {
  const idlePressure = clamp(idleMs / Math.max(1, state.config.activeInputWindowMs), 0, 2);
  const chunkPressure = clamp(appendedCount / Math.max(1, state.config.largeAppendChars), 0, 1);
  const duration =
    STREAM_RESUME_MIN_MS +
    (STREAM_RESUME_MAX_MS - STREAM_RESUME_MIN_MS) * clamp(idlePressure * 0.55 + chunkPressure * 0.45, 0, 1);
  state.resumeStartTs = now;
  state.resumeUntilTs = now + duration;
  state.releaseCarry = 0;
  state.metrics.resumeCount += 1;
}

export function forceSmoothStreamDrain(state, now = 0) {
  state.lastInputTs = now - state.config.settleAfterMs - 1;
}

export function advanceSmoothFrame(state, now = 0) {
  if (state.lastFrameTs === null) {
    state.lastFrameTs = now;
    return { backlog: state.targetCount - state.displayedCount, done: false, revealChars: 0, resumeActive: false };
  }

  const frameIntervalMs = Math.max(0, now - state.lastFrameTs);
  const dtSeconds = Math.max(0.001, Math.min(frameIntervalMs / 1000, 0.05));
  state.lastFrameTs = now;

  const backlog = state.targetCount - state.displayedCount;
  if (backlog <= 0) {
    state.metrics.lastBacklog = 0;
    state.metrics.displayedChars = state.displayedCount;
    return { backlog: 0, done: true, revealChars: 0, resumeActive: false };
  }

  const idleMs = now - state.lastInputTs;
  const inputActive = idleMs <= state.config.activeInputWindowMs;
  const settling = idleMs >= state.config.settleAfterMs;
  const plan = computeReleasePlan(state, backlog, settling, now);
  const targetCps = plan.targetCps;
  const previousReleaseCps = state.releaseCps;
  const speedAlpha = settling ? 0.24 : 0.22;
  const desiredDelta = (targetCps - previousReleaseCps) * speedAlpha;
  const deltaLimit =
    (targetCps >= previousReleaseCps ? STREAM_RELEASE_RISE_CPS_PER_SECOND : STREAM_RELEASE_FALL_CPS_PER_SECOND) *
    dtSeconds;
  state.releaseCps = clamp(
    previousReleaseCps + clamp(desiredDelta, -deltaLimit, deltaLimit),
    state.config.minCps,
    state.config.maxFlushCps
  );
  state.releaseCarry += state.releaseCps * dtSeconds;

  let revealChars = Math.floor(state.releaseCarry);
  if (revealChars <= 0) {
    recordSmoothFrame(state, {
      backlog,
      inputActive,
      releaseCps: state.releaseCps,
      releaseCpsDelta: Math.abs(state.releaseCps - previousReleaseCps),
      reserveChars: plan.reserveChars,
      revealChars: 0,
      resumeActive: plan.resumeActive,
      settling,
      targetCps,
    });
    return {
      backlog,
      done: false,
      reserveChars: plan.reserveChars,
      resumeActive: plan.resumeActive,
      revealChars: 0,
      targetCps,
    };
  }
  const frameRevealLimit = settling ? STREAM_FRAME_CHAR_CHUNK_LIMIT : Math.max(1, STREAM_FRAME_CHAR_CHUNK_LIMIT - 4);
  revealChars = Math.min(revealChars, frameRevealLimit, backlog);
  if (!settling && plan.resumeActive) {
    const resumeLimit = plan.resumeProgress < 0.66 ? 1 : STREAM_RESUME_MAX_REVEAL_CHARS;
    revealChars = Math.min(revealChars, resumeLimit);
  }
  if (!settling && backlog <= plan.reserveChars) {
    revealChars = Math.min(revealChars, 1);
  }
  state.releaseCarry = Math.max(0, state.releaseCarry - revealChars);

  const nextCount = state.displayedCount + revealChars;
  const segment = state.targetChars.slice(state.displayedCount, nextCount).join("");
  if (segment) {
    state.displayedContent += segment;
    state.displayedCount = nextCount;
  } else {
    state.displayedContent = state.targetContent;
    state.displayedCount = state.targetCount;
    revealChars = backlog;
  }

  recordSmoothFrame(state, {
    backlog,
    inputActive,
    releaseCps: state.releaseCps,
    releaseCpsDelta: Math.abs(state.releaseCps - previousReleaseCps),
    reserveChars: plan.reserveChars,
    revealChars,
    resumeActive: plan.resumeActive,
    settling,
    targetCps,
  });

  return {
    backlog: Math.max(0, state.targetCount - state.displayedCount),
    done: state.displayedCount >= state.targetCount,
    revealChars,
    reserveChars: plan.reserveChars,
    resumeActive: plan.resumeActive,
    targetCps,
  };
}

function computeReleasePlan(state, backlog, settling, now = 0) {
  const config = state.config;
  const arrivalCps = clamp(state.arrivalCpsEma, config.minCps, config.maxFlushCps);
  const reserveChars = settling ? 0 : computeTargetReserveChars(state, arrivalCps);
  const overflowChars = Math.max(0, backlog - reserveChars);
  const baseBufferChars = Math.max(3, reserveChars, Math.round(state.chunkSizeEma * 0.45));
  const pressure = clamp(overflowChars / baseBufferChars, 0, 3);
  const quadratic = pressure * pressure;
  const activeCap = clamp(config.maxActiveCps + state.chunkSizeEma * 2, config.maxActiveCps, config.maxFlushCps);
  const curveMax = settling ? config.maxFlushCps : activeCap;
  const reservePressure = reserveChars > 0 ? clamp(backlog / reserveChars, 0, 1) : 1;
  const lowCps = settling ? config.flushCps : Math.max(2, config.minCps * reservePressure * reservePressure);
  let targetCps = lowCps + (curveMax - lowCps) * clamp(quadratic / 6, 0, 1);
  const resume = computeResumeFactor(state, now);

  if (!settling && resume.active) {
    const resumeCap = lowCps + (curveMax - lowCps) * resume.factor;
    targetCps = Math.min(targetCps, resumeCap);
  }

  if (settling) {
    const drainTargetMs = clamp(backlog * 9, config.settleDrainMinMs, config.settleDrainMaxMs);
    const settleCps = (backlog * 1000) / drainTargetMs;
    targetCps = Math.max(targetCps, clamp(settleCps, config.flushCps, config.maxFlushCps));
  }

  return {
    reserveChars,
    resumeActive: resume.active,
    resumeProgress: resume.progress,
    targetCps: clamp(targetCps, 0, config.maxFlushCps),
  };
}

function computeResumeFactor(state, now) {
  if (!state.resumeUntilTs || now >= state.resumeUntilTs) {
    return { active: false, factor: 1, progress: 1 };
  }
  const duration = Math.max(1, state.resumeUntilTs - state.resumeStartTs);
  const progress = clamp((now - state.resumeStartTs) / duration, 0, 1);
  const eased = 1 - (1 - progress) * (1 - progress);
  return {
    active: true,
    factor: 0.16 + eased * 0.84,
    progress,
  };
}

function computeTargetReserveChars(state, arrivalCps) {
  const config = state.config;
  const adaptiveBufferMs = clamp(
    Math.max(config.targetBufferMs, state.inputIntervalEma * 1.1 + state.inputIntervalJitterEma * 0.8),
    config.targetBufferMs,
    config.activeInputWindowMs * 1.8
  );
  const reserveFromArrival = (arrivalCps * adaptiveBufferMs * 0.72) / 1000;
  const reserveFromChunk = state.chunkSizeEma * 0.65;
  return clamp(Math.round(Math.max(2, reserveFromArrival, reserveFromChunk)), 2, 36);
}

function recordSmoothFrame(state, sample) {
  state.metrics.frameCount += 1;
  state.metrics.lastBacklog = Math.max(0, sample.backlog);
  state.metrics.lastInputActive = sample.inputActive;
  state.metrics.lastRevealChars = Math.max(0, sample.revealChars);
  state.metrics.lastReleaseCps = sample.releaseCps || state.releaseCps || 0;
  state.metrics.lastReserveChars = sample.reserveChars || 0;
  state.metrics.lastResumeActive = sample.resumeActive === true;
  state.metrics.lastTargetCps = sample.targetCps || 0;
  state.metrics.lastSettling = sample.settling;
  state.metrics.maxBacklog = Math.max(state.metrics.maxBacklog, sample.backlog);
  state.metrics.maxReleaseCps = Math.max(state.metrics.maxReleaseCps, sample.releaseCps || 0);
  state.metrics.maxReleaseCpsDelta = Math.max(state.metrics.maxReleaseCpsDelta, sample.releaseCpsDelta || 0);
  state.metrics.maxReserveChars = Math.max(state.metrics.maxReserveChars, sample.reserveChars || 0);
  state.metrics.maxRevealChars = Math.max(state.metrics.maxRevealChars, sample.revealChars);
  if (sample.resumeActive) {
    state.metrics.maxResumeRevealChars = Math.max(state.metrics.maxResumeRevealChars, sample.revealChars);
  }
  state.metrics.displayedChars = state.displayedCount;
  state.metrics.targetChars = state.targetCount;

  if (sample.revealChars > 0) {
    state.metrics.revealFrameCount += 1;
  } else {
    state.metrics.skippedFrameCount += 1;
  }
}

export function repairStreamingMarkdown(source) {
  const text = stringValue(source).replaceAll("\r\n", "\n").replaceAll("\r", "\n");
  if (!text) {
    return "";
  }

  const lines = text.split("\n");
  let openFence = null;
  for (const line of lines) {
    const match = line.match(FENCE_RE);
    if (!match) {
      continue;
    }
    const fence = match[2];
    if (!openFence) {
      openFence = fence;
      continue;
    }
    if (fence[0] === openFence[0] && fence.length >= openFence.length) {
      openFence = null;
    }
  }

  if (!openFence) {
    return text;
  }

  const separator = text.endsWith("\n") ? "" : "\n";
  return `${text}${separator}${openFence}`;
}

export function classifyStreamingBlock(raw) {
  const text = stringValue(raw);
  const trimmed = text.trim();
  const fenceMatch = trimmed.match(/^(`{3,}|~{3,})[ \t]*([^\s`~]*)/);
  if (fenceMatch) {
    const language = fenceMatch[2].trim().toLowerCase();
    if (DIAGRAM_LANGUAGES.has(language)) {
      return { animateText: false, heavy: true, kind: "diagram", language };
    }
    if (MATH_LANGUAGES.has(language)) {
      return { animateText: false, heavy: true, kind: "math", language };
    }
    return { animateText: false, heavy: true, kind: "code", language };
  }
  if (/^\|.+\|\s*\n\|[ \t]*:?-{3,}:?[ \t]*(?:\|[ \t]*:?-{3,}:?[ \t]*)*\|?/m.test(trimmed)) {
    return { animateText: false, heavy: true, kind: "table", language: "" };
  }
  if (/^<([a-z][\w:-]*)(\s|>|\/>)/i.test(trimmed)) {
    return { animateText: false, heavy: true, kind: "html", language: "" };
  }
  if (/^#{1,6}\s/.test(trimmed)) {
    return { animateText: true, heavy: false, kind: "heading", language: "" };
  }
  if (/^>\s?/m.test(trimmed)) {
    return { animateText: true, heavy: false, kind: "blockquote", language: "" };
  }
  if (/^(\s*)([-*+]|\d+[.)])\s+/m.test(trimmed)) {
    return { animateText: true, heavy: false, kind: "list", language: "" };
  }
  return { animateText: true, heavy: false, kind: trimmed ? "paragraph" : "blank", language: "" };
}

export function makeStreamingBlock(raw, startOffset, index = 0) {
  const source = stringValue(raw);
  const meta = classifyStreamingBlock(source);
  return {
    ...meta,
    charCount: countChars(source),
    endOffset: startOffset + source.length,
    index,
    key: `b:${startOffset}`,
    raw: source,
    startOffset,
  };
}

export function createBlockQueueState() {
  return {
    activeIndex: -1,
    charDelay: BLOCK_ANIMATION_BASE_DELAY,
    minRevealed: 0,
    previousBlockCount: 0,
    queueLength: 0,
    revealedCount: 0,
  };
}

export function resetBlockQueueState(queue) {
  queue.activeIndex = -1;
  queue.charDelay = BLOCK_ANIMATION_BASE_DELAY;
  queue.minRevealed = 0;
  queue.previousBlockCount = 0;
  queue.queueLength = 0;
  queue.revealedCount = 0;
}

export function computeBlockCharDelay(queueLength, charCount) {
  const acceleration = 1 + queueLength * BLOCK_ANIMATION_ACCELERATION;
  const delay = BLOCK_ANIMATION_BASE_DELAY / acceleration;
  return Math.min(delay, BLOCK_ANIMATION_MAX_DURATION_MS / Math.max(charCount, 1));
}

export function resolveBlockQueue(queue, blocks) {
  const blockCount = Array.isArray(blocks) ? blocks.length : 0;
  if (blockCount === 0) {
    resetBlockQueueState(queue);
    return {
      animatingIndex: -1,
      charDelay: BLOCK_ANIMATION_BASE_DELAY,
      queueLength: 0,
      states: [],
    };
  }

  if (blockCount > queue.previousBlockCount && queue.previousBlockCount > 0) {
    const previousTail = queue.previousBlockCount - 1;
    queue.minRevealed = Math.max(queue.minRevealed, previousTail + 1);
  }
  queue.previousBlockCount = blockCount;

  const effectiveRevealed = Math.max(queue.revealedCount, queue.minRevealed);
  const tailIndex = blockCount - 1;
  const states = blocks.map((_, index) => {
    if (index < effectiveRevealed) {
      return "revealed";
    }
    if (index === effectiveRevealed && index < tailIndex) {
      return "animating";
    }
    if (index === effectiveRevealed && index === tailIndex) {
      return "streaming";
    }
    return "queued";
  });

  const animatingIndex = states.indexOf("animating");
  const streamingIndex = states.indexOf("streaming");
  const activeIndex = animatingIndex >= 0 ? animatingIndex : streamingIndex;
  const queueLength = Math.max(0, tailIndex - effectiveRevealed - 1);
  const activeBlock = activeIndex >= 0 ? blocks[activeIndex] : null;

  if (activeIndex >= 0 && activeIndex !== queue.activeIndex) {
    queue.activeIndex = activeIndex;
    queue.charDelay = computeBlockCharDelay(queueLength, activeBlock?.charCount || 0);
  }
  queue.queueLength = queueLength;

  return {
    animatingIndex,
    charDelay: activeIndex >= 0 ? queue.charDelay : BLOCK_ANIMATION_BASE_DELAY,
    queueLength,
    states,
  };
}

export function revealAnimatingBlock(queue, index) {
  if (!Number.isFinite(index) || index < 0) {
    return;
  }
  queue.revealedCount = Math.max(queue.revealedCount, index + 1);
}

export function shouldAnimateStreamingBlock(block, reducedMotion = false) {
  if (!block || reducedMotion) {
    return false;
  }
  return block.animateText && !block.heavy && block.charCount <= STREAM_CHAR_ANIMATION_LIMIT;
}

export function createStreamProfilerState(options = {}) {
  return {
    currentBacklog: 0,
    currentBlockCount: 0,
    currentQueueLength: 0,
    firstSampleAt: 0,
    frameCount: 0,
    frames: [],
    historyLimit: Number.isFinite(options.historyLimit)
      ? Math.max(1, Math.trunc(options.historyLimit))
      : STREAM_PROFILER_HISTORY_LIMIT,
    lastFrame: null,
    lastRender: null,
    maxBacklog: 0,
    maxBlockCount: 0,
    maxFrameIntervalMs: 0,
    maxQueueLength: 0,
    maxRenderMs: 0,
    maxRevealChars: 0,
    renderCount: 0,
    renders: [],
    revealFrameCount: 0,
    skippedFrameCount: 0,
    slowFrameCount: 0,
    slowRenderCount: 0,
    totalRenderMs: 0,
  };
}

export function resetStreamProfilerState(state, options = {}) {
  const next = createStreamProfilerState({
    historyLimit: Number.isFinite(options.historyLimit) ? options.historyLimit : state.historyLimit,
  });
  Object.assign(state, next);
}

function rememberProfilerSample(list, sample, limit) {
  list.push(sample);
  while (list.length > limit) {
    list.shift();
  }
}

function setProfilerStart(state, at) {
  if (!state.firstSampleAt && Number.isFinite(at) && at > 0) {
    state.firstSampleAt = at;
  }
}

export function recordStreamProfilerFrame(state, sample = {}) {
  const at = Number.isFinite(sample.at) ? sample.at : 0;
  const backlog = Math.max(0, Math.trunc(Number(sample.backlog) || 0));
  const frameIntervalMs = Math.max(0, Number(sample.frameIntervalMs) || 0);
  const revealChars = Math.max(0, Math.trunc(Number(sample.revealChars) || 0));
  const compact = {
    at,
    backlog,
    frameIntervalMs,
    reserveChars: Math.max(0, Math.trunc(Number(sample.reserveChars) || 0)),
    resumeActive: sample.resumeActive === true,
    revealChars,
    waitingForLag: sample.waitingForLag === true,
  };

  setProfilerStart(state, at);
  state.frameCount += 1;
  state.currentBacklog = backlog;
  state.maxBacklog = Math.max(state.maxBacklog, backlog);
  state.maxFrameIntervalMs = Math.max(state.maxFrameIntervalMs, frameIntervalMs);
  state.maxRevealChars = Math.max(state.maxRevealChars, revealChars);
  if (frameIntervalMs > STREAM_PROFILER_SLOW_FRAME_MS) {
    state.slowFrameCount += 1;
  }
  if (revealChars > 0) {
    state.revealFrameCount += 1;
  } else {
    state.skippedFrameCount += 1;
  }
  state.lastFrame = compact;
  rememberProfilerSample(state.frames, compact, state.historyLimit);
}

export function recordStreamProfilerRender(state, sample = {}) {
  const at = Number.isFinite(sample.at) ? sample.at : 0;
  const durationMs = Math.max(0, Number(sample.durationMs) || 0);
  const blockCount = Math.max(0, Math.trunc(Number(sample.blockCount) || 0));
  const queueLength = Math.max(0, Math.trunc(Number(sample.queueLength) || 0));
  const backlog = Math.max(0, Math.trunc(Number(sample.backlog) || 0));
  const compact = {
    at,
    backlog,
    blockCount,
    durationMs,
    queueLength,
  };

  setProfilerStart(state, at);
  state.renderCount += 1;
  state.totalRenderMs += durationMs;
  state.currentBacklog = backlog;
  state.currentBlockCount = blockCount;
  state.currentQueueLength = queueLength;
  state.maxBacklog = Math.max(state.maxBacklog, backlog);
  state.maxBlockCount = Math.max(state.maxBlockCount, blockCount);
  state.maxQueueLength = Math.max(state.maxQueueLength, queueLength);
  state.maxRenderMs = Math.max(state.maxRenderMs, durationMs);
  if (durationMs > STREAM_PROFILER_SLOW_RENDER_MS) {
    state.slowRenderCount += 1;
  }
  state.lastRender = compact;
  rememberProfilerSample(state.renders, compact, state.historyLimit);
}

export function streamProfilerSnapshot(state, smoothSnapshot, at = 0) {
  const elapsedMs = state.firstSampleAt && Number.isFinite(at) ? Math.max(0, at - state.firstSampleAt) : 0;
  const avgRenderMs = state.renderCount > 0 ? state.totalRenderMs / state.renderCount : 0;
  const renderRatePerSecond = elapsedMs > 0 ? (state.renderCount * 1000) / elapsedMs : 0;
  const frameRatePerSecond = elapsedMs > 0 ? (state.frameCount * 1000) / elapsedMs : 0;
  const smoothMetrics = smoothSnapshot?.metrics || {};
  return {
    at,
    blockCount: state.currentBlockCount,
    durationMs: state.lastRender?.durationMs || 0,
    frames: state.frames.slice(),
    lastFrame: state.lastFrame ? { ...state.lastFrame } : null,
    lastRender: state.lastRender ? { ...state.lastRender } : null,
    name: "stream-profiler",
    renders: state.renders.slice(),
    smooth: smoothSnapshot,
    summary: {
      avgRenderMs,
      currentBacklog: Math.max(state.currentBacklog, smoothSnapshot?.backlog || 0),
      elapsedMs,
      frameCount: state.frameCount,
      frameRatePerSecond,
      maxBacklog: Math.max(state.maxBacklog, smoothMetrics.maxBacklog || 0),
      maxBlockCount: state.maxBlockCount,
      maxFrameIntervalMs: state.maxFrameIntervalMs,
      maxQueueLength: state.maxQueueLength,
      maxReleaseCps: smoothMetrics.maxReleaseCps || 0,
      maxReleaseCpsDelta: smoothMetrics.maxReleaseCpsDelta || 0,
      maxRenderMs: state.maxRenderMs,
      maxReserveChars: smoothMetrics.maxReserveChars || 0,
      maxResumeRevealChars: smoothMetrics.maxResumeRevealChars || 0,
      maxRevealChars: Math.max(state.maxRevealChars, smoothMetrics.maxRevealChars || 0),
      releaseCps: smoothMetrics.lastReleaseCps || 0,
      queueLength: state.currentQueueLength,
      reserveChars: smoothMetrics.lastReserveChars || 0,
      resumeActive: smoothMetrics.lastResumeActive === true,
      resumeCount: smoothMetrics.resumeCount || 0,
      renderCount: state.renderCount,
      renderRatePerSecond,
      revealFrameCount: state.revealFrameCount,
      skippedFrameCount: state.skippedFrameCount,
      slowFrameCount: state.slowFrameCount,
      slowRenderCount: state.slowRenderCount,
    },
  };
}
