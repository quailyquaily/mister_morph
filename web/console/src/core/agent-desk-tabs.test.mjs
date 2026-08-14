import assert from "node:assert/strict";
import test from "node:test";

import { createEmptyDeskTab, normalizeDeskTabs } from "./agent-desk-tabs.js";

const ENDPOINTS = ["ep_local", "ep_remote"];
const EMOJIS = ["🌱", "🧭", "🪶"];

function pane(id, endpointRef = "ep_local") {
  return { type: "pane", id, endpointRef, topicID: "" };
}

test("createEmptyDeskTab creates a tab with no panes", () => {
  const tab = createEmptyDeskTab("tab-2", [{ id: "tab-1", emoji: "🌱" }], EMOJIS);

  assert.deepEqual(tab, {
    id: "tab-2",
    emoji: "🧭",
    layout: null,
    activePaneID: "",
  });
});

test("normalizeDeskTabs migrates the previous single-layout state", () => {
  const result = normalizeDeskTabs(
    {
      layout: { ...pane("pane-legacy"), emoji: "🪐" },
      activePaneID: "pane-legacy",
    },
    ENDPOINTS,
    "ep_local",
    EMOJIS
  );

  assert.equal(result.tabs.length, 1);
  assert.equal(result.activeTabID, "tab-legacy");
  assert.deepEqual(result.tabs[0], {
    id: "tab-legacy",
    emoji: "🪐",
    layout: { ...pane("pane-legacy"), emoji: "🪐" },
    activePaneID: "pane-legacy",
  });
});

test("normalizeDeskTabs preserves independent empty and populated tabs", () => {
  const result = normalizeDeskTabs(
    {
      tabs: [
        {
          id: "tab-a",
          emoji: "🌱",
          layout: {
            type: "split",
            id: "split-a",
            direction: "row",
            ratio: 0.5,
            first: pane("pane-a"),
            second: pane("pane-b", "ep_remote"),
          },
          activePaneID: "pane-b",
        },
        {
          id: "tab-b",
          emoji: "",
          layout: null,
          activePaneID: "missing-pane",
        },
      ],
      activeTabID: "tab-b",
    },
    ENDPOINTS,
    "ep_local",
    EMOJIS
  );

  assert.equal(result.activeTabID, "tab-b");
  assert.equal(result.tabs[0].activePaneID, "pane-b");
  assert.equal(result.tabs[1].emoji, "🧭");
  assert.equal(result.tabs[1].layout, null);
  assert.equal(result.tabs[1].activePaneID, "");
});

test("normalizeDeskTabs rejects duplicate tab ids and repairs an invalid active pane", () => {
  const result = normalizeDeskTabs(
    {
      tabs: [
        { id: "tab-a", emoji: "🌱", layout: pane("pane-a"), activePaneID: "missing" },
        { id: "tab-a", emoji: "🧭", layout: pane("pane-b"), activePaneID: "pane-b" },
      ],
      activeTabID: "missing-tab",
    },
    ENDPOINTS,
    "ep_local",
    EMOJIS
  );

  assert.equal(result.tabs.length, 1);
  assert.equal(result.tabs[0].activePaneID, "pane-a");
  assert.equal(result.activeTabID, "tab-a");
});
