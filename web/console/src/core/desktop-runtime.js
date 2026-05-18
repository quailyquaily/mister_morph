import {
  acceptDesktopWindowBusMessage,
  sendDesktopWindowBusMessage,
  subscribeDesktopWindowBus,
} from "./desktop-message-bus.js";

function currentWindow() {
  return typeof window === "undefined" ? null : window;
}

let frontendReadyReported = false;
const DESKTOP_HIDE_WINDOW_MESSAGE = "mistermorph:hide-window";
const DESKTOP_LOG_MESSAGE_PREFIX = "mistermorph:desktop-log:";
const DESKTOP_OPEN_WINDOW_PREFIX = "mistermorph:open-window:";
const DESKTOP_WINDOW_MESSAGE_EVENT = "mistermorph:desktop-window-message";
const DESKTOP_WINDOW_MESSAGE_PREFIX = "mistermorph:window-message:";
const DESKTOP_MESSAGE_SYNC_DELAYS = [0, 150, 600];
const DESKTOP_WINDOW_DEBUG_FLAG = "mistermorph_desktop_window_debug";
const VERBOSE_DESKTOP_LOG_EVENTS = new Set([
  "receive_window_message",
  "send_window_message",
  "send_window_message_direct",
]);
const desktopWindowMessageCallbacks = new Set();
let desktopWindowMessageWindow = null;
let desktopWindowHostListener = null;
let removeDesktopWindowBusListener = null;

function desktopMessageSender() {
  const win = currentWindow();
  if (!win) {
    return null;
  }

  const chromePostMessage = win.chrome?.webview?.postMessage;
  if (typeof chromePostMessage === "function") {
    return (message) => chromePostMessage.call(win.chrome.webview, message);
  }

  const webkitPostMessage = win.webkit?.messageHandlers?.external?.postMessage;
  if (typeof webkitPostMessage === "function") {
    return (message) => webkitPostMessage.call(win.webkit.messageHandlers.external, message);
  }

  const wailsInvoke = win._wails?.invoke || win.wails?.invoke;
  if (typeof wailsInvoke === "function") {
    return (message) => wailsInvoke(message);
  }

  return null;
}

function desktopCallByName() {
  const win = currentWindow();
  const byName = win?.wails?.Call?.ByName || win?._wails?.Call?.ByName;
  return typeof byName === "function" ? byName.bind(win.wails?.Call || win._wails?.Call) : null;
}

function desktopBindingName(method) {
  const win = currentWindow();
  const bindings = win?.__MISTERMORPH_DESKTOP_BINDINGS__;
  const name = bindings && typeof bindings === "object" ? bindings[method] : "";
  return typeof name === "string" && name.trim() ? name.trim() : `main.App.${method}`;
}

export function isDesktopRuntime() {
  if (currentWindow()?.__MISTERMORPH_DESKTOP_RUNTIME__ === true) {
    return true;
  }
  return desktopMessageSender() !== null || desktopCallByName() !== null;
}

export function canPostDesktopRawMessage() {
  return desktopMessageSender() !== null;
}

export function canCheckDesktopUpdate() {
  return desktopCallByName() !== null;
}

export function installDesktopRuntimeMode() {
  if (typeof document === "undefined" || !isDesktopRuntime()) {
    return;
  }
  document.documentElement.dataset.runtime = "desktop";
}

export function reportDesktopFrontendReady() {
  if (frontendReadyReported) {
    return;
  }
  const call = desktopCallByName();
  if (!call) {
    return;
  }
  frontendReadyReported = true;
  Promise.resolve(call(desktopBindingName("ReportFrontendReady"))).catch(() => {
    frontendReadyReported = false;
  });
}

export function postDesktopRawMessage(message) {
  const send = desktopMessageSender();
  if (!send) {
    return false;
  }
  try {
    send(message);
    return true;
  } catch {
    return false;
  }
}

