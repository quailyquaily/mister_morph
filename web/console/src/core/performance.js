const PERF_GLOBAL_NAME = "__MISTERMORPH_PERF__";
const PERF_DEBUG_STORAGE_KEY = "mistermorph_console_perf_debug";
const MAX_RECENT_ITEMS = 80;

const state = {
  installed: false,
  routeSeq: 0,
  currentRoute: null,
  routes: [],
  requests: [],
  inputs: [],
  longTasks: [],
  longAnimationFrames: [],
  componentUpdates: [],
  markdown: {
    mounts: 0,
    updates: 0,
    total_update_ms: 0,
    max_update_ms: 0,
  },
  snapshots: {},
  components: {},
};

function devEnabled() {
  return import.meta.env.DEV === true && typeof window !== "undefined";
}

function debugEnabled() {
  if (!devEnabled()) {
    return false;
  }
  try {
    const query = new URLSearchParams(window.location.search || "");
    return query.get("perf") === "1" || window.localStorage?.getItem(PERF_DEBUG_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function exposeState() {
  if (!devEnabled()) {
    return;
  }
  window[PERF_GLOBAL_NAME] = state;
}

function nowMs() {
  if (typeof performance !== "undefined" && typeof performance.now === "function") {
    return performance.now();
  }
  return Date.now();
}

function trimList(list) {
  if (!Array.isArray(list) || list.length <= MAX_RECENT_ITEMS) {
    return;
  }
  list.splice(0, list.length - MAX_RECENT_ITEMS);
}

function activeRoute() {
  return state.currentRoute || null;
}

function logDebug(label, payload) {
  if (!debugEnabled()) {
    return;
  }
  console.debug(`[console-perf] ${label}`, payload);
}

function routePath(routeLike) {
  return String(routeLike?.fullPath || routeLike?.path || "").trim() || "/";
}

function markRouteStart(routeLike) {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  state.routeSeq += 1;
  const route = {
    id: state.routeSeq,
    path: routePath(routeLike),
    startedAt: nowMs(),
    route_interactive_ms: 0,
    api_request_count: 0,
    api_request_ms: 0,
    api_request_count_by_source: {},
    long_task_count: 0,
    long_task_total_ms: 0,
    long_animation_frame_count: 0,
    long_animation_frame_total_ms: 0,
    markdown_update_count: 0,
    snapshot_build_count: 0,
    component_update_count: 0,
    component_update_total_ms: 0,
    component_update_max_ms: 0,
  };
  state.currentRoute = route;
  state.routes.push(route);
  trimList(state.routes);
}

function markRouteInteractive(routeLike) {
  if (!devEnabled()) {
    return;
  }
  const route = activeRoute();
  if (!route || route.path !== routePath(routeLike)) {
    return;
  }
  window.requestAnimationFrame(() => {
    if (activeRoute() !== route) {
      return;
    }
    route.route_interactive_ms = Math.max(0, nowMs() - route.startedAt);
    logDebug("route", route);
  });
}

function describeAPIPath(pathname) {
  const raw = String(pathname || "");
  try {
    const url = new URL(raw, window.location.origin);
    return {
      pathname: url.pathname,
      endpoint: url.searchParams.get("endpoint") || "",
      uri: url.searchParams.get("uri") || "",
    };
  } catch {
    return {
      pathname: raw,
      endpoint: "",
      uri: "",
    };
  }
}

function requestSource(value) {
  const source = String(value || "").trim();
  return source || "page";
}

function recordApiRequest(input) {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  const route = activeRoute();
  const request = {
    routeID: route?.id || 0,
    routePath: route?.path || "",
    method: String(input?.method || "GET").toUpperCase(),
    status: Number(input?.status || 0),
    duration_ms: Math.max(0, Number(input?.durationMs) || 0),
    ok: input?.ok === true,
    error: String(input?.error || ""),
    source: requestSource(input?.source),
    ...describeAPIPath(input?.pathname),
  };
  state.requests.push(request);
  trimList(state.requests);
  if (route) {
    route.api_request_count += 1;
    route.api_request_ms += request.duration_ms;
    route.api_request_count_by_source ||= {};
    route.api_request_count_by_source[request.source] =
      (route.api_request_count_by_source[request.source] || 0) + 1;
  }
  logDebug("api", request);
}

function installLongTaskObserver() {
  if (typeof PerformanceObserver === "undefined") {
    return;
  }
  try {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        const route = activeRoute();
        const item = {
          routeID: route?.id || 0,
          routePath: route?.path || "",
          duration_ms: Math.max(0, Number(entry.duration) || 0),
          startedAt: Math.max(0, Number(entry.startTime) || 0),
        };
        state.longTasks.push(item);
        trimList(state.longTasks);
        if (route) {
          route.long_task_count += 1;
          route.long_task_total_ms += item.duration_ms;
        }
        logDebug("longtask", item);
      }
    });
    observer.observe({ type: "longtask", buffered: true });
  } catch {
    // Some browsers do not expose long task entries.
  }
}

