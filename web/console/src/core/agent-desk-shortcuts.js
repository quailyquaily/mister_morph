function resolveDeskShortcut(event, prefixActive) {
  const key = String(event?.key || "");
  const lowerKey = key.toLowerCase();
  if (event?.isComposing) {
    return { handled: false, prefixActive: Boolean(prefixActive), action: "" };
  }
  if (!prefixActive) {
    if (event?.ctrlKey === true && event?.altKey !== true && event?.metaKey !== true && lowerKey === "b") {
      return { handled: true, prefixActive: true, action: "prefix" };
    }
    return { handled: false, prefixActive: false, action: "" };
  }
  if (["Shift", "Control", "Alt", "Meta"].includes(key)) {
    return { handled: false, prefixActive: true, action: "" };
  }

  const actions = {
    ArrowLeft: "focus-left",
    ArrowRight: "focus-right",
    ArrowUp: "focus-up",
    ArrowDown: "focus-down",
    h: "focus-left",
    l: "focus-right",
    k: "focus-up",
    j: "focus-down",
    n: "focus-next",
    o: "focus-next",
    p: "focus-previous",
    "%": "split-row",
    v: "split-row",
    '"': "split-column",
    s: "split-column",
    x: "close-pane",
    Enter: "focus-composer",
    e: "exit-desk",
    "?": "show-help",
    Escape: "cancel",
    q: "cancel",
  };
  if (/^[1-9]$/.test(key)) {
    return {
      handled: true,
      prefixActive: false,
      action: "focus-index",
      index: Number(key) - 1,
    };
  }
  return {
    handled: true,
    prefixActive: false,
    action: actions[key] || actions[lowerKey] || "cancel",
  };
}

export { resolveDeskShortcut };
