import { currentLocale, localeState, translate } from "../i18n";
import { authState, authValid, endpointState } from "../stores";
import { recordApiRequest } from "./performance";
import { loadResource, resourceKey } from "./resources";

const BASE_PATH = readBasePath();
const API_BASE = joinBasePath(BASE_PATH, "/api");
const RECORD_API_PERF = import.meta.env.DEV === true;
const ENDPOINT_HEALTH_RETRY_MS = 500;
let endpointHealthRetry = 0;

const TASK_STATUS_META = [
  { titleKey: "status_all", value: "" },
  { titleKey: "status_queued", value: "queued" },
  { titleKey: "status_running", value: "running" },
  { titleKey: "status_pending", value: "pending" },
  { titleKey: "status_done", value: "done" },
  { titleKey: "status_failed", value: "failed" },
  { titleKey: "status_canceled", value: "canceled" },
];

function readBasePath() {
  const meta = document.querySelector('meta[name="mistermorph-base-path"]');
  const value = meta?.getAttribute("content") || "";
  if (!value || value.includes("__MISTERMORPH_BASE_PATH__")) {
    return "/";
  }
  return normalizeBasePath(value);
}

function normalizeBasePath(raw) {
  let value = String(raw || "").trim();
  if (!value) {
    return "/";
  }
  if (!value.startsWith("/")) {
    value = `/${value}`;
  }
  value = value.replace(/\/+/g, "/").replace(/\/+$/, "");
  return value || "/";
}

function joinBasePath(basePath, suffix) {
  const base = normalizeBasePath(basePath);
  const tail = String(suffix || "").trim();
  if (!tail) {
    return base;
  }
  if (base === "/") {
    return tail.startsWith("/") ? tail : `/${tail}`;
  }
  return tail.startsWith("/") ? `${base}${tail}` : `${base}/${tail}`;
}

function defaultPerfSource(pathname) {
  const value = String(pathname || "");
  if (
    value === "/auth/me" ||
    value === "/setup/integrity" ||
    value === "/endpoints" ||
    value === "/auth/config" ||
    value === "/auth/login"
  ) {
    return "bootstrap";
  }
  return "page";
}

async function apiFetch(pathname, options = {}) {
  const method = options.method || "GET";
  const headers = { ...(options.headers || {}) };
  if (!options.noAuth && authState.token) {
    headers.Authorization = `Bearer ${authState.token}`;
  }
  let body = options.body;
  if (body !== undefined && body !== null && typeof body !== "string" && !isRawFetchBody(body)) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(body);
  }

  const startedAt = RECORD_API_PERF ? performance.now() : 0;
  const perfSource = String(options.perfSource || defaultPerfSource(pathname));
  let status = 0;
  let ok = false;
  let error = "";
  try {
    const resp = await fetch(`${API_BASE}${pathname}`, {
      method,
      headers,
      body,
      cache: "no-store",
    });
    status = resp.status;
    ok = resp.ok;
    const raw = await resp.text();
    const parsed = raw ? safeJSON(raw, { error: raw }) : {};
    if (!resp.ok) {
      if (resp.status === 401 && !options.noAuth && resp.headers.get("X-MisterMorph-Proxy-Upstream") !== "1") {
        authState.clear();
      }
      const err = new Error(parsed.error || `HTTP ${resp.status}`);
      err.status = resp.status;
      error = err.message;
      throw err;
    }
    return parsed;
  } catch (err) {
    error = error || String(err?.message || err || "request failed");
    throw err;
  } finally {
    if (RECORD_API_PERF) {
      recordApiRequest({
        pathname,
        method,
        status,
        ok,
        error,
        source: perfSource,
        durationMs: performance.now() - startedAt,
      });
    }
  }
}

function isRawFetchBody(body) {
  return (
    body instanceof Blob ||
    body instanceof FormData ||
    body instanceof ArrayBuffer ||
    ArrayBuffer.isView(body) ||
    body instanceof URLSearchParams
  );
}