function installLongAnimationFrameObserver() {
  if (typeof PerformanceObserver === "undefined") {
    return;
  }
  try {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        const route = activeRoute();
        const item = {
          routeID: route?.id || 0,
          routePath: route?.path || "",
          duration_ms: Math.max(0, Number(entry.duration) || 0),
          startedAt: Math.max(0, Number(entry.startTime) || 0),
        };
        state.longAnimationFrames.push(item);
        trimList(state.longAnimationFrames);
        if (route) {
          route.long_animation_frame_count += 1;
          route.long_animation_frame_total_ms += item.duration_ms;
        }
        logDebug("long-animation-frame", item);
      }
    });
    observer.observe({ type: "long-animation-frame", buffered: true });
  } catch {
    // The entry type is still not available in every Chromium version.
  }
}

function installInputObserver() {
  document.addEventListener(
    "input",
    (event) => {
      const route = activeRoute();
      const startedAt = nowMs();
      const target = event.target;
      const tagName =
        target instanceof Element ? String(target.tagName || "").toLowerCase() : "";
      window.requestAnimationFrame(() => {
        const item = {
          routeID: route?.id || 0,
          routePath: route?.path || "",
          target: tagName,
          input_to_paint_ms: Math.max(0, nowMs() - startedAt),
        };
        state.inputs.push(item);
        trimList(state.inputs);
        logDebug("input", item);
      });
    },
    { capture: true, passive: true }
  );
}

function installConsolePerformanceObservers() {
  if (!devEnabled() || state.installed) {
    return;
  }
  state.installed = true;
  exposeState();
  installLongTaskObserver();
  installLongAnimationFrameObserver();
  installInputObserver();
}

function recordMarkdownMount() {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  state.markdown.mounts += 1;
}

function recordMarkdownUpdate(durationMs) {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  const duration = Math.max(0, Number(durationMs) || 0);
  const route = activeRoute();
  state.markdown.updates += 1;
  state.markdown.total_update_ms += duration;
  state.markdown.max_update_ms = Math.max(state.markdown.max_update_ms, duration);
  if (route) {
    route.markdown_update_count += 1;
  }
  logDebug("markdown-update", { duration_ms: duration, routePath: route?.path || "" });
}

function recordSnapshotBuild(name) {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  const key = String(name || "unknown").trim() || "unknown";
  state.snapshots[key] = (state.snapshots[key] || 0) + 1;
  const route = activeRoute();
  if (route) {
    route.snapshot_build_count += 1;
  }
}

function componentStats(name) {
  const key = String(name || "unknown").trim() || "unknown";
  const current = state.components[key];
  if (current && typeof current === "object") {
    return current;
  }
  const next = {
    update_count: Number(current || 0),
    total_update_ms: 0,
    max_update_ms: 0,
    last_update_ms: 0,
  };
  state.components[key] = next;
  return next;
}

function recordComponentUpdate(name, durationMs = 0) {
  if (!devEnabled()) {
    return;
  }
  exposeState();
  const key = String(name || "unknown").trim() || "unknown";
  const duration = Math.max(0, Number(durationMs) || 0);
  const stats = componentStats(key);
  stats.update_count += 1;
  stats.total_update_ms += duration;
  stats.max_update_ms = Math.max(stats.max_update_ms, duration);
  stats.last_update_ms = duration;
  const route = activeRoute();
  const item = {
    routeID: route?.id || 0,
    routePath: route?.path || "",
    name: key,
    duration_ms: duration,
  };
  state.componentUpdates.push(item);
  trimList(state.componentUpdates);
  if (route) {
    route.component_update_count += 1;
    route.component_update_total_ms += duration;
    route.component_update_max_ms = Math.max(route.component_update_max_ms, duration);
  }
  logDebug("component-update", item);
}

export {
  installConsolePerformanceObservers,
  markRouteInteractive,
  markRouteStart,
  recordApiRequest,
  recordComponentUpdate,
  recordMarkdownMount,
  recordMarkdownUpdate,
  recordSnapshotBuild,
};
