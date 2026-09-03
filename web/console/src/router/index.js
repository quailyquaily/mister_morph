import { createRouter, createWebHistory } from "vue-router";

import {
  BASE_PATH,
  apiFetch,
  authState,
  authValid,
  ensureConsoleSession,
  endpointState,
  ensureEndpointsLoaded,
} from "../core/context";
import {
  blockingSetupIntegrityItems,
  fetchConsoleSetupIntegrity,
  isAllowedRepairSetupRoute,
  resolveConsoleSetupStage,
  setupStagePath,
} from "../core/setup";
import {
  CONSOLE_LOCAL_ENDPOINT_REF,
  isEndpointSelectable,
  rootEntryEndpoint,
} from "../core/endpoints";
import {
  ENDPOINT_ROUTE_PREFIX,
  endpointPagePath,
  endpointRefFromRouteParam,
  endpointRoutePath,
  endpointRouteRef,
} from "../core/endpoint-routes";
import { markRouteInteractive, markRouteStart } from "../core/performance";
import { routeExtensions } from "../core/route-extensions";
import "../views/common.css";

const ROUTE_VIEW_LOADERS = {
  agentDesk: () => import("../views/AgentDeskView"),
  audit: () => import("../views/AuditView"),
  bootPreview: () => import("../views/BootPreviewView"),
  chat: () => import("../views/ChatView"),
  contacts: () => import("../views/ContactsView"),
  desktopWindow: () => import("../views/DesktopWindowView"),
  login: () => import("../views/LoginView"),
  logs: () => import("../views/LogsView"),
  overview: () => import("../views/OverviewView"),
  repair: () => import("../views/RepairView"),
  setup: () => import("../views/SetupView"),
  settings: () => import("../views/SettingsView"),
  stats: () => import("../views/StatsView"),
  todo: () => import("../views/TodoView"),
};
const routePreloadPromises = new Map();

const AgentDeskView = ROUTE_VIEW_LOADERS.agentDesk;
const AuditView = ROUTE_VIEW_LOADERS.audit;
const BootPreviewView = ROUTE_VIEW_LOADERS.bootPreview;
const ChatView = ROUTE_VIEW_LOADERS.chat;
const ContactsView = ROUTE_VIEW_LOADERS.contacts;
const DesktopWindowView = ROUTE_VIEW_LOADERS.desktopWindow;
const LoginView = ROUTE_VIEW_LOADERS.login;
const LogsView = ROUTE_VIEW_LOADERS.logs;
const OverviewView = ROUTE_VIEW_LOADERS.overview;
const RepairView = ROUTE_VIEW_LOADERS.repair;
const SetupView = ROUTE_VIEW_LOADERS.setup;
const SettingsView = ROUTE_VIEW_LOADERS.settings;
const StatsView = ROUTE_VIEW_LOADERS.stats;
const TodoView = ROUTE_VIEW_LOADERS.todo;

const RootRedirectView = {
  template: `<div aria-hidden="true"></div>`,
};

const ENDPOINT_SCOPE_PATH = `${ENDPOINT_ROUTE_PREFIX}/:endpoint_ref`;

function pagePath(path) {
  return endpointPagePath(path) || String(path || "").trim();
}

function isDesktopWindowPath(path) {
  const value = String(path || "").trim();
  return value === "/window" || value.startsWith("/window/");
}

function preloadKeyForPath(path) {
  const value = pagePath(path);
  if (value === "/chat/desk") {
    return "agentDesk";
  }
  if (value === "/chat" || value.startsWith("/chat/")) {
    return "chat";
  }
  if (value === "/settings" || value.startsWith("/settings/")) {
    return "settings";
  }
  switch (value) {
    case "/audit":
      return "audit";
    case "/contacts":
      return "contacts";
    case "/logs":
      return "logs";
    case "/overview":
      return "overview";
    case "/runtime":
      return "settings";
    case "/stats":
      return "stats";
    case "/todo":
      return "todo";
    default:
      return "";
  }
}

function preloadRouteComponent(path) {
  const key = preloadKeyForPath(path);
  if (!key) {
    return null;
  }
  const cached = routePreloadPromises.get(key);
  if (cached) {
    return cached;
  }
  const loader = ROUTE_VIEW_LOADERS[key];
  if (typeof loader !== "function") {
    return null;
  }
  const promise = loader().catch((error) => {
    routePreloadPromises.delete(key);
    throw error;
  });
  routePreloadPromises.set(key, promise);
  return promise;
}

const extensionRoutes = Array.isArray(routeExtensions.routes) ? routeExtensions.routes : [];
const extensionSetupFreePaths = Array.isArray(routeExtensions.setupFreePaths)
  ? routeExtensions.setupFreePaths
  : [];

