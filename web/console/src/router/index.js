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
  consoleSetupTargetEndpointRef,
  fetchConsoleSetupIntegrity,
  isAllowedRepairSetupRoute,
  resolveConsoleSetupStage,
  setupStagePath,
} from "../core/setup";
import { visibleEndpoints } from "../core/endpoints";
import { markRouteInteractive, markRouteStart } from "../core/performance";
import "../views/common.css";

const ROUTE_VIEW_LOADERS = {
  audit: () => import("../views/AuditView"),
  bootPreview: () => import("../views/BootPreviewView"),
  chat: () => import("../views/ChatView"),
  contacts: () => import("../views/ContactsView"),
  desktopWindow: () => import("../views/DesktopWindowView"),
  login: () => import("../views/LoginView"),
  logs: () => import("../views/LogsView"),
  memory: () => import("../views/MemoryView"),
  overview: () => import("../views/OverviewView"),
  repair: () => import("../views/RepairView"),
  runtime: () => import("../views/RuntimeView"),
  setup: () => import("../views/SetupView"),
  settingsCredits: () => import("../views/SettingsCreditsView"),
  settings: () => import("../views/SettingsView"),
  stats: () => import("../views/StatsView"),
  tasks: () => import("../views/TasksView"),
  todo: () => import("../views/TodoView"),
};
const routePreloadPromises = new Map();

const AuditView = ROUTE_VIEW_LOADERS.audit;
const BootPreviewView = ROUTE_VIEW_LOADERS.bootPreview;
const ChatView = ROUTE_VIEW_LOADERS.chat;
const ContactsView = ROUTE_VIEW_LOADERS.contacts;
const DesktopWindowView = ROUTE_VIEW_LOADERS.desktopWindow;
const LoginView = ROUTE_VIEW_LOADERS.login;
const LogsView = ROUTE_VIEW_LOADERS.logs;
const MemoryView = ROUTE_VIEW_LOADERS.memory;
const OverviewView = ROUTE_VIEW_LOADERS.overview;
const RepairView = ROUTE_VIEW_LOADERS.repair;
const RuntimeView = ROUTE_VIEW_LOADERS.runtime;
const SetupView = ROUTE_VIEW_LOADERS.setup;
const SettingsCreditsView = ROUTE_VIEW_LOADERS.settingsCredits;
const SettingsView = ROUTE_VIEW_LOADERS.settings;
const StatsView = ROUTE_VIEW_LOADERS.stats;
const TasksView = ROUTE_VIEW_LOADERS.tasks;
const TodoView = ROUTE_VIEW_LOADERS.todo;

const RootRedirectView = {
  template: `<div aria-hidden="true"></div>`,
};

function isSetupPath(path) {
  const value = String(path || "").trim();
  return value === "/setup" || value.startsWith("/setup/");
}

function isChatPath(path) {
  const value = String(path || "").trim();
  return value === "/chat" || value.startsWith("/chat/");
}

function isDesktopWindowPath(path) {
  const value = String(path || "").trim();
  return value === "/window" || value.startsWith("/window/");
}

