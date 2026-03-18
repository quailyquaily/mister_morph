import { applyLanguageChange, currentLocale, hydrateLanguage, localeState, setLanguage, translate } from "../i18n";
import { authState, authValid, clearAuth, hydrateAuth, saveAuth } from "../stores";
import {
  endpointState,
  ensureEndpointSelection,
  hydrateEndpointSelection,
  setSelectedEndpointRef,
} from "../stores";

const BASE_PATH = "";
const API_BASE = "/api";

const TASK_STATUS_META = [
  { titleKey: "status_all", value: "" },
  { titleKey: "status_queued", value: "queued" },
  { titleKey: "status_running", value: "running" },
  { titleKey: "status_pending", value: "pending" },
  { titleKey: "status_done", value: "done" },
  { titleKey: "status_failed", value: "failed" },
  { titleKey: "status_canceled", value: "canceled" },
];

async function apiFetch(pathname, options = {}) {
  const method = options.method || "GET";
  const headers = { ...(options.headers || {}) };
  if (!options.noAuth && authState.token) {
    headers.Authorization = `Bearer ${authState.token}`;
  }
  let body = options.body;
  if (body !== undefined && body !== null && typeof body !== "string") {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(body);
  }

  const resp = await fetch(`${API_BASE}${pathname}`, {
    method,
    headers,
    body,
    cache: "no-store",
  });
  const raw = await resp.text();
  const parsed = raw ? safeJSON(raw, { error: raw }) : {};
  if (!resp.ok) {
    if (resp.status === 401 && !options.noAuth) {
      clearAuth();
    }
    const err = new Error(parsed.error || `HTTP ${resp.status}`);
    err.status = resp.status;
    throw err;
  }
  return parsed;
}

async function fetchConsoleAuthConfig() {
  return apiFetch("/auth/config", { noAuth: true });
}

async function ensureConsoleSession() {
  if (authValid.value) {
    return true;
  }
  const authConfig = await fetchConsoleAuthConfig();
  if (authConfig?.password_required !== false) {
    return false;
  }
  const body = await apiFetch("/auth/login", {
    method: "POST",
    body: {},
    noAuth: true,
  });
  authState.token = typeof body.access_token === "string" ? body.access_token : "";
  authState.expiresAt = typeof body.expires_at === "string" ? body.expires_at : "";
  authState.account = typeof body.account === "string" ? body.account : "console";
  saveAuth();
  return Boolean(authState.token);
}

async function loadEndpoints() {
  const data = await apiFetch("/endpoints");
  const items = Array.isArray(data.items)
    ? data.items.map((item) => ({
        endpoint_ref: item && typeof item.endpoint_ref === "string" ? item.endpoint_ref : "",
        name: item && typeof item.name === "string" ? item.name : "",
        url: item && typeof item.url === "string" ? item.url : "",
        connected: toBool(item && item.connected, false),
        agent_name: item && typeof item.agent_name === "string" ? item.agent_name : "",
        mode: item && typeof item.mode === "string" ? item.mode : "",
        can_submit: toBool(item && item.can_submit, false),
        submit_endpoint_ref:
          item && typeof item.submit_endpoint_ref === "string" ? item.submit_endpoint_ref : "",
      }))
    : [];
  endpointState.items = items.filter((item) => item.endpoint_ref);
  ensureEndpointSelection();
  return endpointState.items;
}

function runtimeEndpointByRef(endpointRef) {
  const ref = typeof endpointRef === "string" ? endpointRef.trim() : "";
  if (!ref) {
    return null;
  }
  return endpointState.items.find((item) => item && item.endpoint_ref === ref) || null;
}

function pushUniqueEndpointRef(list, value) {
  const ref = typeof value === "string" ? value.trim() : "";
  if (!ref || list.includes(ref)) {
    return;
  }
  list.push(ref);
}

function taskEndpointRefsForSelection(endpointRef = endpointState.selectedRef) {
  const refs = [];
  const selected = runtimeEndpointByRef(endpointRef);
  if (!selected) {
    pushUniqueEndpointRef(refs, endpointRef);
    return refs;
  }
  pushUniqueEndpointRef(refs, selected.endpoint_ref);
  pushUniqueEndpointRef(refs, selected.submit_endpoint_ref);
  return refs;
}

