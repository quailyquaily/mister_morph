import { createRouter, createWebHistory } from "vue-router";

import {
  BASE_PATH,
  apiFetch,
  authState,
  authValid,
  clearAuth,
  ensureConsoleSession,
  endpointState,
  loadEndpoints,
  saveAuth,
  setSelectedEndpointRef,
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
import "../views/common.css";

const AuditView = () => import("../views/AuditView");
const BootPreviewView = () => import("../views/BootPreviewView");
const ChatView = () => import("../views/ChatView");
const ContactsView = () => import("../views/ContactsView");
const DesktopWindowView = () => import("../views/DesktopWindowView");
const LoginView = () => import("../views/LoginView");
const LogsView = () => import("../views/LogsView");
const MemoryView = () => import("../views/MemoryView");
const OverviewView = () => import("../views/OverviewView");
const RepairView = () => import("../views/RepairView");
const RuntimeView = () => import("../views/RuntimeView");
const SetupView = () => import("../views/SetupView");
const SettingsCreditsView = () => import("../views/SettingsCreditsView");
const SettingsView = () => import("../views/SettingsView");
const StatsView = () => import("../views/StatsView");
const StateFilesView = () => import("../views/StateFilesView");
const TasksView = () => import("../views/TasksView");

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
  { path: "/files", component: StateFilesView },
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
  { id: "__sep_primary", separator: true },
  { id: "/tasks", titleKey: "nav_tasks", icon: "QIconInbox" },
  { id: "/files", titleKey: "nav_files", icon: "QIconFileLock" },
  { id: "/stats", titleKey: "nav_stats", icon: "QIconBarChart" },
  { id: "/audit", titleKey: "nav_audit", icon: "QIconFingerprint" },
  { id: "__sep_secondary", separator: true },
  { id: "/runtime", titleKey: "nav_runtime", icon: "QIconSpeedoMeter" },
  { id: "/settings", titleKey: "nav_settings", icon: "QIconSettings" },
];

router.beforeEach(async (to) => {
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
    saveAuth();
  } catch {
    clearAuth();
    try {
      const ok = await ensureConsoleSession();
      if (ok) {
        const me = await apiFetch("/auth/me");
        authState.account = me.account || "console";
        authState.expiresAt = me.expires_at || authState.expiresAt;
        saveAuth();
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
    await loadEndpoints();
  } catch {
    endpointState.items = [];
  }
  const setupState = await resolveConsoleSetupStage(endpointState.items);
  if (setupState.stage !== "ready") {
    if (SETUP_FREE_PATHS.has(to.path)) {
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
      setSelectedEndpointRef(targetRef);
    }
    return true;
  }
  if (to.path === "/") {
    const endpoint = rootEntryEndpoint(endpointState.items);
    if (endpoint?.endpoint_ref) {
      setSelectedEndpointRef(endpoint.endpoint_ref);
      return { path: "/chat", query: to.query };
    }
    return { path: "/overview", query: to.query };
  }
  return true;
});

export { router, NAV_ITEMS_META };