const SETUP_FREE_PATHS = new Set([
  "/setup",
  "/setup/llm",
  "/setup/persona",
  "/setup/soul",
  "/setup/done",
	"/troubleshooting",
  "/settings",
  "/settings/agent",
  "/settings/tools",
  "/settings/skills",
  "/settings/persona",
  "/settings/channels",
  "/settings/runtimes",
  "/settings/guard",
  "/settings/console",
  "/settings/runtime",
  "/settings/credits",
  ...extensionSetupFreePaths,
]);

function legacyEndpointRedirect(pattern) {
  return (to) => {
    const targetPagePath = pattern.replace(/:([A-Za-z_][A-Za-z0-9_]*)/g, (_match, name) =>
      encodeURIComponent(String(to.params?.[name] || "")),
    );
    return {
      path: endpointRoutePath(CONSOLE_LOCAL_ENDPOINT_REF, targetPagePath),
      query: to.query,
      hash: to.hash,
    };
  };
}

const routes = [
  { path: "/login", component: LoginView, meta: { public: true, shellless: true } },
  { path: "/__boot-preview", component: BootPreviewView, meta: { public: true, shellless: true } },
  { path: "/overview", component: OverviewView },
  { path: "/chat/desk", component: AgentDeskView },
  { path: `${ENDPOINT_SCOPE_PATH}/setup`, component: SetupView, meta: { endpointScoped: true } },
  {
    path: `${ENDPOINT_SCOPE_PATH}/setup/llm`,
    component: SetupView,
    meta: { endpointScoped: true, setupStage: "llm" },
  },
  {
    path: `${ENDPOINT_SCOPE_PATH}/setup/persona`,
    component: SetupView,
    meta: { endpointScoped: true, setupStage: "persona" },
  },
  {
    path: `${ENDPOINT_SCOPE_PATH}/setup/soul`,
    component: SetupView,
    meta: { endpointScoped: true, setupStage: "soul" },
  },
  {
    path: `${ENDPOINT_SCOPE_PATH}/setup/done`,
    component: SetupView,
    meta: { endpointScoped: true, setupStage: "done" },
  },
  {
	path: `${ENDPOINT_SCOPE_PATH}/troubleshooting`,
	component: RepairView,
	meta: { endpointScoped: true, shellless: true },
  },
  { path: `${ENDPOINT_SCOPE_PATH}/chat`, component: ChatView, meta: { endpointScoped: true } },
  {
    path: `${ENDPOINT_SCOPE_PATH}/chat/:topic_id`,
    component: ChatView,
    meta: { endpointScoped: true, mobileBottomNav: false },
  },
  { path: `${ENDPOINT_SCOPE_PATH}/stats`, component: StatsView, meta: { endpointScoped: true } },
  { path: `${ENDPOINT_SCOPE_PATH}/audit`, component: AuditView, meta: { endpointScoped: true } },
  { path: `${ENDPOINT_SCOPE_PATH}/logs`, component: LogsView, meta: { endpointScoped: true } },
  { path: `${ENDPOINT_SCOPE_PATH}/todo`, component: TodoView, meta: { endpointScoped: true } },
  {
    path: `${ENDPOINT_SCOPE_PATH}/contacts`,
    component: ContactsView,
    meta: { endpointScoped: true },
  },
  {
    path: `${ENDPOINT_SCOPE_PATH}/settings/:section`,
    component: SettingsView,
    meta: { endpointScoped: true },
  },
  {
    path: `${ENDPOINT_SCOPE_PATH}/settings`,
    component: SettingsView,
    meta: { endpointScoped: true },
  },
  { path: "/setup", redirect: legacyEndpointRedirect("/setup") },
  { path: "/setup/llm", redirect: legacyEndpointRedirect("/setup/llm") },
  { path: "/setup/persona", redirect: legacyEndpointRedirect("/setup/persona") },
  { path: "/setup/soul", redirect: legacyEndpointRedirect("/setup/soul") },
  { path: "/setup/done", redirect: legacyEndpointRedirect("/setup/done") },
	{ path: "/troubleshooting", redirect: legacyEndpointRedirect("/troubleshooting") },
  { path: "/chat", redirect: legacyEndpointRedirect("/chat") },
  { path: "/chat/:topic_id", redirect: legacyEndpointRedirect("/chat/:topic_id") },
  { path: "/runtime", redirect: legacyEndpointRedirect("/settings/runtime") },
  { path: "/dashboard", redirect: legacyEndpointRedirect("/settings/runtime") },
  { path: "/stats", redirect: legacyEndpointRedirect("/stats") },
  { path: "/audit", redirect: legacyEndpointRedirect("/audit") },
  { path: "/logs", redirect: legacyEndpointRedirect("/logs") },
  { path: "/todo", redirect: legacyEndpointRedirect("/todo") },
  { path: "/files", redirect: legacyEndpointRedirect("/todo") },
  { path: "/contacts", redirect: legacyEndpointRedirect("/contacts") },
  { path: "/settings/:section", redirect: legacyEndpointRedirect("/settings/:section") },
  { path: "/settings", redirect: legacyEndpointRedirect("/settings") },
  { path: "/window/:window_id?", component: DesktopWindowView, meta: { shellless: true } },
  ...extensionRoutes,
  { path: "/", component: RootRedirectView, meta: { shellless: true } },
];

