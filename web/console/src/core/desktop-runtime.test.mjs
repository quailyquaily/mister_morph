import assert from "node:assert/strict";
import test from "node:test";

const DESKTOP_WINDOW_MESSAGE_EVENT = "mistermorph:desktop-window-message";

function createLocalStorage() {
  const values = new Map();
  return {
    get length() {
      return values.size;
    },
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    key(index) {
      return Array.from(values.keys())[index] || null;
    },
    removeItem(key) {
      values.delete(key);
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
  };
}

function installDesktopWindow() {
  const listeners = new Map();
  const win = {
    __MISTERMORPH_DESKTOP_RUNTIME__: true,
    location: {
      pathname: "/window/test",
      search: "",
    },
    localStorage: createLocalStorage(),
    setTimeout,
    clearTimeout,
    addEventListener(type, callback) {
      const callbacks = listeners.get(type) || new Set();
      callbacks.add(callback);
      listeners.set(type, callbacks);
    },
    removeEventListener(type, callback) {
      listeners.get(type)?.delete(callback);
    },
    dispatchDesktopEvent(type, event) {
      Array.from(listeners.get(type) || []).forEach((callback) => callback(event));
    },
  };
  globalThis.window = win;
  return win;
}

async function importDesktopRuntime() {
  const url = new URL("./desktop-runtime.js", import.meta.url);
  url.search = `test=${Date.now()}-${Math.random()}`;
  return await import(url.href);
}

test("desktop window messages fan out to all local subscribers", async () => {
  const win = installDesktopWindow();
  const { onDesktopWindowMessage } = await importDesktopRuntime();
  const received = [];
  const offA = onDesktopWindowMessage((message) => received.push(["a", message.type]));
  const offB = onDesktopWindowMessage((message) => received.push(["b", message.type]));

  win.dispatchDesktopEvent(DESKTOP_WINDOW_MESSAGE_EVENT, {
    detail: {
      type: "dialog:update",
      window_id: "codex-auth",
      _delivery_id: "fanout-1",
    },
  });

  assert.deepEqual(received, [
    ["a", "dialog:update"],
    ["b", "dialog:update"],
  ]);
  offA();
  offB();
});

test("desktop window messages dedupe repeated delivery ids before fan-out", async () => {
  const win = installDesktopWindow();
  const { onDesktopWindowMessage } = await importDesktopRuntime();
  const received = [];
  const off = onDesktopWindowMessage((message) => received.push(message.type));
  const detail = {
    type: "dialog:update",
    window_id: "codex-auth",
    _delivery_id: "dedupe-1",
  };

  win.dispatchDesktopEvent(DESKTOP_WINDOW_MESSAGE_EVENT, { detail });
  win.dispatchDesktopEvent(DESKTOP_WINDOW_MESSAGE_EVENT, { detail });

  assert.deepEqual(received, ["dialog:update"]);
  off();
});

test("desktop window message subscribers can be removed", async () => {
  const win = installDesktopWindow();
  const { onDesktopWindowMessage } = await importDesktopRuntime();
  const received = [];
  const off = onDesktopWindowMessage((message) => received.push(message.type));
  off();

  win.dispatchDesktopEvent(DESKTOP_WINDOW_MESSAGE_EVENT, {
    detail: {
      type: "dialog:update",
      window_id: "codex-auth",
      _delivery_id: "removed-1",
    },
  });

  assert.deepEqual(received, []);
});

test("desktop update check uses configured binding name", async () => {
  const win = installDesktopWindow();
  const calls = [];
  win.__MISTERMORPH_DESKTOP_BINDINGS__ = {
    CheckUpdate: "custom.App.CheckUpdate",
  };
  win.wails = {
    Call: {
      ByName(name, ...args) {
        calls.push([name, ...args]);
        if (name === "custom.App.CheckUpdate") {
          return { status: "up_to_date" };
        }
        return true;
      },
    },
  };

  const { canCheckDesktopUpdate, checkDesktopUpdate } = await importDesktopRuntime();
  assert.equal(canCheckDesktopUpdate(), true);
  assert.deepEqual(await checkDesktopUpdate(), { status: "up_to_date" });
  assert.deepEqual(calls, [["custom.App.CheckUpdate"]]);
});

test("desktop runtime exposes injected version", async () => {
  const win = installDesktopWindow();
  win.__MISTERMORPH_DESKTOP_VERSION__ = "0.2.42";

  const { desktopRuntimeVersion } = await importDesktopRuntime();

  assert.equal(desktopRuntimeVersion(), "0.2.42");
});
