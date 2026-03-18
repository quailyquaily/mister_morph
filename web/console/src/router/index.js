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
} from "../core/context";
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

const routes = [
  { path: "/login", component: LoginView },
  { path: "/setup", component: SetupView },
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

async function isSetupRequiredMode() {
  try {
    const status = await apiFetch("/setup/status", { noAuth: true });
    return status && status.mode === "setup_required";
  } catch (e) {
    if (e && e.status === 404) {
      return false;
    }
    return false;
  }
}

router.beforeEach(async (to) => {
  const setupRequired = await isSetupRequiredMode();
  if (setupRequired) {
    if (to.path !== "/setup") {
      return { path: "/setup" };
    }
    return true;
  }

  if (to.path === "/setup") {
    return authValid.value ? { path: "/overview" } : { path: "/login" };
  }

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
  return true;
});

export { router, NAV_ITEMS_META };
