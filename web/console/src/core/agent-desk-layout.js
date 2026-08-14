const MIN_SPLIT_RATIO = 0.2;
const MAX_SPLIT_RATIO = 0.8;

function cleanText(value) {
  return String(value || "").trim();
}

function replaceDeskNode(node, targetID, replacement) {
  if (!node || typeof node !== "object") {
    return node;
  }
  if (node.id === targetID) {
    return replacement(node);
  }
  if (node.type !== "split") {
    return node;
  }
  const first = replaceDeskNode(node.first, targetID, replacement);
  const second = replaceDeskNode(node.second, targetID, replacement);
  return first === node.first && second === node.second ? node : { ...node, first, second };
}

function clampSplitRatio(value) {
  const ratio = Number(value);
  if (!Number.isFinite(ratio)) {
    return 0.5;
  }
  return Math.round(Math.min(MAX_SPLIT_RATIO, Math.max(MIN_SPLIT_RATIO, ratio)) * 10000) / 10000;
}

function deskPanes(layout) {
  if (!layout || typeof layout !== "object") {
    return [];
  }
  if (layout.type === "pane") {
    return [layout];
  }
  if (layout.type !== "split") {
    return [];
  }
  return [...deskPanes(layout.first), ...deskPanes(layout.second)];
}

function deskPaneRects(layout) {
  const panes = [];

  function visit(node, bounds) {
    if (!node || typeof node !== "object") {
      return;
    }
    if (node.type === "pane") {
      panes.push({ id: cleanText(node.id), ...bounds });
      return;
    }
    if (node.type !== "split") {
      return;
    }
    const ratio = clampSplitRatio(node.ratio);
    if (node.direction === "column") {
      const firstHeight = bounds.height * ratio;
      visit(node.first, { ...bounds, height: firstHeight });
      visit(node.second, {
        ...bounds,
        y: bounds.y + firstHeight,
        height: bounds.height - firstHeight,
      });
      return;
    }
    const firstWidth = bounds.width * ratio;
    visit(node.first, { ...bounds, width: firstWidth });
    visit(node.second, {
      ...bounds,
      x: bounds.x + firstWidth,
      width: bounds.width - firstWidth,
    });
  }

  visit(layout, { x: 0, y: 0, width: 1, height: 1 });
  return panes;
}

function adjacentDeskPaneID(layout, paneID, direction) {
  const targetID = cleanText(paneID);
  const axis = cleanText(direction).toLowerCase();
  if (!targetID || !["left", "right", "up", "down"].includes(axis)) {
    return "";
  }
  const rects = deskPaneRects(layout);
  const current = rects.find((pane) => pane.id === targetID);
  if (!current) {
    return "";
  }
  const currentX = current.x + current.width / 2;
  const currentY = current.y + current.height / 2;
  let bestID = "";
  let bestScore = Number.POSITIVE_INFINITY;

  for (const candidate of rects) {
    if (candidate.id === targetID) {
      continue;
    }
    const candidateX = candidate.x + candidate.width / 2;
    const candidateY = candidate.y + candidate.height / 2;
    const horizontal = candidateX - currentX;
    const vertical = candidateY - currentY;
    const primary = axis === "left" ? -horizontal : axis === "right" ? horizontal : axis === "up" ? -vertical : vertical;
    if (primary <= 0) {
      continue;
    }
    const currentStart = axis === "left" || axis === "right" ? current.y : current.x;
    const currentEnd = currentStart + (axis === "left" || axis === "right" ? current.height : current.width);
    const candidateStart = axis === "left" || axis === "right" ? candidate.y : candidate.x;
    const candidateEnd = candidateStart + (axis === "left" || axis === "right" ? candidate.height : candidate.width);
    const orthogonalOverlap = Math.min(currentEnd, candidateEnd) - Math.max(currentStart, candidateStart);
    if (orthogonalOverlap <= 0) {
      continue;
    }
    const orthogonalCenterDistance = axis === "left" || axis === "right" ? Math.abs(vertical) : Math.abs(horizontal);
    const score = primary + orthogonalCenterDistance * 0.1;
    if (score < bestScore) {
      bestScore = score;
      bestID = candidate.id;
    }
  }
  return bestID;
}

