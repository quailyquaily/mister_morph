import {
  isDesktopRuntime,
  logDesktopRuntimeEvent,
  openDesktopWindow,
  summarizeDesktopPayload,
} from "./desktop-runtime";

const DESKTOP_WINDOW_PAYLOAD_PREFIX = "mistermorph_desktop_window_payload:";
const DESKTOP_WINDOW_PAYLOAD_MAX_AGE_MS = 5 * 60 * 1000;
export const RAW_JSON_WINDOW_ID = "raw-json";
export const POKE_WINDOW_ID = "poke";
export const SETUP_PICKER_WINDOW_ID = "setup-picker";
export const SETUP_CONNECTION_TEST_WINDOW_ID = "setup-connection-test";
export const CODEX_AUTH_WINDOW_ID = "codex-auth";
export const RAW_TEXT_EDITOR_WINDOW_ID = "raw-text-editor";
const DEFAULT_DESKTOP_DIALOG_WIDTH = 720;
const DEFAULT_DESKTOP_DIALOG_HEIGHT = 560;
const DEFAULT_DESKTOP_DIALOG_MIN_WIDTH = 480;
const DEFAULT_DESKTOP_DIALOG_MIN_HEIGHT = 360;

function payloadStorage() {
  return typeof window === "undefined" ? null : window.localStorage;
}

