import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./ChatView.css";

import AppKicker from "../components/AppKicker";
import AppPage from "../components/AppPage";
import MarkdownContent from "../components/MarkdownContent";
import RawJsonDialog from "../components/RawJsonDialog";
import { endpointChannelLabel } from "../core/endpoints";
import { workspaceTreeIcon } from "../core/workspace-icons";
import {
  buildConsoleStreamURL,
  createConsoleStreamTicket,
  currentLocale,
  endpointState,
  formatBytes,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  safeJSON,
  translate,
} from "../core/context";

const POLL_INTERVAL_MS = 1200;
const COMPOSER_MAX_ROWS = 5;
const CHAT_HISTORY_LIMIT = 100;
const HEARTBEAT_TOPIC_ID = "_heartbeat";
const RECENT_WORKSPACE_DIRS_STORAGE_KEY = "mistermorph_console_recent_workspaces_v1";
const WORKSPACE_SIDEBAR_OPEN_STORAGE_KEY = "mistermorph_console_workspace_sidebar_open_v1";
const RECENT_WORKSPACE_DIRS_LIMIT = 32;
const WORKSPACE_BROWSER_SOURCE_RECENT = "recent";
const WORKSPACE_BROWSER_SOURCE_HOME = "home";
const WORKSPACE_BROWSER_SOURCE_SYSTEM = "system";
const POLLING_ACTION_KEYS = [
  "chat_polling_action_ponder",
  "chat_polling_action_think",
  "chat_polling_action_research",
  "chat_polling_action_weigh",
  "chat_polling_action_reflect",
  "chat_polling_action_tinker",
];

function normalizeTaskStatus(raw) {
  const value = String(raw || "").trim().toLowerCase();
  switch (value) {
    case "queued":
    case "running":
    case "pending":
    case "done":
    case "failed":
    case "canceled":
      return value;
    default:
      return "queued";
  }
}

function normalizeEndpointMode(raw) {
  return String(raw || "").trim().toLowerCase();
}

function normalizeTopicID(raw) {
  return String(raw || "").trim();
}

function isWorkspaceCommandText(raw) {
  return String(raw || "").trim().toLowerCase().startsWith("/workspace");
}

function isTerminalStatus(status) {
  return status === "done" || status === "failed" || status === "canceled";
}

function hasOwnTreePath(map, path) {
  return Boolean(map) && Object.prototype.hasOwnProperty.call(map, path);
}

function normalizeTreeItems(raw) {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .map((item) => ({
      name: String(item?.name || "").trim(),
      path: String(item?.path || "").trim(),
      is_dir: item?.is_dir === true,
      has_children: item?.has_children === true,
      size_bytes: Number.isFinite(Number(item?.size_bytes)) ? Math.trunc(Number(item.size_bytes)) : -1,
    }))
    .filter((item) => item.name && item.path);
}

function buildTreeRows(itemsByPath, expandedByPath, parentPath = "", depth = 0) {
  const items = Array.isArray(itemsByPath?.[parentPath]) ? itemsByPath[parentPath] : [];
  const rows = [];
  for (const entry of items) {
    const entryPath = String(entry?.path || "").trim();
    const hasLoadedChildren = hasOwnTreePath(itemsByPath, entryPath);
    const hasVisibleChildren = hasLoadedChildren && Array.isArray(itemsByPath?.[entryPath]) && itemsByPath[entryPath].length > 0;
    const expandable = Boolean(entry?.is_dir) && (entry?.has_children || hasVisibleChildren);
    const expanded = expandable && expandedByPath?.[entryPath] === true;
    rows.push({
      key: `${parentPath}:${entryPath}`,
      depth,
      entry,
      expandable,
      expanded,
    });
    if (expandable && expanded && hasLoadedChildren) {
      rows.push(...buildTreeRows(itemsByPath, expandedByPath, entryPath, depth + 1));
    }
  }
  return rows;
}

const WORKSPACE_TAB_ID = "workspace";

function normalizeRecentWorkspaceDirs(raw) {
  if (!Array.isArray(raw)) {
    return [];
  }
  const seen = new Set();
  const items = [];
  for (const item of raw) {
    const path = String(item || "").trim();
    if (!path || seen.has(path)) {
      continue;
    }
    seen.add(path);
    items.push(path);
    if (items.length >= RECENT_WORKSPACE_DIRS_LIMIT) {
      break;
    }
  }
  return items;
}

function loadRecentWorkspaceDirs() {
  if (typeof localStorage === "undefined") {
    return [];
  }
  try {
    const raw = localStorage.getItem(RECENT_WORKSPACE_DIRS_STORAGE_KEY);
    if (!raw) {
      return [];
    }
    return normalizeRecentWorkspaceDirs(JSON.parse(raw));
  } catch {
    return [];
  }
}

function saveRecentWorkspaceDirs(items) {
  if (typeof localStorage === "undefined") {
    return;
  }
  localStorage.setItem(
    RECENT_WORKSPACE_DIRS_STORAGE_KEY,
    JSON.stringify(normalizeRecentWorkspaceDirs(items))
  );
}

function rememberRecentWorkspaceDir(items, dir) {
  const path = String(dir || "").trim();
  if (!path) {
    return normalizeRecentWorkspaceDirs(items);
  }
  return normalizeRecentWorkspaceDirs([path, ...(Array.isArray(items) ? items : [])]);
}

