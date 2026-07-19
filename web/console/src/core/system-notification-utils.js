const CLAIM_STORAGE_KEY = "mistermorph_console_notification_ids_v1";
const CLAIM_LIMIT = 64;

export function normalizeConsoleNotification(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const id = String(raw.id || "").trim();
  const title = String(raw.title || "").trim();
  if (!id || !title) {
    return null;
  }
  return {
    id,
    title,
    body: String(raw.body || "").trim(),
  };
}

export function claimConsoleNotification(storage, id, limit = CLAIM_LIMIT) {
  const notificationID = String(id || "").trim();
  if (!notificationID) {
    return false;
  }
  if (!storage || typeof storage.getItem !== "function" || typeof storage.setItem !== "function") {
    return true;
  }
  try {
    const parsed = JSON.parse(storage.getItem(CLAIM_STORAGE_KEY) || "[]");
    const ids = Array.isArray(parsed) ? parsed.map((item) => String(item || "").trim()).filter(Boolean) : [];
    if (ids.includes(notificationID)) {
      return false;
    }
    ids.push(notificationID);
    const max = Number.isInteger(limit) && limit > 0 ? limit : CLAIM_LIMIT;
    storage.setItem(CLAIM_STORAGE_KEY, JSON.stringify(ids.slice(-max)));
    return true;
  } catch {
    return true;
  }
}

export function resolveBrowserNotificationPermission(win = typeof window === "undefined" ? null : window) {
  if (!win || win.isSecureContext !== true || typeof win.Notification !== "function") {
    return "unsupported";
  }
  const permission = String(win.Notification.permission || "default").trim().toLowerCase();
  return permission === "granted" || permission === "denied" ? permission : "default";
}
