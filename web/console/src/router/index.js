import { createRouter, createWebHistory } from "vue-router";

import {
  BASE_PATH,
  apiFetch,
  authState,
  authValid,
  clearAuth,
  endpointState,
  loadEndpoints,
  saveAuth,
  setSelectedEndpointRef,
} from "../core/context";
import {
  consoleSetupTargetEndpointRef,
  resolveConsoleSetupStage,
  setupStagePath,
} from "../core/setup";
import {
  AuditView,
  ChatView,
  ContactsView,
  DashboardView,
  LoginView,
  MemoryView,
  OverviewView,
  SetupView,
  SettingsView,
  StatsView,
  StateFilesView,
  TasksView,
  TaskDetailView,
} from "../views";

function isSetupPath(path) {
  const value = String(path || "").trim();
  return value === "/setup" || value.startsWith("/setup/");
}

const SETUP_FREE_PATHS = new Set(["/setup", "/setup/llm", "/setup/persona", "/setup/soul", "/setup/done", "/settings"]);

function selectedEndpointCanChat() {
  const selectedRef = typeof endpointState.selectedRef === "string" ? endpointState.selectedRef.trim() : "";
  if (!selectedRef) {
    return false;
  }
  return endpointState.items.some(
    (item) => item && item.endpoint_ref === selectedRef && item.connected === true && item.can_submit === true
  );
}

const routes = [
  { path: "/login", component: LoginView },
  { path: "/setup", component: SetupView },
  { path: "/setup/llm", component: SetupView, meta: { setupStage: "llm" } },
  { path: "/setup/persona", component: SetupView, meta: { setupStage: "persona" } },
  { path: "/setup/soul", component: SetupView, meta: { setupStage: "soul" } },
  { path: "/setup/done", component: SetupView, meta: { setupStage: "done" } },
  { path: "/overview", component: OverviewView },
  { path: "/chat", component: ChatView },
  { path: "/dashboard", component: DashboardView },
  { path: "/tasks", component: TasksView },
  { path: "/tasks/:id", component: TaskDetailView },
  { path: "/stats", component: StatsView },
  { path: "/audit", component: AuditView },
  { path: "/memory", component: MemoryView },
  { path: "/files", component: StateFilesView },
  { path: "/contacts", component: ContactsView },
  { path: "/settings", component: SettingsView },
  { path: "/", redirect: "/overview" },
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
  { id: "/dashboard", titleKey: "nav_runtime", icon: "QIconSpeedoMeter" },
  { id: "/settings", titleKey: "nav_settings", icon: "QIconSettings" },
];

router.beforeEach(async (to) => {
  if (to.path === "/login") {
    return true;
  }
  if (!authValid.value) {
    return { path: "/login", query: { redirect: to.fullPath } };
  }
  try {
    const me = await apiFetch("/auth/me");
    authState.account = me.account || "console";
    authState.expiresAt = me.expires_at || authState.expiresAt;
    saveAuth();
  } catch {
    clearAuth();
    return { path: "/login", query: { redirect: to.fullPath } };
  }
  try {
    await loadEndpoints();
  } catch {
    endpointState.items = [];
  }
  const setupState = await resolveConsoleSetupStage(endpointState.items);
  if (setupState.stage !== "ready") {
    const setupPath = setupStagePath(setupState.stage);
    if (SETUP_FREE_PATHS.has(to.path)) {
      if (isSetupPath(to.path) && to.path !== setupPath) {
        return { path: setupPath, query: to.query };
      }
      return true;
    }
    return { path: setupPath, query: { redirect: to.fullPath } };
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
  const targetRef = consoleSetupTargetEndpointRef(setupState.setup);
  if (to.path === "/chat" && targetRef && !selectedEndpointCanChat()) {
    setSelectedEndpointRef(targetRef);
  }
  return true;
});

export { router, NAV_ITEMS_META };
