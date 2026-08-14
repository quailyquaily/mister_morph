import assert from "node:assert/strict";
import test from "node:test";

import { resolveDeskShortcut } from "./agent-desk-shortcuts.js";

test("Ctrl+B enters the desk keyboard prefix without capturing unrelated keys", () => {
  assert.deepEqual(resolveDeskShortcut({ key: "b", ctrlKey: true }, false), {
    handled: true,
    prefixActive: true,
    action: "prefix",
  });
  assert.deepEqual(resolveDeskShortcut({ key: "ArrowLeft" }, false), {
    handled: false,
    prefixActive: false,
    action: "",
  });
});

test("the desk prefix covers pane navigation and lifecycle actions", () => {
  const cases = [
    ["ArrowLeft", "focus-left"],
    ["h", "focus-left"],
    ["j", "focus-down"],
    ["k", "focus-up"],
    ["l", "focus-right"],
    ["n", "focus-next"],
    ["p", "focus-previous"],
    ["1", "focus-index", 0],
    ["9", "focus-index", 8],
    ["%", "split-row"],
    ['"', "split-column"],
    ["x", "close-pane"],
    ["Enter", "focus-composer"],
    ["e", "exit-desk"],
    ["?", "show-help"],
    ["Escape", "cancel"],
  ];

  for (const [key, action, index] of cases) {
    assert.deepEqual(resolveDeskShortcut({ key }, true), {
      handled: true,
      prefixActive: false,
      action,
      ...(index === undefined ? {} : { index }),
    });
  }
});

test("an unknown prefixed key cancels the prefix", () => {
  assert.deepEqual(resolveDeskShortcut({ key: "z" }, true), {
    handled: true,
    prefixActive: false,
    action: "cancel",
  });
});

test("modifier keydown keeps the prefix active for shifted commands", () => {
  assert.deepEqual(resolveDeskShortcut({ key: "Shift" }, true), {
    handled: false,
    prefixActive: true,
    action: "",
  });
});