export function randomDesktopWindowID() {
  const cryptoID = typeof crypto !== "undefined" ? crypto.randomUUID : null;
  if (typeof cryptoID === "function") {
    return cryptoID.call(crypto);
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function payloadKey(id) {
  return `${DESKTOP_WINDOW_PAYLOAD_PREFIX}${id}`;
}

function pruneDesktopWindowPayloads(store, now = Date.now()) {
  if (!store) {
    return;
  }
  for (let i = store.length - 1; i >= 0; i -= 1) {
    const key = store.key(i);
    if (!key || !key.startsWith(DESKTOP_WINDOW_PAYLOAD_PREFIX)) {
      continue;
    }
    try {
      const item = JSON.parse(store.getItem(key) || "{}");
      const createdAt = Number(item.created_at || 0);
      if (!Number.isFinite(createdAt) || now - createdAt > DESKTOP_WINDOW_PAYLOAD_MAX_AGE_MS) {
        store.removeItem(key);
      }
    } catch {
      store.removeItem(key);
    }
  }
}

function saveDesktopWindowPayload(kind, payload) {
  const store = payloadStorage();
  if (!store) {
    logDesktopRuntimeEvent("payload_save_failed", {
      kind,
      reason: "localStorage unavailable",
    });
    return "";
  }
  pruneDesktopWindowPayloads(store);
  const id = randomDesktopWindowID();
  store.setItem(
    payloadKey(id),
    JSON.stringify({
      kind: String(kind || "").trim(),
      created_at: Date.now(),
      payload,
    })
  );
  logDesktopRuntimeEvent("payload_saved", {
    kind,
    payload_id: id,
    payload: summarizeDesktopPayload(payload),
  });
  return id;
}

export function takeDesktopWindowPayload(id, expectedKind = "") {
  const store = payloadStorage();
  const payloadID = String(id || "").trim();
  if (!store || !payloadID) {
    logDesktopRuntimeEvent("payload_take_failed", {
      expected_kind: expectedKind,
      payload_id: payloadID,
      reason: !store ? "localStorage unavailable" : "empty payload id",
    });
    return null;
  }
  const key = payloadKey(payloadID);
  const raw = store.getItem(key);
  store.removeItem(key);
  if (!raw) {
    logDesktopRuntimeEvent("payload_take_miss", {
      expected_kind: expectedKind,
      payload_id: payloadID,
    });
    return null;
  }
  try {
    const item = JSON.parse(raw);
    const kind = String(item?.kind || "").trim();
    if (expectedKind && kind !== expectedKind) {
      logDesktopRuntimeEvent("payload_take_kind_mismatch", {
        expected_kind: expectedKind,
        kind,
        payload_id: payloadID,
      });
      return null;
    }
    const createdAt = Number(item?.created_at || 0);
    if (!Number.isFinite(createdAt) || Date.now() - createdAt > DESKTOP_WINDOW_PAYLOAD_MAX_AGE_MS) {
      logDesktopRuntimeEvent("payload_take_expired", {
        expected_kind: expectedKind,
        kind,
        payload_id: payloadID,
      });
      return null;
    }
    logDesktopRuntimeEvent("payload_taken", {
      expected_kind: expectedKind,
      kind,
      payload_id: payloadID,
      payload: summarizeDesktopPayload(item?.payload),
    });
    return item?.payload || null;
  } catch (error) {
    logDesktopRuntimeEvent("payload_take_parse_error", {
      expected_kind: expectedKind,
      payload_id: payloadID,
      error: error instanceof Error ? error.message : String(error || ""),
    });
    return null;
  }
}

function numericOption(value, fallback) {
  return Number.isFinite(value) ? Math.trunc(value) : fallback;
}

function buildDesktopWindowRequest(path, title, options = {}, defaults = {}) {
  const value = options && typeof options === "object" ? options : {};
  const fallback = defaults && typeof defaults === "object" ? defaults : {};
  return {
    path,
    title,
    width: numericOption(value.width, numericOption(fallback.width, DEFAULT_DESKTOP_DIALOG_WIDTH)),
    height: numericOption(value.height, numericOption(fallback.height, DEFAULT_DESKTOP_DIALOG_HEIGHT)),
    min_width: numericOption(value.min_width, numericOption(fallback.min_width, DEFAULT_DESKTOP_DIALOG_MIN_WIDTH)),
    min_height: numericOption(value.min_height, numericOption(fallback.min_height, DEFAULT_DESKTOP_DIALOG_MIN_HEIGHT)),
    position: typeof value.position === "string" ? value.position : "center",
    x: numericOption(value.x, 0),
    y: numericOption(value.y, 0),
  };
}

function desktopWindowPath(windowID, query) {
  const id = String(windowID || "").trim();
  if (!id) {
    return "";
  }
  const queryString = query instanceof URLSearchParams ? query.toString() : "";
  return `/window/${encodeURIComponent(id)}${queryString ? `?${queryString}` : ""}`;
}

export async function openDesktopRouteWindow({ windowID, title, query, options = {}, defaults = {} } = {}) {
  if (!isDesktopRuntime()) {
    return false;
  }
  const path = desktopWindowPath(windowID, query);
  if (!path) {
    return false;
  }
  return await openDesktopWindow(buildDesktopWindowRequest(path, title, options, defaults));
}

function queryParam(value, fallback) {
  if (typeof value !== "string") {
    return fallback;
  }
  const text = value.trim();
  return text || fallback;
}

function boolQueryParam(value, fallback) {
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  if (typeof value === "string") {
    const text = value.trim().toLowerCase();
    if (text === "0" || text === "false" || text === "none" || text === "off") {
      return "false";
    }
    if (text === "1" || text === "true" || text === "window" || text === "on") {
      return "true";
    }
  }
  return fallback ? "true" : "false";
}

export async function openRawJsonDesktopWindow(options = {}) {
  if (!isDesktopRuntime()) {
    return false;
  }
  const title = String(options.title || "RAW JSON").trim() || "RAW JSON";
  const json = String(options.json || "");
  if (!json) {
    return false;
  }
  const payloadID = saveDesktopWindowPayload(RAW_JSON_WINDOW_ID, { title, json });
  if (!payloadID) {
    return false;
  }
  const query = new URLSearchParams({
    padding: queryParam(options.padding, "none"),
    payload_id: payloadID,
    scroll: boolQueryParam(options.scroll, true),
    title,
  });
  return await openDesktopRouteWindow({
    windowID: RAW_JSON_WINDOW_ID,
    title,
    query,
    options,
    defaults: {
      width: 980,
      height: 720,
      min_width: 640,
      min_height: 420,
    },
  });
}

export async function openPokeDesktopWindow(options = {}) {
  if (!isDesktopRuntime()) {
    return false;
  }
  const title = String(options.title || "Poke").trim() || "Poke";
  const query = new URLSearchParams({
    padding: queryParam(options.padding, "default"),
    scroll: boolQueryParam(options.scroll, true),
    title,
    t: Date.now().toString(36),
  });
  return await openDesktopRouteWindow({
    windowID: POKE_WINDOW_ID,
    title,
    query,
    options,
  });
}

function openPayloadDesktopWindow(windowID, options = {}, defaults = {}) {
  if (!isDesktopRuntime()) {
    return false;
  }
  const id = String(windowID || "").trim();
  const title = String(options.title || "").trim();
  if (!id || !title) {
    return false;
  }
  const payloadID = saveDesktopWindowPayload(id, options.payload || {});
  if (!payloadID) {
    return false;
  }
  const requestID = String(options.payload?.request_id || "").trim();
  const query = new URLSearchParams({
    padding: queryParam(options.padding, "default"),
    payload_id: payloadID,
    request_id: requestID,
    scroll: boolQueryParam(options.scroll, true),
    title,
  });
  logDesktopRuntimeEvent("open_payload_window", {
    window_id: id,
    title,
    payload_id: payloadID,
    request_id: requestID,
    payload: summarizeDesktopPayload(options.payload || {}),
  });
  return openDesktopRouteWindow({
    windowID: id,
    title,
    query,
    options,
    defaults,
  });
}

export async function openSetupPickerDesktopWindow(options = {}) {
  return openPayloadDesktopWindow(SETUP_PICKER_WINDOW_ID, options, {
    width: 620,
    height: 620,
    min_width: 480,
    min_height: 420,
  });
}

export async function openSetupConnectionTestDesktopWindow(options = {}) {
  return openPayloadDesktopWindow(SETUP_CONNECTION_TEST_WINDOW_ID, options, {
    width: 640,
    height: 620,
    min_width: 500,
    min_height: 420,
  });
}

export async function openCodexAuthDesktopWindow(options = {}) {
  return openPayloadDesktopWindow(CODEX_AUTH_WINDOW_ID, options, {
    width: 620,
    height: 520,
    min_width: 500,
    min_height: 360,
  });
}

export async function openRawTextEditorDesktopWindow(options = {}) {
  return openPayloadDesktopWindow(
    RAW_TEXT_EDITOR_WINDOW_ID,
    {
      ...options,
      padding: queryParam(options.padding, "default"),
      scroll: boolQueryParam(options.scroll, false),
    },
    {
      width: 980,
      height: 760,
      min_width: 640,
      min_height: 460,
    }
  );
}