export async function openDesktopWindow(options = {}) {
  const normalized = normalizeDesktopWindowOptions(options);
  logDesktopRuntimeEvent("open_window_request", {
    path: normalized.path,
    title: normalized.title,
    width: normalized.width,
    height: normalized.height,
    position: normalized.position,
  });
  const rawMessage = `${DESKTOP_OPEN_WINDOW_PREFIX}${JSON.stringify(normalized)}`;
  if (postDesktopRawMessage(rawMessage)) {
    logDesktopRuntimeEvent("open_window_raw_sent", {
      path: normalized.path,
      title: normalized.title,
    });
    return true;
  }
  const call = desktopCallByName();
  if (call) {
    logDesktopRuntimeEvent("open_window_binding_call", {
      path: normalized.path,
      title: normalized.title,
    });
    try {
      await call(desktopBindingName("OpenWindow"), normalized);
      logDesktopRuntimeEvent("open_window_binding_ok", {
        path: normalized.path,
        title: normalized.title,
      });
    } catch (error) {
      logDesktopRuntimeEvent("open_window_binding_error", {
        path: normalized.path,
        title: normalized.title,
        error: errorMessage(error),
      });
      throw error;
    }
    return true;
  }
  logDesktopRuntimeEvent("open_window_unavailable", {
    path: normalized.path,
    title: normalized.title,
  });
  return false;
}

export function hideDesktopWindow() {
  logDesktopRuntimeEvent("hide_window");
  return postDesktopRawMessage(DESKTOP_HIDE_WINDOW_MESSAGE);
}

export async function checkDesktopUpdate() {
  const call = desktopCallByName();
  if (!call) {
    throw new Error("desktop update binding is unavailable");
  }
  return await call(desktopBindingName("CheckUpdate"));
}

export async function setDesktopAutoUpdateEnabled(enabled) {
  const call = desktopCallByName();
  if (!call) {
    return false;
  }
  return (await call(desktopBindingName("SetAutoUpdateEnabled"), enabled === true)) === true;
}

export function sendDesktopWindowMessage(message = {}) {
  const normalized = normalizeDesktopWindowMessage(message);
  if (!normalized) {
    logDesktopRuntimeEvent("send_window_message_invalid");
    return false;
  }
  try {
    const busResult = sendDesktopWindowBusMessage(normalized);
    const deliverable = busResult.message;
    logDesktopRuntimeEvent("send_window_message", summarizeDesktopWindowMessage(deliverable));
    busResult.deliveries.forEach((channel) => {
      logDesktopRuntimeEvent("send_window_message_direct", {
        ...summarizeDesktopWindowMessage(deliverable),
        channel,
      });
    });
    busResult.errors.forEach((item) => {
      logDesktopRuntimeEvent("send_window_message_direct_error", {
        ...summarizeDesktopWindowMessage(deliverable),
        channel: item.channel,
        error: item.error,
      });
    });
    const sent = postDesktopRawMessage(`${DESKTOP_WINDOW_MESSAGE_PREFIX}${JSON.stringify(deliverable)}`);
    if (!sent) {
      logDesktopRuntimeEvent(
        busResult.sent ? "send_window_message_host_unavailable" : "send_window_message_failed",
        summarizeDesktopWindowMessage(deliverable)
      );
    }
    return busResult.sent || sent;
  } catch (error) {
    logDesktopRuntimeEvent("send_window_message_error", {
      ...summarizeDesktopWindowMessage(normalized),
      error: errorMessage(error),
    });
    return false;
  }
}

export function onDesktopWindowMessage(callback) {
  const win = currentWindow();
  if (!win || !isDesktopRuntime() || typeof callback !== "function") {
    return () => {};
  }
  desktopWindowMessageCallbacks.add(callback);
  installDesktopWindowMessageListener(win);
  let removed = false;
  return () => {
    if (removed) {
      return;
    }
    removed = true;
    desktopWindowMessageCallbacks.delete(callback);
    if (desktopWindowMessageCallbacks.size === 0) {
      uninstallDesktopWindowMessageListener();
    }
  };
}