async function runtimeApiFetchForEndpoint(endpointRef, pathname, options = {}) {
  endpointRef = String(endpointRef || "").trim();
  if (!endpointRef) {
    const err = new Error(translate("msg_select_endpoint"));
    err.status = 400;
    throw err;
  }
  const uri = String(pathname || "").trim();
  if (!uri) {
    const err = new Error("missing uri");
    err.status = 400;
    throw err;
  }
  const normalizedURI = uri.startsWith("/") ? uri : `/${uri}`;
  const q = new URLSearchParams();
  q.set("endpoint", endpointRef);
  q.set("uri", normalizedURI);
  return apiFetch(`/proxy?${q.toString()}`, options);
}

async function runtimeApiFetch(pathname, options = {}) {
  return runtimeApiFetchForEndpoint(endpointState.selectedRef.trim(), pathname, options);
}

async function runtimeApiFetchFirstForEndpoints(endpointRefs, pathname, options = {}) {
  const refs = Array.isArray(endpointRefs)
    ? endpointRefs.map((value) => String(value || "").trim()).filter(Boolean)
    : [];
  if (refs.length === 0) {
    const err = new Error(translate("msg_select_endpoint"));
    err.status = 400;
    throw err;
  }
  let lastErr = null;
  for (const endpointRef of refs) {
    try {
      return await runtimeApiFetchForEndpoint(endpointRef, pathname, options);
    } catch (err) {
      lastErr = err;
      if (err?.status !== 404) {
        throw err;
      }
    }
  }
  throw lastErr || new Error(`HTTP 404`);
}

function safeJSON(raw, fallback) {
  try {
    return JSON.parse(raw);
  } catch {
    return fallback;
  }
}

function formatTime(ts) {
  if (!ts) {
    return "-";
  }
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) {
    return ts;
  }
  return d.toLocaleString(currentLocale());
}

function formatRemainingUntil(ts) {
  if (!ts) {
    return translate("ttl_unknown");
  }
  const ms = new Date(ts).getTime() - Date.now();
  if (!Number.isFinite(ms)) {
    return translate("ttl_invalid");
  }
  if (ms <= 0) {
    return translate("ttl_expired");
  }
  const totalMinutes = Math.floor(ms / 60000);
  if (totalMinutes < 60) {
    return translate("ttl_min_left", { m: totalMinutes });
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours < 24) {
    return translate("ttl_hour_left", { h: hours, m: minutes });
  }
  const days = Math.floor(hours / 24);
  const hourPart = hours % 24;
  return translate("ttl_day_left", { d: days, h: hourPart });
}

function toInt(value, fallback = 0) {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return fallback;
  }
  return Math.trunc(n);
}

function toBool(value, fallback = false) {
  if (typeof value === "boolean") {
    return value;
  }
  if (typeof value === "number") {
    return value !== 0;
  }
  if (typeof value === "string") {
    const v = value.trim().toLowerCase();
    if (v === "true" || v === "1" || v === "yes" || v === "on") {
      return true;
    }
    if (v === "false" || v === "0" || v === "no" || v === "off") {
      return false;
    }
  }
  return fallback;
}

function formatBytes(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) {
    return "-";
  }
  if (n < 1024) {
    return `${Math.trunc(n)} B`;
  }
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let v = n;
  let idx = -1;
  while (v >= 1024 && idx < units.length - 1) {
    v /= 1024;
    idx += 1;
  }
  const digits = v >= 100 ? 0 : v >= 10 ? 1 : 2;
  return `${v.toFixed(digits)} ${units[idx]}`;
}

export {
  BASE_PATH,
  localeState,
  translate,
  applyLanguageChange,
  currentLocale,
  setLanguage,
  hydrateLanguage,
  TASK_STATUS_META,
  authState,
  authValid,
  saveAuth,
  clearAuth,
  hydrateAuth,
  endpointState,
  setSelectedEndpointRef,
  hydrateEndpointSelection,
  apiFetch,
  fetchConsoleAuthConfig,
  ensureConsoleSession,
  loadEndpoints,
  ensureEndpointSelection,
  runtimeApiFetch,
  runtimeApiFetchForEndpoint,
  runtimeApiFetchFirstForEndpoints,
  runtimeEndpointByRef,
  taskEndpointRefsForSelection,
  safeJSON,
  formatTime,
  formatRemainingUntil,
  toInt,
  toBool,
  formatBytes,
};
