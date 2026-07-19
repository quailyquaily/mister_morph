import { watch } from "vue";

import {
  authValid,
  buildConsoleNotificationURL,
  createConsoleStreamTicket,
  safeJSON,
} from "./context";
import {
  canUseDesktopNotifications,
  isDesktopRuntime,
  requestDesktopNotificationPermission,
  showDesktopNotification,
} from "./desktop-runtime";
import {
  claimConsoleNotification,
  normalizeConsoleNotification,
  resolveBrowserNotificationPermission,
} from "./system-notification-utils";

const RECONNECT_DELAY_MS = 3000;
const NOTIFICATION_TEXT_LIMIT = 600;

let installed = false;
let activeSocket = null;
let reconnectTimer = 0;
let connectionGeneration = 0;

export async function requestSystemNotificationPermission() {
  if (isDesktopRuntime() && canUseDesktopNotifications()) {
    try {
      return (await requestDesktopNotificationPermission()) ? "granted" : "denied";
    } catch {
      return await requestBrowserNotificationPermission();
    }
  }
  return await requestBrowserNotificationPermission();
}

async function requestBrowserNotificationPermission() {
  const current = resolveBrowserNotificationPermission();
  if (current !== "default") {
    return current;
  }
  try {
    return await window.Notification.requestPermission();
  } catch {
    return "unsupported";
  }
}

export async function showSystemNotification(event) {
  const notification = normalizeConsoleNotification(event);
  if (!notification) {
    return false;
  }
  const title = truncateNotificationText(notification.title);
  const body = truncateNotificationText(notification.body);
  if (isDesktopRuntime() && canUseDesktopNotifications()) {
    try {
      await showDesktopNotification({
        id: notification.id,
        title,
        body,
      });
      return true;
    } catch {
      // Fall back to the Web Notification API when the native service is unavailable.
    }
  }
  if (resolveBrowserNotificationPermission() !== "granted") {
    return false;
  }
  try {
    const systemNotification = new window.Notification(title, {
      body,
      tag: notification.id,
    });
    systemNotification.onclick = () => {
      window.focus();
      systemNotification.close();
    };
    return true;
  } catch {
    return false;
  }
}

export function installSystemNotifications() {
  if (installed || typeof window === "undefined") {
    return;
  }
  installed = true;
  watch(
    authValid,
    (valid) => {
      if (valid) {
        void connectNotificationSocket();
      } else {
        disconnectNotificationSocket();
      }
    },
    { immediate: true }
  );
}

async function connectNotificationSocket() {
  if (!authValid.value || activeSocket) {
    return;
  }
  clearReconnectTimer();
  const generation = ++connectionGeneration;
  let payload;
  try {
    payload = await createConsoleStreamTicket();
  } catch {
    scheduleReconnect(generation);
    return;
  }
  if (generation !== connectionGeneration || !authValid.value) {
    return;
  }
  const url = buildConsoleNotificationURL(payload?.ticket);
  if (!url) {
    scheduleReconnect(generation);
    return;
  }
  const socket = new WebSocket(url);
  activeSocket = socket;
  socket.onmessage = (message) => {
    const event = normalizeConsoleNotification(safeJSON(message.data, null));
    if (!event || !claimConsoleNotification(window.localStorage, event.id)) {
      return;
    }
    void showSystemNotification(event);
  };
  socket.onclose = () => {
    if (activeSocket === socket) {
      activeSocket = null;
    }
    scheduleReconnect(generation);
  };
  socket.onerror = () => {
    socket.close();
  };
}

function disconnectNotificationSocket() {
  connectionGeneration += 1;
  clearReconnectTimer();
  const socket = activeSocket;
  activeSocket = null;
  if (socket) {
    socket.onclose = null;
    socket.close();
  }
}

function scheduleReconnect(generation) {
  if (!authValid.value || reconnectTimer || generation !== connectionGeneration) {
    return;
  }
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = 0;
    void connectNotificationSocket();
  }, RECONNECT_DELAY_MS);
}

function clearReconnectTimer() {
  if (!reconnectTimer) {
    return;
  }
  window.clearTimeout(reconnectTimer);
  reconnectTimer = 0;
}

function truncateNotificationText(value) {
  const text = String(value || "").trim().replace(/\s+/g, " ");
  if (text.length <= NOTIFICATION_TEXT_LIMIT) {
    return text;
  }
  return `${text.slice(0, NOTIFICATION_TEXT_LIMIT - 1)}…`;
}