async function apiFetchBlob(pathname, options = {}) {
  const method = options.method || "GET";
  const headers = { ...(options.headers || {}) };
  if (!options.noAuth && authState.token) {
    headers.Authorization = `Bearer ${authState.token}`;
  }

  const startedAt = RECORD_API_PERF ? performance.now() : 0;
  const perfSource = String(options.perfSource || defaultPerfSource(pathname));
  let status = 0;
  let ok = false;
  let error = "";
  try {
    const resp = await fetch(`${API_BASE}${pathname}`, {
      method,
      headers,
      cache: "no-store",
    });
    status = resp.status;
    ok = resp.ok;
    if (!resp.ok) {
      const raw = await resp.text();
      const parsed = raw ? safeJSON(raw, { error: raw }) : {};
      if (resp.status === 401 && !options.noAuth && resp.headers.get("X-MisterMorph-Proxy-Upstream") !== "1") {
        authState.clear();
      }
      const err = new Error(parsed.error || `HTTP ${resp.status}`);
      err.status = resp.status;
      error = err.message;
      throw err;
    }
    return resp.blob();
  } catch (err) {
    error = error || String(err?.message || err || "request failed");
    throw err;
  } finally {
    if (RECORD_API_PERF) {
      recordApiRequest({
        pathname,
        method,
        status,
        ok,
        error,
        source: perfSource,
        durationMs: performance.now() - startedAt,
      });
    }
  }
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
  authState.save();
  return Boolean(authState.token);
}

async function fetchEndpoints() {
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
        health_pending: toBool(item && item.health_pending, false),
        submit_endpoint_ref:
          item && typeof item.submit_endpoint_ref === "string" ? item.submit_endpoint_ref : "",
        avatar_url: item && typeof item.avatar_url === "string" ? item.avatar_url : "",
      }))
    : [];
  endpointState.items = items.filter((item) => item.endpoint_ref);
  if (!endpointState.items.some((item) => item.health_pending)) {
    endpointState.ensureEndpointSelection();
  }
  scheduleEndpointHealthReload(endpointState.items);
  return endpointState.items;
}

function scheduleEndpointHealthReload(items) {
  const pending = Array.isArray(items)
    ? items.some((item) => item?.health_pending === true)
    : false;
  if (!pending || endpointHealthRetry) {
    return;
  }
  endpointHealthRetry = window.setTimeout(() => {
    endpointHealthRetry = 0;
    if (authValid.value) {
      void loadEndpoints().catch(() => {});
    }
  }, ENDPOINT_HEALTH_RETRY_MS);
}

async function loadEndpoints(options = {}) {
  return loadResource(resourceKey("console", "endpoints"), fetchEndpoints, {
    force: options.force !== false,
  });
}

async function ensureEndpointsLoaded() {
  if (endpointState.items.length > 0) {
    endpointState.ensureEndpointSelection();
    return endpointState.items;
  }
  return loadEndpoints();
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

async function runtimeApiDownloadForEndpoint(endpointRef, pathname, options = {}) {
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
  return apiFetchBlob(`/proxy/download?${q.toString()}`, options);
}

async function createArtifactPreviewTicket(payload) {
  return apiFetch("/artifacts/preview-ticket", {
    method: "POST",
    body: payload || {},
  });
}

async function renewArtifactPreviewTicket(ticket) {
  return apiFetch("/artifacts/preview-ticket/renew", {
    method: "POST",
    body: { ticket: String(ticket || "").trim() },
  });
}

async function runtimeApiFetch(pathname, options = {}) {
  return runtimeApiFetchForEndpoint(endpointState.selectedRef.trim(), pathname, options);
}

async function createConsoleStreamTicket() {
  return apiFetch("/stream/ticket", {
    method: "POST",
    body: {},
  });
}

function buildConsoleStreamURL(ticket, taskID) {
  const streamTicket = String(ticket || "").trim();
  const streamTaskID = String(taskID || "").trim();
  if (!streamTicket || !streamTaskID) {
    return "";
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams();
  query.set("ticket", streamTicket);
  query.set("task_id", streamTaskID);
  return `${protocol}//${window.location.host}${API_BASE}/stream/ws?${query.toString()}`;
}

function buildConsoleNotificationURL(ticket) {
  const streamTicket = String(ticket || "").trim();
  if (!streamTicket) {
    return "";
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams();
  query.set("ticket", streamTicket);
  return `${protocol}//${window.location.host}${API_BASE}/notifications/ws?${query.toString()}`;
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
  currentLocale,
  TASK_STATUS_META,
  authState,
  authValid,
  endpointState,
  apiFetch,
  fetchConsoleAuthConfig,
  ensureConsoleSession,
  loadEndpoints,
  ensureEndpointsLoaded,
  runtimeApiFetch,
  runtimeApiFetchForEndpoint,
  runtimeApiDownloadForEndpoint,
  createArtifactPreviewTicket,
  renewArtifactPreviewTicket,
  runtimeApiFetchFirstForEndpoints,
  createConsoleStreamTicket,
  buildConsoleStreamURL,
  buildConsoleNotificationURL,
  runtimeEndpointByRef,
  taskEndpointRefsForSelection,
  safeJSON,
  formatTime,
  formatRemainingUntil,
  toInt,
  toBool,
  formatBytes,
};
