import { isDesktopRuntime, openDesktopWindow } from "./desktop-runtime";

const DESKTOP_WINDOW_PAYLOAD_PREFIX = "mistermorph_desktop_window_payload:";
const DESKTOP_WINDOW_PAYLOAD_MAX_AGE_MS = 5 * 60 * 1000;
const RAW_JSON_WINDOW_ID = "raw-json";
const POKE_WINDOW_ID = "poke";
const DEFAULT_DESKTOP_DIALOG_WIDTH = 720;
const DEFAULT_DESKTOP_DIALOG_HEIGHT = 560;
const DEFAULT_DESKTOP_DIALOG_MIN_WIDTH = 480;
const DEFAULT_DESKTOP_DIALOG_MIN_HEIGHT = 360;

function payloadStorage() {
  return typeof window === "undefined" ? null : window.localStorage;
}

function randomPayloadID() {
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
    return "";
  }
  pruneDesktopWindowPayloads(store);
  const id = randomPayloadID();
  store.setItem(
    payloadKey(id),
    JSON.stringify({
      kind: String(kind || "").trim(),
      created_at: Date.now(),
      payload,
    })
  );
  return id;
}

export function takeDesktopWindowPayload(id, expectedKind = "") {
  const store = payloadStorage();
  const payloadID = String(id || "").trim();
  if (!store || !payloadID) {
    return null;
  }
  const key = payloadKey(payloadID);
  const raw = store.getItem(key);
  store.removeItem(key);
  if (!raw) {
    return null;
  }
  try {
    const item = JSON.parse(raw);
    const kind = String(item?.kind || "").trim();
    if (expectedKind && kind !== expectedKind) {
      return null;
    }
    const createdAt = Number(item?.created_at || 0);
    if (!Number.isFinite(createdAt) || Date.now() - createdAt > DESKTOP_WINDOW_PAYLOAD_MAX_AGE_MS) {
      return null;
    }
    return item?.payload || null;
  } catch {
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