function installDesktopWindowMessageListener(win) {
  if (desktopWindowMessageWindow === win && desktopWindowHostListener) {
    return;
  }
  uninstallDesktopWindowMessageListener();
  desktopWindowMessageWindow = win;
  desktopWindowHostListener = (event) => {
    dispatchDesktopWindowMessage(event?.detail || {}, "host");
  };
  removeDesktopWindowBusListener = subscribeDesktopWindowBus(dispatchDesktopWindowMessage);
  win.addEventListener(DESKTOP_WINDOW_MESSAGE_EVENT, desktopWindowHostListener);
}

function uninstallDesktopWindowMessageListener() {
  if (desktopWindowMessageWindow && desktopWindowHostListener) {
    desktopWindowMessageWindow.removeEventListener(DESKTOP_WINDOW_MESSAGE_EVENT, desktopWindowHostListener);
  }
  if (typeof removeDesktopWindowBusListener === "function") {
    removeDesktopWindowBusListener();
  }
  desktopWindowMessageWindow = null;
  desktopWindowHostListener = null;
  removeDesktopWindowBusListener = null;
}

function dispatchDesktopWindowMessage(raw, channel) {
  const detail = acceptDesktopWindowBusMessage(raw);
  if (!detail) {
    return;
  }
  logDesktopRuntimeEvent("receive_window_message", {
    ...summarizeDesktopWindowMessage(detail),
    channel,
  });
  Array.from(desktopWindowMessageCallbacks).forEach((callback) => {
    try {
      callback(detail);
    } catch (error) {
      logDesktopRuntimeEvent("receive_window_message_callback_error", {
        ...summarizeDesktopWindowMessage(detail),
        error: errorMessage(error),
      });
    }
  });
}

export function createDesktopMessageScheduler(send, delays = DESKTOP_MESSAGE_SYNC_DELAYS) {
  let timers = [];

  function clear() {
    const win = currentWindow();
    if (!win) {
      timers = [];
      return;
    }
    timers.forEach((timer) => win.clearTimeout(timer));
    timers = [];
  }

  function schedule(nextDelays = delays) {
    const win = currentWindow();
    if (!win || typeof send !== "function") {
      return;
    }
    clear();
    timers = (Array.isArray(nextDelays) ? nextDelays : delays).map((delay) => {
      const ms = Math.max(0, Number(delay) || 0);
      const timer = win.setTimeout(() => {
        timers = timers.filter((item) => item !== timer);
        send();
      }, ms);
      return timer;
    });
  }

  return {
    clear,
    schedule,
  };
}

export function logDesktopRuntimeEvent(event, fields = {}) {
  const name = typeof event === "string" ? event.trim() : "";
  if (!name) {
    return false;
  }
  if (VERBOSE_DESKTOP_LOG_EVENTS.has(name) && !isDesktopWindowDebugEnabled()) {
    return false;
  }
  const win = currentWindow();
  const body = {
    event: name,
    path: typeof win?.location?.pathname === "string" ? win.location.pathname : "",
    search: typeof win?.location?.search === "string" ? win.location.search : "",
    fields: sanitizeDesktopLogFields(fields),
  };
  try {
    return postDesktopRawMessage(`${DESKTOP_LOG_MESSAGE_PREFIX}${JSON.stringify(body)}`);
  } catch {
    return false;
  }
}

function isDesktopWindowDebugEnabled() {
  const win = currentWindow();
  if (win?.__MISTERMORPH_DESKTOP_WINDOW_DEBUG__ === true) {
    return true;
  }
  try {
    const text = String(win?.localStorage?.getItem(DESKTOP_WINDOW_DEBUG_FLAG) || "").trim().toLowerCase();
    return text === "1" || text === "true" || text === "yes" || text === "on";
  } catch {
    return false;
  }
}