function preloadKeyForPath(path) {
  const value = String(path || "").trim();
  if (value === "/chat" || value.startsWith("/chat/")) {
    return "chat";
  }
  if (value === "/settings/credits") {
    return "settingsCredits";
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
    case "/memory":
      return "memory";
    case "/overview":
      return "overview";
    case "/runtime":
      return "runtime";
    case "/stats":
      return "stats";
    case "/tasks":
      return "tasks";
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

const SETUP_FREE_PATHS = new Set([
  "/setup",
  "/setup/llm",
  "/setup/persona",
  "/setup/soul",
  "/setup/done",
  "/setup/repair",
  "/settings",
  "/settings/agent",
  "/settings/tools",
  "/settings/skills",
  "/settings/persona",
  "/settings/channels",
  "/settings/runtimes",
  "/settings/guard",
  "/settings/console",
  "/settings/credits",
]);

function selectedEndpointCanChat() {
  const selectedRef = typeof endpointState.selectedRef === "string" ? endpointState.selectedRef.trim() : "";
  if (!selectedRef) {
    return false;
  }
  return endpointState.items.some(
    (item) => item && item.endpoint_ref === selectedRef && item.connected === true && item.can_submit === true
  );
}

function rootEntryEndpoint(items) {
  const endpoints = visibleEndpoints(items);
  if (endpoints.length !== 1) {
    return null;
  }
  const [endpoint] = endpoints;
  return endpoint && endpoint.connected === true ? endpoint : null;
}

const routes = [
  { path: "/login", component: LoginView, meta: { public: true, shellless: true } },
  { path: "/__boot-preview", component: BootPreviewView, meta: { public: true, shellless: true } },
  { path: "/setup", component: SetupView },
  { path: "/setup/llm", component: SetupView, meta: { setupStage: "llm" } },
  { path: "/setup/persona", component: SetupView, meta: { setupStage: "persona" } },
  { path: "/setup/soul", component: SetupView, meta: { setupStage: "soul" } },
  { path: "/setup/done", component: SetupView, meta: { setupStage: "done" } },
  { path: "/setup/repair", component: RepairView },
  { path: "/overview", component: OverviewView },
  { path: "/chat", component: ChatView },
  { path: "/chat/:topic_id", component: ChatView },
  { path: "/runtime", component: RuntimeView },
  { path: "/dashboard", redirect: "/runtime" },
  { path: "/tasks", component: TasksView },
  { path: "/stats", component: StatsView },
  { path: "/audit", component: AuditView },
  { path: "/logs", component: LogsView },
  { path: "/memory", component: MemoryView },
  { path: "/todo", component: TodoView },
  { path: "/files", redirect: "/todo" },
  { path: "/contacts", component: ContactsView },
  { path: "/settings/credits", component: SettingsCreditsView },
  { path: "/settings/:section", component: SettingsView },
  { path: "/settings", component: SettingsView },
  { path: "/window/:window_id?", component: DesktopWindowView, meta: { shellless: true } },
  { path: "/", component: RootRedirectView, meta: { shellless: true } },
];

const router = createRouter({
  history: createWebHistory(BASE_PATH || "/"),
  routes,
});

const NAV_ITEMS_META = [
  { id: "/chat", titleKey: "nav_chat", icon: "QIconMessageChatSquare" },
  { id: "/contacts", titleKey: "nav_contacts", icon: "QIconUsers" },
  { id: "/memory", titleKey: "nav_memory", icon: "QIconEcosystem" },
  { id: "/todo", titleKey: "nav_todo", icon: "QIconInbox" },
  { id: "__sep_primary", separator: true },
  { id: "/tasks", titleKey: "nav_tasks", icon: "QIconInbox" },
  { id: "/stats", titleKey: "nav_stats", icon: "QIconBarChart" },
  { id: "/audit", titleKey: "nav_audit", icon: "QIconFingerprint" },
  { id: "__sep_secondary", separator: true },
  { id: "/runtime", titleKey: "nav_runtime", icon: "QIconSpeedoMeter" },
  { id: "/settings", titleKey: "nav_settings", icon: "QIconSettings" },
];

router.beforeEach(async (to) => {
  markRouteStart(to);
  if (to.meta && to.meta.public === true) {
    return true;
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
      if (to.path === "/setup/repair" || isAllowedRepairSetupRoute(to, integrityItems)) {
        return true;
      }
      return { path: "/setup/repair", query: { redirect: to.fullPath } };
    }
    if (to.path === "/setup/repair") {
      return { path: "/setup", query: to.query };
    }
    await ensureEndpointsLoaded();
  } catch {
    endpointState.items = [];
  }
  const setupState = await resolveConsoleSetupStage(endpointState.items);
  if (setupState.stage !== "ready") {
    if (SETUP_FREE_PATHS.has(to.path) || isDesktopWindowPath(to.path)) {
      return true;
    }
    return { path: setupStagePath(setupState.stage), query: { redirect: to.fullPath } };
  }
  if (to.path === "/setup") {
    return { path: "/setup/done", query: to.query };
  }
  if (isSetupPath(to.path)) {
    const targetRef = consoleSetupTargetEndpointRef(setupState.setup);
    if (targetRef && !selectedEndpointCanChat()) {
      endpointState.setSelectedEndpointRef(targetRef);
    }
    return true;
  }
  if (to.path === "/") {
    const endpoint = rootEntryEndpoint(endpointState.items);
    if (endpoint?.endpoint_ref) {
      endpointState.setSelectedEndpointRef(endpoint.endpoint_ref);
      return { path: "/chat", query: to.query };
    }
    return { path: "/overview", query: to.query };
  }
  return true;
});

router.afterEach((to) => {
  markRouteInteractive(to);
});

export { router, NAV_ITEMS_META, preloadRouteComponent };
