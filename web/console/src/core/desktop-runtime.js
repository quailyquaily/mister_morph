function currentWindow() {
  return typeof window === "undefined" ? null : window;
}

let frontendReadyReported = false;
const DESKTOP_HIDE_WINDOW_MESSAGE = "mistermorph:hide-window";

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

export function isDesktopRuntime() {
  if (currentWindow()?.__MISTERMORPH_DESKTOP_RUNTIME__ === true) {
    return true;
  }
  return desktopMessageSender() !== null || desktopCallByName() !== null;
}

export function canPostDesktopRawMessage() {
  return desktopMessageSender() !== null;
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
  Promise.resolve(call("main.App.ReportFrontendReady")).catch(() => {
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
  const call = desktopCallByName();
  if (call) {
    await call("main.App.OpenWindow", normalizeDesktopWindowOptions(options));
    return true;
  }
  return false;
}

export function hideDesktopWindow() {
  return postDesktopRawMessage(DESKTOP_HIDE_WINDOW_MESSAGE);
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