export function summarizeDesktopPayload(payload) {
  const value = payload && typeof payload === "object" ? payload : {};
  const status = value.status && typeof value.status === "object" ? value.status : {};
  const summary = {};
  if (Object.prototype.hasOwnProperty.call(value, "request_id")) {
    summary.request_id = String(value.request_id || "");
  }
  if (Object.prototype.hasOwnProperty.call(value, "loading")) {
    summary.loading = value.loading === true;
  }
  if (Object.prototype.hasOwnProperty.call(value, "busy")) {
    summary.busy = value.busy === true;
  }
  if (Object.prototype.hasOwnProperty.call(value, "error")) {
    summary.has_error = String(value.error || "") !== "";
  }
  if (Array.isArray(value.benchmarks)) {
    summary.benchmarks_len = value.benchmarks.length;
  }
  if (Array.isArray(value.items)) {
    summary.items_len = value.items.length;
  }
  if (Object.keys(status).length > 0) {
    summary.status_logged_in = status.logged_in === true;
    summary.status_has_account = String(status.account_id || "").trim() !== "";
  }
  if (Object.prototype.hasOwnProperty.call(value, "loginSession")) {
    summary.has_login_session = String(value.loginSession || "").trim() !== "";
  }
  if (Object.prototype.hasOwnProperty.call(value, "modelValue")) {
    summary.model_value_len = String(value.modelValue || "").length;
  }
  if (Object.prototype.hasOwnProperty.call(value, "path")) {
    summary.has_path = String(value.path || "").trim() !== "";
  }
  return summary;
}

function summarizeDesktopWindowMessage(message) {
  const value = message && typeof message === "object" ? message : {};
  return {
    type: typeof value.type === "string" ? value.type : "",
    target: typeof value.target === "string" ? value.target : "",
    window_id: typeof value.window_id === "string" ? value.window_id : "",
    request_id: typeof value.request_id === "string" ? value.request_id : "",
    source: typeof value.source === "string" ? value.source : "",
    delivery_id: typeof value._delivery_id === "string" ? value._delivery_id : "",
    payload: summarizeDesktopPayload(value.payload),
  };
}

function sanitizeDesktopLogFields(fields) {
  const value = fields && typeof fields === "object" ? fields : {};
  const out = {};
  Object.entries(value).forEach(([key, raw]) => {
    out[key] = sanitizeDesktopLogValue(raw);
  });
  return out;
}

function sanitizeDesktopLogValue(value) {
  if (value === null || value === undefined) {
    return value;
  }
  if (typeof value === "boolean" || typeof value === "number") {
    return value;
  }
  if (typeof value === "string") {
    return value.length > 240 ? `${value.slice(0, 240)}...(truncated)` : value;
  }
  if (Array.isArray(value)) {
    return { array_len: value.length };
  }
  if (typeof value === "object") {
    const out = {};
    Object.entries(value).forEach(([key, raw]) => {
      out[key] = sanitizeDesktopLogValue(raw);
    });
    return out;
  }
  return String(value);
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error || "");
}

function normalizeDesktopWindowOptions(options) {
  const value = options && typeof options === "object" ? options : {};
  return {
    path: typeof value.path === "string" ? value.path : "",
    title: typeof value.title === "string" ? value.title : "",
    width: Number.isFinite(value.width) ? Math.trunc(value.width) : 0,
    height: Number.isFinite(value.height) ? Math.trunc(value.height) : 0,
    min_width: Number.isFinite(value.min_width) ? Math.trunc(value.min_width) : 0,
    min_height: Number.isFinite(value.min_height) ? Math.trunc(value.min_height) : 0,
    position: typeof value.position === "string" ? value.position : "center",
    x: Number.isFinite(value.x) ? Math.trunc(value.x) : 0,
    y: Number.isFinite(value.y) ? Math.trunc(value.y) : 0,
  };
}

function normalizeDesktopWindowMessage(message) {
  const value = message && typeof message === "object" ? message : {};
  const type = typeof value.type === "string" ? value.type.trim() : "";
  const target = typeof value.target === "string" ? value.target.trim() : "";
  const windowID = typeof value.window_id === "string" ? value.window_id.trim() : "";
  if (!type || (!target && !windowID)) {
    return null;
  }
  const normalized = {
    type,
    target,
    window_id: windowID,
    request_id: typeof value.request_id === "string" ? value.request_id.trim() : "",
  };
  if (Object.prototype.hasOwnProperty.call(value, "payload")) {
    normalized.payload = value.payload;
  }
  return normalized;
}
