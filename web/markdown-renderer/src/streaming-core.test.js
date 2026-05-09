import assert from "node:assert/strict";
import test from "node:test";

import {
  advanceSmoothFrame,
  createBlockQueueState,
  createStreamProfilerState,
  createSmoothStreamState,
  forceSmoothStreamDrain,
  makeStreamingBlock,
  recordStreamProfilerFrame,
  recordStreamProfilerRender,
  repairStreamingMarkdown,
  resolveBlockQueue,
  revealAnimatingBlock,
  shouldAnimateStreamingBlock,
  smoothStateSnapshot,
  streamProfilerSnapshot,
  updateSmoothTarget,
} from "./streaming-core.js";

function drainFrames(state, start, frameMs = 16, maxFrames = 240) {
  let now = start;
  for (let i = 0; i < maxFrames; i += 1) {
    now += frameMs;
    const result = advanceSmoothFrame(state, now);
    if (result.done) {
      return { frames: i + 1, now };
    }
  }
  return { frames: maxFrames, now };
}

test("smooth stream keeps active input buffered and drains after settling", () => {
  const state = createSmoothStreamState("", { mode: "balanced", now: 0 });
  let now = 0;

  for (const chunk of ["Hello ", "world, ", "this ", "is ", "smooth."]) {
    now += 60;
    updateSmoothTarget(state, state.targetContent + chunk, now);
    advanceSmoothFrame(state, now + 16);
    advanceSmoothFrame(state, now + 32);
  }

  const activeSnapshot = smoothStateSnapshot(state);
  assert.equal(activeSnapshot.targetContent, "Hello world, this is smooth.");
  assert.ok(activeSnapshot.backlog > 0, "active stream should keep a small display backlog");
  assert.ok(
    activeSnapshot.backlog <= 18,
    `active backlog should stay bounded, got ${activeSnapshot.backlog}`
  );
  assert.ok(
    activeSnapshot.metrics.maxRevealChars <= 4,
    `active reveal should avoid burst jumps, got ${activeSnapshot.metrics.maxRevealChars}`
  );

  const settled = drainFrames(state, now + 500);
  const finalSnapshot = smoothStateSnapshot(state);
  assert.equal(finalSnapshot.displayedContent, finalSnapshot.targetContent);
  assert.ok(settled.now - (now + 500) <= 1000, "settling should drain within one second");
  assert.ok(finalSnapshot.metrics.revealFrameCount > 0);
  assert.ok(finalSnapshot.metrics.skippedFrameCount > 0);
});

test("smooth stream reveals large appends by frames instead of synchronizing immediately", () => {
  const state = createSmoothStreamState("seed", { mode: "balanced", now: 0 });
  const large = "x".repeat(180);
  const result = updateSmoothTarget(state, `seed${large}`, 20);

  assert.equal(result.immediate, false);
  assert.equal(result.appendOnly, true);
  assert.notEqual(state.displayedContent, state.targetContent);
  assert.equal(state.metrics.largeAppendCount, 1);

  advanceSmoothFrame(state, 36);
  advanceSmoothFrame(state, 52);
  assert.ok(
    state.metrics.maxRevealChars <= 6,
    `large append should avoid full-line jumps, got ${state.metrics.maxRevealChars}`
  );
  assert.ok(
    state.metrics.maxReleaseCpsDelta <= 13,
    `release speed should change smoothly, got ${state.metrics.maxReleaseCpsDelta}`
  );

  forceSmoothStreamDrain(state, 500);
  drainFrames(state, 500);
  assert.equal(smoothStateSnapshot(state).backlog, 0);
});

test("smooth stream resets immediately on non-append replacement", () => {
  const state = createSmoothStreamState("hello", { mode: "balanced", now: 0 });
  updateSmoothTarget(state, "hello world", 30);
  const result = updateSmoothTarget(state, "bye", 60);

  assert.equal(result.appendOnly, false);
  assert.equal(result.reset, true);
  assert.equal(state.displayedContent, "bye");
  assert.equal(state.targetContent, "bye");
  assert.equal(state.metrics.resetCount, 1);
});

test("smooth stream cools release speed while buffer is empty", () => {
  const state = createSmoothStreamState("seed", { mode: "balanced", now: 0 });
  state.releaseCps = 180;
  state.releaseCarry = 0.9;
  state.lastFrameTs = 100;
  state.lastInputTs = 20;

  updateSmoothTarget(state, "seed next", 900);

  assert.ok(state.releaseCps <= 38, `idle release speed should cool down, got ${state.releaseCps}`);
  assert.equal(state.releaseCarry, 0);
});

