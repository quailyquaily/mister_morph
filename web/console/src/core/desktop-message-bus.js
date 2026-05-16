const DESKTOP_WINDOW_BROADCAST_CHANNEL = "mistermorph:desktop-window-message";
const DESKTOP_WINDOW_STORAGE_KEY = "mistermorph_desktop_window_message";
const MAX_SEEN_MESSAGE_IDS = 500;

let desktopBroadcastChannel = null;
const seenDesktopMessageIDs = new Set();

function currentWindow() {
  return typeof window === "undefined" ? null : window;
}

export function sendDesktopWindowBusMessage(message = {}) {
  const deliveries = [];
  const errors = [];
  const plainResult = toPlainDesktopWindowMessage(ensureDesktopDeliveryID(message));
  const deliverable = plainResult.message;
  if (!deliverable) {
    if (plainResult.error) {
      errors.push({
        channel: "serialize",
        error: plainResult.error,
      });
    }
    return {
      deliveries,
      errors,
      message,
      sent: false,
    };
  }

  const channel = getDesktopBroadcastChannel();
  if (channel) {
    try {
      channel.postMessage(deliverable);
      deliveries.push("broadcast");
    } catch (error) {
      errors.push({
        channel: "broadcast",
        error: errorMessage(error),
      });
    }
  }

  const storageResult = postDesktopStorageMessage(deliverable);
  if (storageResult.ok) {
    deliveries.push("storage");
  } else if (storageResult.error) {
    errors.push({
      channel: "storage",
      error: storageResult.error,
    });
  }

  return {
    deliveries,
    errors,
    message: deliverable,
    sent: deliveries.length > 0,
  };
}

function toPlainDesktopWindowMessage(message) {
  try {
    return {
      message: JSON.parse(JSON.stringify(message)),
      error: "",
    };
  } catch (error) {
    return {
      message: null,
      error: errorMessage(error),
    };
  }
}

export function subscribeDesktopWindowBus(callback) {
  const win = currentWindow();
  if (!win || typeof callback !== "function") {
    return () => {};
  }

  const handleMessage = (raw, channel) => {
    callback(raw, channel);
  };

  const storageListener = (event) => {
    if (event?.key !== DESKTOP_WINDOW_STORAGE_KEY || !event.newValue) {
      return;
    }
    try {
      const item = JSON.parse(event.newValue);
      handleMessage(item?.message || item, "storage");
    } catch {}
  };

  const channel = getDesktopBroadcastChannel();
  const broadcastListener = (event) => {
    handleMessage(event?.data || null, "broadcast");
  };

  win.addEventListener("storage", storageListener);
  if (channel) {
    channel.addEventListener("message", broadcastListener);
  }

  return () => {
    win.removeEventListener("storage", storageListener);
    if (channel) {
      channel.removeEventListener("message", broadcastListener);
    }
  };
}

export function acceptDesktopWindowBusMessage(raw) {
  const message = normalizeReceivedDesktopWindowMessage(raw);
  if (!message || hasSeenDesktopMessage(message._delivery_id)) {
    return null;
  }
  return message;
}

function getDesktopBroadcastChannel() {
  const win = currentWindow();
  if (!win || typeof win.BroadcastChannel !== "function") {
    return null;
  }
  if (desktopBroadcastChannel) {
    return desktopBroadcastChannel;
  }
  try {
    desktopBroadcastChannel = new win.BroadcastChannel(DESKTOP_WINDOW_BROADCAST_CHANNEL);
  } catch {
    desktopBroadcastChannel = null;
  }
  return desktopBroadcastChannel;
}

function postDesktopStorageMessage(message) {
  const win = currentWindow();
  const store = win?.localStorage;
  if (!store) {
    return { ok: false, error: "" };
  }
  try {
    store.setItem(DESKTOP_WINDOW_STORAGE_KEY, JSON.stringify(message));
    win.setTimeout(() => {
      try {
        if (store.getItem(DESKTOP_WINDOW_STORAGE_KEY)) {
          store.removeItem(DESKTOP_WINDOW_STORAGE_KEY);
        }
      } catch {}
    }, 1000);
    return { ok: true, error: "" };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

function normalizeReceivedDesktopWindowMessage(message) {
  if (!message || typeof message !== "object") {
    return null;
  }
  const type = typeof message.type === "string" ? message.type.trim() : "";
  if (!type) {
    return null;
  }
  return message;
}

function ensureDesktopDeliveryID(message) {
  const value = message && typeof message === "object" ? message : {};
  const current = typeof value._delivery_id === "string" ? value._delivery_id.trim() : "";
  if (current) {
    return value;
  }
  return {
    ...value,
    _delivery_id: randomDesktopDeliveryID(),
  };
}

function hasSeenDesktopMessage(id) {
  const value = typeof id === "string" ? id.trim() : "";
  if (!value) {
    return false;
  }
  if (seenDesktopMessageIDs.has(value)) {
    return true;
  }
  seenDesktopMessageIDs.add(value);
  if (seenDesktopMessageIDs.size > MAX_SEEN_MESSAGE_IDS) {
    const first = seenDesktopMessageIDs.values().next().value;
    seenDesktopMessageIDs.delete(first);
  }
  return false;
}

function randomDesktopDeliveryID() {
  const cryptoID = typeof crypto !== "undefined" ? crypto.randomUUID : null;
  if (typeof cryptoID === "function") {
    return cryptoID.call(crypto);
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error || "");
}
