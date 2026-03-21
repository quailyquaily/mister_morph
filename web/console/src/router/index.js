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
  consoleSetupTargetEndpointRef,
  resolveConsoleSetupStage,
} from "../core/setup";
import {
  AuditView,
  ChatView,
  ContactsView,
  LoginView,
  MemoryView,
  OverviewView,
  RuntimeView,
  SetupView,
  SettingsView,
  StatsView,
  StateFilesView,
  TasksView,
} from "../views";

function isSetupPath(path) {
  const value = String(path || "").trim();
  return value === "/setup" || value.startsWith("/setup/");
}

function isChatPath(path) {
  const value = String(path || "").trim();
  return value === "/chat" || value.startsWith("/chat/");
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
  { path: "/chat/:topic_id", component: ChatView },
  { path: "/runtime", component: RuntimeView },
  { path: "/dashboard", redirect: "/runtime" },
  { path: "/tasks", component: TasksView },
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
  { id: "/runtime", titleKey: "nav_runtime", icon: "QIconSpeedoMeter" },
  { id: "/settings", titleKey: "nav_settings", icon: "QIconSettings" },
];

router.beforeEach(async (to) => {
  if (to.path === "/login") {
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
    await loadEndpoints();
  } catch {
    endpointState.items = [];
  }
  const setupState = await resolveConsoleSetupStage(endpointState.items);
  if (setupState.stage !== "ready") {
    if (SETUP_FREE_PATHS.has(to.path)) {
      return true;
    }
    return { path: "/setup/llm", query: { redirect: to.fullPath } };
  }
  if (to.path === "/setup") {
    return { path: "/setup/llm", query: to.query };
  }
  if (isSetupPath(to.path)) {
    const targetRef = consoleSetupTargetEndpointRef(setupState.setup);
    if (targetRef && !selectedEndpointCanChat()) {
      setSelectedEndpointRef(targetRef);
    }
    return true;
  }
  const targetRef = consoleSetupTargetEndpointRef(setupState.setup);
  if (isChatPath(to.path) && targetRef && !selectedEndpointCanChat()) {
    setSelectedEndpointRef(targetRef);
  }
  return true;
});

export { router, NAV_ITEMS_META };
