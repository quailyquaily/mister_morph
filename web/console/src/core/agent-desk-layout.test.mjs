import assert from "node:assert/strict";
import test from "node:test";

import {
  adjacentDeskPaneID,
  closeDeskPane,
  deskPanes,
  normalizeDeskLayout,
  resizeDeskSplit,
  splitDeskPane,
  updateDeskPaneEndpoint,
  updateDeskPaneTopic,
} from "./agent-desk-layout.js";

const paneA = { type: "pane", id: "pane-a", endpointRef: "ep-a" };
const paneB = { type: "pane", id: "pane-b", endpointRef: "ep-b" };
const paneC = { type: "pane", id: "pane-c", endpointRef: "ep-c" };

test("splitDeskPane replaces only the target pane", () => {
  const initial = {
    type: "split",
    id: "split-root",
    direction: "row",
    ratio: 0.6,
    first: paneA,
    second: paneB,
  };

  const result = splitDeskPane(initial, "pane-b", {
    splitID: "split-nested",
    direction: "column",
    pane: paneC,
  });

  assert.deepEqual(result, {
    ...initial,
    second: {
      type: "split",
      id: "split-nested",
      direction: "column",
      ratio: 0.5,
      first: paneB,
      second: paneC,
    },
  });
  assert.deepEqual(deskPanes(result), [paneA, paneB, paneC]);
});

test("closeDeskPane collapses the parent split into the remaining sibling", () => {
  const layout = splitDeskPane(paneA, "pane-a", {
    splitID: "split-root",
    direction: "row",
    pane: paneB,
  });

  assert.deepEqual(closeDeskPane(layout, "pane-a"), paneB);
  assert.equal(closeDeskPane(paneA, "pane-a"), null);
});

test("pane endpoint changes and divider resizing preserve the rest of the tree", () => {
  const layout = splitDeskPane({ ...paneA, topicID: "topic-a", emoji: "🌱" }, "pane-a", {
    splitID: "split-root",
    direction: "row",
    pane: { ...paneB, topicID: "topic-b", emoji: "🧭" },
  });

  const changed = updateDeskPaneEndpoint(layout, "pane-b", "ep-c");
  assert.equal(changed.second.endpointRef, "ep-c");
  assert.equal(changed.second.topicID, "");
  assert.equal(changed.second.emoji, "🧭");
  assert.equal(changed.first, layout.first);

  const topicChanged = updateDeskPaneTopic(changed, "pane-a", "topic-next");
  assert.equal(topicChanged.first.topicID, "topic-next");
  assert.equal(topicChanged.second, changed.second);

  assert.equal(resizeDeskSplit(topicChanged, "split-root", 0.73).ratio, 0.73);
  assert.equal(resizeDeskSplit(changed, "split-root", 0.02).ratio, 0.2);
  assert.equal(resizeDeskSplit(changed, "split-root", 0.98).ratio, 0.8);
});

test("normalizeDeskLayout restores valid saved state without retaining unknown endpoints", () => {
  const saved = {
    type: "split",
    id: "split-root",
    direction: "column",
    ratio: 0.68,
    first: { ...paneA, topicID: "topic-a", emoji: "🌱" },
    second: { ...paneB, endpointRef: "ep-removed", topicID: "topic-b", emoji: "🧭" },
  };

  assert.deepEqual(normalizeDeskLayout(saved, ["ep-a", "ep-b"], "ep-b"), {
    ...saved,
    first: saved.first,
    second: { ...saved.second, endpointRef: "ep-b", topicID: "" },
  });
  assert.equal(normalizeDeskLayout({ type: "unknown" }, ["ep-a"], "ep-a"), null);
});

test("adjacentDeskPaneID follows the spatial split layout", () => {
  const layout = {
    type: "split",
    id: "split-root",
    direction: "row",
    ratio: 0.5,
    first: paneA,
    second: {
      type: "split",
      id: "split-right",
      direction: "column",
      ratio: 0.5,
      first: paneB,
      second: paneC,
    },
  };

  assert.equal(adjacentDeskPaneID(layout, "pane-a", "right"), "pane-b");
  assert.equal(adjacentDeskPaneID(layout, "pane-b", "down"), "pane-c");
  assert.equal(adjacentDeskPaneID(layout, "pane-c", "left"), "pane-a");
  assert.equal(adjacentDeskPaneID(layout, "pane-a", "up"), "");
});