function loadWorkspaceSidebarOpen() {
  if (typeof localStorage === "undefined") {
    return false;
  }
  try {
    return localStorage.getItem(WORKSPACE_SIDEBAR_OPEN_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function saveWorkspaceSidebarOpen(open) {
  if (typeof localStorage === "undefined") {
    return;
  }
  localStorage.setItem(
    WORKSPACE_SIDEBAR_OPEN_STORAGE_KEY,
    open ? "true" : "false"
  );
}

function workspaceBrowserSource(sourceID) {
  const value = String(sourceID || "").trim();
  if (value === WORKSPACE_BROWSER_SOURCE_RECENT) {
    return {
      id: WORKSPACE_BROWSER_SOURCE_RECENT,
      kind: "recent",
      path: "",
      selection: "",
    };
  }
  if (value === WORKSPACE_BROWSER_SOURCE_SYSTEM) {
    return {
      id: WORKSPACE_BROWSER_SOURCE_SYSTEM,
      kind: "system",
      path: "",
      selection: "",
    };
  }
  return {
    id: WORKSPACE_BROWSER_SOURCE_HOME,
    kind: "home",
    path: "~",
    selection: "",
  };
}

function browserPathLabel(path) {
  const value = String(path || "").trim();
  if (!value) {
    return "";
  }
  const normalized = value.replace(/[\\/]+$/u, "");
  if (!normalized) {
    return value;
  }
  const parts = normalized.split(/[\\/]/u).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : value;
}

function splitWorkspaceDisplayPath(path) {
  const value = String(path || "").trim();
  if (!value) {
    return {
      prefix: "",
      separator: "",
      tail: "",
    };
  }
  if (/^[\\/]+$/u.test(value) || /^[A-Za-z]:[\\/]?$/u.test(value)) {
    return {
      prefix: "",
      separator: "",
      tail: value,
    };
  }
  const normalized = value.replace(/[\\/]+$/u, "");
  if (!normalized) {
    return {
      prefix: "",
      separator: "",
      tail: value,
    };
  }
  const slashIndex = normalized.lastIndexOf("/");
  const backslashIndex = normalized.lastIndexOf("\\");
  const separatorIndex = Math.max(slashIndex, backslashIndex);
  if (separatorIndex < 0) {
    return {
      prefix: "",
      separator: "",
      tail: normalized,
    };
  }
  const separator = normalized.charAt(separatorIndex);
  const prefix = normalized.slice(0, separatorIndex);
  const tail = normalized.slice(separatorIndex + 1);
  if (!tail) {
    return {
      prefix: "",
      separator: "",
      tail: value,
    };
  }
  return {
    prefix,
    separator,
    tail,
  };
}

function stringifyResult(result) {
  if (typeof result === "string") {
    return result.trim();
  }
  if (result === undefined || result === null) {
    return "";
  }
  try {
    return JSON.stringify(result, null, 2);
  } catch {
    return String(result);
  }
}

function taskCreatedAt(task) {
  const value = Date.parse(String(task?.created_at || "").trim());
  return Number.isFinite(value) ? value : 0;
}

function topicUpdatedAt(topic) {
  const value = Date.parse(String(topic?.updated_at || topic?.created_at || "").trim());
  return Number.isFinite(value) ? value : 0;
}

function topicTimeLabel(topic) {
  const raw = String(topic?.updated_at || topic?.created_at || "").trim();
  if (!raw) {
    return "";
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return raw;
  }
  const now = new Date();
  const sameDay =
    now.getFullYear() === date.getFullYear() &&
    now.getMonth() === date.getMonth() &&
    now.getDate() === date.getDate();
  if (sameDay) {
    return date.toLocaleTimeString(currentLocale(), {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
  return date.toLocaleDateString(currentLocale(), {
    month: "short",
    day: "numeric",
  });
}

function historyTimeLabel(raw) {
  const text = String(raw || "").trim();
  if (!text) {
    return "";
  }
  const date = new Date(text);
  if (Number.isNaN(date.getTime())) {
    return text;
  }
  const now = new Date();
  const sameDay =
    now.getFullYear() === date.getFullYear() &&
    now.getMonth() === date.getMonth() &&
    now.getDate() === date.getDate();
  const timeLabel = date.toLocaleTimeString(currentLocale(), {
    hour: "2-digit",
    minute: "2-digit",
  });
  if (sameDay) {
    return timeLabel;
  }
  const dayLabel = date.toLocaleDateString(currentLocale(), {
    month: "short",
    day: "numeric",
  });
  return `${dayLabel} ${timeLabel}`;
}

function taskRawJSON(task) {
  if (!task) {
    return "";
  }
  return stringifyResult(task);
}

function taskOutputText(task) {
  const finalOutput = task?.result?.final?.output;
  if (typeof finalOutput === "string") {
    return finalOutput.trim();
  }
  if (finalOutput !== undefined && finalOutput !== null) {
    return stringifyResult(finalOutput);
  }
  return "";
}

function stableHash(raw) {
  const text = String(raw || "");
  let hash = 2166136261;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function pollingActionKey(seed) {
  return POLLING_ACTION_KEYS[stableHash(seed || "agent") % POLLING_ACTION_KEYS.length];
}

function agentDisplayName(agentName, t) {
  const value = String(agentName || "").trim();
  return value || t("chat_agent_name_fallback");
}

function buildPollingHint(agentName, t, seed) {
  return t("chat_polling_hint", {
    name: agentDisplayName(agentName, t),
    action: t(pollingActionKey(seed)),
  });
}

function historyPendingSeed(item, fallback = "agent") {
  const candidates = [item?.pendingSeed, item?.taskId, item?.id, fallback];
  for (const candidate of candidates) {
    const value = String(candidate || "").trim();
    if (value) {
      return value;
    }
  }
  return "agent";
}

function taskAgentText(task, t, options = {}) {
  const output = taskOutputText(task);
  if (output) {
    return output;
  }
  const errorText = String(task?.error || "").trim();
  if (errorText) {
    return errorText;
  }
  const status = normalizeTaskStatus(task?.status);
  if (isTerminalStatus(status)) {
    return t("chat_result_empty");
  }
  const pendingText = String(options.pendingText || "").trim();
  if (pendingText) {
    return pendingText;
  }
  return buildPollingHint(options.agentName, t, options.pendingSeed || task?.id || task?.created_at);
}

function taskHistoryItems(task, t, options = {}) {
  const taskID = String(task?.id || "").trim();
  if (!taskID) {
    return [];
  }
  const items = [];
  const userText = String(task?.task || "").trim();
  if (userText) {
    items.push({
      id: `${taskID}:user`,
      role: "user",
      text: userText,
      status: "",
      timeText: historyTimeLabel(task?.created_at),
      taskId: "",
      rawJSON: "",
    });
  }
  items.push({
    id: `${taskID}:agent`,
    role: "agent",
    text: taskAgentText(task, t, {
      agentName: options.agentName,
      pendingSeed: taskID,
    }),
    status: normalizeTaskStatus(task?.status),
    timeText: historyTimeLabel(task?.finished_at),
    taskId: taskID,
    rawJSON: taskRawJSON(task),
    pendingSeed: taskID,
  });
  return items;
}

function newHistoryID() {
  return `${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

function kickerChannelLabel(mode) {
  switch (normalizeEndpointMode(mode)) {
    case "console":
      return "Console";
    case "serve":
      return "Serve";
    case "telegram":
      return "Telegram";
    case "slack":
      return "Slack";
    case "line":
      return "LINE";
    case "lark":
      return "Lark";
    default:
      return "Endpoint";
  }
}

const ChatView = {
  components: {
    AppKicker,
    AppPage,
    RawJsonDialog,
    MarkdownContent,
  },
  setup() {
    const t = translate;
    const route = useRoute();
    const router = useRouter();
    const mobileMode = ref(window.innerWidth <= 920);
    const mobileTopicView = ref("chat");
    const chatHistoryItems = ref([]);
    const renderedHistoryItems = ref({});
    const historyLoading = ref(false);
    const historyViewport = ref(null);
    const topics = ref([]);
    const topicsLoading = ref(false);
    const selectedTopicID = ref("");
    const creatingTopic = ref(false);
    const showSystemTopics = ref(false);
    const taskInput = ref("");
    const sending = ref(false);
    const err = ref("");
    const workspaceDir = ref("");
    const workspaceLoading = ref(false);
    const workspaceSaving = ref(false);
    const workspaceOpening = ref(false);
    const workspaceError = ref("");
    const workspaceSidebarOpen = ref(loadWorkspaceSidebarOpen());
    const workspaceSidebarTabID = ref(WORKSPACE_TAB_ID);
    const workspaceTreeItems = ref({});
    const workspaceTreeExpanded = ref({ "": true });
    const workspaceTreeLoading = ref(false);
    const workspaceTreeLoadingPath = ref("");
    const workspaceTreeError = ref("");
    const workspaceTreeSelectionPath = ref("");
    const workspaceBrowserOpen = ref(false);
    const workspaceBrowserItems = ref({});
    const workspaceBrowserExpanded = ref({ "": true });
    const workspaceBrowserLoading = ref(false);
    const workspaceBrowserLoadingPath = ref("");
    const workspaceBrowserError = ref("");
    const workspaceBrowserSourceID = ref(WORKSPACE_BROWSER_SOURCE_HOME);
    const workspaceBrowserRecentDirs = ref(loadRecentWorkspaceDirs());
    const workspaceBrowserSelection = ref("");
    const pollTimers = new Set();
    const streamSockets = new Map();
    const composerField = ref(null);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const rawRevealItemID = ref("");
    const rawRevealCount = ref(0);
    const heartbeatRevealCount = ref(0);
    const historyAutoStick = ref(true);
    let rawRevealTimerID = 0;
    let heartbeatRevealTimerID = 0;

    const selectedEndpoint = computed(() => runtimeEndpointByRef(endpointState.selectedRef));
    const routeTopicID = computed(() => normalizeTopicID(route.params.topic_id));
    const submitEndpointRef = computed(() => {
      const selected = selectedEndpoint.value;
      if (!selected) {
        return "";
      }
      const mapped = String(selected.submit_endpoint_ref || "").trim();
      if (mapped) {
        return mapped;
      }
      return selected.can_submit ? String(selected.endpoint_ref || "").trim() : "";
    });
    const submitEndpoint = computed(() => runtimeEndpointByRef(submitEndpointRef.value));
    const activeAgentName = computed(() => {
      const submitName = String(submitEndpoint.value?.agent_name || "").trim();
      if (submitName) {
        return submitName;
      }
      return String(selectedEndpoint.value?.agent_name || "").trim();
    });
    const displayAgentName = computed(() => agentDisplayName(activeAgentName.value, t));
    const consoleTopicsEnabled = computed(() => {
      if (!submitEndpointRef.value) {
        return false;
      }
      const mode = submitEndpoint.value?.mode || selectedEndpoint.value?.mode;
      return normalizeEndpointMode(mode) === "console";
    });
    const submitBlockedMessage = computed(() => {
      const selected = selectedEndpoint.value;
      if (!selected || !selected.connected) {
        return "";
      }
      if (submitEndpointRef.value) {
        return "";
      }
      return t("chat_submit_unsupported", {
        name: selected.name || selected.endpoint_ref || "-",
      });
    });
    const chatReadonly = computed(() => Boolean(submitBlockedMessage.value));
    const readonlyTitle = computed(() => {
      return t("chat_readonly_title", {
        channel: endpointChannelLabel(selectedEndpoint.value?.mode, t),
      });
    });
    const readonlyKickerLeft = computed(() => kickerChannelLabel(selectedEndpoint.value?.mode));
    const readonlyReason = computed(() => {
      const selected = selectedEndpoint.value;
      if (!selected) {
        return "";
      }
      return t("chat_readonly_reason", {
        name: selected.name || selected.endpoint_ref || "-",
        channel: endpointChannelLabel(selected.mode, t),
      });
    });
    const composerDisabled = computed(() => Boolean(submitBlockedMessage.value) || sending.value);
    const sendDisabled = computed(
      () => composerDisabled.value || String(taskInput.value || "").trim() === ""
    );
    const composerPlaceholder = computed(() =>
      t("chat_input_placeholder", {
        name: displayAgentName.value,
      })
    );
    const mobileTopicSplitEnabled = computed(() => consoleTopicsEnabled.value && mobileMode.value);
    const visibleTopics = computed(() => {
      const selectedTopic = normalizeTopicID(selectedTopicID.value);
      const items = [];
      let heartbeatTopic = null;
      const heartbeatVisible = showSystemTopics.value || selectedTopic === HEARTBEAT_TOPIC_ID;
      for (const topic of topics.value) {
        const topicID = normalizeTopicID(topic?.id);
        if (!topicID) {
          continue;
        }
        if (topicID === HEARTBEAT_TOPIC_ID) {
          if (heartbeatVisible) {
            heartbeatTopic = topic;
          }
          continue;
        }
        items.push(topic);
      }
      if (!heartbeatTopic && heartbeatVisible) {
        heartbeatTopic = {
          id: HEARTBEAT_TOPIC_ID,
          title: t("chat_topic_heartbeat"),
          created_at: "",
          updated_at: "",
        };
      }
      if (heartbeatTopic) {
        return [heartbeatTopic, ...items];
      }
      return items;
    });
    const hasVisibleTopics = computed(() => visibleTopics.value.length > 0);
    const selectedTopic = computed(() => {
      const selectedID = normalizeTopicID(selectedTopicID.value);
      if (!selectedID) {
        return null;
      }
      const matched = topics.value.find((topic) => normalizeTopicID(topic?.id) === selectedID);
      if (matched) {
        return matched;
      }
      if (selectedID === HEARTBEAT_TOPIC_ID) {
        return {
          id: HEARTBEAT_TOPIC_ID,
          title: t("chat_topic_heartbeat"),
          created_at: "",
          updated_at: "",
        };
      }
      return {
        id: selectedID,
        title: "",
        created_at: "",
        updated_at: "",
      };
    });
    const hasSelectedTopic = computed(() => normalizeTopicID(selectedTopicID.value) !== "");
    const showChatPlaceholder = computed(
      () => consoleTopicsEnabled.value && !hasSelectedTopic.value && chatHistoryItems.value.length === 0
    );
    const chatPlaceholderText = computed(() => t("chat_intro"));
    const pageClass = computed(() => {
      const classes = ["chat-page"];
      if (consoleTopicsEnabled.value) {
        classes.push("chat-page-topics");
      }
      if (mobileTopicSplitEnabled.value) {
        classes.push("chat-page-mobile-split");
      }
      return classes.join(" ");
    });
    const mobileBarTitle = computed(() => {
      if (!mobileTopicSplitEnabled.value) {
        return t("chat_title");
      }
      if (!hasVisibleTopics.value) {
        return creatingTopic.value ? t("chat_topic_new") : t("chat_title");
      }
      if (mobileTopicView.value === "topics") {
        return t("chat_topics_title");
      }
      if (creatingTopic.value) {
        return t("chat_topic_new");
      }
      return selectedTopic.value ? topicTitle(selectedTopic.value) : t("chat_title");
    });
    const mobileShowBack = computed(
      () => mobileTopicSplitEnabled.value && hasVisibleTopics.value && mobileTopicView.value === "chat"
    );
    const showTopicSidebar = computed(() => {
      if (!consoleTopicsEnabled.value || !hasVisibleTopics.value) {
        return false;
      }
      if (!mobileTopicSplitEnabled.value) {
        return true;
      }
      return mobileTopicView.value === "topics";
    });
    const showChatPane = computed(() => {
      if (!mobileTopicSplitEnabled.value || !hasVisibleTopics.value) {
        return true;
      }
      return mobileTopicView.value === "chat";
    });
    const desktopWorkspaceSidebarVisible = computed(
      () => workspaceSidebarAvailable.value && !mobileMode.value && showChatPane.value && workspaceSidebarOpen.value
    );
    const shellClass = computed(() => {
      const classes = ["chat-shell"];
      if (consoleTopicsEnabled.value && hasVisibleTopics.value && !mobileTopicSplitEnabled.value) {
        classes.push("has-sidebar");
      }
      if (desktopWorkspaceSidebarVisible.value) {
        classes.push("has-workspace-panel");
      }
      if (mobileTopicSplitEnabled.value) {
        classes.push(mobileTopicView.value === "topics" ? "is-mobile-topics" : "is-mobile-chat");
      }
      return classes.join(" ");
    });
    const chatMainClass = computed(() => {
      const classes = ["chat-main"];
      if (showChatPlaceholder.value) {
        classes.push("is-placeholder-mode");
      }
      return classes.join(" ");
    });
    const topicSidebarKicker = computed(() =>
      endpointChannelLabel(submitEndpoint.value?.mode || selectedEndpoint.value?.mode, t)
    );
    const deskTitle = computed(() => {
      if (creatingTopic.value || !hasSelectedTopic.value || !selectedTopic.value) {
        return t("chat_topic_new");
      }
      return topicTitle(selectedTopic.value);
    });
    const deskMeta = computed(() => {
      const parts = [];
      if (!creatingTopic.value && hasSelectedTopic.value && selectedTopic.value) {
        const time = topicTime(selectedTopic.value);
        if (time) {
          parts.push(time);
        }
      }
      const channel = endpointChannelLabel(submitEndpoint.value?.mode || selectedEndpoint.value?.mode, t);
      if (channel) {
        parts.push(channel);
      }
      const name = displayAgentName.value;
      if (name) {
        parts.push(name);
      }
      return parts.join(" · ");
    });
    const workspaceTopicID = computed(() => {
      if (!consoleTopicsEnabled.value || creatingTopic.value) {
        return "";
      }
      const topicID = normalizeTopicID(selectedTopicID.value);
      if (!topicID || topicID === HEARTBEAT_TOPIC_ID) {
        return "";
      }
      return topicID;
    });
    const workspaceSidebarAvailable = computed(() => Boolean(workspaceTopicID.value));
    const workspaceReady = computed(() => Boolean(submitEndpointRef.value && workspaceTopicID.value));
    const workspaceBusy = computed(() => workspaceLoading.value || workspaceSaving.value);
    const workspaceHintText = computed(() => {
      if (workspaceTopicID.value) {
        return String(workspaceDir.value || "").trim() ? "" : t("chat_workspace_hint_empty");
      }
      if (creatingTopic.value) {
        return t("chat_workspace_hint_needs_topic");
      }
      if (normalizeTopicID(selectedTopicID.value) === HEARTBEAT_TOPIC_ID) {
        return t("chat_workspace_hint_system_topic");
      }
      return t("chat_workspace_hint_no_topic");
    });
    const workspaceAttachDisabled = computed(() => !workspaceReady.value || workspaceBusy.value);
    const workspaceDetachDisabled = computed(
      () => !workspaceReady.value || workspaceBusy.value || String(workspaceDir.value || "").trim() === ""
    );
    const workspaceDirDisplay = computed(() => splitWorkspaceDisplayPath(workspaceDir.value));
    const workspacePanelTabs = computed(() => [
      {
        id: WORKSPACE_TAB_ID,
        title: "",
        icon: "QIconEcosystem",
      },
    ]);
    const selectedWorkspacePanelTab = computed(
      () => workspacePanelTabs.value.find((item) => item.id === workspaceSidebarTabID.value) || workspacePanelTabs.value[0]
    );
    const workspaceTreeRows = computed(() =>
      buildTreeRows(workspaceTreeItems.value, workspaceTreeExpanded.value)
    );
    const workspaceSelectedTreeEntry = computed(() => {
      const selectedPath = String(workspaceTreeSelectionPath.value || "").trim();
      if (!selectedPath) {
        return null;
      }
      const row = workspaceTreeRows.value.find(
        (item) => String(item?.entry?.path || "").trim() === selectedPath
      );
      return row?.entry || null;
    });
    const workspaceBrowserRecentItems = computed(() =>
      workspaceBrowserRecentDirs.value.map((path) => ({
        path,
        title: browserPathLabel(path),
        meta: path,
      }))
    );
    const workspaceBrowserCurrentSource = computed(() =>
      workspaceBrowserSource(workspaceBrowserSourceID.value)
    );
    const workspaceBrowserRows = computed(() => {
      if (workspaceBrowserCurrentSource.value.kind === "recent") {
        return workspaceBrowserRecentItems.value.map((item) => ({
          key: `recent:${item.path}`,
          depth: 0,
          entry: {
            name: item.title,
            path: item.path,
            is_dir: true,
            has_children: false,
          },
          expandable: false,
          expanded: false,
        }));
      }
      return buildTreeRows(
        workspaceBrowserItems.value,
        workspaceBrowserExpanded.value,
        workspaceBrowserCurrentSource.value.path
      );
    });
    const workspaceBrowserConfirmDisabled = computed(
      () => !workspaceReady.value || workspaceSaving.value || String(workspaceBrowserSelection.value || "").trim() === ""
    );
    const workspaceSidebarToggleLabel = computed(() =>
      workspaceSidebarOpen.value ? t("chat_workspace_sidebar_close") : t("chat_workspace_sidebar_open")
    );
    const workspaceBrowserEmptyText = computed(() =>
      workspaceBrowserCurrentSource.value.kind === "recent"
        ? t("chat_workspace_dialog_recent_empty")
        : t("chat_workspace_dialog_empty")
    );
    const chatPlaceholderHint = computed(() => {
      if (visibleTopics.value.length > 0) {
        return t("chat_placeholder_choose_topic");
      }
      return chatPlaceholderText.value;
    });
    let workspaceRequestSeq = 0;

    function syncMobileTopicView(options = {}) {
      if (!mobileTopicSplitEnabled.value) {
        mobileTopicView.value = "chat";
        return;
      }
      if (!hasVisibleTopics.value) {
        mobileTopicView.value = "chat";
        return;
      }
      if (options.preferTopics) {
        mobileTopicView.value = "topics";
        return;
      }
      if (options.preferChat) {
        mobileTopicView.value = "chat";
        return;
      }
      if (!creatingTopic.value && !normalizeTopicID(selectedTopicID.value)) {
        mobileTopicView.value = "topics";
        return;
      }
      if (mobileTopicView.value !== "topics" && mobileTopicView.value !== "chat") {
        mobileTopicView.value = "chat";
      }
    }

    function showTopicsView() {
      if (!hasVisibleTopics.value) {
        return;
      }
      syncMobileTopicView({ preferTopics: true });
    }

    function refreshMobileMode() {
      const nextValue = window.innerWidth <= 920;
      const changed = mobileMode.value !== nextValue;
      mobileMode.value = nextValue;
      if (!changed) {
        return;
      }
      syncMobileTopicView({
        preferChat: Boolean(creatingTopic.value || normalizeTopicID(selectedTopicID.value)),
      });
      focusComposer();
    }

    function composerTextarea() {
      const root = composerField.value?.$el || composerField.value;
      if (!root || typeof root.querySelector !== "function") {
        return null;
      }
      return root.querySelector("textarea");
    }

    function focusComposer() {
      if (chatReadonly.value || (mobileTopicSplitEnabled.value && !showChatPane.value)) {
        return;
      }
      void nextTick(() => {
        window.requestAnimationFrame(() => {
          const textarea = composerTextarea();
          if (!textarea || textarea.disabled) {
            return;
          }
          textarea.focus({ preventScroll: true });
          const length = textarea.value.length;
          textarea.setSelectionRange(length, length);
        });
      });
    }

    function insertComposerText(rawText) {
      const insertText = String(rawText || "");
      if (!insertText) {
        return;
      }
      const current = String(taskInput.value || "");
      const textarea = composerTextarea();
      const active = typeof document !== "undefined" ? document.activeElement : null;
      let start = current.length;
      let end = current.length;
      if (
        textarea &&
        active === textarea &&
        typeof textarea.selectionStart === "number" &&
        typeof textarea.selectionEnd === "number"
      ) {
        start = textarea.selectionStart;
        end = textarea.selectionEnd;
      }
      taskInput.value = `${current.slice(0, start)}${insertText}${current.slice(end)}`;
      void nextTick(() => {
        const field = composerTextarea();
        if (!field || field.disabled) {
          return;
        }
        const nextOffset = start + insertText.length;
        field.focus({ preventScroll: true });
        field.setSelectionRange(nextOffset, nextOffset);
      });
    }

    function setTreeItems(target, path, items) {
      target.value = {
        ...target.value,
        [path]: normalizeTreeItems(items),
      };
    }

    function setTreeExpanded(target, path, expanded) {
      const nextValue = { ...target.value };
      if (expanded) {
        nextValue[path] = true;
      } else {
        delete nextValue[path];
      }
      target.value = nextValue;
    }

    function resetWorkspaceTreeState() {
      workspaceTreeItems.value = {};
      workspaceTreeExpanded.value = { "": true };
      workspaceTreeLoading.value = false;
      workspaceTreeLoadingPath.value = "";
      workspaceTreeError.value = "";
      workspaceTreeSelectionPath.value = "";
    }

    function resetWorkspaceBrowserState() {
      workspaceBrowserItems.value = {};
      workspaceBrowserExpanded.value = { "": true };
      workspaceBrowserLoading.value = false;
      workspaceBrowserLoadingPath.value = "";
      workspaceBrowserError.value = "";
      workspaceBrowserSelection.value = "";
    }

    function saveWorkspaceBrowserRecentDirs(items) {
      const nextItems = normalizeRecentWorkspaceDirs(items);
      workspaceBrowserRecentDirs.value = nextItems;
      saveRecentWorkspaceDirs(nextItems);
    }

    function rememberWorkspaceBrowserRecentDir(dir) {
      saveWorkspaceBrowserRecentDirs(
        rememberRecentWorkspaceDir(workspaceBrowserRecentDirs.value, dir)
      );
    }

    function resetWorkspaceState() {
      workspaceRequestSeq += 1;
      workspaceDir.value = "";
      workspaceLoading.value = false;
      workspaceSaving.value = false;
      workspaceOpening.value = false;
      workspaceError.value = "";
      workspaceBrowserOpen.value = false;
      workspaceSidebarTabID.value = WORKSPACE_TAB_ID;
      resetWorkspaceTreeState();
      resetWorkspaceBrowserState();
    }

    function applyWorkspacePayload(data) {
      const nextDir = String(data?.workspace_dir || "").trim();
      workspaceDir.value = nextDir;
      workspaceError.value = "";
      resetWorkspaceTreeState();
      resetWorkspaceBrowserState();
      if (nextDir) {
        workspaceBrowserSelection.value = nextDir;
      }
    }

    async function refreshWorkspaceState() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const requestID = ++workspaceRequestSeq;

      if (!endpointRef || !topicID) {
        resetWorkspaceState();
        return true;
      }

      workspaceLoading.value = true;
      workspaceError.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(
          endpointRef,
          `/workspace?topic_id=${encodeURIComponent(topicID)}`
        );
        if (requestID !== workspaceRequestSeq) {
          return false;
        }
        applyWorkspacePayload(data);
        if (workspaceSidebarOpen.value && String(workspaceDir.value || "").trim()) {
          await loadWorkspaceTree("", { force: true });
        }
        return true;
      } catch (e) {
        if (requestID !== workspaceRequestSeq) {
          return false;
        }
        workspaceDir.value = "";
        resetWorkspaceTreeState();
        workspaceError.value = e?.message || t("msg_load_failed");
        return false;
      } finally {
        if (requestID === workspaceRequestSeq) {
          workspaceLoading.value = false;
        }
      }
    }

    function toggleWorkspaceSidebar() {
      if (!workspaceSidebarAvailable.value) {
        return;
      }
      workspaceSidebarOpen.value = !workspaceSidebarOpen.value;
      if (workspaceSidebarOpen.value) {
        workspaceSidebarTabID.value = WORKSPACE_TAB_ID;
        if (String(workspaceDir.value || "").trim() && !hasOwnTreePath(workspaceTreeItems.value, "")) {
          void loadWorkspaceTree("", { force: true });
        }
      }
    }

    function onWorkspaceTabChange(detail) {
      const nextID = String(detail?.tab?.id || "").trim();
      workspaceSidebarTabID.value = nextID || WORKSPACE_TAB_ID;
    }

    function workspaceBrowserSourceItemClass(sourceID) {
      const classes = ["workspace-sidebar-item", "chat-workspace-dialog-sidebar-item"];
      if (String(sourceID || "").trim() === workspaceBrowserSourceID.value) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    async function loadWorkspaceTree(treePath = "", options = {}) {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const currentDir = String(workspaceDir.value || "").trim();
      const path = String(treePath || "").trim();
      if (!endpointRef || !topicID || !currentDir) {
        resetWorkspaceTreeState();
        return false;
      }
      if (!path && options.force === true) {
        resetWorkspaceTreeState();
      }
      workspaceTreeLoading.value = true;
      workspaceTreeLoadingPath.value = path;
      try {
        const query = new URLSearchParams();
        query.set("topic_id", topicID);
        if (path) {
          query.set("path", path);
        }
        const data = await runtimeApiFetchForEndpoint(
          endpointRef,
          `/workspace/tree?${query.toString()}`
        );
        setTreeItems(workspaceTreeItems, path, data?.items);
        if (path) {
          setTreeExpanded(workspaceTreeExpanded, path, true);
        }
        workspaceTreeError.value = "";
        return true;
      } catch (e) {
        workspaceTreeError.value = e?.message || t("msg_load_failed");
        return false;
      } finally {
        if (workspaceTreeLoadingPath.value === path) {
          workspaceTreeLoading.value = false;
          workspaceTreeLoadingPath.value = "";
        }
      }
    }

    async function toggleWorkspaceTreeNode(entry) {
      const path = String(entry?.path || "").trim();
      if (!entry?.is_dir || !path) {
        return;
      }
      if (workspaceTreeExpanded.value[path]) {
        setTreeExpanded(workspaceTreeExpanded, path, false);
        return;
      }
      if (!hasOwnTreePath(workspaceTreeItems.value, path)) {
        const ok = await loadWorkspaceTree(path);
        if (!ok) {
          return;
        }
      }
      setTreeExpanded(workspaceTreeExpanded, path, true);
    }

    function workspaceTreeEntryClass(row) {
      const classes = ["chat-workspace-tree-entry", "is-actionable", "is-selectable"];
      if (row?.entry?.is_dir) {
        classes.push("is-dir");
      }
      if (String(row?.entry?.path || "").trim() === String(workspaceTreeSelectionPath.value || "").trim()) {
        classes.push("is-selected");
      }
      return classes.join(" ");
    }

    async function selectWorkspaceTreeNode(row) {
      const entry = row?.entry || row;
      const path = String(entry?.path || "").trim();
      if (!path) {
        return;
      }
      workspaceTreeSelectionPath.value = path;
      if (row?.expandable) {
        await toggleWorkspaceTreeNode(entry);
      }
    }

    function addWorkspaceSelectionToComposer() {
      if (composerDisabled.value) {
        return;
      }
      const path = String(workspaceSelectedTreeEntry.value?.path || "").trim();
      if (!path) {
        return;
      }
      insertComposerText(path);
    }

    async function openWorkspaceSelection() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const path = String(workspaceSelectedTreeEntry.value?.path || "").trim();
      if (!endpointRef || !topicID || !path || workspaceOpening.value) {
        return;
      }
      workspaceOpening.value = true;
      workspaceError.value = "";
      try {
        await runtimeApiFetchForEndpoint(endpointRef, "/workspace/open", {
          method: "POST",
          body: {
            topic_id: topicID,
            path,
          },
        });
      } catch (e) {
        workspaceError.value = e?.message || t("msg_load_failed");
      } finally {
        workspaceOpening.value = false;
      }
    }

    async function openWorkspaceBrowser() {
      if (workspaceAttachDisabled.value) {
        return;
      }
      workspaceBrowserOpen.value = true;
      workspaceBrowserError.value = "";
      await activateWorkspaceBrowserSource(WORKSPACE_BROWSER_SOURCE_HOME);
    }

    function closeWorkspaceBrowser() {
      workspaceBrowserOpen.value = false;
      workspaceBrowserError.value = "";
    }

    async function activateWorkspaceBrowserSource(sourceID) {
      const source = workspaceBrowserSource(sourceID);
      workspaceBrowserSourceID.value = source.id;
      resetWorkspaceBrowserState();
      if (source.kind === "recent") {
        workspaceBrowserError.value = "";
        return true;
      }
      const ok = await loadWorkspaceBrowser(source.path);
      if (ok) {
        workspaceBrowserSelection.value = source.selection;
      }
      return ok;
    }

    async function loadWorkspaceBrowser(treePath = "") {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const path = String(treePath || "").trim();
      if (!endpointRef) {
        resetWorkspaceBrowserState();
        return false;
      }
      workspaceBrowserLoading.value = true;
      workspaceBrowserLoadingPath.value = path;
      try {
        const query = new URLSearchParams();
        if (path) {
          query.set("path", path);
        }
        const data = await runtimeApiFetchForEndpoint(
          endpointRef,
          query.toString() ? `/workspace/browse?${query.toString()}` : "/workspace/browse"
        );
        setTreeItems(workspaceBrowserItems, path, data?.items);
        if (path) {
          setTreeExpanded(workspaceBrowserExpanded, path, true);
        }
        workspaceBrowserError.value = "";
        return true;
      } catch (e) {
        workspaceBrowserError.value = e?.message || t("msg_load_failed");
        return false;
      } finally {
        if (workspaceBrowserLoadingPath.value === path) {
          workspaceBrowserLoading.value = false;
          workspaceBrowserLoadingPath.value = "";
        }
      }
    }

    async function toggleWorkspaceBrowserNode(entry) {
      const path = String(entry?.path || "").trim();
      if (!entry?.is_dir || !path) {
        return;
      }
      if (workspaceBrowserExpanded.value[path]) {
        setTreeExpanded(workspaceBrowserExpanded, path, false);
        return;
      }
      if (!hasOwnTreePath(workspaceBrowserItems.value, path)) {
        const ok = await loadWorkspaceBrowser(path);
        if (!ok) {
          return;
        }
      }
      setTreeExpanded(workspaceBrowserExpanded, path, true);
    }

    async function selectWorkspaceBrowserNode(row) {
      const entry = row?.entry || row;
      if (!entry?.is_dir) {
        return;
      }
      workspaceBrowserSelection.value = String(entry.path || "").trim();
      if (!row?.expandable || workspaceBrowserCurrentSource.value.kind === "recent") {
        return;
      }
      await toggleWorkspaceBrowserNode(entry);
    }

    async function attachWorkspace() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const nextDir = String(workspaceBrowserSelection.value || "").trim();
      if (!endpointRef || !topicID || !nextDir || workspaceSaving.value) {
        return;
      }
      workspaceSaving.value = true;
      workspaceError.value = "";
      workspaceBrowserError.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/workspace", {
          method: "PUT",
          body: {
            topic_id: topicID,
            workspace_dir: nextDir,
          }
        });
        rememberWorkspaceBrowserRecentDir(String(data?.workspace_dir || nextDir || "").trim());
        applyWorkspacePayload(data);
        workspaceBrowserOpen.value = false;
        if (workspaceSidebarOpen.value) {
          await loadWorkspaceTree("", { force: true });
        }
      } catch (e) {
        const message = e?.message || t("msg_save_failed");
        workspaceError.value = message;
        workspaceBrowserError.value = message;
      } finally {
        workspaceSaving.value = false;
      }
    }

    async function detachWorkspace() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      if (!endpointRef || !topicID || workspaceDetachDisabled.value) {
        return;
      }
      workspaceSaving.value = true;
      workspaceError.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(
          endpointRef,
          `/workspace?topic_id=${encodeURIComponent(topicID)}`,
          {
            method: "DELETE",
          }
        );
        applyWorkspacePayload(data);
      } catch (e) {
        workspaceError.value = e?.message || t("msg_save_failed");
      } finally {
        workspaceSaving.value = false;
      }
    }

    function chatRoutePath(topicID = "") {
      const normalized = normalizeTopicID(topicID);
      return normalized ? `/chat/${encodeURIComponent(normalized)}` : "/chat";
    }

    function syncChatRoute(topicID, options = {}) {
      const nextPath = chatRoutePath(topicID);
      if (route.path === nextPath) {
        return Promise.resolve();
      }
      const method = options.replace ? "replace" : "push";
      return router[method]({
        path: nextPath,
        query: route.query,
      });
    }

    function handleComposerPointerDown(event) {
      const target = event?.target;
      if (!(target instanceof Element)) {
        focusComposer();
        return;
      }
      if (target.closest(".chat-composer-send")) {
        return;
      }
      if (target.closest("textarea, input, button, a, [role='button']")) {
        return;
      }
      event.preventDefault();
      focusComposer();
    }

    function historyViewportElement() {
      return historyViewport.value;
    }

    function historyDistanceFromBottom() {
      const viewport = historyViewportElement();
      if (!viewport) {
        return 0;
      }
      return viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop;
    }

    function historyNearBottom() {
      return historyDistanceFromBottom() <= 28;
    }

    function handleHistoryScroll() {
      historyAutoStick.value = historyNearBottom();
    }

    function scrollHistoryToBottom(options = {}) {
      const force = Boolean(options.force);
      void nextTick(() => {
        const viewport = historyViewportElement();
        if (!viewport) {
          return;
        }
        if (!force && !historyAutoStick.value) {
          return;
        }
        window.requestAnimationFrame(() => {
          const node = historyViewportElement();
          if (!node) {
            return;
          }
          node.scrollTop = node.scrollHeight;
          historyAutoStick.value = true;
        });
      });
    }

    function handleMarkdownRendered() {
      if (!historyAutoStick.value) {
        return;
      }
      scrollHistoryToBottom({ force: true });
    }

    function syncRenderedHistoryItems(items) {
      const previous = renderedHistoryItems.value;
      const next = {};
      for (const item of Array.isArray(items) ? items : []) {
        const itemID = String(item?.id || "").trim();
        if (!itemID) {
          continue;
        }
        if (String(item?.role || "").trim().toLowerCase() !== "agent") {
          next[itemID] = true;
          continue;
        }
        next[itemID] = previous[itemID] === true;
      }
      renderedHistoryItems.value = next;
    }

    function replaceHistoryItems(items) {
      const nextItems = Array.isArray(items) ? items : [];
      chatHistoryItems.value = nextItems;
      syncRenderedHistoryItems(nextItems);
    }

    function historyItemRenderReady(item) {
      if (String(item?.role || "").trim().toLowerCase() !== "agent") {
        return true;
      }
      const itemID = String(item?.id || "").trim();
      return itemID !== "" && renderedHistoryItems.value[itemID] === true;
    }

    function showHistorySkeleton(item) {
      return String(item?.role || "").trim().toLowerCase() === "agent" && !historyItemRenderReady(item);
    }

    function markHistoryItemRendered(itemID) {
      const key = String(itemID || "").trim();
      if (key && renderedHistoryItems.value[key] !== true) {
        renderedHistoryItems.value = {
          ...renderedHistoryItems.value,
          [key]: true,
        };
      }
      handleMarkdownRendered();
    }

    function syncComposerHeight() {
      void nextTick(() => {
        const textarea = composerTextarea();
        if (!textarea) {
          return;
        }
        const styles = window.getComputedStyle(textarea);
        const lineHeight = Number.parseFloat(styles.lineHeight) || 20;
        const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
        const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
        const borderTop = Number.parseFloat(styles.borderTopWidth) || 0;
        const borderBottom = Number.parseFloat(styles.borderBottomWidth) || 0;
        const minHeight = lineHeight + paddingTop + paddingBottom + borderTop + borderBottom;
        const maxHeight =
          lineHeight * COMPOSER_MAX_ROWS + paddingTop + paddingBottom + borderTop + borderBottom;

        textarea.style.height = "auto";
        const nextHeight = Math.max(minHeight, Math.min(textarea.scrollHeight, maxHeight));
        textarea.style.height = `${nextHeight}px`;
        textarea.style.overflowY = textarea.scrollHeight > maxHeight ? "auto" : "hidden";
      });
    }

    function clearPollTimers() {
      for (const timerID of pollTimers) {
        window.clearTimeout(timerID);
      }
      pollTimers.clear();
    }

    function closeTaskStream(taskID) {
      const key = String(taskID || "").trim();
      if (!key) {
        return;
      }
      const active = streamSockets.get(key);
      if (!active) {
        return;
      }
      active.closing = true;
      try {
        active.socket.close();
      } catch {
        // Ignore local close errors.
      }
      streamSockets.delete(key);
    }

    function clearStreamSockets() {
      for (const taskID of streamSockets.keys()) {
        closeTaskStream(taskID);
      }
    }

    function supportsConsoleLocalStream(endpointRef) {
      const endpoint = runtimeEndpointByRef(endpointRef);
      return String(endpoint?.url || "").trim() === "in-process://console-local";
    }

    async function startTaskStream(taskID, historyID, endpointRef) {
      const key = String(taskID || "").trim();
      if (!key || !supportsConsoleLocalStream(endpointRef)) {
        return;
      }
      const existing = streamSockets.get(key);
      if (existing && existing.historyID === historyID && existing.endpointRef === endpointRef) {
        return;
      }
      closeTaskStream(key);

      let ticketPayload;
      try {
        ticketPayload = await createConsoleStreamTicket();
      } catch {
        return;
      }
      const ticket = String(ticketPayload?.ticket || "").trim();
      const url = buildConsoleStreamURL(ticket, key);
      if (!url) {
        return;
      }

      const socket = new WebSocket(url);
      const entry = {
        socket,
        historyID,
        endpointRef,
        closing: false,
      };
      streamSockets.set(key, entry);

      socket.onmessage = (event) => {
        const active = streamSockets.get(key);
        if (active !== entry) {
          return;
        }
        const frame = safeJSON(event.data, null);
        if (!frame || typeof frame !== "object") {
          return;
        }
        const patch = {};
        if (typeof frame.text === "string" && frame.text !== "") {
          patch.text = frame.text;
        } else if (typeof frame.error === "string" && frame.error !== "") {
          patch.text = frame.error;
        }
        if (typeof frame.status === "string" && frame.status !== "") {
          patch.status = normalizeTaskStatus(frame.status);
        }
        if (Object.keys(patch).length > 0) {
          patchAgentHistoryItem(key, historyID, patch);
          scrollHistoryToBottom();
        }
        if (frame.done) {
          closeTaskStream(key);
        }
      };
      socket.onclose = () => {
        const active = streamSockets.get(key);
        if (active === entry) {
          streamSockets.delete(key);
        }
      };
      socket.onerror = () => {
        // Polling stays active as the fallback path.
      };
    }

    function staticHistoryItem(id, text) {
      return {
        id,
        role: "system",
        text,
        status: "",
        timeText: "",
        taskId: "",
        rawJSON: "",
      };
    }

    function emptyHistoryItem() {
      if (consoleTopicsEnabled.value && creatingTopic.value) {
        return staticHistoryItem("chat-new-topic", t("chat_new_topic_intro"));
      }
      if (consoleTopicsEnabled.value && normalizeTopicID(selectedTopicID.value)) {
        return staticHistoryItem("chat-topic-empty", t("chat_topic_empty"));
      }
      return staticHistoryItem("chat-intro", t("chat_intro"));
    }

    function historyClass(item) {
      const role = String(item?.role || "").trim().toLowerCase();
      if (role === "user") {
        return "chat-history-item chat-history-user";
      }
      if (role === "agent") {
        return "chat-history-item chat-history-agent";
      }
      return "chat-history-item chat-history-system";
    }

    function historySurfaceClass(item) {
      const role = String(item?.role || "").trim().toLowerCase();
      if (role === "agent") {
        return "chat-history-copy";
      }
      return "chat-history-bubble";
    }

    function isSystemTopic(topic) {
      return normalizeTopicID(topic?.id) === HEARTBEAT_TOPIC_ID;
    }

    function topicTitle(topic) {
      const title = String(topic?.title || "").trim();
      if (title) {
        return title;
      }
      const topicID = normalizeTopicID(topic?.id);
      if (topicID === "default") {
        return t("chat_topic_default");
      }
      if (topicID === HEARTBEAT_TOPIC_ID) {
        return t("chat_topic_heartbeat");
      }
      return t("chat_topic_untitled");
    }

    function topicTime(topic) {
      return topicTimeLabel(topic);
    }

    function topicBadgeText(topic) {
      if (isSystemTopic(topic)) {
        return t("chat_topic_system");
      }
      return "";
    }

    function topicBadgeType(topic) {
      return topicIsActive(topic) ? "primary" : "default";
    }

    function topicItemClass(topic) {
      const classes = ["chat-topic-item", "workspace-sidebar-item"];
      if (normalizeTopicID(topic?.id) === normalizeTopicID(selectedTopicID.value) && !creatingTopic.value) {
        classes.push("is-active");
      }
      if (isSystemTopic(topic)) {
        classes.push("is-system");
      }
      return classes.join(" ");
    }

    function topicIsActive(topic) {
      return normalizeTopicID(topic?.id) === normalizeTopicID(selectedTopicID.value) && !creatingTopic.value;
    }

    function pushHistoryItem(partial) {
      const item = {
        id: newHistoryID(),
        role: String(partial?.role || "system"),
        text: String(partial?.text || ""),
        status: String(partial?.status || ""),
        timeText: String(partial?.timeText || ""),
        taskId: String(partial?.taskId || ""),
        rawJSON: String(partial?.rawJSON || ""),
        pendingSeed: String(partial?.pendingSeed || ""),
      };
      replaceHistoryItems([...chatHistoryItems.value, item]);
      return item.id;
    }

    function patchHistoryItem(id, patch) {
      const idx = chatHistoryItems.value.findIndex((item) => item.id === id);
      if (idx < 0) {
        return;
      }
      const next = chatHistoryItems.value.slice();
      next[idx] = {
        ...next[idx],
        ...patch,
      };
      replaceHistoryItems(next);
    }

    function resolveAgentHistoryID(taskID, preferredHistoryID = "") {
      const preferred = String(preferredHistoryID || "").trim();
      if (preferred && chatHistoryItems.value.some((item) => item.id === preferred)) {
        return preferred;
      }
      const key = String(taskID || "").trim();
      if (!key) {
        return "";
      }
      const matched = chatHistoryItems.value.find((item) => {
        return String(item?.role || "") === "agent" && String(item?.taskId || "").trim() === key;
      });
      return String(matched?.id || "").trim();
    }

    function patchAgentHistoryItem(taskID, historyID, patch) {
      const resolvedID = resolveAgentHistoryID(taskID, historyID);
      if (!resolvedID) {
        return "";
      }
      patchHistoryItem(resolvedID, patch);
      return resolvedID;
    }

    function schedulePoll(fn) {
      const timerID = window.setTimeout(async () => {
        pollTimers.delete(timerID);
        await fn();
      }, POLL_INTERVAL_MS);
      pollTimers.add(timerID);
    }

    async function pollTask(taskID, historyID, endpointRef) {
      try {
        const detail = await runtimeApiFetchForEndpoint(endpointRef, `/tasks/${encodeURIComponent(taskID)}`);
        const status = normalizeTaskStatus(detail?.status);
        const resolvedHistoryID = resolveAgentHistoryID(taskID, historyID);
        const existingItem = chatHistoryItems.value.find((item) => item.id === resolvedHistoryID) || null;
        const pendingSeed = historyPendingSeed(existingItem, taskID);
        patchAgentHistoryItem(taskID, historyID, {
          status,
          text: taskAgentText(detail, t, {
            agentName: activeAgentName.value,
            pendingSeed,
            pendingText: !isTerminalStatus(status) ? existingItem?.text : "",
          }),
          timeText: historyTimeLabel(detail?.finished_at),
          rawJSON: taskRawJSON(detail),
          pendingSeed,
        });
        if (isTerminalStatus(status)) {
          closeTaskStream(taskID);
          if (consoleTopicsEnabled.value && isWorkspaceCommandText(detail?.task)) {
            void refreshWorkspaceState();
          }
          scrollHistoryToBottom();
        }
        if (!isTerminalStatus(status)) {
          schedulePoll(async () => {
            await pollTask(taskID, historyID, endpointRef);
          });
        }
      } catch (e) {
        patchAgentHistoryItem(taskID, historyID, {
          status: "failed",
          text: e?.message || t("msg_load_failed"),
          rawJSON: "",
        });
      }
    }

    function resetTopicState() {
      topics.value = [];
      topicsLoading.value = false;
      selectedTopicID.value = "";
      creatingTopic.value = false;
      showSystemTopics.value = false;
      resetWorkspaceState();
      syncMobileTopicView({ preferTopics: true });
    }

    async function loadTopics(options = {}) {
      if (!consoleTopicsEnabled.value) {
        resetTopicState();
        return true;
      }
      const preferredTopicID = normalizeTopicID(options.preferredTopicID);
      const preserveDraft = Boolean(options.preserveDraft);
      const preserveSelection = Boolean(options.preserveSelection);

      topicsLoading.value = true;
      try {
        const data = await runtimeApiFetchForEndpoint(submitEndpointRef.value, "/topics");
        const items = Array.isArray(data?.items) ? [...data.items] : [];
        items.sort((left, right) => topicUpdatedAt(right) - topicUpdatedAt(left));
        topics.value = items;

        if (preferredTopicID && items.some((topic) => normalizeTopicID(topic?.id) === preferredTopicID)) {
          selectedTopicID.value = preferredTopicID;
          creatingTopic.value = false;
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (preserveDraft && creatingTopic.value) {
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        const currentID = normalizeTopicID(selectedTopicID.value);
        if (currentID && items.some((topic) => normalizeTopicID(topic?.id) === currentID)) {
          creatingTopic.value = false;
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (currentID === HEARTBEAT_TOPIC_ID && showSystemTopics.value) {
          creatingTopic.value = false;
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (!preserveSelection) {
          selectedTopicID.value = "";
          creatingTopic.value = false;
          syncMobileTopicView({ preferTopics: true });
        }
        return true;
      } catch (e) {
        err.value = e?.message || t("msg_load_failed");
        if (!preserveSelection) {
          selectedTopicID.value = "";
          creatingTopic.value = false;
          syncMobileTopicView({ preferTopics: true });
        }
        return false;
      } finally {
        topicsLoading.value = false;
      }
    }

    async function loadHistory(options = {}) {
      clearPollTimers();
      clearStreamSockets();
      err.value = "";
      const endpointRef = submitEndpointRef.value;
      if (!endpointRef) {
        replaceHistoryItems([]);
        return true;
      }
      historyLoading.value = true;
      const preserveCurrent = Boolean(options.preserveCurrent);
      try {
        let path = `/tasks?limit=${CHAT_HISTORY_LIMIT}`;
        if (consoleTopicsEnabled.value) {
          if (creatingTopic.value) {
            replaceHistoryItems([]);
            historyAutoStick.value = true;
            return true;
          }
          const topicID = normalizeTopicID(selectedTopicID.value);
          if (!topicID) {
            replaceHistoryItems([]);
            historyAutoStick.value = true;
            return true;
          }
          path = `/tasks?limit=${CHAT_HISTORY_LIMIT}&topic_id=${encodeURIComponent(topicID)}`;
        }

        const data = await runtimeApiFetchForEndpoint(endpointRef, path);
        const tasks = Array.isArray(data?.items) ? [...data.items] : [];
        tasks.sort((left, right) => taskCreatedAt(left) - taskCreatedAt(right));
        const nextItems = tasks.flatMap((task) =>
          taskHistoryItems(task, t, {
            agentName: activeAgentName.value,
          })
        );
        replaceHistoryItems(nextItems.length > 0 ? nextItems : [emptyHistoryItem()]);
        scrollHistoryToBottom({ force: true });
        for (const item of chatHistoryItems.value) {
          if (item.role === "agent" && item.taskId && !isTerminalStatus(item.status)) {
            void startTaskStream(item.taskId, item.id, endpointRef);
            schedulePoll(async () => {
              await pollTask(item.taskId, item.id, endpointRef);
            });
          }
        }
        return true;
      } catch (e) {
        if (!preserveCurrent) {
          replaceHistoryItems([]);
        }
        err.value = e?.message || t("msg_load_failed");
        return false;
      } finally {
        historyLoading.value = false;
      }
    }

    async function refreshChatData(options = {}) {
      if (consoleTopicsEnabled.value) {
        await loadTopics(options);
      } else {
        resetTopicState();
      }
      await loadHistory();
    }

    async function syncTopicFromRoute(options = {}) {
      if (!consoleTopicsEnabled.value) {
        return;
      }
      const topicID = routeTopicID.value;
      if (!topicID) {
        if (!options.force && !normalizeTopicID(selectedTopicID.value) && !creatingTopic.value) {
          return;
        }
        creatingTopic.value = false;
        selectedTopicID.value = "";
        syncMobileTopicView({ preferTopics: true });
        await loadHistory();
        return;
      }
      if (topicID === HEARTBEAT_TOPIC_ID) {
        showSystemTopics.value = true;
        creatingTopic.value = false;
        selectedTopicID.value = topicID;
        syncMobileTopicView({ preferChat: true });
        await loadHistory();
        return;
      }
      if (!options.force && normalizeTopicID(selectedTopicID.value) === topicID && !creatingTopic.value) {
        return;
      }
      creatingTopic.value = false;
      selectedTopicID.value = "";
      await loadTopics({
        preferredTopicID: topicID,
        preserveSelection: true,
      });
      const resolvedTopicID = normalizeTopicID(selectedTopicID.value);
      if (!resolvedTopicID) {
        syncMobileTopicView({ preferTopics: true });
        await loadHistory();
        return;
      }
      syncMobileTopicView({ preferChat: true });
      await loadHistory();
    }

    function openRawDialog(item) {
      resetRawReveal();
      rawDialogJSON.value = String(item?.rawJSON || "").trim();
      rawDialogOpen.value = rawDialogJSON.value !== "";
    }

    function closeRawDialog() {
      rawDialogOpen.value = false;
    }

    function resetRawReveal() {
      if (rawRevealTimerID) {
        window.clearTimeout(rawRevealTimerID);
        rawRevealTimerID = 0;
      }
      rawRevealItemID.value = "";
      rawRevealCount.value = 0;
    }

    function queueRawRevealReset() {
      if (rawRevealTimerID) {
        window.clearTimeout(rawRevealTimerID);
      }
      rawRevealTimerID = window.setTimeout(() => {
        resetRawReveal();
      }, 1200);
    }

    function clickHistoryTime(item) {
      if (String(item?.role || "") !== "agent") {
        return;
      }
      if (!String(item?.rawJSON || "").trim()) {
        return;
      }
      const itemID = String(item?.id || "").trim();
      if (!itemID) {
        return;
      }
      if (rawRevealItemID.value !== itemID) {
        rawRevealItemID.value = itemID;
        rawRevealCount.value = 0;
      }
      rawRevealCount.value += 1;
      if (rawRevealCount.value >= 5) {
        openRawDialog(item);
        return;
      }
      queueRawRevealReset();
    }

    function resetHeartbeatReveal() {
      if (heartbeatRevealTimerID) {
        window.clearTimeout(heartbeatRevealTimerID);
        heartbeatRevealTimerID = 0;
      }
      heartbeatRevealCount.value = 0;
    }

    function queueHeartbeatRevealReset() {
      if (heartbeatRevealTimerID) {
        window.clearTimeout(heartbeatRevealTimerID);
      }
      heartbeatRevealTimerID = window.setTimeout(() => {
        resetHeartbeatReveal();
      }, 1200);
    }

    function clickPageBarTitle() {
      heartbeatRevealCount.value += 1;
      if (heartbeatRevealCount.value >= 5) {
        showSystemTopics.value = !showSystemTopics.value;
        resetHeartbeatReveal();
        return;
      }
      queueHeartbeatRevealReset();
    }

    function selectTopic(topicID) {
      const normalized = normalizeTopicID(topicID);
      if (!normalized) {
        return;
      }
      creatingTopic.value = false;
      selectedTopicID.value = normalized;
      syncMobileTopicView({ preferChat: true });
      void loadHistory().finally(() => {
        focusComposer();
      });
      void syncChatRoute(normalized);
    }

    function startNewTopic() {
      creatingTopic.value = true;
      selectedTopicID.value = "";
      err.value = "";
      resetHeartbeatReveal();
      syncMobileTopicView({ preferChat: true });
      void loadHistory();
      syncComposerHeight();
      focusComposer();
      void syncChatRoute("", { replace: true });
    }

    async function submitTask() {
      const task = String(taskInput.value || "").trim();
      if (!task || sending.value) {
        return;
      }
      const endpointRef = submitEndpointRef.value;
      if (!endpointRef) {
        err.value = submitBlockedMessage.value || t("msg_select_endpoint");
        return;
      }
      const requestBody = { task };
      if (consoleTopicsEnabled.value && !creatingTopic.value) {
        const topicID = normalizeTopicID(selectedTopicID.value);
        if (topicID) {
          requestBody.topic_id = topicID;
        }
      }

      sending.value = true;
      err.value = "";
      taskInput.value = "";
      if (consoleTopicsEnabled.value && !normalizeTopicID(selectedTopicID.value)) {
        creatingTopic.value = true;
      }

      pushHistoryItem({
        role: "user",
        text: task,
        timeText: historyTimeLabel(new Date().toISOString()),
      });
      const pendingSeed = newHistoryID();
      const agentHistoryID = pushHistoryItem({
        role: "agent",
        text: buildPollingHint(activeAgentName.value, t, pendingSeed),
        status: "queued",
        timeText: "",
        pendingSeed,
      });
      scrollHistoryToBottom();

      try {
        const submitted = await runtimeApiFetchForEndpoint(endpointRef, "/tasks", {
          method: "POST",
          body: requestBody,
        });
        const taskID = String(submitted?.id || "").trim();
        const status = normalizeTaskStatus(submitted?.status);
        if (!taskID) {
          throw new Error(t("chat_missing_task_id"));
        }
        const existingAgentItem = chatHistoryItems.value.find((item) => item.id === agentHistoryID) || null;
        patchHistoryItem(agentHistoryID, {
          taskId: taskID,
          status,
          pendingSeed: historyPendingSeed(existingAgentItem, pendingSeed),
          rawJSON: "",
        });
        void startTaskStream(taskID, agentHistoryID, endpointRef);

        if (consoleTopicsEnabled.value) {
          const topicID = normalizeTopicID(submitted?.topic_id);
          if (!topicID) {
            throw new Error(t("chat_missing_topic_id"));
          }
          creatingTopic.value = false;
          selectedTopicID.value = topicID;
          await loadTopics({
            preferredTopicID: topicID,
            preserveSelection: true,
          });
          await syncChatRoute(topicID, { replace: true });
          await pollTask(taskID, agentHistoryID, endpointRef);
          return;
        }

        await pollTask(taskID, agentHistoryID, endpointRef);
      } catch (e) {
        const message = e?.message || t("msg_load_failed");
        err.value = message;
        patchHistoryItem(agentHistoryID, {
          status: "failed",
          text: message,
          rawJSON: "",
        });
      } finally {
        sending.value = false;
        syncComposerHeight();
        focusComposer();
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      focusComposer();
      void refreshChatData({
        preferredTopicID: routeTopicID.value,
        preserveSelection: Boolean(routeTopicID.value),
      }).finally(() => {
        focusComposer();
      });
      syncComposerHeight();
    });
    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
      clearPollTimers();
      clearStreamSockets();
      resetRawReveal();
      resetHeartbeatReveal();
    });
    watch(
      () => [endpointState.selectedRef, submitEndpointRef.value],
      () => {
        resetTopicState();
        void refreshChatData({
          preferredTopicID: routeTopicID.value,
          preserveSelection: Boolean(routeTopicID.value),
        }).finally(() => {
          focusComposer();
        });
        syncComposerHeight();
      }
    );
    watch(
      () => [submitEndpointRef.value, workspaceTopicID.value, consoleTopicsEnabled.value],
      () => {
        void refreshWorkspaceState();
      }
    );
    watch(
      () => workspaceSidebarOpen.value,
      (open) => {
        saveWorkspaceSidebarOpen(open);
        if (open && String(workspaceDir.value || "").trim() && !hasOwnTreePath(workspaceTreeItems.value, "")) {
          void loadWorkspaceTree("", { force: true });
        }
      }
    );
    watch(
      () => routeTopicID.value,
      () => {
        void syncTopicFromRoute().finally(() => {
          focusComposer();
        });
      }
    );
    watch(
      () => showChatPane.value,
      (visible) => {
        if (visible) {
          focusComposer();
        }
      }
    );
    watch(taskInput, () => {
      syncComposerHeight();
    });

    return {
      t,
      chatHistoryItems,
      historyLoading,
      historyViewport,
      topics,
      topicsLoading,
      visibleTopics,
      creatingTopic,
      taskInput,
      sending,
      err,
      workspaceDir,
      workspaceDirDisplay,
      workspaceLoading,
      workspaceSaving,
      workspaceOpening,
      workspaceBusy,
      workspaceSidebarOpen,
      workspaceSidebarTabID,
      workspacePanelTabs,
      selectedWorkspacePanelTab,
      workspaceError,
      workspaceReady,
      workspaceHintText,
      workspaceAttachDisabled,
      workspaceDetachDisabled,
      workspaceSidebarToggleLabel,
      workspaceTreeLoading,
      workspaceTreeLoadingPath,
      workspaceTreeError,
      workspaceTreeRows,
      workspaceSelectedTreeEntry,
      workspaceBrowserOpen,
      workspaceBrowserLoading,
      workspaceBrowserLoadingPath,
      workspaceBrowserError,
      workspaceBrowserRows,
      workspaceBrowserRecentItems,
      workspaceBrowserSelection,
      workspaceBrowserEmptyText,
      workspaceBrowserConfirmDisabled,
      formatBytes,
      workspaceTreeIcon,
      workspaceTreeEntryClass,
      composerField,
      submitBlockedMessage,
      chatReadonly,
      readonlyTitle,
      readonlyKickerLeft,
      readonlyReason,
      handleComposerPointerDown,
      pageClass,
      showChatPlaceholder,
      chatPlaceholderText,
      composerDisabled,
      sendDisabled,
      composerPlaceholder,
      displayAgentName,
      consoleTopicsEnabled,
      mobileTopicSplitEnabled,
      mobileBarTitle,
      mobileShowBack,
      shellClass,
      chatMainClass,
      topicSidebarKicker,
      deskTitle,
      deskMeta,
      chatPlaceholderHint,
      showTopicSidebar,
      showChatPane,
      workspaceSidebarAvailable,
      desktopWorkspaceSidebarVisible,
      submitTask,
      toggleWorkspaceSidebar,
      onWorkspaceTabChange,
      selectWorkspaceTreeNode,
      addWorkspaceSelectionToComposer,
      openWorkspaceSelection,
      toggleWorkspaceTreeNode,
      openWorkspaceBrowser,
      closeWorkspaceBrowser,
      activateWorkspaceBrowserSource,
      workspaceBrowserSourceItemClass,
      toggleWorkspaceBrowserNode,
      selectWorkspaceBrowserNode,
      attachWorkspace,
      detachWorkspace,
      selectTopic,
      startNewTopic,
      showTopicsView,
      topicTitle,
      topicTime,
      topicBadgeText,
      topicBadgeType,
      topicItemClass,
      topicIsActive,
      clickPageBarTitle,
      handleHistoryScroll,
      handleMarkdownRendered,
      historyItemRenderReady,
      historyClass,
      historySurfaceClass,
      markHistoryItemRendered,
      showHistorySkeleton,
      clickHistoryTime,
      openRawDialog,
      closeRawDialog,
      rawDialogOpen,
      rawDialogJSON,
    };
  },
  template: `
    <AppPage :title="t('chat_title')" :class="pageClass" :hideDesktopBar="true" :showMobileNavTrigger="!mobileShowBack">
      <template v-if="consoleTopicsEnabled" #leading>
        <div :class="mobileTopicSplitEnabled ? 'chat-page-bar-mobile' : 'chat-page-bar-desktop'">
          <QButton
            v-if="mobileShowBack"
            class="outlined xs icon chat-page-bar-back"
            :title="t('chat_topics_title')"
            :aria-label="t('chat_topics_title')"
            @click="showTopicsView"
          >
            <QIconArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title" @click="clickPageBarTitle">{{ mobileTopicSplitEnabled ? mobileBarTitle : t("chat_title") }}</h2>
        </div>
      </template>
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <section v-if="chatReadonly" class="chat-main is-readonly">
        <section class="chat-readonly">
          <AppKicker as="h3" class="chat-readonly-title" :left="readonlyKickerLeft" right="Read Only" />
          <p class="chat-readonly-text">{{ readonlyReason }}</p>
        </section>
      </section>
      <template v-else>
        <section :class="shellClass">
          <aside v-if="showTopicSidebar" class="chat-topic-sidebar workspace-sidebar-section">
            <header class="chat-topic-sidebar-head workspace-sidebar-head">
              <div class="chat-topic-sidebar-copy">
                <p class="ui-kicker chat-topic-sidebar-kicker" @click="clickPageBarTitle">{{ topicSidebarKicker }}</p>
                <div class="chat-topic-sidebar-title-row">
                  <h3 class="chat-topic-sidebar-title workspace-section-title">{{ t("chat_topics_title") }}</h3>
                </div>
                <p v-if="displayAgentName" class="chat-topic-sidebar-meta">{{ displayAgentName }}</p>
              </div>
              <QButton
                class="plain sm icon chat-topic-sidebar-new"
                :title="t('chat_topic_new')"
                :aria-label="t('chat_topic_new')"
                @click="startNewTopic"
              >
                <QIconPlus class="icon" />
              </QButton>
            </header>
            <p v-if="topicsLoading" class="muted chat-topic-loading">{{ t("chat_topics_loading") }}</p>
            <div :class="topicsLoading ? 'chat-topic-list workspace-sidebar-list is-busy' : 'chat-topic-list workspace-sidebar-list'">
              <button
                v-for="topic in visibleTopics"
                :key="topic.id"
                type="button"
                :class="topicItemClass(topic)"
                :aria-current="topicIsActive(topic) ? 'page' : undefined"
                @click="selectTopic(topic.id)"
              >
                <span class="chat-topic-item-copy workspace-sidebar-item-copy">
                  <span class="chat-topic-item-main">
                    <span class="chat-topic-item-title workspace-sidebar-item-title">{{ topicTitle(topic) }}</span>
                    <span v-if="topicTime(topic) || topicBadgeText(topic)" class="chat-topic-item-meta workspace-sidebar-item-meta">
                      <time v-if="topicTime(topic)" class="chat-topic-item-time">{{ topicTime(topic) }}</time>
                      <QBadge
                        v-if="topicBadgeText(topic)"
                        class="chat-topic-item-badge"
                        :type="topicBadgeType(topic)"
                        size="sm"
                      >
                        {{ topicBadgeText(topic) }}
                      </QBadge>
                    </span>
                  </span>
                </span>
                <span class="chat-topic-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                  <QBadge v-if="topicIsActive(topic)" dot type="primary" size="sm" />
                </span>
              </button>
            </div>
          </aside>
          <section v-if="showChatPane" :class="chatMainClass">
            <header v-if="consoleTopicsEnabled && !showChatPlaceholder" class="chat-desk-head">
              <div class="chat-desk-head-main">
                <div class="chat-desk-copy">
                  <p v-if="deskMeta" class="chat-desk-meta">{{ deskMeta }}</p>
                  <h3 class="chat-desk-title workspace-document-title">{{ deskTitle }}</h3>
                </div>
                <div v-if="workspaceSidebarAvailable" class="chat-desk-tools">
                  <QButton
                    :class="workspaceSidebarOpen ? 'plain sm icon chat-workspace-toggle is-active' : 'plain sm icon chat-workspace-toggle'"
                    :title="workspaceSidebarToggleLabel"
                    :aria-label="workspaceSidebarToggleLabel"
                    @click="toggleWorkspaceSidebar"
                  >
                    <QIconLayoutRight class="icon" />
                  </QButton>
                </div>
              </div>
            </header>
            <section v-if="showChatPlaceholder" class="chat-placeholder">
              <div class="chat-placeholder-copy">
                <h3 class="chat-placeholder-title workspace-document-title">{{ deskTitle }}</h3>
                <p class="chat-placeholder-note">{{ chatPlaceholderHint }}</p>
              </div>
              <div class="chat-composer chat-composer-landing" @pointerdown="handleComposerPointerDown">
                <QTextarea
                  ref="composerField"
                  v-model="taskInput"
                  :rows="1"
                  :disabled="composerDisabled"
                  :placeholder="composerPlaceholder"
                  @keydown.enter.exact.prevent="submitTask"
                />
                <div class="chat-composer-actions">
                  <QButton
                    class="primary chat-composer-send"
                    :loading="sending"
                    :disabled="sendDisabled"
                    shortcut="↵"
                    :title="t('chat_action_send') + ' (Enter)'"
                    :aria-label="t('chat_action_send') + ' (Enter)'"
                    @click="submitTask"
                  >
                    <span class="chat-composer-send-label">Send</span>
                  </QButton>
                </div>
              </div>
            </section>
            <template v-else>
              <div
                ref="historyViewport"
                class="chat-history"
                @scroll.passive="handleHistoryScroll"
              >
                <p v-if="historyLoading" class="muted">{{ t("chat_history_loading") }}</p>
                <article v-for="item in chatHistoryItems" :key="item.id" :class="historyClass(item)">
                  <code
                    v-if="item.timeText"
                    class="chat-history-status"
                    @click="clickHistoryTime(item)"
                  >
                    {{ item.timeText }}
                  </code>
                  <div :class="historySurfaceClass(item)">
                    <template v-if="item.role === 'agent'">
                      <div v-if="showHistorySkeleton(item)" class="chat-history-skeleton" aria-hidden="true">
                        <QSkeleton variant="text" width="92%" />
                        <QSkeleton variant="text" width="100%" />
                        <QSkeleton variant="text" width="68%" />
                      </div>
                      <MarkdownContent
                        :class="showHistorySkeleton(item) ? 'chat-history-markdown is-render-pending' : 'chat-history-markdown'"
                        :source="item.text"
                        format="auto"
                        theme="blueprint"
                        @rendered="markHistoryItemRendered(item.id)"
                      />
                    </template>
                    <div v-else class="chat-history-body">{{ item.text }}</div>
                  </div>
                </article>
                <p v-if="chatHistoryItems.length === 0 && !historyLoading" class="muted">{{ t("chat_empty") }}</p>
              </div>
            </template>
            <div v-if="!showChatPlaceholder" class="chat-composer" @pointerdown="handleComposerPointerDown">
              <QTextarea
                ref="composerField"
                v-model="taskInput"
                :rows="1"
                :disabled="composerDisabled"
                :placeholder="composerPlaceholder"
                @keydown.enter.exact.prevent="submitTask"
              >
                <template #append>
                  <QButton
                    class="primary chat-composer-send"
                    :loading="sending"
                    :disabled="sendDisabled"
                    shortcut="↵"
                    :title="t('chat_action_send') + ' (Enter)'"
                    :aria-label="t('chat_action_send') + ' (Enter)'"
                    @click="submitTask"
                  >
                    <span class="chat-composer-send-label">Send</span>
                  </QButton>
                </template>
              </QTextarea>
            </div>
          </section>
          <aside
            v-if="desktopWorkspaceSidebarVisible"
            class="chat-workspace-sidebar workspace-sidebar-section"
            :aria-label="t('chat_workspace_label')"
          >
            <div class="chat-workspace-sidebar-shell">
              <QTabs
                class="chat-workspace-tabs"
                :tabs="workspacePanelTabs"
                :modelValue="selectedWorkspacePanelTab"
                variant="plain"
                @change="onWorkspaceTabChange"
              />

              <div class="chat-workspace-pane ui-track-panel">
                <template v-if="workspaceReady">
                  <template v-if="workspaceDir">
                    <header class="chat-workspace-toolbar">
                      <div class="chat-workspace-pane-copy">
                        <p class="chat-workspace-pane-label ui-kicker">{{ t("chat_workspace_label") }}</p>
                        <code class="chat-workspace-pane-path" :title="workspaceDir">
                          <span
                            v-if="workspaceDirDisplay.prefix"
                            class="chat-workspace-pane-path-prefix"
                          >
                            {{ workspaceDirDisplay.prefix }}
                          </span>
                          <span
                            v-if="workspaceDirDisplay.separator"
                            class="chat-workspace-pane-path-separator"
                            aria-hidden="true"
                          >
                            {{ workspaceDirDisplay.separator }}
                          </span>
                          <span class="chat-workspace-pane-path-tail">{{ workspaceDirDisplay.tail }}</span>
                        </code>
                        <p v-if="workspaceHintText" class="chat-workspace-pane-note">{{ workspaceHintText }}</p>
                      </div>

                      <div class="chat-workspace-toolbar-actions">
                        <QButton
                          class="plain xs icon"
                          :title="t('chat_workspace_action_attach')"
                          :aria-label="t('chat_workspace_action_attach')"
                          :disabled="workspaceAttachDisabled"
                          @click="openWorkspaceBrowser"
                        >
                          <QIconPlus class="icon" />
                        </QButton>
                        <QButton
                          class="plain xs icon"
                          :title="t('chat_workspace_action_detach')"
                          :aria-label="t('chat_workspace_action_detach')"
                          :disabled="workspaceDetachDisabled"
                          :loading="workspaceSaving"
                          @click="detachWorkspace"
                        >
                          <QIconTrash class="icon" />
                        </QButton>
                      </div>
                    </header>

                    <QFence
                      v-if="workspaceError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="QIconCloseCircle"
                      :text="workspaceError"
                    />

                    <QFence
                      v-if="workspaceTreeError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="QIconCloseCircle"
                      :text="workspaceTreeError"
                    />

                    <div class="chat-workspace-tree-shell">
                      <p
                        v-if="workspaceTreeLoading && workspaceTreeRows.length === 0"
                        class="chat-workspace-tree-status"
                      >
                        {{ t("chat_workspace_tree_loading") }}
                      </p>
                      <div v-else-if="workspaceTreeRows.length > 0" class="chat-workspace-tree-list">
                        <div
                          v-for="row in workspaceTreeRows"
                          :key="'workspace:' + row.key"
                          class="chat-workspace-tree-row"
                          :style="{ '--tree-depth': row.depth }"
                        >
                          <button
                            type="button"
                            :class="workspaceTreeEntryClass(row)"
                            :title="row.entry.path"
                            @click="selectWorkspaceTreeNode(row)"
                          >
                            <span class="chat-workspace-tree-kind" aria-hidden="true">
                              <img class="chat-workspace-tree-icon" :src="workspaceTreeIcon(row.entry, row.expanded)" alt="" />
                            </span>
                            <span class="chat-workspace-tree-name">{{ row.entry.name }}</span>
                          </button>
                        </div>
                      </div>
                      <p v-else class="chat-workspace-tree-status">{{ t("chat_workspace_tree_empty") }}</p>
                    </div>

                    <footer v-if="workspaceSelectedTreeEntry" class="chat-workspace-status">
                      <div class="chat-workspace-status-head">
                        <p class="chat-workspace-status-title">{{ workspaceSelectedTreeEntry.name }}</p>
                        <span class="chat-workspace-status-kind ui-kicker">
                          {{
                            workspaceSelectedTreeEntry.is_dir
                              ? t("chat_workspace_kind_dir")
                              : t("chat_workspace_kind_file")
                          }}
                        </span>
                      </div>

                      <dl class="chat-workspace-status-grid">
                        <div class="chat-workspace-status-row">
                          <dt class="chat-workspace-status-term">{{ t("audit_size") }}</dt>
                          <dd class="chat-workspace-status-value">
                            {{ formatBytes(workspaceSelectedTreeEntry.size_bytes) }}
                          </dd>
                        </div>
                        <div class="chat-workspace-status-row">
                          <dt class="chat-workspace-status-term">{{ t("audit_action") }}</dt>
                          <dd class="chat-workspace-status-actions">
                            <QButton
                              class="plain xs icon"
                              :title="t('chat_workspace_action_insert')"
                              :aria-label="t('chat_workspace_action_insert')"
                              :disabled="composerDisabled"
                              @click="addWorkspaceSelectionToComposer"
                            >
                              <QIconPlus class="icon" />
                            </QButton>
                            <QButton
                              class="plain xs icon"
                              :title="t('chat_workspace_action_open')"
                              :aria-label="t('chat_workspace_action_open')"
                              :loading="workspaceOpening"
                              @click="openWorkspaceSelection"
                            >
                              <QIconLinkExternal class="icon" />
                            </QButton>
                          </dd>
                        </div>
                      </dl>
                    </footer>
                  </template>

                  <template v-else>
                    <QFence
                      v-if="workspaceError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="QIconCloseCircle"
                      :text="workspaceError"
                    />

                    <div class="chat-workspace-empty-state">
                      <div class="chat-workspace-empty-lead">
                        <p class="chat-workspace-empty-title">{{ t("chat_workspace_empty_title") }}</p>
                      </div>
                      <div class="chat-workspace-empty-actions">
                        <QButton
                          class="primary sm"
                          :disabled="workspaceAttachDisabled"
                          @click="openWorkspaceBrowser"
                        >
                          {{ t("chat_workspace_action_attach") }}
                        </QButton>
                      </div>
                    </div>
                  </template>
                </template>

                <div v-else class="chat-workspace-empty-state is-disabled">
                  <div class="chat-workspace-empty-lead">
                    <p class="chat-workspace-empty-title">{{ t("chat_workspace_unavailable_title") }}</p>
                    <p v-if="workspaceHintText" class="chat-workspace-empty-copy">{{ workspaceHintText }}</p>
                  </div>
                </div>
              </div>
            </div>
          </aside>
        </section>
        <QDrawer
          :modelValue="mobileMode && workspaceSidebarAvailable && workspaceSidebarOpen"
          placement="right"
          size="min(88vw, 360px)"
          :closable="false"
          :showMask="true"
          :maskClosable="true"
          :lockScroll="true"
          @update:modelValue="!$event && toggleWorkspaceSidebar()"
          @close="workspaceSidebarOpen = false"
        >
          <div class="chat-workspace-sidebar-shell chat-workspace-sidebar-shell-mobile">
            <QTabs
              class="chat-workspace-tabs"
              :tabs="workspacePanelTabs"
              :modelValue="selectedWorkspacePanelTab"
              variant="plain"
              @change="onWorkspaceTabChange"
            />

            <div class="chat-workspace-pane ui-track-panel">
              <template v-if="workspaceReady">
                <template v-if="workspaceDir">
                  <header class="chat-workspace-toolbar">
                    <div class="chat-workspace-pane-copy">
                      <p class="chat-workspace-pane-label ui-kicker">{{ t("chat_workspace_label") }}</p>
                      <code class="chat-workspace-pane-path" :title="workspaceDir">
                        <span
                          v-if="workspaceDirDisplay.prefix"
                          class="chat-workspace-pane-path-prefix"
                        >
                          {{ workspaceDirDisplay.prefix }}
                        </span>
                        <span
                          v-if="workspaceDirDisplay.separator"
                          class="chat-workspace-pane-path-separator"
                          aria-hidden="true"
                        >
                          {{ workspaceDirDisplay.separator }}
                        </span>
                        <span class="chat-workspace-pane-path-tail">{{ workspaceDirDisplay.tail }}</span>
                      </code>
                      <p v-if="workspaceHintText" class="chat-workspace-pane-note">{{ workspaceHintText }}</p>
                    </div>

                    <div class="chat-workspace-toolbar-actions">
                      <QButton
                        class="plain xs icon"
                        :title="t('chat_workspace_action_attach')"
                        :aria-label="t('chat_workspace_action_attach')"
                        :disabled="workspaceAttachDisabled"
                        @click="openWorkspaceBrowser"
                      >
                        <QIconPlus class="icon" />
                      </QButton>
                      <QButton
                        class="plain xs icon"
                        :title="t('chat_workspace_action_detach')"
                        :aria-label="t('chat_workspace_action_detach')"
                        :disabled="workspaceDetachDisabled"
                        :loading="workspaceSaving"
                        @click="detachWorkspace"
                      >
                        <QIconTrash class="icon" />
                      </QButton>
                    </div>
                  </header>

                  <QFence
                    v-if="workspaceError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="workspaceError"
                  />

                  <QFence
                    v-if="workspaceTreeError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="workspaceTreeError"
                  />

                  <div class="chat-workspace-tree-shell">
                    <p
                      v-if="workspaceTreeLoading && workspaceTreeRows.length === 0"
                      class="chat-workspace-tree-status"
                    >
                      {{ t("chat_workspace_tree_loading") }}
                    </p>
                    <div v-else-if="workspaceTreeRows.length > 0" class="chat-workspace-tree-list">
                      <div
                        v-for="row in workspaceTreeRows"
                        :key="'workspace-mobile:' + row.key"
                        class="chat-workspace-tree-row"
                        :style="{ '--tree-depth': row.depth }"
                      >
                        <button
                          type="button"
                          :class="workspaceTreeEntryClass(row)"
                          :title="row.entry.path"
                          @click="selectWorkspaceTreeNode(row)"
                        >
                          <span class="chat-workspace-tree-kind" aria-hidden="true">
                            <img class="chat-workspace-tree-icon" :src="workspaceTreeIcon(row.entry, row.expanded)" alt="" />
                          </span>
                          <span class="chat-workspace-tree-name">{{ row.entry.name }}</span>
                        </button>
                      </div>
                    </div>
                    <p v-else class="chat-workspace-tree-status">{{ t("chat_workspace_tree_empty") }}</p>
                  </div>

                  <footer v-if="workspaceSelectedTreeEntry" class="chat-workspace-status">
                    <div class="chat-workspace-status-head">
                      <p class="chat-workspace-status-title">{{ workspaceSelectedTreeEntry.name }}</p>
                      <span class="chat-workspace-status-kind ui-kicker">
                        {{
                          workspaceSelectedTreeEntry.is_dir
                            ? t("chat_workspace_kind_dir")
                            : t("chat_workspace_kind_file")
                        }}
                      </span>
                    </div>

                    <dl class="chat-workspace-status-grid">
                      <div class="chat-workspace-status-row">
                        <dt class="chat-workspace-status-term">{{ t("audit_size") }}</dt>
                        <dd class="chat-workspace-status-value">
                          {{ formatBytes(workspaceSelectedTreeEntry.size_bytes) }}
                        </dd>
                      </div>
                      <div class="chat-workspace-status-row">
                        <dt class="chat-workspace-status-term">{{ t("audit_action") }}</dt>
                        <dd class="chat-workspace-status-actions">
                          <QButton
                            class="plain xs icon"
                            :title="t('chat_workspace_action_insert')"
                            :aria-label="t('chat_workspace_action_insert')"
                            :disabled="composerDisabled"
                            @click="addWorkspaceSelectionToComposer"
                          >
                            <QIconPlus class="icon" />
                          </QButton>
                          <QButton
                            class="plain xs icon"
                            :title="t('chat_workspace_action_open')"
                            :aria-label="t('chat_workspace_action_open')"
                            :loading="workspaceOpening"
                            @click="openWorkspaceSelection"
                          >
                            <QIconLinkExternal class="icon" />
                          </QButton>
                        </dd>
                      </div>
                    </dl>
                  </footer>
                </template>

                <template v-else>
                  <QFence
                    v-if="workspaceError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="workspaceError"
                  />

                  <div class="chat-workspace-empty-state">
                    <div class="chat-workspace-empty-lead">
                      <p class="chat-workspace-empty-title">{{ t("chat_workspace_empty_title") }}</p>
                    </div>
                    <div class="chat-workspace-empty-actions">
                      <QButton
                        class="primary sm"
                        :disabled="workspaceAttachDisabled"
                        @click="openWorkspaceBrowser"
                      >
                        {{ t("chat_workspace_action_attach") }}
                      </QButton>
                    </div>
                  </div>
                </template>
              </template>

              <div v-else class="chat-workspace-empty-state is-disabled">
                <div class="chat-workspace-empty-lead">
                  <p class="chat-workspace-empty-title">{{ t("chat_workspace_unavailable_title") }}</p>
                  <p v-if="workspaceHintText" class="chat-workspace-empty-copy">{{ workspaceHintText }}</p>
                </div>
              </div>
            </div>
          </div>
        </QDrawer>
        <QDialog
          :modelValue="workspaceBrowserOpen"
          width="720px"
          @update:modelValue="!$event && closeWorkspaceBrowser()"
          @close="closeWorkspaceBrowser"
        >
          <template #header>
            <header class="chat-workspace-dialog-head">
              <h3 class="chat-workspace-dialog-title">{{ t("chat_workspace_dialog_title") }}</h3>
            </header>
          </template>

          <section class="chat-workspace-dialog">
            <QFence
              v-if="workspaceBrowserError"
              class="chat-workspace-pane-fence"
              type="danger"
              icon="QIconCloseCircle"
              :text="workspaceBrowserError"
            />

            <div class="chat-workspace-dialog-shell">
              <aside class="chat-workspace-dialog-sidebar workspace-sidebar-section">
                <section class="chat-workspace-dialog-sidebar-group">
                  <p class="chat-workspace-dialog-sidebar-title ui-kicker">{{ t("chat_workspace_dialog_places") }}</p>
                  <div class="chat-workspace-dialog-sidebar-list workspace-sidebar-list">
                    <button
                      type="button"
                      :class="workspaceBrowserSourceItemClass('recent')"
                      @click="activateWorkspaceBrowserSource('recent')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_recent") }}</span>
                      </span>
                    </button>
                    <button
                      type="button"
                      :class="workspaceBrowserSourceItemClass('home')"
                      @click="activateWorkspaceBrowserSource('home')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_home") }}</span>
                      </span>
                    </button>
                    <button
                      type="button"
                      :class="workspaceBrowserSourceItemClass('system')"
                      @click="activateWorkspaceBrowserSource('system')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_system") }}</span>
                      </span>
                    </button>
                  </div>
                </section>
              </aside>

              <div class="chat-workspace-dialog-main">
                <div class="chat-workspace-browser-shell">
                  <p
                    v-if="workspaceBrowserLoading && workspaceBrowserRows.length === 0"
                    class="chat-workspace-tree-status"
                  >
                    {{ t("chat_workspace_dialog_loading") }}
                  </p>
                  <div v-else-if="workspaceBrowserRows.length > 0" class="chat-workspace-tree-list is-browser">
                    <div
                      v-for="row in workspaceBrowserRows"
                      :key="'browser:' + row.key"
                      class="chat-workspace-tree-row"
                      :style="{ '--tree-depth': row.depth }"
                    >
                      <button
                      type="button"
                      :class="workspaceBrowserSelection === row.entry.path
                          ? 'chat-workspace-tree-entry is-selectable is-selected is-actionable'
                          : 'chat-workspace-tree-entry is-selectable is-actionable'"
                        :disabled="!row.entry.is_dir"
                        :title="row.entry.path"
                        @click="selectWorkspaceBrowserNode(row)"
                      >
                        <span class="chat-workspace-tree-kind" aria-hidden="true">
                          <img class="chat-workspace-tree-icon" :src="workspaceTreeIcon(row.entry, row.expanded)" alt="" />
                        </span>
                        <span class="chat-workspace-tree-name">{{ row.entry.name }}</span>
                      </button>
                    </div>
                  </div>
                  <p v-else class="chat-workspace-tree-status">{{ workspaceBrowserEmptyText }}</p>
                </div>
              </div>

              <div class="chat-workspace-dialog-actions">
                <QButton
                  class="plain sm"
                  :disabled="workspaceSaving"
                  @click="closeWorkspaceBrowser"
                >
                  {{ t("action_cancel") }}
                </QButton>
                <QButton
                  class="primary sm"
                  :loading="workspaceSaving"
                  :disabled="workspaceBrowserConfirmDisabled"
                  @click="attachWorkspace"
                >
                  {{ t("chat_workspace_action_attach") }}
                </QButton>
              </div>
            </div>
          </section>
        </QDialog>
        <RawJsonDialog
          :open="rawDialogOpen"
          :json="rawDialogJSON"
          @close="closeRawDialog"
        />
      </template>
    </AppPage>
  `,
};

export default ChatView;
