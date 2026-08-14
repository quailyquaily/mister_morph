import { deskPanes, normalizeDeskLayout } from "./agent-desk-layout.js";

function cleanText(value) {
  return String(value || "").trim();
}

function emojiPalette(values) {
  return [...new Set((Array.isArray(values) ? values : []).map(cleanText).filter(Boolean))];
}

function nextEmoji(tabs, emojiOptions) {
  const emojis = emojiPalette(emojiOptions);
  const used = new Set((Array.isArray(tabs) ? tabs : []).map((tab) => cleanText(tab?.emoji)).filter(Boolean));
  return emojis.find((emoji) => !used.has(emoji)) || emojis[used.size % emojis.length] || "💬";
}

function createEmptyDeskTab(tabID, existingTabs, emojiOptions) {
  const id = cleanText(tabID);
  if (!id) {
    return null;
  }
  return {
    id,
    emoji: nextEmoji(existingTabs, emojiOptions),
    layout: null,
    activePaneID: "",
  };
}

function normalizeDeskTabs(stored, validEndpointRefs, fallbackEndpointRef = "", emojiOptions = []) {
  const legacyPanes = deskPanes(stored?.layout);
  const legacyActivePaneID = cleanText(stored?.activePaneID);
  const legacyEmoji = cleanText(
    legacyPanes.find((pane) => pane.id === legacyActivePaneID)?.emoji || legacyPanes[0]?.emoji
  );
  const rawTabs = Array.isArray(stored?.tabs)
    ? stored.tabs
    : stored && Object.hasOwn(stored, "layout")
      ? [{
          id: "tab-legacy",
          emoji: legacyEmoji,
          layout: stored.layout,
          activePaneID: stored.activePaneID,
        }]
      : [];
  const usedIDs = new Set();
  const tabs = [];

  for (const rawTab of rawTabs) {
    const id = cleanText(rawTab?.id);
    if (!id || usedIDs.has(id)) {
      continue;
    }
    usedIDs.add(id);
    const layout = normalizeDeskLayout(
      rawTab?.layout,
      validEndpointRefs,
      fallbackEndpointRef
    );
    const paneIDs = deskPanes(layout).map((pane) => pane.id);
    const savedActivePaneID = cleanText(rawTab?.activePaneID);
    tabs.push({
      id,
      emoji: cleanText(rawTab?.emoji),
      layout,
      activePaneID: paneIDs.includes(savedActivePaneID) ? savedActivePaneID : paneIDs[0] || "",
    });
  }

  for (const tab of tabs) {
    if (!tab.emoji) {
      tab.emoji = nextEmoji(tabs, emojiOptions);
    }
  }

  const requestedActiveTabID = cleanText(stored?.activeTabID);
  return {
    tabs,
    activeTabID: tabs.some((tab) => tab.id === requestedActiveTabID)
      ? requestedActiveTabID
      : tabs[0]?.id || "",
  };
}

export { createEmptyDeskTab, normalizeDeskTabs };