function splitDeskPane(layout, paneID, split) {
  const targetID = cleanText(paneID);
  const splitID = cleanText(split?.splitID);
  const direction = split?.direction === "column" ? "column" : "row";
  const pane = split?.pane;
  if (!targetID || !splitID || pane?.type !== "pane" || !cleanText(pane.id)) {
    return layout;
  }
  return replaceDeskNode(layout, targetID, (current) => {
    if (current?.type !== "pane") {
      return current;
    }
    return {
      type: "split",
      id: splitID,
      direction,
      ratio: 0.5,
      first: current,
      second: pane,
    };
  });
}

function closeDeskPane(layout, paneID) {
  const targetID = cleanText(paneID);
  if (!targetID || !layout || typeof layout !== "object") {
    return layout;
  }
  if (layout.type === "pane") {
    return layout.id === targetID ? null : layout;
  }
  if (layout.type !== "split") {
    return layout;
  }
  const first = closeDeskPane(layout.first, targetID);
  if (first !== layout.first) {
    return first ? { ...layout, first } : layout.second;
  }
  const second = closeDeskPane(layout.second, targetID);
  if (second !== layout.second) {
    return second ? { ...layout, second } : layout.first;
  }
  return layout;
}

function updateDeskPaneEndpoint(layout, paneID, endpointRef) {
  const targetID = cleanText(paneID);
  const nextEndpointRef = cleanText(endpointRef);
  if (!targetID || !nextEndpointRef) {
    return layout;
  }
  return replaceDeskNode(layout, targetID, (current) =>
    current?.type === "pane" && current.endpointRef !== nextEndpointRef
      ? { ...current, endpointRef: nextEndpointRef, topicID: "" }
      : current
  );
}

function updateDeskPaneTopic(layout, paneID, topicID) {
  const targetID = cleanText(paneID);
  if (!targetID) {
    return layout;
  }
  const nextTopicID = cleanText(topicID);
  return replaceDeskNode(layout, targetID, (current) =>
    current?.type === "pane" && cleanText(current.topicID) !== nextTopicID
      ? { ...current, topicID: nextTopicID }
      : current
  );
}

function resizeDeskSplit(layout, splitID, ratio) {
  const targetID = cleanText(splitID);
  if (!targetID) {
    return layout;
  }
  const nextRatio = clampSplitRatio(ratio);
  return replaceDeskNode(layout, targetID, (current) =>
    current?.type === "split" && current.ratio !== nextRatio
      ? { ...current, ratio: nextRatio }
      : current
  );
}

function normalizeDeskLayout(layout, validEndpointRefs, fallbackEndpointRef = "") {
  const validRefs = [...new Set((Array.isArray(validEndpointRefs) ? validEndpointRefs : []).map(cleanText).filter(Boolean))];
  const validRefSet = new Set(validRefs);
  const requestedFallback = cleanText(fallbackEndpointRef);
  const fallbackRef = validRefSet.has(requestedFallback) ? requestedFallback : validRefs[0] || "";
  const usedIDs = new Set();

  function normalizeNode(node) {
    if (!node || typeof node !== "object") {
      return null;
    }
    const id = cleanText(node.id);
    if (!id || usedIDs.has(id)) {
      return null;
    }
    usedIDs.add(id);

    if (node.type === "pane") {
      const savedEndpointRef = cleanText(node.endpointRef);
      const endpointRef = validRefSet.has(savedEndpointRef) ? savedEndpointRef : fallbackRef;
      if (!endpointRef) {
        return null;
      }
      const normalized = { type: "pane", id, endpointRef };
      if (Object.hasOwn(node, "topicID")) {
        normalized.topicID = endpointRef === savedEndpointRef ? cleanText(node.topicID) : "";
      }
      const emoji = cleanText(node.emoji);
      if (emoji) {
        normalized.emoji = emoji;
      }
      return normalized;
    }
    if (node.type !== "split" || (node.direction !== "row" && node.direction !== "column")) {
      return null;
    }
    const first = normalizeNode(node.first);
    const second = normalizeNode(node.second);
    if (!first || !second) {
      return first || second;
    }
    return {
      type: "split",
      id,
      direction: node.direction,
      ratio: clampSplitRatio(node.ratio),
      first,
      second,
    };
  }

  return normalizeNode(layout);
}

export {
  adjacentDeskPaneID,
  closeDeskPane,
  deskPanes,
  normalizeDeskLayout,
  resizeDeskSplit,
  splitDeskPane,
  updateDeskPaneEndpoint,
  updateDeskPaneTopic,
};