test("smooth stream warms up after an empty-buffer burst", () => {
  const state = createSmoothStreamState("seed", { mode: "balanced", now: 0 });
  state.releaseCps = 180;
  state.lastFrameTs = 120;
  state.lastInputTs = 20;

  updateSmoothTarget(state, `seed${"x".repeat(80)}`, 520);

  assert.equal(state.metrics.resumeCount, 1);
  assert.ok(state.releaseCps <= 38, `resume should start from a cooled speed, got ${state.releaseCps}`);

  let now = 520;
  let maxResumeReveal = 0;
  for (let i = 0; i < 12; i += 1) {
    now += 16;
    const frame = advanceSmoothFrame(state, now);
    if (frame.resumeActive) {
      maxResumeReveal = Math.max(maxResumeReveal, frame.revealChars);
    }
  }

  assert.ok(maxResumeReveal <= 1, `early resume should reveal at most one char per frame, got ${maxResumeReveal}`);
  assert.ok(smoothStateSnapshot(state).backlog > 0, "resume should keep enough backlog to cover upstream gaps");
});

test("smooth stream counts unicode characters without splitting emoji", () => {
  const state = createSmoothStreamState("", { mode: "realtime", now: 0 });
  updateSmoothTarget(state, "A🧪B", 20);
  drainFrames(state, 400);

  assert.equal(state.targetCount, 3);
  assert.equal(state.displayedContent, "A🧪B");
});

test("repairStreamingMarkdown closes unfinished fenced code blocks", () => {
  assert.equal(repairStreamingMarkdown("```js\nconsole.log(1)"), "```js\nconsole.log(1)\n```");
  assert.equal(repairStreamingMarkdown("```js\nconsole.log(1)\n```"), "```js\nconsole.log(1)\n```");
  assert.equal(repairStreamingMarkdown("plain text"), "plain text");
});

test("block queue reveals prior tail when a new block appears", () => {
  const queue = createBlockQueueState();
  let blocks = [makeStreamingBlock("alpha", 0, 0)];
  let resolved = resolveBlockQueue(queue, blocks);
  assert.deepEqual(resolved.states, ["streaming"]);

  blocks = [
    makeStreamingBlock("alpha\n\n", 0, 0),
    makeStreamingBlock("beta\n\n", 7, 1),
    makeStreamingBlock("gamma", 13, 2),
  ];
  resolved = resolveBlockQueue(queue, blocks);
  assert.deepEqual(resolved.states, ["revealed", "animating", "queued"]);
  assert.ok(resolved.charDelay > 0);

  revealAnimatingBlock(queue, resolved.animatingIndex);
  resolved = resolveBlockQueue(queue, blocks);
  assert.deepEqual(resolved.states, ["revealed", "revealed", "streaming"]);
});

test("streaming animation only applies to normal text blocks below threshold", () => {
  assert.equal(shouldAnimateStreamingBlock(makeStreamingBlock("A paragraph", 0, 0)), true);
  assert.equal(shouldAnimateStreamingBlock(makeStreamingBlock("```js\nx\n```", 0, 0)), false);
  assert.equal(shouldAnimateStreamingBlock(makeStreamingBlock("| a |\n|---|\n| b |", 0, 0)), false);
  assert.equal(shouldAnimateStreamingBlock(makeStreamingBlock("x".repeat(1300), 0, 0)), false);
  assert.equal(shouldAnimateStreamingBlock(makeStreamingBlock("A paragraph", 0, 0), true), false);
});

test("stream profiler records render and frame smoothness metrics", () => {
  const profiler = createStreamProfilerState({ historyLimit: 2 });
  const smooth = createSmoothStreamState("", { mode: "balanced", now: 0 });
  updateSmoothTarget(smooth, "hello world", 20);
  advanceSmoothFrame(smooth, 36);

  recordStreamProfilerFrame(profiler, {
    at: 36,
    backlog: 8,
    frameIntervalMs: 16,
    revealChars: 2,
  });
  recordStreamProfilerFrame(profiler, {
    at: 88,
    backlog: 6,
    frameIntervalMs: 52,
    revealChars: 0,
    waitingForLag: true,
  });
  recordStreamProfilerRender(profiler, {
    at: 90,
    backlog: 6,
    blockCount: 4,
    durationMs: 2,
    queueLength: 1,
  });
  recordStreamProfilerRender(profiler, {
    at: 110,
    backlog: 4,
    blockCount: 5,
    durationMs: 22,
    queueLength: 0,
  });
  recordStreamProfilerRender(profiler, {
    at: 126,
    backlog: 3,
    blockCount: 5,
    durationMs: 4,
    queueLength: 0,
  });

  const snapshot = streamProfilerSnapshot(profiler, smoothStateSnapshot(smooth), 126);
  assert.equal(snapshot.summary.frameCount, 2);
  assert.equal(snapshot.summary.renderCount, 3);
  assert.equal(snapshot.summary.slowFrameCount, 1);
  assert.equal(snapshot.summary.slowRenderCount, 1);
  assert.equal(snapshot.summary.maxRenderMs, 22);
  assert.equal(snapshot.summary.maxBlockCount, 5);
  assert.equal(snapshot.summary.maxQueueLength, 1);
  assert.equal(snapshot.renders.length, 2);
  assert.equal(snapshot.frames.length, 2);
});