const router = createRouter({
  history: createWebHistory(BASE_PATH || "/"),
  routes,
});

const NAV_ITEMS_META = [
  { id: "/chat", titleKey: "nav_chat", icon: "PhChats" },
  { id: "/contacts", titleKey: "nav_contacts", icon: "PhUsers" },
  { id: "/todo", titleKey: "nav_todo", icon: "PhTray" },
  { id: "__sep_primary", separator: true },
  { id: "/stats", titleKey: "nav_stats", icon: "PhChartBar" },
  { id: "/audit", titleKey: "nav_audit", icon: "PhFingerprint" },
  { id: "__sep_secondary", separator: true },
  { id: "/settings", titleKey: "nav_settings", icon: "PhGearSix" },
];

router.beforeEach(async (to) => {
  markRouteStart(to);
  if (to.meta && to.meta.public === true) {
    return true;
  }
  const toPagePath = pagePath(to.path);
  const requestedEndpointRef = to.meta?.endpointScoped
    ? endpointRefFromRouteParam(to.params.endpoint_ref)
    : "";
  if (to.meta?.endpointScoped) {
    if (!requestedEndpointRef) {
      return { path: "/overview" };
    }
    const canonicalRouteRef = endpointRouteRef(requestedEndpointRef);
    const currentRouteRef = String(to.params.endpoint_ref || "").trim();
    if (currentRouteRef !== canonicalRouteRef) {
      return {
        path: endpointRoutePath(requestedEndpointRef, toPagePath),
        query: to.query,
        hash: to.hash,
      };
    }
  }
  if (!authValid.value) {
    try {
      const ok = await ensureConsoleSession();
      if (!ok) {
        return { path: "/login", query: { redirect: to.fullPath } };
      }
    } catch {
      return { path: "/login", query: { redirect: to.fullPath } };
    }
  }
  try {
    const me = await apiFetch("/auth/me");
    authState.account = me.account || "console";
    authState.expiresAt = me.expires_at || authState.expiresAt;
    authState.save();
  } catch {
    authState.clear();
    try {
      const ok = await ensureConsoleSession();
      if (ok) {
        const me = await apiFetch("/auth/me");
        authState.account = me.account || "console";
        authState.expiresAt = me.expires_at || authState.expiresAt;
        authState.save();
      } else {
        return { path: "/login", query: { redirect: to.fullPath } };
      }
    } catch {
      return { path: "/login", query: { redirect: to.fullPath } };
    }
  }
  try {
    const integrityItems = blockingSetupIntegrityItems(await fetchConsoleSetupIntegrity().catch(() => []));
    if (integrityItems.length > 0) {
	  if (
		toPagePath === "/troubleshooting" ||
		isAllowedRepairSetupRoute(to, integrityItems)
	  ) {
        return true;
      }
      return { path: setupStagePath("repair"), query: { redirect: to.fullPath } };
    }
    await ensureEndpointsLoaded();
  } catch {
    endpointState.items = [];
  }
  if (requestedEndpointRef) {
    const endpoint = endpointState.items.find(
      (item) => item?.endpoint_ref === requestedEndpointRef,
    );
    if (!isEndpointSelectable(endpoint)) {
      return { path: "/overview" };
    }
    endpointState.setSelectedEndpointRef(requestedEndpointRef);
  }
  const setupState = await resolveConsoleSetupStage(endpointState.items);
  if (setupState.stage !== "ready") {
    if (SETUP_FREE_PATHS.has(toPagePath) || isDesktopWindowPath(to.path)) {
      return true;
    }
    return { path: setupStagePath(setupState.stage), query: { redirect: to.fullPath } };
  }
  if (to.path === "/") {
    const endpoint = rootEntryEndpoint(endpointState.items);
    if (endpoint?.endpoint_ref) {
      return { path: endpointRoutePath(endpoint.endpoint_ref, "/chat"), query: to.query };
    }
    return { path: "/overview", query: to.query };
  }
  return true;
});

router.afterEach((to) => {
  markRouteInteractive(to);
});

export { router, NAV_ITEMS_META, preloadRouteComponent };
