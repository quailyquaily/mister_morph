import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useToast } from "quail-ui";
import "./ChatView.css";

import AppKicker from "../components/AppKicker";
import AppPage from "../components/AppPage";
import ChatComposer from "../components/ChatComposer";
import ChatHistoryList from "../components/ChatHistoryList";
import { approvalDetailsByID, taskApprovalState } from "../core/chat-approvals";
import {
  buildComposerSubmission,
  composerFileDraftKey,
  composerFileExtension,
} from "../core/chat-composer-files";
import { chatDraft, clearChatDraft, rememberChatDraft } from "../core/chat-draft-memory";
import { normalizeComposerCommandItems, normalizeComposerSkillItems } from "../core/chat-composer-suggestions";
import {
  lastUsedChatLLMProfile,
  normalizeChatLLMProfileMetadata,
  normalizeChatLLMProfiles,
  resolveAvailableChatLLMProfile,
} from "../core/chat-llm-profiles";
import { placeSteeredAgentsAfterUsers } from "../core/chat-history-steering";
import { rememberLastTopicID } from "../core/chat-topic-memory";
import {
  agentDisplayName,
  buildPollingHint,
  chatApprovalReasonText,
  historyPendingSeed,
  historyTimeLabel as formatChatHistoryTime,
  isContextCompactCommand,
  isTerminalStatus,
  normalizeActivity,
  normalizeHistoryFileReferences,
  normalizePlan,
  normalizeReasoning,
  normalizeTaskStatus,
  normalizeTopicID,
  taskActivity,
  taskAgentText,
  taskDurationLabel,
  taskListHistoryItems,
  taskPlan,
  taskRawJSON,
  taskReasoning,
} from "../core/chat-task-history";
import { openRawJsonDesktopWindow } from "../core/desktop-windows";
import { endpointChannelLabel } from "../core/endpoints";
import { endpointRoutePath } from "../core/endpoint-routes";
import { modelVendorMeta } from "../core/model-vendor";
import { loadResource, resourceKey } from "../core/resources";
import { workspaceTreeIcon } from "../core/workspace-icons";
import {
  apiFetch,
  buildConsoleStreamURL,
  createConsoleStreamTicket,
  currentLocale,
  endpointState,
  formatBytes,
  formatTime,
  runtimeApiDownloadForEndpoint,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  safeJSON,
  supportsConsoleTaskStream,
  translate,
} from "../core/context";

const POLL_INTERVAL_MS = 1200;
const CHAT_HISTORY_LIMIT = 100;
const TOPIC_PAGE_LIMIT = 100;
const CHAT_COMPOSER_MIN_HEIGHT = 80;
const DEFAULT_TOPIC_ID = "default";
const AWARENESS_TOPIC_ID = "_awareness";
const LOCAL_CONSOLE_ENDPOINT_REF = "ep_console_local";
const RECENT_WORKSPACE_DIRS_STORAGE_KEY = "mistermorph_console_recent_workspaces_v1";
const WORKSPACE_SIDEBAR_OPEN_STORAGE_KEY = "mistermorph_console_workspace_sidebar_open_v1";
const RECENT_WORKSPACE_DIRS_LIMIT = 32;
const WORKSPACE_BROWSER_SOURCE_RECENT = "recent";
const WORKSPACE_BROWSER_SOURCE_HOME = "home";
const WORKSPACE_BROWSER_SOURCE_SYSTEM = "system";
const WORKSPACE_BROWSER_SOURCE_STATE_DIR = "state_dir";
const WORKSPACE_BROWSER_SOURCE_CACHE_DIR = "cache_dir";
const COMPOSER_FILE_IMAGE_EXTENSIONS = new Set([
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".avif",
  ".bmp",
  ".ico",
]);
const loadAppDialogShell = () => import("../components/AppDialogShell");
const loadRawJsonDialog = () => import("../components/RawJsonDialog");
const AppDialogShell = defineAsyncComponent(loadAppDialogShell);
const RawJsonDialog = defineAsyncComponent(loadRawJsonDialog);

function normalizeEndpointMode(raw) {
  return String(raw || "").trim().toLowerCase();
}

function rememberTopicSelection(endpointRef, topicID) {
  const normalizedTopicID = normalizeTopicID(topicID);
  if (!normalizedTopicID || normalizedTopicID === AWARENESS_TOPIC_ID) {
    return;
  }
  rememberLastTopicID(endpointRef, normalizedTopicID);
}

function scheduleIdleCallback(callback, timeout = 2000) {
  if (typeof window.requestIdleCallback === "function") {
    const handle = window.requestIdleCallback(callback, { timeout });
    return () => window.cancelIdleCallback(handle);
  }
  const handle = window.setTimeout(callback, 160);
  return () => window.clearTimeout(handle);
}

function composerDraftTopicID(consoleTopicsEnabled, creatingTopic, selectedTopicID, routeTopicID) {
  if (!consoleTopicsEnabled || creatingTopic) {
    return "";
  }
  const normalizedSelectedTopicID = normalizeTopicID(selectedTopicID);
  if (normalizedSelectedTopicID) {
    return normalizedSelectedTopicID;
  }
  return normalizeTopicID(routeTopicID);
}

function hasArtifactBlock(raw) {
  return /(^|\n)(`{3,}|~{3,})[ \t]*artifact[^\n]*\n/iu.test(String(raw || ""));
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
const TOPIC_TAB_ID = "topic";

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

function workspaceBrowserSource(sourceID, stateDir = "", cacheDir = "") {
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
  const statePath = String(stateDir || "").trim();
  if (value === WORKSPACE_BROWSER_SOURCE_STATE_DIR && statePath) {
    return {
      id: WORKSPACE_BROWSER_SOURCE_STATE_DIR,
      kind: "place",
      path: statePath,
      selection: statePath,
    };
  }
  const cachePath = String(cacheDir || "").trim();
  if (value === WORKSPACE_BROWSER_SOURCE_CACHE_DIR && cachePath) {
    return {
      id: WORKSPACE_BROWSER_SOURCE_CACHE_DIR,
      kind: "place",
      path: cachePath,
      selection: cachePath,
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

function workspaceDownloadFilename(entry) {
  const name = String(entry?.name || "").trim().replace(/[\\/]+/gu, "_");
  return name || "download";
}

function triggerBrowserDownload(blob, filename) {
  const objectURL = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = objectURL;
  link.download = String(filename || "").trim() || "download";
  link.rel = "noopener";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);
}

function topicUpdatedAt(topic) {
  const value = Date.parse(String(topic?.updated_at || topic?.created_at || "").trim());
  return Number.isFinite(value) ? value : 0;
}

function compareTopicsNewestFirst(left, right) {
  const timeDelta = topicUpdatedAt(right) - topicUpdatedAt(left);
  if (timeDelta !== 0) {
    return timeDelta;
  }
  const leftID = normalizeTopicID(left?.id);
  const rightID = normalizeTopicID(right?.id);
  if (leftID === rightID) {
    return 0;
  }
  return leftID < rightID ? 1 : -1;
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

function formatTokenCount(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) {
    return "-";
  }
  return Math.round(n).toLocaleString(currentLocale());
}

function formatUsageRatio(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) {
    return "-";
  }
  return `${(n * 100).toFixed(1)}%`;
}

function topicContextUsageRatio(context) {
  const usedInputTokens = Number(context?.used_input_tokens);
  const contextWindowTokens = Number(context?.context_window_tokens);
  if (
    Number.isFinite(usedInputTokens) &&
    usedInputTokens >= 0 &&
    Number.isFinite(contextWindowTokens) &&
    contextWindowTokens > 0
  ) {
    return usedInputTokens / contextWindowTokens;
  }
  const ratio = Number(context?.usage_ratio);
  return Number.isFinite(ratio) && ratio >= 0 ? ratio : null;
}

function historyTimeLabel(raw) {
  return formatChatHistoryTime(raw, currentLocale());
}

function applyDefaultHistoryDurationVisibility(items) {
  const list = Array.isArray(items) ? items : [];
  let lastAgentIndex = -1;
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const item = list[i];
    if (String(item?.role || "") === "agent" && String(item?.durationText || "").trim()) {
      lastAgentIndex = i;
      break;
    }
  }
  return list.map((item, index) => {
    const defaultDurationVisible = index === lastAgentIndex;
    if (item?.durationVisibleManual === true) {
      return {
        ...item,
        durationVisibleManual: true,
        durationVisible: item?.durationVisible === true,
      };
    }
    return {
      ...item,
      durationVisibleManual: false,
      durationVisible: defaultDurationVisible,
    };
  });
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
	case "mixin":
	  return "Mixin";
    default:
      return "Endpoint";
  }
}

const WorkspaceBrowserRecentItem = {
  props: {
    name: {
      type: String,
      required: true,
    },
    path: {
      type: String,
      required: true,
    },
  },
  template: `
    <span class="chat-workspace-recent-item">
      <span class="chat-workspace-recent-item-name">{{ name }}</span>
      <span class="chat-workspace-recent-item-path">{{ path }}</span>
    </span>
  `,
};

const ChatView = {
  components: {
    AppDialogShell,
    AppKicker,
    AppPage,
    ChatComposer,
    ChatHistoryList,
    RawJsonDialog,
    WorkspaceBrowserRecentItem,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const route = useRoute();
    const router = useRouter();
    const mobileMode = ref(window.innerWidth <= 920);
    const mobileTopicView = ref(
      mobileMode.value && !normalizeTopicID(route.params.topic_id) ? "topics" : "chat",
    );
    const chatHistoryItems = shallowRef([]);
    const copiedHistoryItemID = ref("");
    const historyLoading = ref(false);
    const historyLoadingOlder = ref(false);
    const historyNextCursor = ref("");
    const historyViewport = ref(null);
    const topics = shallowRef([]);
    const topicsLoading = ref(false);
    const topicsLoadingOlder = ref(false);
    const topicsNextCursor = ref("");
    const selectedTopicID = ref("");
    const creatingTopic = ref(false);
    const showSystemTopics = ref(false);
    const topicDeleteDialogOpen = ref(false);
    const topicDeleteTarget = ref(null);
    const topicDeleting = ref(false);
    const topicDeleteError = ref("");
    const taskInput = ref("");
    const sending = ref(false);
    const err = ref("");
    const workspaceDir = ref("");
    const workspaceSource = ref("none");
    const workspaceLoading = ref(false);
    const workspaceSaving = ref(false);
    const workspaceOpening = ref(false);
    const workspaceDownloading = ref(false);
    const composerUploading = ref(false);
    const composerFileInput = ref(null);
    const composerFiles = shallowRef([]);
    const composerFilePreviewOpen = ref(false);
    const composerFilePreviewLoading = ref(false);
    const composerFilePreviewID = ref("");
    const composerFilePreviewName = ref("");
    const composerFilePreviewKind = ref("");
    const composerFilePreviewURL = ref("");
    const composerFilePreviewText = ref("");
    const composerFilePreviewError = ref("");
    const composerFilePreviewItems = shallowRef([]);
    const composerFilePreviewIndex = ref(-1);
    const workspaceError = ref("");
    const topicContext = ref(null);
    const workspaceSidebarOpen = ref(loadWorkspaceSidebarOpen());
    const workspaceSidebarTabID = ref(TOPIC_TAB_ID);
    const workspaceTreeItems = shallowRef({});
    const workspaceTreeExpanded = ref({ "": true });
    const workspaceTreeLoading = ref(false);
    const workspaceTreeLoadingPath = ref("");
    const workspaceTreeError = ref("");
    const workspaceTreeSelectionPath = ref("");
    const workspaceBrowserOpen = ref(false);
    const workspaceBrowserItems = shallowRef({});
    const workspaceBrowserExpanded = ref({ "": true });
    const workspaceBrowserLoading = ref(false);
    const workspaceBrowserLoadingPath = ref("");
    const workspaceBrowserError = ref("");
    const workspaceBrowserSourceID = ref(WORKSPACE_BROWSER_SOURCE_HOME);
    const workspaceBrowserRecentDirs = ref(loadRecentWorkspaceDirs());
    const workspaceBrowserStateDir = ref("");
    const workspaceBrowserCacheDir = ref("");
    const workspaceBrowserSelection = ref("");
    const workspaceBrowserShowHidden = ref(false);
    const workspaceBrowserPendingMode = ref(false);
    const workspaceBrowserCreateOpen = ref(false);
    const workspaceBrowserCreateName = ref("");
    const workspaceBrowserCreating = ref(false);
    const workspaceBrowserCreateField = ref(null);
    const pendingWorkspaceDir = ref("");
    const composerFileDrafts = new Map();
    const pollTimers = new Set();
    const streamSockets = new Map();
    const approvalDetailAttempts = new Set();
    const composerRef = ref(null);
    const mobileWorkspaceSidebarPanel = ref(null);
    const composerHeight = ref(CHAT_COMPOSER_MIN_HEIGHT);
    const composerCommands = shallowRef([]);
    const composerCommandsLoading = ref(false);
    const composerDefaultLLMProfile = shallowRef(null);
    const composerLLMProfiles = shallowRef([]);
    const composerTopicLLMProfile = ref("");
    const composerLLMProfile = ref("");
    const composerSkills = shallowRef([]);
    const composerSkillsLoading = ref(false);
    const composerSkillsError = ref("");
    const suppressDraftPersistence = ref(false);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const rawRevealItemID = ref("");
    const rawRevealCount = ref(0);
    const heartbeatRevealCount = ref(0);
    const chatStatusExpandedState = ref({});
    const historyAutoStick = ref(true);
    let dialogShellPreloadCancel = null;
    let historyUserScrollIntentAt = 0;
    let rawRevealTimerID = 0;
    let heartbeatRevealTimerID = 0;
    let copiedHistoryTimerID = 0;
    let viewActive = true;
    let mobileWorkspaceFocusBeforeOpen = null;
    let mobileWorkspaceBodyOverflowBeforeOpen = "";
    let mobileWorkspaceScrollLocked = false;
    let historyLoadVersion = 0;
    let composerCommandsLoadSeq = 0;
    let composerLLMProfilesLoadSeq = 0;
    let composerSkillsLoadSeq = 0;
    let composerFileSequence = 0;
    let composerFilePreviewSequence = 0;
    let composerFilePreviewObjectURL = "";

    const composerFilePreviewHasPrevious = computed(() => composerFilePreviewIndex.value > 0);
    const composerFilePreviewHasNext = computed(
      () =>
        composerFilePreviewIndex.value >= 0 &&
        composerFilePreviewIndex.value < composerFilePreviewItems.value.length - 1
    );

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
    const composerDraftScope = computed(() => ({
      endpointRef: String(submitEndpointRef.value || "").trim(),
      topicID: composerDraftTopicID(
        consoleTopicsEnabled.value,
        creatingTopic.value,
        selectedTopicID.value,
        routeTopicID.value
      ),
    }));
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
    const activeTaskItem = computed(() => {
      for (let index = chatHistoryItems.value.length - 1; index >= 0; index -= 1) {
        const item = chatHistoryItems.value[index];
        if (
          String(item?.role || "").trim().toLowerCase() === "agent" &&
          String(item?.taskId || "").trim() !== "" &&
          !isTerminalStatus(normalizeTaskStatus(item?.status))
        ) {
          return item;
        }
      }
      return null;
    });
    const composerHasInput = computed(() => String(taskInput.value || "").trim() !== "");
    const composerStopMode = computed(() => Boolean(activeTaskItem.value) && !composerHasInput.value);
    const composerDisabled = computed(() => Boolean(submitBlockedMessage.value) || sending.value);
    const sendDisabled = computed(
      () =>
        composerDisabled.value ||
        (!composerStopMode.value && (composerUploading.value || !composerHasInput.value))
    );
    const composerActionLabel = computed(() => {
      if (composerStopMode.value) {
        return t("chat_action_stop");
      }
      return mobileMode.value ? t("chat_action_send") : `${t("chat_action_send")} (Enter)`;
    });
    const composerPlaceholder = computed(() =>
      t("chat_input_placeholder", {
        name: displayAgentName.value,
      })
    );
    const composerAttachActive = computed(() => Boolean(pendingWorkspaceDir.value));
    const chatDisclaimer = computed(() =>
      `${displayAgentName.value} can make mistakes. Check important info.`
    );
    const composerSuggestionLabels = computed(() => ({
      commands: t("chat_composer_suggestions_commands"),
      skills: t("chat_composer_suggestions_skills"),
      loading: t("chat_composer_suggestions_loading"),
      empty: t("chat_composer_suggestions_empty"),
    }));
    const composerFileLabels = computed(() => ({
      files: t("chat_composer_files"),
      preview: t("chat_composer_file_preview"),
      remove: t("chat_composer_file_remove"),
      uploading: t("chat_composer_file_uploading"),
      failed: t("chat_composer_upload_failed"),
    }));
    const composerLLMProfileItems = computed(() => {
      const defaultProfile = composerDefaultLLMProfile.value || {};
      const defaultVendor = modelVendorMeta(defaultProfile.modelName);
      return [
        {
          id: "chat-llm-profile-default-route",
          title: t("chat_llm_profile_default"),
          subtitle: defaultProfile.modelName,
          value: "",
          image: defaultVendor.icon || undefined,
          icon: defaultVendor.icon ? undefined : "PhCpu",
        },
        ...composerLLMProfiles.value.map((profile) => {
          const vendor = modelVendorMeta(profile.modelName);
          return {
            id: `chat-llm-profile-${profile.name}`,
            title: profile.name,
            subtitle: profile.modelName,
            value: profile.name,
            image: vendor.icon || undefined,
            icon: vendor.icon ? undefined : "PhCpu",
          };
        }),
      ];
    });
    const composerInputHistory = computed(() => {
      const items = Array.isArray(chatHistoryItems.value) ? chatHistoryItems.value : [];
      const history = [];
      for (let i = items.length - 1; i >= 0; i--) {
        const item = items[i];
        if (String(item?.role || "").trim().toLowerCase() !== "user") {
          continue;
        }
        const text = String(item?.text || "").trim();
        if (text) {
          history.push(text);
        }
      }
      return history;
    });
    const mobileTopicSplitEnabled = computed(() => consoleTopicsEnabled.value && mobileMode.value);
    const visibleTopics = computed(() => {
      const selectedTopic = normalizeTopicID(selectedTopicID.value);
      const items = [];
      let awarenessTopic = null;
      const awarenessVisible = showSystemTopics.value || selectedTopic === AWARENESS_TOPIC_ID;
      for (const topic of topics.value) {
        const topicID = normalizeTopicID(topic?.id);
        if (!topicID) {
          continue;
        }
        if (topicID === AWARENESS_TOPIC_ID) {
          if (awarenessVisible) {
            awarenessTopic = topic;
          }
          continue;
        }
        items.push(topic);
      }
      if (!awarenessTopic && awarenessVisible) {
        awarenessTopic = {
          id: AWARENESS_TOPIC_ID,
          title: t("chat_topic_awareness"),
          created_at: "",
          updated_at: "",
        };
      }
      if (awarenessTopic) {
        return [awarenessTopic, ...items];
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
      if (selectedID === AWARENESS_TOPIC_ID) {
        return {
          id: AWARENESS_TOPIC_ID,
          title: t("chat_topic_awareness"),
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
    const autoPreviewHistoryID = computed(() => {
      for (let i = chatHistoryItems.value.length - 1; i >= 0; i -= 1) {
        const item = chatHistoryItems.value[i];
        if (
          String(item?.role || "").trim().toLowerCase() === "agent" &&
          normalizeTaskStatus(item?.status) === "done" &&
          hasArtifactBlock(item?.text)
        ) {
          return String(item?.id || "").trim();
        }
      }
      return "";
    });
    const historyStreamProfilerEnabled = computed(() => historyItemStreamProfiler());
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
      if (!consoleTopicsEnabled.value) {
        return false;
      }
      if (!mobileTopicSplitEnabled.value) {
        return hasVisibleTopics.value;
      }
      return mobileTopicView.value === "topics";
    });
    const showChatPane = computed(() => {
      if (!mobileTopicSplitEnabled.value) {
        return true;
      }
      return mobileTopicView.value === "chat";
    });
    const desktopWorkspaceSidebarVisible = computed(
      () => workspaceSidebarAvailable.value && !mobileMode.value && showChatPane.value && workspaceSidebarOpen.value
    );
    const mobileWorkspaceSidebarVisible = computed(
      () => workspaceSidebarAvailable.value && mobileMode.value && showChatPane.value && workspaceSidebarOpen.value
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
    const chatMainStyle = computed(() => ({
      "--chat-overlay-compose-h": `${Math.max(
        CHAT_COMPOSER_MIN_HEIGHT,
        Math.ceil(Number(composerHeight.value) || 0)
      )}px`,
    }));
    const deskTitle = computed(() => {
      if (creatingTopic.value || !hasSelectedTopic.value || !selectedTopic.value) {
        return t("chat_topic_new");
      }
      return topicTitle(selectedTopic.value);
    });
    const workspaceTopicID = computed(() => {
      if (!consoleTopicsEnabled.value || creatingTopic.value) {
        return "";
      }
      const topicID = normalizeTopicID(selectedTopicID.value);
      if (!topicID || topicID === AWARENESS_TOPIC_ID) {
        return "";
      }
      return topicID;
    });
    const selectedTopicIsReserved = computed(() => {
      const topicID = normalizeTopicID(selectedTopicID.value);
      return topicID === DEFAULT_TOPIC_ID || topicID === AWARENESS_TOPIC_ID;
    });
    const topicDeleteAvailable = computed(
      () => Boolean(workspaceTopicID.value) && !selectedTopicIsReserved.value
    );
    const topicDeleteDisabled = computed(() => !topicDeleteAvailable.value || topicDeleting.value);
    const topicPropertyRows = computed(() => {
      const topic = selectedTopic.value;
      if (!topic) {
        return [];
      }
      const rows = [
        {
          key: "created",
          label: t("chat_topic_created_label"),
          value: formatTime(topic.created_at),
          code: false,
        },
        {
          key: "updated",
          label: t("chat_topic_updated_label"),
          value: formatTime(topic.updated_at),
          code: false,
        },
      ];
      return rows;
    });
    const topicContextProgress = computed(() => {
      const context = topicContext.value && typeof topicContext.value === "object" ? topicContext.value : null;
      if (!context?.available) {
        return null;
      }
      const ratio = topicContextUsageRatio(context);
      if (ratio === null) {
        return null;
      }
      const label = formatUsageRatio(ratio);
      const usedInputLabel = formatTokenCount(context.used_input_tokens);
      const windowLabel = formatTokenCount(context.context_window_tokens);
      return {
        value: Math.min(ratio, 1),
        label,
        usedInputLabel,
        windowLabel,
        title: `${t("chat_topic_context_ratio_label")}: ${label}; ${t("chat_topic_context_used_label")}: ${usedInputLabel}; ${t("chat_topic_context_window_label")}: ${windowLabel}`,
      };
    });
    const topicDeleteDialogText = computed(() =>
      t("chat_topic_delete_confirm", {
        title: topicTitle(topicDeleteTarget.value || selectedTopic.value || {}),
      })
    );
    const topicDeleteDialogActions = computed(() => [
      {
        name: "cancel",
        label: t("action_cancel"),
        class: "outlined",
        action: closeTopicDeleteDialog,
      },
      {
        name: "delete",
        label: t("action_delete"),
        class: "danger",
        action: deleteSelectedTopic,
      },
    ]);
    const workspaceSidebarAvailable = computed(() => Boolean(workspaceTopicID.value));
    const workspaceReady = computed(() => Boolean(submitEndpointRef.value && workspaceTopicID.value));
    const workspaceBusy = computed(() => workspaceLoading.value || workspaceSaving.value);
    const workspaceHintText = computed(() => {
      if (workspaceTopicID.value) {
        if (!String(workspaceDir.value || "").trim()) {
          return t("chat_workspace_hint_empty");
        }
        if (workspaceSource.value === "default") {
          return t("chat_workspace_hint_default");
        }
        if (workspaceSource.value === "attachment") {
          return t("chat_workspace_hint_attachment");
        }
        return "";
      }
      if (creatingTopic.value) {
        return t("chat_workspace_hint_needs_topic");
      }
      if (normalizeTopicID(selectedTopicID.value) === AWARENESS_TOPIC_ID) {
        return t("chat_workspace_hint_system_topic");
      }
      return t("chat_workspace_hint_no_topic");
    });
    const workspaceAttachDisabled = computed(() => !workspaceReady.value || workspaceBusy.value);
    const composerWorkspaceAttachDisabled = computed(
      () => !String(submitEndpointRef.value || "").trim() || workspaceBusy.value
    );
    const composerAddDisabled = computed(
      () =>
        composerDisabled.value ||
        !String(submitEndpointRef.value || "").trim() ||
        composerUploading.value
    );
    const workspaceDetachDisabled = computed(
      () =>
        !workspaceReady.value ||
        workspaceBusy.value ||
        workspaceSource.value !== "attachment" ||
        String(workspaceDir.value || "").trim() === ""
    );
    const workspaceDirDisplay = computed(() => splitWorkspaceDisplayPath(workspaceDir.value));
    const workspacePanelTabs = computed(() => [
      {
        id: TOPIC_TAB_ID,
        title: t("chat_topic_kicker"),
        icon: "PhChats",
      },
      {
        id: WORKSPACE_TAB_ID,
        title: t("chat_workspace_label"),
        icon: "PhCube",
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
    const workspaceBrowserPlaceSourceItems = computed(() => {
      return [
        {
          id: WORKSPACE_BROWSER_SOURCE_STATE_DIR,
          title: t("chat_workspace_dialog_state_dir"),
          path: workspaceBrowserStateDir.value,
        },
        {
          id: WORKSPACE_BROWSER_SOURCE_CACHE_DIR,
          title: t("chat_workspace_dialog_cache_dir"),
          path: workspaceBrowserCacheDir.value,
        },
      ].filter((item) => String(item.path || "").trim() !== "");
    });
    const workspaceBrowserCurrentSource = computed(() =>
      workspaceBrowserSource(
        workspaceBrowserSourceID.value,
        workspaceBrowserStateDir.value,
        workspaceBrowserCacheDir.value
      )
    );
    const workspaceBrowserRows = computed(() => {
      if (workspaceBrowserCurrentSource.value.kind === "recent") {
        return workspaceBrowserRecentItems.value.map((item) => ({
          key: `recent:${item.path}`,
          depth: 0,
          source: "recent",
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
    const workspaceBrowserCreateParent = computed(() => {
      const selectedPath = String(workspaceBrowserSelection.value || "").trim();
      if (selectedPath) {
        return selectedPath;
      }
      const source = workspaceBrowserCurrentSource.value;
      if (source.kind === "home" || source.kind === "place") {
        return String(source.path || "").trim();
      }
      return "";
    });
    const workspaceBrowserCreateDisabled = computed(
      () =>
        workspaceBrowserLoading.value ||
        workspaceSaving.value ||
        workspaceBrowserCreating.value ||
        !String(submitEndpointRef.value || "").trim() ||
        !workspaceBrowserCreateParent.value
    );
    const workspaceBrowserCreateSubmitDisabled = computed(
      () => workspaceBrowserCreateDisabled.value || !String(workspaceBrowserCreateName.value || "").trim()
    );
    const workspaceBrowserConfirmDisabled = computed(() => {
      if (
        workspaceSaving.value ||
        workspaceBrowserCreating.value ||
        String(workspaceBrowserSelection.value || "").trim() === ""
      ) {
        return true;
      }
      if (workspaceBrowserPendingMode.value) {
        return !String(submitEndpointRef.value || "").trim();
      }
      return !workspaceReady.value;
    });
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
      void syncChatRoute("", { replace: true });
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

    async function ensureComposerSkillsLoaded() {
      if (composerSkills.value.length > 0 || composerSkillsLoading.value) {
        return;
      }
      const seq = composerSkillsLoadSeq + 1;
      composerSkillsLoadSeq = seq;
      composerSkillsLoading.value = true;
      composerSkillsError.value = "";
      try {
        const endpointRef = String(submitEndpointRef.value || endpointState.selectedRef || "").trim();
        const data = endpointRef && endpointRef !== LOCAL_CONSOLE_ENDPOINT_REF
          ? await runtimeApiFetchForEndpoint(endpointRef, "/settings/agent")
          : await apiFetch("/settings/agent");
        if (seq !== composerSkillsLoadSeq) {
          return;
        }
        const skills = data?.skills && typeof data.skills === "object" ? data.skills : {};
        composerSkills.value = normalizeComposerSkillItems([
          ...(Array.isArray(skills.loaded) ? skills.loaded : []),
          ...(Array.isArray(skills.available) ? skills.available : []),
        ]);
      } catch (error) {
        if (seq === composerSkillsLoadSeq) {
          composerSkillsError.value = error?.message || t("chat_composer_suggestions_load_error");
          composerSkills.value = [];
        }
      } finally {
        if (seq === composerSkillsLoadSeq) {
          composerSkillsLoading.value = false;
        }
      }
    }

    async function ensureComposerCommandsLoaded() {
      if (composerCommands.value.length > 0 || composerCommandsLoading.value) {
        return;
      }
      const seq = composerCommandsLoadSeq + 1;
      composerCommandsLoadSeq = seq;
      composerCommandsLoading.value = true;
      try {
        const endpointRef = String(submitEndpointRef.value || endpointState.selectedRef || "").trim();
        const data = endpointRef && endpointRef !== LOCAL_CONSOLE_ENDPOINT_REF
          ? await runtimeApiFetchForEndpoint(endpointRef, "/commands")
          : await apiFetch("/commands");
        if (seq !== composerCommandsLoadSeq) {
          return;
        }
        const rawItems = Array.isArray(data?.items)
          ? data.items
          : Array.isArray(data?.commands)
            ? data.commands
            : [];
        composerCommands.value = normalizeComposerCommandItems(rawItems);
      } catch {
        if (seq === composerCommandsLoadSeq) {
          composerCommands.value = [];
        }
      } finally {
        if (seq === composerCommandsLoadSeq) {
          composerCommandsLoading.value = false;
        }
      }
    }

    function syncComposerLLMProfile() {
      composerLLMProfile.value = resolveAvailableChatLLMProfile(
        composerTopicLLMProfile.value,
        composerLLMProfiles.value
      );
    }

    function applyComposerTopicLLMProfile(rawProfile) {
      composerTopicLLMProfile.value = String(rawProfile || "").trim();
      syncComposerLLMProfile();
    }

    async function loadComposerLLMProfiles() {
      const seq = composerLLMProfilesLoadSeq + 1;
      composerLLMProfilesLoadSeq = seq;
      const endpointRef = String(submitEndpointRef.value || endpointState.selectedRef || "").trim();
      if (!endpointRef) {
        composerDefaultLLMProfile.value = null;
        composerLLMProfiles.value = [];
        applyComposerTopicLLMProfile("");
        return;
      }
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/llm/profiles");
        if (seq !== composerLLMProfilesLoadSeq) {
          return;
        }
        composerDefaultLLMProfile.value = normalizeChatLLMProfileMetadata(data?.default);
        const profiles = normalizeChatLLMProfiles(data?.items);
        composerLLMProfiles.value = profiles;
        syncComposerLLMProfile();
      } catch {
        if (seq === composerLLMProfilesLoadSeq) {
          composerDefaultLLMProfile.value = null;
          composerLLMProfiles.value = [];
          syncComposerLLMProfile();
        }
      }
    }

    function persistComposerDraft(scope = composerDraftScope.value, text = taskInput.value) {
      const endpointRef = String(scope?.endpointRef || "").trim();
      if (!endpointRef) {
        return;
      }
      rememberChatDraft(endpointRef, normalizeTopicID(scope?.topicID), text);
    }

    function restoreComposerDraft(scope = composerDraftScope.value) {
      const endpointRef = String(scope?.endpointRef || "").trim();
      const nextText = endpointRef ? chatDraft(endpointRef, normalizeTopicID(scope?.topicID)) : "";
      suppressDraftPersistence.value = true;
      taskInput.value = nextText;
      syncComposer();
      void nextTick(() => {
        suppressDraftPersistence.value = false;
      });
    }

    function setComposerFileDraft(scope, items) {
      const key = composerFileDraftKey(scope);
      if (!key) {
        return;
      }
      const nextItems = Array.isArray(items) ? [...items] : [];
      if (nextItems.length > 0) {
        composerFileDrafts.set(key, nextItems);
      } else {
        composerFileDrafts.delete(key);
      }
      if (key === composerFileDraftKey(composerDraftScope.value)) {
        composerFiles.value = nextItems;
      }
    }

    function updateComposerFileDraft(scope, update) {
      const key = composerFileDraftKey(scope);
      if (!key || typeof update !== "function") {
        return;
      }
      const current = composerFileDrafts.get(key) || [];
      setComposerFileDraft(scope, update([...current]));
    }

    function restoreComposerFileDraft(scope = composerDraftScope.value) {
      const key = composerFileDraftKey(scope);
      composerFiles.value = key ? [...(composerFileDrafts.get(key) || [])] : [];
    }

    function clearComposerFileDraft(scope = composerDraftScope.value) {
      setComposerFileDraft(scope, []);
    }

    function syncComposer() {
      void nextTick(() => {
        composerRef.value?.syncHeight?.();
      });
    }

    function updateComposerHeight(height) {
      const nextHeight = Math.max(
        CHAT_COMPOSER_MIN_HEIGHT,
        Math.ceil(Number(height) || 0)
      );
      if (composerHeight.value !== nextHeight) {
        composerHeight.value = nextHeight;
      }
    }

    function focusComposer() {
      if (chatReadonly.value || (mobileTopicSplitEnabled.value && !showChatPane.value)) {
        return;
      }
      void nextTick(() => {
        composerRef.value?.focus?.();
      });
    }

    function insertComposerText(rawText) {
      const insertText = String(rawText || "");
      if (!insertText) {
        return;
      }
      composerRef.value?.insertText?.(insertText);
    }

    function openComposerFilePicker() {
      if (composerAddDisabled.value) {
        return;
      }
      const input = composerFileInput.value;
      if (!input) {
        return;
      }
      input.value = "";
      input.click();
    }

    async function uploadComposerFiles(event) {
      const input = event?.target;
      const files = Array.from(input?.files || []);
      if (input) {
        input.value = "";
      }
      if (files.length === 0 || composerUploading.value) {
        return;
      }

      const endpointRef = String(submitEndpointRef.value || "").trim();
      if (!endpointRef) {
        err.value = t("msg_select_endpoint");
        return;
      }

      const uploadScope = {
        endpointRef: String(composerDraftScope.value.endpointRef || "").trim(),
        topicID: normalizeTopicID(composerDraftScope.value.topicID),
      };
      const uploadItems = files.map((file) => {
        composerFileSequence += 1;
        return {
          id: `composer-file-${Date.now()}-${composerFileSequence}`,
          name: String(file?.name || "").trim() || "file",
          status: "uploading",
          dirName: "",
          path: "",
          error: "",
          sourceFile: file,
        };
      });
      const uploadIDs = new Set(uploadItems.map((item) => item.id));
      updateComposerFileDraft(uploadScope, (current) => [...current, ...uploadItems]);

      const form = new FormData();
      for (const file of files) {
        form.append("files", file, file.name);
      }
      const pendingDir = String(pendingWorkspaceDir.value || "").trim();
      const attachedDir = String(workspaceDir.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      if (pendingDir) {
        form.append("workspace_dir", pendingDir);
      } else if (topicID) {
        form.append("topic_id", topicID);
      } else if (attachedDir) {
        form.append("workspace_dir", attachedDir);
      }

      composerUploading.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/files/upload", {
          method: "POST",
          body: form,
        });
        const uploadedFiles = Array.isArray(data?.files) ? data.files : [];
        if (uploadedFiles.length !== uploadItems.length) {
          throw new Error(t("chat_composer_upload_failed"));
        }
        updateComposerFileDraft(uploadScope, (current) =>
          current.map((item) => {
            if (!uploadIDs.has(item.id)) {
              return item;
            }
            const uploadIndex = uploadItems.findIndex((uploadItem) => uploadItem.id === item.id);
            const uploaded = uploadedFiles[uploadIndex] || {};
            const dirName = String(uploaded?.dir_name || "").trim();
            const path = String(uploaded?.path || "").trim();
            if (!path || (dirName !== "workspace_dir" && dirName !== "file_cache_dir")) {
              return {
                ...item,
                status: "failed",
                error: t("chat_composer_upload_failed"),
              };
            }
            return {
              ...item,
              name: String(uploaded?.name || "").trim() || item.name,
              status: "ready",
              dirName,
              path,
              error: "",
            };
          })
        );
        if (
          uploadedFiles.some((file) => String(file?.dir_name || "").trim() === "workspace_dir") &&
          workspaceSidebarOpen.value &&
          workspaceSidebarTabID.value === WORKSPACE_TAB_ID &&
          attachedDir
        ) {
          await loadWorkspaceTree("", { force: true });
        }
      } catch (error) {
        const message = error?.message || t("chat_composer_upload_failed");
        updateComposerFileDraft(uploadScope, (current) =>
          current.map((item) =>
            uploadIDs.has(item.id)
              ? {
                  ...item,
                  status: "failed",
                  error: message,
                }
              : item
          )
        );
      } finally {
        composerUploading.value = false;
        if (composerFileDraftKey(uploadScope) === composerFileDraftKey(composerDraftScope.value)) {
          void nextTick(() => {
            composerRef.value?.focus?.({ preserveSelection: true });
          });
        }
      }
    }

    function releaseComposerFilePreviewURL() {
      if (composerFilePreviewObjectURL) {
        URL.revokeObjectURL(composerFilePreviewObjectURL);
        composerFilePreviewObjectURL = "";
      }
      composerFilePreviewURL.value = "";
    }

    async function resolveFilePreviewSource(item) {
      if (item?.sourceFile && typeof item.sourceFile.arrayBuffer === "function") {
        return item.sourceFile;
      }

      const endpointRef = String(item?.endpointRef || submitEndpointRef.value || "").trim();
      const dirName = String(item?.dirName || "").trim();
      const path = String(item?.path || "").trim();
      if (!endpointRef || !path || (dirName !== "workspace_dir" && dirName !== "file_cache_dir")) {
        throw new Error(t("chat_composer_file_preview_unavailable"));
      }

      const query = new URLSearchParams();
      query.set("dir_name", dirName);
      query.set("path", path);
      if (dirName === "workspace_dir") {
        const topicID = normalizeTopicID(item?.topicID || selectedTopicID.value);
        if (!topicID) {
          throw new Error(t("chat_composer_file_preview_unavailable"));
        }
        query.set("topic_id", topicID);
      }
      return runtimeApiDownloadForEndpoint(endpointRef, `/files/download?${query.toString()}`);
    }

    function closeComposerFilePreview() {
      composerFilePreviewSequence += 1;
      releaseComposerFilePreviewURL();
      composerFilePreviewOpen.value = false;
      composerFilePreviewLoading.value = false;
      composerFilePreviewID.value = "";
      composerFilePreviewName.value = "";
      composerFilePreviewKind.value = "";
      composerFilePreviewText.value = "";
      composerFilePreviewError.value = "";
      composerFilePreviewItems.value = [];
      composerFilePreviewIndex.value = -1;
    }

    async function loadComposerFilePreview(item) {
      if (String(item?.status || "").trim() !== "ready") {
        return;
      }
      releaseComposerFilePreviewURL();
      composerFilePreviewSequence += 1;
      const previewSequence = composerFilePreviewSequence;
      composerFilePreviewOpen.value = true;
      composerFilePreviewLoading.value = true;
      composerFilePreviewID.value = String(item?.id || "").trim();
      composerFilePreviewName.value = String(item?.name || "").trim();
      composerFilePreviewKind.value = "";
      composerFilePreviewText.value = "";
      composerFilePreviewError.value = "";

      const extension = composerFileExtension(item?.name);
      try {
        const sourceFile = await resolveFilePreviewSource(item);
        if (previewSequence !== composerFilePreviewSequence) {
          return;
        }
        const mimeType = String(sourceFile?.type || "").trim().toLowerCase();
        if (
          COMPOSER_FILE_IMAGE_EXTENSIONS.has(extension) ||
          (mimeType.startsWith("image/") && mimeType !== "image/svg+xml")
        ) {
          composerFilePreviewObjectURL = URL.createObjectURL(sourceFile);
          composerFilePreviewURL.value = composerFilePreviewObjectURL;
          composerFilePreviewKind.value = "image";
        } else if (extension === ".pdf" || mimeType === "application/pdf") {
          composerFilePreviewObjectURL = URL.createObjectURL(sourceFile);
          composerFilePreviewURL.value = composerFilePreviewObjectURL;
          composerFilePreviewKind.value = "pdf";
        } else {
          const buffer = await sourceFile.arrayBuffer();
          if (previewSequence !== composerFilePreviewSequence) {
            return;
          }
          composerFilePreviewText.value = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
          composerFilePreviewKind.value = "text";
        }
      } catch {
        if (previewSequence === composerFilePreviewSequence) {
          composerFilePreviewError.value = t("chat_composer_file_preview_unavailable");
        }
      } finally {
        if (previewSequence === composerFilePreviewSequence) {
          composerFilePreviewLoading.value = false;
        }
      }
    }

    function previewComposerFile(item) {
      const previewItems = (Array.isArray(item?.previewItems) ? item.previewItems : [item]).filter(
        (candidate) => String(candidate?.status || "").trim() === "ready"
      );
      if (previewItems.length === 0) {
        return;
      }
      const selectedID = String(item?.id || "").trim();
      const selectedIndex = Math.max(
        0,
        previewItems.findIndex((candidate) => String(candidate?.id || "").trim() === selectedID)
      );
      composerFilePreviewItems.value = previewItems;
      composerFilePreviewIndex.value = selectedIndex;
      void loadComposerFilePreview(previewItems[selectedIndex]);
    }

    function navigateComposerFilePreview(offset) {
      const nextIndex = composerFilePreviewIndex.value + Number(offset || 0);
      if (nextIndex < 0 || nextIndex >= composerFilePreviewItems.value.length) {
        return;
      }
      composerFilePreviewIndex.value = nextIndex;
      void loadComposerFilePreview(composerFilePreviewItems.value[nextIndex]);
    }

    function removeComposerFile(item) {
      const itemID = String(item?.id || "").trim();
      if (!itemID) {
        return;
      }
      updateComposerFileDraft(composerDraftScope.value, (current) =>
        current.filter((currentItem) => currentItem.id !== itemID)
      );
      if (composerFilePreviewID.value === itemID) {
        closeComposerFilePreview();
      }
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
      workspaceBrowserCreateOpen.value = false;
      workspaceBrowserCreateName.value = "";
      workspaceBrowserCreating.value = false;
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
      workspaceSource.value = "none";
      workspaceLoading.value = false;
      workspaceSaving.value = false;
      workspaceOpening.value = false;
      workspaceDownloading.value = false;
      workspaceError.value = "";
      topicContext.value = null;
      workspaceBrowserOpen.value = false;
      workspaceBrowserPendingMode.value = false;
      pendingWorkspaceDir.value = "";
      workspaceSidebarTabID.value = TOPIC_TAB_ID;
      resetWorkspaceTreeState();
      resetWorkspaceBrowserState();
      workspaceBrowserStateDir.value = "";
      workspaceBrowserCacheDir.value = "";
    }

    function applyWorkspacePayload(data) {
      const nextDir = String(data?.workspace_dir || "").trim();
      const rawSource = String(data?.source || "").trim().toLowerCase();
      const nextSource = ["attachment", "default", "none"].includes(rawSource)
        ? rawSource
        : nextDir
          ? "attachment"
          : "none";
      workspaceDir.value = nextDir;
      workspaceSource.value = nextSource;
      workspaceError.value = "";
      resetWorkspaceTreeState();
      resetWorkspaceBrowserState();
      if (nextDir) {
        workspaceBrowserSelection.value = nextDir;
      }
    }

    function applyTopicMetadataPayload(data) {
      const workspace = data?.workspace && typeof data.workspace === "object" ? data.workspace : data;
      applyWorkspacePayload(workspace);
      const context = data?.context && typeof data.context === "object" ? data.context : null;
      topicContext.value = context && context.available ? context : null;
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
          `/topic/${encodeURIComponent(topicID)}/metadata`
        );
        if (requestID !== workspaceRequestSeq) {
          return false;
        }
        applyTopicMetadataPayload(data);
        if (
          workspaceSidebarOpen.value &&
          workspaceSidebarTabID.value === WORKSPACE_TAB_ID &&
          String(workspaceDir.value || "").trim()
        ) {
          await loadWorkspaceTree("", { force: true });
        }
        return true;
      } catch (e) {
        if (requestID !== workspaceRequestSeq) {
          return false;
        }
        workspaceDir.value = "";
        workspaceSource.value = "none";
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
        if (
          workspaceSidebarTabID.value === WORKSPACE_TAB_ID &&
          String(workspaceDir.value || "").trim() &&
          !hasOwnTreePath(workspaceTreeItems.value, "")
        ) {
          void loadWorkspaceTree("", { force: true });
        }
      }
    }

    function unlockMobileWorkspaceSidebarScroll() {
      if (!mobileWorkspaceScrollLocked) {
        return;
      }
      document.body.style.overflow = mobileWorkspaceBodyOverflowBeforeOpen;
      mobileWorkspaceScrollLocked = false;
    }

    function closeMobileWorkspaceSidebar() {
      workspaceSidebarOpen.value = false;
    }

    function onMobileWorkspaceSidebarKeydown(event) {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      closeMobileWorkspaceSidebar();
    }

    function onWorkspaceTabChange(detail) {
      const nextID = String(detail?.tab?.id || "").trim();
      workspaceSidebarTabID.value = nextID || TOPIC_TAB_ID;
      if (
        workspaceSidebarTabID.value === WORKSPACE_TAB_ID &&
        String(workspaceDir.value || "").trim() &&
        !hasOwnTreePath(workspaceTreeItems.value, "")
      ) {
        void loadWorkspaceTree("", { force: true });
      }
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

    function workspaceBrowserTreeEntryClass(row) {
      const classes = ["chat-workspace-tree-entry", "is-actionable", "is-selectable"];
      if (row?.entry?.is_dir) {
        classes.push("is-dir");
      }
      if (row?.source === "recent") {
        classes.push("is-recent");
      }
      if (String(workspaceBrowserSelection.value || "").trim() === String(row?.entry?.path || "").trim()) {
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

    async function downloadWorkspaceSelection() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const entry = workspaceSelectedTreeEntry.value;
      const path = String(entry?.path || "").trim();
      if (!endpointRef || !topicID || !path || entry?.is_dir || workspaceDownloading.value) {
        return;
      }
      workspaceDownloading.value = true;
      workspaceError.value = "";
      try {
        const query = new URLSearchParams();
        query.set("dir_name", "workspace_dir");
        query.set("topic_id", topicID);
        query.set("path", path);
        const blob = await runtimeApiDownloadForEndpoint(endpointRef, `/files/download?${query.toString()}`);
        triggerBrowserDownload(blob, workspaceDownloadFilename(entry));
      } catch (e) {
        workspaceError.value = e?.message || t("msg_load_failed");
      } finally {
        workspaceDownloading.value = false;
      }
    }

    async function openWorkspaceBrowser(options = {}) {
      const pendingMode = Boolean(options?.pending);
      if (pendingMode) {
        if (composerWorkspaceAttachDisabled.value) {
          return;
        }
      } else if (workspaceAttachDisabled.value) {
        return;
      }
      workspaceBrowserPendingMode.value = pendingMode;
      workspaceBrowserOpen.value = true;
      workspaceBrowserError.value = "";
      workspaceBrowserShowHidden.value = false;
      await activateWorkspaceBrowserSource(WORKSPACE_BROWSER_SOURCE_HOME);
      const selectedDir = String(pendingMode ? pendingWorkspaceDir.value : workspaceDir.value || "").trim();
      if (selectedDir) {
        workspaceBrowserSelection.value = selectedDir;
      }
    }

    async function openComposerWorkspaceBrowser() {
      await openWorkspaceBrowser({ pending: true });
    }

    function closeWorkspaceBrowser() {
      if (workspaceBrowserCreating.value) {
        return;
      }
      workspaceBrowserOpen.value = false;
      workspaceBrowserPendingMode.value = false;
      workspaceBrowserError.value = "";
      workspaceBrowserCreateOpen.value = false;
      workspaceBrowserCreateName.value = "";
    }

    async function activateWorkspaceBrowserSource(sourceID) {
      const source = workspaceBrowserSource(
        sourceID,
        workspaceBrowserStateDir.value,
        workspaceBrowserCacheDir.value
      );
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
        workspaceBrowserStateDir.value = "";
        workspaceBrowserCacheDir.value = "";
        return false;
      }
      workspaceBrowserLoading.value = true;
      workspaceBrowserLoadingPath.value = path;
      try {
        const query = new URLSearchParams();
        if (path) {
          query.set("path", path);
        }
        if (workspaceBrowserShowHidden.value) {
          query.set("show_hidden", "true");
        }
        const data = await runtimeApiFetchForEndpoint(
          endpointRef,
          query.toString() ? `/workspace/browse?${query.toString()}` : "/workspace/browse"
        );
        workspaceBrowserStateDir.value = String(data?.state_dir || "").trim();
        workspaceBrowserCacheDir.value = String(data?.cache_dir || "").trim();
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

    async function setWorkspaceBrowserShowHidden(value) {
      const nextValue = Boolean(value);
      if (workspaceBrowserShowHidden.value === nextValue) {
        return;
      }
      workspaceBrowserShowHidden.value = nextValue;
      if (!workspaceBrowserOpen.value || workspaceBrowserCurrentSource.value.kind === "recent") {
        return;
      }
      const source = workspaceBrowserCurrentSource.value;
      resetWorkspaceBrowserState();
      const ok = await loadWorkspaceBrowser(source.path);
      if (ok) {
        workspaceBrowserSelection.value = source.selection;
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

    function openWorkspaceBrowserCreate() {
      if (workspaceBrowserCreateDisabled.value) {
        return;
      }
      workspaceBrowserCreateOpen.value = true;
      workspaceBrowserCreateName.value = "";
      void nextTick(() => {
        workspaceBrowserCreateField.value?.querySelector("input")?.focus();
      });
    }

    function cancelWorkspaceBrowserCreate() {
      if (workspaceBrowserCreating.value) {
        return;
      }
      workspaceBrowserCreateOpen.value = false;
      workspaceBrowserCreateName.value = "";
    }

    async function createWorkspaceBrowserDir() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const parentPath = String(workspaceBrowserCreateParent.value || "").trim();
      const name = String(workspaceBrowserCreateName.value || "").trim();
      if (!endpointRef || !parentPath || !name || workspaceBrowserCreating.value) {
        return;
      }

      const sourceKind = workspaceBrowserCurrentSource.value.kind;
      workspaceBrowserCreating.value = true;
      workspaceBrowserError.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/workspace/directory", {
          method: "POST",
          body: {
            parent_path: parentPath,
            name,
          },
        });
        const createdPath = String(data?.path || "").trim();
        if (!createdPath) {
          throw new Error(t("msg_save_failed"));
        }
        if (sourceKind === "recent") {
          rememberWorkspaceBrowserRecentDir(createdPath);
        } else {
          const refreshed = await loadWorkspaceBrowser(parentPath);
          if (!refreshed) {
            return;
          }
        }
        workspaceBrowserSelection.value = createdPath;
        workspaceBrowserCreateOpen.value = false;
        workspaceBrowserCreateName.value = "";
      } catch (e) {
        toast.error(e?.message || t("msg_save_failed"));
      } finally {
        workspaceBrowserCreating.value = false;
      }
    }

    async function attachWorkspace() {
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = String(workspaceTopicID.value || "").trim();
      const nextDir = String(workspaceBrowserSelection.value || "").trim();
      if (!endpointRef || !nextDir || workspaceSaving.value) {
        return;
      }
      if (workspaceBrowserPendingMode.value || !topicID) {
        pendingWorkspaceDir.value = nextDir;
        rememberWorkspaceBrowserRecentDir(nextDir);
        workspaceBrowserOpen.value = false;
        workspaceBrowserPendingMode.value = false;
        workspaceBrowserError.value = "";
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
        toast.error(message);
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
        toast.error(e?.message || t("msg_save_failed"));
      } finally {
        workspaceSaving.value = false;
      }
    }

    function chatRoutePath(topicID = "") {
      const normalized = normalizeTopicID(topicID);
      const chatPagePath = normalized ? `/chat/${encodeURIComponent(normalized)}` : "/chat";
      return endpointRoutePath(endpointState.selectedRef, chatPagePath);
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

    function markHistoryScrollIntent() {
      historyUserScrollIntentAt = Date.now();
    }

    function markHistoryPointerScrollIntent(event) {
      const viewport = historyViewportElement();
      if (!viewport || event?.target !== viewport) {
        return;
      }
      const rect = viewport.getBoundingClientRect();
      const nearVerticalScrollbar = event.clientX >= rect.right - 24;
      const nearHorizontalScrollbar = event.clientY >= rect.bottom - 24;
      if (nearVerticalScrollbar || nearHorizontalScrollbar) {
        markHistoryScrollIntent();
      }
    }

    function hasRecentHistoryScrollIntent() {
      return Date.now() - historyUserScrollIntentAt <= 1200;
    }

    function handleHistoryScroll() {
      if (historyNearBottom()) {
        historyAutoStick.value = true;
        return;
      }
      if (hasRecentHistoryScrollIntent()) {
        historyAutoStick.value = false;
      }
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

    function replaceHistoryItems(items) {
      const nextItems = applyDefaultHistoryDurationVisibility(items);
      chatHistoryItems.value = nextItems;
    }

    function resetHistoryCopyState() {
      if (copiedHistoryTimerID) {
        window.clearTimeout(copiedHistoryTimerID);
        copiedHistoryTimerID = 0;
      }
      copiedHistoryItemID.value = "";
    }

    async function copyHistoryItem(item) {
      const text = String(item?.text || "");
      if (!text.trim()) {
        return;
      }
      try {
        if (navigator?.clipboard?.writeText) {
          await navigator.clipboard.writeText(text);
        } else {
          const textarea = document.createElement("textarea");
          textarea.value = text;
          textarea.setAttribute("readonly", "true");
          textarea.style.position = "fixed";
          textarea.style.left = "-9999px";
          textarea.style.top = "0";
          document.body.appendChild(textarea);
          textarea.focus();
          textarea.select();
          document.execCommand("copy");
          document.body.removeChild(textarea);
        }
        copiedHistoryItemID.value = String(item?.id || "");
        if (copiedHistoryTimerID) {
          window.clearTimeout(copiedHistoryTimerID);
        }
        copiedHistoryTimerID = window.setTimeout(() => {
          copiedHistoryItemID.value = "";
          copiedHistoryTimerID = 0;
        }, 1200);
      } catch {
        // Copy should not compete with task errors in the chat error surface.
      }
    }

    function historyItemStreamProfiler() {
      try {
        return window.localStorage?.getItem("mistermorph_markdown_stream_profiler") === "true";
      } catch {
        return false;
      }
    }

    function chatStatusExpandedPanel(itemID) {
      const key = String(itemID || "").trim();
      const value = String(chatStatusExpandedState.value[key] || "").trim();
      return value === "plan" || value === "activity" || value === "reasoning" ? value : "";
    }

    function toggleChatStatus(itemID, panel) {
      const key = String(itemID || "").trim();
      const value = String(panel || "").trim();
      if (!key || (value !== "plan" && value !== "activity" && value !== "reasoning")) {
        return;
      }
      const nextState = {
        ...chatStatusExpandedState.value,
      };
      if (chatStatusExpandedPanel(itemID) === value) {
        delete nextState[key];
      } else {
        nextState[key] = value;
      }
      chatStatusExpandedState.value = nextState;
    }

    function markHistoryItemRendered() {
      handleMarkdownRendered();
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

    async function startTaskStream(taskID, historyID, endpointRef) {
      const key = String(taskID || "").trim();
      if (!key || !supportsConsoleTaskStream(endpointRef)) {
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
      const url = buildConsoleStreamURL(ticket, key, endpointRef);
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
        const existingItem = chatHistoryItems.value.find((item) => item.id === historyID) || null;
        const nextPlan = normalizePlan(frame.plan || existingItem?.plan);
        const nextActivity = normalizeActivity(frame.activity || existingItem?.activity);
        const nextStatus = normalizeTaskStatus(frame.status || existingItem?.status);
        const isPreview = frame.preview === true;
        const patch = {};
        if (frame.plan && typeof frame.plan === "object") {
          patch.plan = nextPlan;
        }
        if (frame.activity && typeof frame.activity === "object") {
          patch.activity = nextActivity;
        }
        if (typeof frame.reasoning === "string" && frame.reasoning.trim() !== "") {
          patch.reasoning = normalizeReasoning(frame.reasoning);
        }
        if (!isPreview && typeof frame.text === "string" && frame.text !== "") {
          patch.text = frame.text;
        } else if (!isPreview && typeof frame.error === "string" && frame.error !== "") {
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
        durationText: "",
        durationVisible: false,
        durationVisibleManual: false,
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

    function topicTitle(topic) {
      const title = String(topic?.title || "").trim();
      if (title) {
        return title;
      }
      const topicID = normalizeTopicID(topic?.id);
      if (topicID === DEFAULT_TOPIC_ID) {
        return t("chat_topic_default");
      }
      if (topicID === AWARENESS_TOPIC_ID) {
        return t("chat_topic_awareness");
      }
      return t("chat_topic_untitled");
    }

    function topicTime(topic) {
      return topicTimeLabel(topic);
    }

    function topicItemClass(topic) {
      const classes = ["chat-topic-item", "workspace-sidebar-item"];
      if (
        !mobileMode.value &&
        normalizeTopicID(topic?.id) === normalizeTopicID(selectedTopicID.value) &&
        !creatingTopic.value
      ) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function topicIsActive(topic) {
      return (
        !mobileMode.value &&
        normalizeTopicID(topic?.id) === normalizeTopicID(selectedTopicID.value) &&
        !creatingTopic.value
      );
    }

    function pushHistoryItem(partial) {
      const item = {
        id: newHistoryID(),
        role: String(partial?.role || "system"),
        text: String(partial?.text || ""),
        files: normalizeHistoryFileReferences(partial?.files),
        endpointRef: String(partial?.endpointRef || "").trim(),
        topicID: normalizeTopicID(partial?.topicID),
        plan: normalizePlan(partial?.plan),
        activity: normalizeActivity(partial?.activity),
        reasoning: normalizeReasoning(partial?.reasoning),
        approval: partial?.approval || null,
        approvalBusy: partial?.approvalBusy === true,
        approvalError: String(partial?.approvalError || ""),
        status: String(partial?.status || ""),
        timeText: String(partial?.timeText || ""),
        durationText: String(partial?.durationText || ""),
        durationVisible: partial?.durationVisible === true,
        durationVisibleManual: partial?.durationVisibleManual === true,
        taskId: String(partial?.taskId || ""),
        rawJSON: String(partial?.rawJSON || ""),
        pendingSeed: String(partial?.pendingSeed || ""),
        presentation: String(partial?.presentation || ""),
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

    function placeSteeredAgentAfterUser(userHistoryID, provisionalAgentHistoryID, targetTaskID) {
      const targetID = String(targetTaskID || "").trim();
      const targetAgent = chatHistoryItems.value.find((item) => {
        return (
          item.id !== provisionalAgentHistoryID &&
          String(item?.role || "") === "agent" &&
          String(item?.taskId || "").trim() === targetID
        );
      });
      if (!targetAgent) {
        patchHistoryItem(provisionalAgentHistoryID, {
          taskId: targetID,
          status: "running",
          rawJSON: "",
        });
        return {
          historyID: provisionalAgentHistoryID,
          needsTracking: true,
        };
      }

      const next = chatHistoryItems.value.filter((item) => {
        return item.id !== provisionalAgentHistoryID && item.id !== targetAgent.id;
      });
      const userIndex = next.findIndex((item) => item.id === userHistoryID);
      if (userIndex < 0) {
        next.push(targetAgent);
      } else {
        next.splice(userIndex + 1, 0, targetAgent);
      }
      replaceHistoryItems(next);
      return {
        historyID: targetAgent.id,
        needsTracking: false,
      };
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

    async function loadApprovalDetails(endpointRef, expectedHistoryVersion = historyLoadVersion) {
      const unrequested = chatHistoryItems.value
        .map((item) => ({
          approvalRequestID: String(item?.approval?.approvalRequestID || "").trim(),
          status: String(item?.approval?.status || "pending").trim().toLowerCase(),
        }))
        .filter(
          (item) =>
            item.approvalRequestID !== "" &&
            !approvalDetailAttempts.has(`${item.status}:${item.approvalRequestID}`)
        );
      if (!endpointRef || unrequested.length === 0) {
        return;
      }
      for (const item of unrequested) {
        approvalDetailAttempts.add(`${item.status}:${item.approvalRequestID}`);
      }

      const requests = [];
      if (unrequested.some((item) => item.status === "pending")) {
        const path = "/approvals?status=pending&limit=200";
        requests.push(
          loadResource(
            resourceKey("chat", "approval-details", endpointRef, expectedHistoryVersion),
            () => runtimeApiFetchForEndpoint(endpointRef, path)
          )
            .then((payload) => (Array.isArray(payload?.items) ? payload.items : []))
            .catch(() => [])
        );
      }
      for (const item of unrequested) {
        if (item.status === "pending") {
          continue;
        }
        const path = `/approvals/${encodeURIComponent(item.approvalRequestID)}`;
        requests.push(
          runtimeApiFetchForEndpoint(endpointRef, path)
            .then((payload) => [payload])
            .catch(() => [])
        );
      }
      const responseItems = (await Promise.all(requests)).flat();
      if (!viewActive || expectedHistoryVersion !== historyLoadVersion) {
        return;
      }

      const details = approvalDetailsByID({ items: responseItems });
      let changed = false;
      const nextItems = chatHistoryItems.value.map((item) => {
        const approvalRequestID = String(item?.approval?.approvalRequestID || "").trim();
        const detail = details.get(approvalRequestID);
        if (!detail) {
          return item;
        }
        changed = true;
        const existingStatus = String(item?.approval?.status || "").trim().toLowerCase();
        return {
          ...item,
          approval: {
            ...item.approval,
            ...detail,
            status:
              existingStatus === "denied" || existingStatus === "expired"
                ? existingStatus
                : detail.status || existingStatus || "pending",
            reasons: detail.reasons.map((reason) => chatApprovalReasonText(reason, t)),
          },
        };
      });
      if (changed) {
        replaceHistoryItems(nextItems);
        scrollHistoryToBottom();
      }
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
        const plan = taskPlan(detail) || normalizePlan(existingItem?.plan);
        const activity = taskActivity(detail) || normalizeActivity(existingItem?.activity);
        const pendingSeed = historyPendingSeed(existingItem, taskID);
        const preservePendingText =
          !isTerminalStatus(status) && String(existingItem?.approval?.approvalRequestID || "").trim() === "";
        const polledApproval = taskApprovalState(detail);
        const nextApproval =
          polledApproval &&
          polledApproval.approvalRequestID === String(existingItem?.approval?.approvalRequestID || "").trim()
            ? { ...existingItem.approval, ...polledApproval }
            : polledApproval;
        patchAgentHistoryItem(taskID, historyID, {
          plan,
          activity,
          reasoning: taskReasoning(detail) || normalizeReasoning(existingItem?.reasoning),
          approval: nextApproval,
          approvalBusy: false,
          approvalError: "",
          status,
          text: taskAgentText(detail, t, {
            agentName: activeAgentName.value,
            pendingSeed,
            pendingText: preservePendingText ? existingItem?.text : "",
          }),
          timeText: historyTimeLabel(detail?.finished_at || detail?.started_at || detail?.created_at),
          durationText: taskDurationLabel(detail, t),
          rawJSON: taskRawJSON(detail),
          pendingSeed,
        });
        if (nextApproval) {
          void loadApprovalDetails(endpointRef);
        }
        if (isTerminalStatus(status)) {
          closeTaskStream(taskID);
          if (consoleTopicsEnabled.value) {
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
          approvalBusy: false,
          approvalError: "",
          text: e?.message || t("msg_load_failed"),
          rawJSON: "",
        });
      }
    }

    function resetTopicState() {
      topics.value = [];
      topicsLoading.value = false;
      topicsLoadingOlder.value = false;
      topicsNextCursor.value = "";
      selectedTopicID.value = "";
      applyComposerTopicLLMProfile("");
      creatingTopic.value = false;
      showSystemTopics.value = false;
      topicDeleteDialogOpen.value = false;
      topicDeleteTarget.value = null;
      topicDeleting.value = false;
      topicDeleteError.value = "";
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
      const currentID = normalizeTopicID(selectedTopicID.value);

      topicsLoading.value = true;
      try {
        const data = await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/topics?limit=${encodeURIComponent(TOPIC_PAGE_LIMIT)}`
        );
        const items = Array.isArray(data?.items) ? [...data.items] : [];
        const targetTopicID = preferredTopicID || currentID;
        if (targetTopicID && !items.some((topic) => normalizeTopicID(topic?.id) === targetTopicID)) {
          try {
            const targetTopic = await runtimeApiFetchForEndpoint(
              submitEndpointRef.value,
              `/topics/${encodeURIComponent(targetTopicID)}`
            );
            if (normalizeTopicID(targetTopic?.id) === targetTopicID) {
              items.push(targetTopic);
            }
          } catch (e) {
            if (e?.status !== 404) {
              throw e;
            }
          }
        }
        items.sort(compareTopicsNewestFirst);
        topics.value = items;
        topicsNextCursor.value = String(data?.next_cursor || "").trim();

        if (preferredTopicID && items.some((topic) => normalizeTopicID(topic?.id) === preferredTopicID)) {
          selectedTopicID.value = preferredTopicID;
          rememberTopicSelection(submitEndpointRef.value, preferredTopicID);
          creatingTopic.value = false;
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (preserveDraft && creatingTopic.value) {
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (currentID && items.some((topic) => normalizeTopicID(topic?.id) === currentID)) {
          rememberTopicSelection(submitEndpointRef.value, currentID);
          creatingTopic.value = false;
          syncMobileTopicView({ preferChat: true });
          return true;
        }
        if (currentID === AWARENESS_TOPIC_ID && showSystemTopics.value) {
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

    async function loadOlderTopics() {
      const cursor = String(topicsNextCursor.value || "").trim();
      if (!cursor || topicsLoading.value || topicsLoadingOlder.value || !consoleTopicsEnabled.value) {
        return;
      }
      topicsLoadingOlder.value = true;
      try {
        const data = await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/topics?limit=${encodeURIComponent(TOPIC_PAGE_LIMIT)}&cursor=${encodeURIComponent(cursor)}`
        );
        const known = new Set(topics.value.map((topic) => normalizeTopicID(topic?.id)).filter(Boolean));
        const older = (Array.isArray(data?.items) ? data.items : []).filter((topic) => {
          const id = normalizeTopicID(topic?.id);
          if (!id || known.has(id)) {
            return false;
          }
          known.add(id);
          return true;
        });
        const items = [...topics.value, ...older];
        items.sort(compareTopicsNewestFirst);
        topics.value = items;
        topicsNextCursor.value = String(data?.next_cursor || "").trim();
      } catch (e) {
        err.value = e?.message || t("msg_load_failed");
      } finally {
        topicsLoadingOlder.value = false;
      }
    }

    async function loadHistory(options = {}) {
      clearPollTimers();
      clearStreamSockets();
      err.value = "";
      const currentHistoryLoadVersion = historyLoadVersion + 1;
      historyLoadVersion = currentHistoryLoadVersion;
      approvalDetailAttempts.clear();
      historyLoadingOlder.value = false;
      historyNextCursor.value = "";
      const endpointRef = submitEndpointRef.value;
      if (!endpointRef) {
        applyComposerTopicLLMProfile("");
        replaceHistoryItems([]);
        historyLoading.value = false;
        return true;
      }
      historyLoading.value = true;
      const preserveCurrent = Boolean(options.preserveCurrent);
      try {
        let path = `/tasks?limit=${CHAT_HISTORY_LIMIT}`;
        if (consoleTopicsEnabled.value) {
          if (creatingTopic.value) {
            applyComposerTopicLLMProfile("");
            replaceHistoryItems([]);
            historyAutoStick.value = true;
            return true;
          }
          const topicID = normalizeTopicID(selectedTopicID.value);
          if (!topicID) {
            applyComposerTopicLLMProfile("");
            replaceHistoryItems([]);
            historyAutoStick.value = true;
            return true;
          }
          path = `/tasks?limit=${CHAT_HISTORY_LIMIT}&topic_id=${encodeURIComponent(topicID)}`;
        }

        const data = await loadResource(
          resourceKey("chat", "history", endpointRef, path),
          () => runtimeApiFetchForEndpoint(endpointRef, path)
        );
        if (!viewActive || currentHistoryLoadVersion !== historyLoadVersion) {
          return true;
        }
        const tasks = Array.isArray(data?.items) ? data.items : [];
        historyNextCursor.value = String(data?.next_cursor || "").trim();
        if (consoleTopicsEnabled.value) {
          applyComposerTopicLLMProfile(lastUsedChatLLMProfile(tasks));
        }
        const nextItems = taskListHistoryItems(tasks, t, {
          agentName: activeAgentName.value,
          endpointRef,
          locale: currentLocale(),
        });
        replaceHistoryItems(nextItems.length > 0 ? nextItems : [emptyHistoryItem()]);
        scrollHistoryToBottom({ force: true });
        void loadApprovalDetails(endpointRef, currentHistoryLoadVersion);
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
        if (viewActive && currentHistoryLoadVersion === historyLoadVersion) {
          if (!preserveCurrent) {
            replaceHistoryItems([]);
          }
          err.value = e?.message || t("msg_load_failed");
        }
        return false;
      } finally {
        if (viewActive && currentHistoryLoadVersion === historyLoadVersion) {
          historyLoading.value = false;
        }
      }
    }

    async function loadOlderHistory() {
      const cursor = String(historyNextCursor.value || "").trim();
      const endpointRef = String(submitEndpointRef.value || "").trim();
      if (!cursor || !endpointRef || historyLoading.value || historyLoadingOlder.value) {
        return;
      }
      const currentHistoryLoadVersion = historyLoadVersion;
      let path = `/tasks?limit=${CHAT_HISTORY_LIMIT}&cursor=${encodeURIComponent(cursor)}`;
      if (consoleTopicsEnabled.value) {
        const topicID = normalizeTopicID(selectedTopicID.value);
        if (!topicID || creatingTopic.value) {
          return;
        }
        path += `&topic_id=${encodeURIComponent(topicID)}`;
      }

      historyLoadingOlder.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, path);
        if (!viewActive || currentHistoryLoadVersion !== historyLoadVersion) {
          return;
        }
        const olderItems = taskListHistoryItems(Array.isArray(data?.items) ? data.items : [], t, {
          agentName: activeAgentName.value,
          endpointRef,
          locale: currentLocale(),
        });
        const viewport = historyViewportElement();
        const previousHeight = viewport?.scrollHeight || 0;
        const previousTop = viewport?.scrollTop || 0;
        historyNextCursor.value = String(data?.next_cursor || "").trim();
        replaceHistoryItems(
          placeSteeredAgentsAfterUsers([...olderItems, ...chatHistoryItems.value])
        );
        void loadApprovalDetails(endpointRef, currentHistoryLoadVersion);
        await nextTick();
        if (viewport) {
          viewport.scrollTop = viewport.scrollHeight - previousHeight + previousTop;
        }
      } catch (e) {
        if (viewActive && currentHistoryLoadVersion === historyLoadVersion) {
          err.value = e?.message || t("msg_load_failed");
        }
      } finally {
        if (currentHistoryLoadVersion === historyLoadVersion) {
          historyLoadingOlder.value = false;
        }
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
        applyComposerTopicLLMProfile("");
        syncMobileTopicView({ preferTopics: true });
        await loadHistory();
        return;
      }
      if (topicID === AWARENESS_TOPIC_ID) {
        showSystemTopics.value = true;
        creatingTopic.value = false;
        selectedTopicID.value = topicID;
        applyComposerTopicLLMProfile("");
        syncMobileTopicView({ preferChat: true });
        await loadHistory();
        return;
      }
      if (!options.force && normalizeTopicID(selectedTopicID.value) === topicID && !creatingTopic.value) {
        return;
      }
      creatingTopic.value = false;
      selectedTopicID.value = "";
      applyComposerTopicLLMProfile("");
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

    async function openRawDialog(item) {
      resetRawReveal();
      const json = String(item?.rawJSON || "").trim();
      if (!json) {
        rawDialogJSON.value = "";
        rawDialogOpen.value = false;
        return;
      }
      if (await openRawJsonDesktopWindow({ title: "RAW JSON", json }).catch(() => false)) {
        return;
      }
      rawDialogJSON.value = json;
      rawDialogOpen.value = true;
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
      const itemID = String(item?.id || "").trim();
      if (!itemID) {
        return;
      }
      if (String(item?.durationText || "").trim()) {
        patchHistoryItem(itemID, {
          durationVisible: item?.durationVisible !== true,
          durationVisibleManual: true,
        });
      }
      if (!String(item?.rawJSON || "").trim()) {
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
      }, 2000);
    }

    function clickTopicSidebarTitle() {
      heartbeatRevealCount.value += 1;
      if (heartbeatRevealCount.value >= 5) {
        showSystemTopics.value = !showSystemTopics.value;
        resetHeartbeatReveal();
        return;
      }
      queueHeartbeatRevealReset();
    }

    function closeTopicDeleteDialog() {
      topicDeleteDialogOpen.value = false;
      topicDeleteTarget.value = null;
    }

    function confirmDeleteTopic() {
      if (!topicDeleteAvailable.value || !selectedTopic.value) {
        return;
      }
      topicDeleteTarget.value = {
        id: normalizeTopicID(selectedTopic.value.id),
        title: topicTitle(selectedTopic.value),
        created_at: selectedTopic.value.created_at,
        updated_at: selectedTopic.value.updated_at,
      };
      topicDeleteDialogOpen.value = true;
    }

    async function deleteSelectedTopic() {
      if (topicDeleting.value) {
        return;
      }
      const endpointRef = String(submitEndpointRef.value || "").trim();
      const topicID = normalizeTopicID(topicDeleteTarget.value?.id || selectedTopicID.value);
      if (!endpointRef || !topicID || topicID === DEFAULT_TOPIC_ID || topicID === AWARENESS_TOPIC_ID) {
        closeTopicDeleteDialog();
        return;
      }

      topicDeleting.value = true;
      topicDeleteDialogOpen.value = false;
      topicDeleteError.value = "";
      err.value = "";
      try {
        await runtimeApiFetchForEndpoint(endpointRef, `/topics/${encodeURIComponent(topicID)}`, {
          method: "DELETE",
        });
        clearChatDraft(endpointRef, topicID);
        if (normalizeTopicID(selectedTopicID.value) === topicID) {
          selectedTopicID.value = "";
          creatingTopic.value = false;
          resetWorkspaceState();
          await syncChatRoute("", { replace: true });
        }
        await loadTopics();
        await loadHistory();
      } catch (e) {
        topicDeleteError.value = e?.message || t("msg_delete_failed");
      } finally {
        topicDeleting.value = false;
        topicDeleteTarget.value = null;
      }
    }

    async function decideHistoryApproval(item, decision) {
      const approvalRequestID = String(item?.approval?.approvalRequestID || "").trim();
      const taskID = String(item?.taskId || "").trim();
      const itemID = String(item?.id || "").trim();
      const action = String(decision || "").trim().toLowerCase();
      if (!approvalRequestID || !taskID || !itemID || (action !== "approve" && action !== "deny")) {
        return;
      }
      patchHistoryItem(itemID, {
        approvalBusy: true,
        approvalError: "",
      });
      try {
        const decisionResult = await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/approvals/${encodeURIComponent(approvalRequestID)}/${action}`,
          {
            method: "POST",
            body: {
              actor: "console:user",
            },
          }
        );
        const decisionError = String(decisionResult?.error || "").trim();
        if (action === "approve") {
          if (decisionResult?.resumed === false && decisionError) {
            patchHistoryItem(itemID, {
              approval: null,
              approvalBusy: false,
              approvalError: "",
              status: "failed",
              text: decisionError,
            });
            await pollTask(taskID, itemID, submitEndpointRef.value);
            return;
          }
          patchHistoryItem(itemID, {
            approval: null,
            approvalBusy: false,
            approvalError: "",
            status: "queued",
            text: buildPollingHint(activeAgentName.value, t, historyPendingSeed(item, taskID)),
          });
        } else {
          patchHistoryItem(itemID, {
            approval: {
              ...item.approval,
              message: t("chat_approval_denied"),
              status: "denied",
            },
            approvalBusy: false,
            approvalError: "",
            status: "canceled",
            text: t("chat_approval_denied"),
          });
        }
        await pollTask(taskID, itemID, submitEndpointRef.value);
      } catch (e) {
        patchHistoryItem(itemID, {
          approvalBusy: false,
          approvalError: e?.message || t("msg_load_failed"),
        });
      }
    }

    function approveHistoryApproval(item) {
      void decideHistoryApproval(item, "approve");
    }

    function denyHistoryApproval(item) {
      void decideHistoryApproval(item, "deny");
    }

    function selectTopic(topicID) {
      const normalized = normalizeTopicID(topicID);
      if (!normalized) {
        return;
      }
      topicDeleteError.value = "";
      closeTopicDeleteDialog();
      creatingTopic.value = false;
      pendingWorkspaceDir.value = "";
      workspaceBrowserPendingMode.value = false;
      selectedTopicID.value = normalized;
      applyComposerTopicLLMProfile("");
      rememberTopicSelection(submitEndpointRef.value, normalized);
      syncMobileTopicView({ preferChat: true });
      void loadHistory().finally(() => {
        focusComposer();
      });
      void syncChatRoute(normalized);
    }

    function startNewTopic() {
      creatingTopic.value = true;
      selectedTopicID.value = "";
      applyComposerTopicLLMProfile("");
      err.value = "";
      topicDeleteError.value = "";
      pendingWorkspaceDir.value = "";
      workspaceBrowserPendingMode.value = false;
      closeTopicDeleteDialog();
      resetHeartbeatReveal();
      syncMobileTopicView({ preferChat: true });
      void loadHistory();
      syncComposer();
      focusComposer();
      void syncChatRoute("", { replace: true });
    }

    async function stopActiveTask() {
      const taskID = String(activeTaskItem.value?.taskId || "").trim();
      if (!taskID || sending.value) {
        return;
      }
      const endpointRef = String(submitEndpointRef.value || "").trim();
      if (!endpointRef) {
        err.value = submitBlockedMessage.value || t("msg_select_endpoint");
        return;
      }

      sending.value = true;
      err.value = "";
      try {
        await runtimeApiFetchForEndpoint(endpointRef, `/tasks/${encodeURIComponent(taskID)}/stop`, {
          method: "POST",
        });
      } catch (e) {
        err.value = e?.message || t("msg_load_failed");
      } finally {
        sending.value = false;
        focusComposer();
      }
    }

    async function submitTask() {
      const task = String(taskInput.value || "").trim();
      if (!task || sending.value || composerUploading.value) {
        return;
      }
      const submittedDraftScope = composerDraftScope.value;
      const endpointRef = submitEndpointRef.value;
      if (!endpointRef) {
        err.value = submitBlockedMessage.value || t("msg_select_endpoint");
        return;
      }
      const llmProfile = String(composerLLMProfile.value || "").trim();
      const pendingWorkspace = String(pendingWorkspaceDir.value || "").trim();
      const { requestBody, submittedFiles, fileReferences } = buildComposerSubmission({
        task,
        llmProfile,
        files: composerFiles.value,
        topicID:
          consoleTopicsEnabled.value && !creatingTopic.value
            ? normalizeTopicID(selectedTopicID.value)
            : "",
        workspaceDir: consoleTopicsEnabled.value ? pendingWorkspace : "",
      });

      sending.value = true;
      err.value = "";
      suppressDraftPersistence.value = true;
      taskInput.value = "";
      if (consoleTopicsEnabled.value && !normalizeTopicID(selectedTopicID.value)) {
        creatingTopic.value = true;
      }

      const userHistoryID = pushHistoryItem({
        role: "user",
        text: task,
        files: submittedFiles,
        endpointRef,
        topicID: requestBody.topic_id,
        timeText: historyTimeLabel(new Date().toISOString()),
      });
      const pendingSeed = newHistoryID();
      const agentHistoryID = pushHistoryItem({
        role: "agent",
        text: buildPollingHint(activeAgentName.value, t, pendingSeed),
        status: "queued",
        timeText: "",
        pendingSeed,
        presentation: isContextCompactCommand(task) ? "context-compact" : "",
      });
      scrollHistoryToBottom();

      try {
        const submitted = await runtimeApiFetchForEndpoint(endpointRef, "/tasks", {
          method: "POST",
          body: requestBody,
        });
        const taskID = String(submitted?.id || "").trim();
        const status = normalizeTaskStatus(submitted?.status);
        const steerTargetTaskID = String(submitted?.steer_target_task_id || "").trim();
        if (!taskID) {
          throw new Error(t("chat_missing_task_id"));
        }
        if (!steerTargetTaskID) {
          applyComposerTopicLLMProfile(llmProfile);
        }
        const submittedTopicID = normalizeTopicID(submitted?.topic_id || requestBody.topic_id);
        patchHistoryItem(userHistoryID, {
          taskId: taskID,
          files: normalizeHistoryFileReferences(fileReferences),
          endpointRef,
          topicID: submittedTopicID,
        });
        clearChatDraft(submittedDraftScope.endpointRef, submittedDraftScope.topicID);
        clearComposerFileDraft(submittedDraftScope);
        let trackedTaskID = taskID;
        let trackedHistoryID = agentHistoryID;
        let needsTracking = true;
        if (steerTargetTaskID) {
          const placement = placeSteeredAgentAfterUser(
            userHistoryID,
            agentHistoryID,
            steerTargetTaskID
          );
          trackedTaskID = steerTargetTaskID;
          trackedHistoryID = placement.historyID;
          needsTracking = placement.needsTracking;
          if (needsTracking) {
            void startTaskStream(trackedTaskID, trackedHistoryID, endpointRef);
          }
        } else {
          const existingAgentItem =
            chatHistoryItems.value.find((item) => item.id === agentHistoryID) || null;
          patchHistoryItem(agentHistoryID, {
            taskId: taskID,
            status,
            pendingSeed: historyPendingSeed(existingAgentItem, pendingSeed),
            rawJSON: "",
          });
          void startTaskStream(taskID, agentHistoryID, endpointRef);
        }

        if (consoleTopicsEnabled.value) {
          const topicID = normalizeTopicID(submitted?.topic_id);
          if (!topicID) {
            throw new Error(t("chat_missing_topic_id"));
          }
          creatingTopic.value = false;
          selectedTopicID.value = topicID;
          if (String(requestBody.workspace_dir || "").trim()) {
            pendingWorkspaceDir.value = "";
            applyWorkspacePayload({
              topic_id: topicID,
              workspace_dir: requestBody.workspace_dir,
              source: "attachment",
            });
            rememberWorkspaceBrowserRecentDir(requestBody.workspace_dir);
          }
          rememberTopicSelection(submitEndpointRef.value, topicID);
          await loadTopics({
            preferredTopicID: topicID,
            preserveSelection: true,
          });
          await syncChatRoute(topicID, { replace: true });
          if (steerTargetTaskID) {
            if (needsTracking) {
              await pollTask(trackedTaskID, trackedHistoryID, endpointRef);
            } else {
              scrollHistoryToBottom();
            }
            return;
          }
          await pollTask(taskID, agentHistoryID, endpointRef);
          return;
        }

        if (steerTargetTaskID) {
          if (needsTracking) {
            await pollTask(trackedTaskID, trackedHistoryID, endpointRef);
          } else {
            scrollHistoryToBottom();
          }
          return;
        }
        await pollTask(taskID, agentHistoryID, endpointRef);
      } catch (e) {
        const message = e?.message || t("msg_load_failed");
        err.value = message;
        rememberChatDraft(submittedDraftScope.endpointRef, submittedDraftScope.topicID, task);
        taskInput.value = task;
        patchHistoryItem(agentHistoryID, {
          status: "failed",
          text: message,
          rawJSON: "",
        });
      } finally {
        suppressDraftPersistence.value = false;
        sending.value = false;
        syncComposer();
        focusComposer();
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      dialogShellPreloadCancel = scheduleIdleCallback(() => {
        dialogShellPreloadCancel = null;
        void loadAppDialogShell().catch(() => {});
      });
      void refreshChatData({
        preferredTopicID: routeTopicID.value,
        preserveSelection: Boolean(routeTopicID.value),
      }).finally(() => {
        focusComposer();
      });
      void loadComposerLLMProfiles();
      syncComposer();
    });
    onUnmounted(() => {
      viewActive = false;
      closeComposerFilePreview();
      historyLoadVersion += 1;
      persistComposerDraft();
      window.removeEventListener("resize", refreshMobileMode);
      window.removeEventListener("keydown", onMobileWorkspaceSidebarKeydown);
      unlockMobileWorkspaceSidebarScroll();
      if (dialogShellPreloadCancel) {
        dialogShellPreloadCancel();
        dialogShellPreloadCancel = null;
      }
      clearPollTimers();
      clearStreamSockets();
      resetRawReveal();
      resetHeartbeatReveal();
      resetHistoryCopyState();
    });
    watch(
      () => `${endpointState.selectedRef}\u0000${submitEndpointRef.value}`,
      () => {
        composerCommandsLoadSeq += 1;
        composerCommands.value = [];
        composerCommandsLoading.value = false;
        composerLLMProfilesLoadSeq += 1;
        composerDefaultLLMProfile.value = null;
        composerLLMProfiles.value = [];
        applyComposerTopicLLMProfile("");
        composerSkillsLoadSeq += 1;
        composerSkills.value = [];
        composerSkillsLoading.value = false;
        composerSkillsError.value = "";
        resetTopicState();
        void refreshChatData({
          preferredTopicID: routeTopicID.value,
          preserveSelection: Boolean(routeTopicID.value),
        }).finally(() => {
          focusComposer();
        });
        void loadComposerLLMProfiles();
        syncComposer();
      }
    );
    watch(
      () => `${submitEndpointRef.value}\u0000${workspaceTopicID.value}\u0000${consoleTopicsEnabled.value ? "1" : "0"}`,
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
      () => mobileWorkspaceSidebarVisible.value,
      async (visible) => {
        if (visible) {
          mobileWorkspaceFocusBeforeOpen = document.activeElement;
          mobileWorkspaceBodyOverflowBeforeOpen = document.body.style.overflow;
          document.body.style.overflow = "hidden";
          mobileWorkspaceScrollLocked = true;
          window.addEventListener("keydown", onMobileWorkspaceSidebarKeydown);
          await nextTick();
          mobileWorkspaceSidebarPanel.value?.focus();
          return;
        }

        window.removeEventListener("keydown", onMobileWorkspaceSidebarKeydown);
        unlockMobileWorkspaceSidebarScroll();
        if (
          mobileWorkspaceFocusBeforeOpen instanceof HTMLElement &&
          document.contains(mobileWorkspaceFocusBeforeOpen)
        ) {
          mobileWorkspaceFocusBeforeOpen.focus();
        }
        mobileWorkspaceFocusBeforeOpen = null;
      },
      { immediate: true }
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
    watch(
      () => composerDraftScope.value,
      (nextScope, prevScope) => {
        if (prevScope?.endpointRef) {
          persistComposerDraft(prevScope);
        }
        restoreComposerDraft(nextScope);
        restoreComposerFileDraft(nextScope);
      },
      { immediate: true }
    );
    watch(taskInput, () => {
      if (!suppressDraftPersistence.value) {
        persistComposerDraft();
      }
      syncComposer();
    });

    return {
      t,
      chatHistoryItems,
      copiedHistoryItemID,
      historyLoading,
      historyLoadingOlder,
      historyNextCursor,
      historyViewport,
      loadOlderHistory,
      selectedTopicID,
      topics,
      topicsLoading,
      topicsLoadingOlder,
      topicsNextCursor,
      visibleTopics,
      creatingTopic,
      loadOlderTopics,
      taskInput,
      sending,
      err,
      workspaceDir,
      workspaceSource,
      workspaceDirDisplay,
      workspaceLoading,
      workspaceSaving,
      workspaceOpening,
      workspaceDownloading,
      composerUploading,
      composerFileInput,
      composerFiles,
      composerFileLabels,
      composerFilePreviewOpen,
      composerFilePreviewLoading,
      composerFilePreviewName,
      composerFilePreviewKind,
      composerFilePreviewURL,
      composerFilePreviewText,
      composerFilePreviewError,
      composerFilePreviewItems,
      composerFilePreviewHasPrevious,
      composerFilePreviewHasNext,
      workspaceBusy,
      workspaceSidebarOpen,
      mobileWorkspaceSidebarPanel,
      workspaceSidebarTabID,
      workspacePanelTabs,
      selectedWorkspacePanelTab,
      topicPropertyRows,
      topicContextProgress,
      topicDeleteAvailable,
      topicDeleteDisabled,
      topicDeleting,
      topicDeleteError,
      topicDeleteDialogOpen,
      topicDeleteDialogText,
      topicDeleteDialogActions,
      workspaceError,
      workspaceReady,
      workspaceHintText,
      workspaceAttachDisabled,
      composerWorkspaceAttachDisabled,
      composerAddDisabled,
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
      workspaceBrowserPlaceSourceItems,
      workspaceBrowserSelection,
      workspaceBrowserShowHidden,
      workspaceBrowserCreateOpen,
      workspaceBrowserCreateName,
      workspaceBrowserCreating,
      workspaceBrowserCreateField,
      workspaceBrowserCreateParent,
      workspaceBrowserCreateDisabled,
      workspaceBrowserCreateSubmitDisabled,
      pendingWorkspaceDir,
      workspaceBrowserEmptyText,
      workspaceBrowserConfirmDisabled,
      workspaceBrowserTreeEntryClass,
      formatBytes,
      workspaceTreeIcon,
      workspaceTreeEntryClass,
      composerRef,
      composerAttachActive,
      chatDisclaimer,
      composerInputHistory,
      composerCommands,
      composerLLMProfile,
      composerLLMProfileItems,
      composerSkills,
      composerSkillsLoading,
      composerSkillsError,
      composerSuggestionLabels,
      ensureComposerCommandsLoaded,
      ensureComposerSkillsLoaded,
      submitBlockedMessage,
      chatReadonly,
      readonlyTitle,
      readonlyKickerLeft,
      readonlyReason,
      pageClass,
      showChatPlaceholder,
      chatPlaceholderText,
      composerDisabled,
      sendDisabled,
      composerStopMode,
      composerActionLabel,
      composerPlaceholder,
      displayAgentName,
      submitEndpointRef,
      consoleTopicsEnabled,
      mobileMode,
      mobileTopicSplitEnabled,
      mobileBarTitle,
      mobileShowBack,
      shellClass,
      chatMainClass,
      chatMainStyle,
      deskTitle,
      chatPlaceholderHint,
      showTopicSidebar,
      showChatPane,
      workspaceSidebarAvailable,
      desktopWorkspaceSidebarVisible,
      mobileWorkspaceSidebarVisible,
      submitTask,
      stopActiveTask,
      updateComposerHeight,
      toggleWorkspaceSidebar,
      closeMobileWorkspaceSidebar,
      onWorkspaceTabChange,
      selectWorkspaceTreeNode,
      addWorkspaceSelectionToComposer,
      openWorkspaceSelection,
      downloadWorkspaceSelection,
      toggleWorkspaceTreeNode,
      openWorkspaceBrowser,
      openComposerWorkspaceBrowser,
      openComposerFilePicker,
      uploadComposerFiles,
      previewComposerFile,
      navigateComposerFilePreview,
      removeComposerFile,
      closeComposerFilePreview,
      closeWorkspaceBrowser,
      activateWorkspaceBrowserSource,
      workspaceBrowserSourceItemClass,
      setWorkspaceBrowserShowHidden,
      toggleWorkspaceBrowserNode,
      selectWorkspaceBrowserNode,
      openWorkspaceBrowserCreate,
      cancelWorkspaceBrowserCreate,
      createWorkspaceBrowserDir,
      attachWorkspace,
      detachWorkspace,
      selectTopic,
      startNewTopic,
      confirmDeleteTopic,
      showTopicsView,
      topicTitle,
      topicTime,
      topicItemClass,
      topicIsActive,
      clickTopicSidebarTitle,
      handleHistoryScroll,
      handleMarkdownRendered,
      markHistoryPointerScrollIntent,
      markHistoryScrollIntent,
      autoPreviewHistoryID,
      historyStreamProfilerEnabled,
      chatStatusExpandedState,
      markHistoryItemRendered,
      copyHistoryItem,
      toggleChatStatus,
      clickHistoryTime,
      approveHistoryApproval,
      denyHistoryApproval,
      openRawDialog,
      closeRawDialog,
      rawDialogOpen,
      rawDialogJSON,
    };
  },
  template: `
    <AppPage
      :title="t('chat_title')"
      :class="pageClass"
      :hideDesktopBar="true"
      :hideMobileBar="showTopicSidebar"
      :overlayBar="true"
    >
      <template v-if="consoleTopicsEnabled" #leading>
        <div :class="mobileTopicSplitEnabled ? 'chat-page-bar-mobile' : 'chat-page-bar-desktop'">
          <QButton
            v-if="mobileShowBack"
            class="plain xs icon chat-page-bar-back"
            :title="t('chat_topics_title')"
            :aria-label="t('chat_topics_title')"
            @click="showTopicsView"
          >
            <PhArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ mobileTopicSplitEnabled ? mobileBarTitle : t("chat_title") }}</h2>
          <QButton
            v-if="mobileTopicSplitEnabled && showChatPane && workspaceSidebarAvailable"
            :class="workspaceSidebarOpen ? 'plain sm icon chat-workspace-toggle is-active' : 'plain sm icon chat-workspace-toggle'"
            :title="workspaceSidebarToggleLabel"
            :aria-label="workspaceSidebarToggleLabel"
            @click="toggleWorkspaceSidebar"
          >
            <PhSidebarSimple class="icon" />
          </QButton>
        </div>
      </template>
      <QFence v-if="err" type="danger" icon="PhXCircle" :text="err" />
      <input
        ref="composerFileInput"
        type="file"
        multiple
        hidden
        @change="uploadComposerFiles"
      />
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
              <div class="chat-topic-sidebar-copy" @click="clickTopicSidebarTitle">
                <div class="chat-topic-sidebar-title-row">
                  <h3 class="chat-topic-sidebar-title workspace-section-title">{{ t("chat_topics_title") }}</h3>
                </div>
              </div>
              <QButton
                class="plain sm icon chat-topic-sidebar-new"
                :title="t('chat_topic_new')"
                :aria-label="t('chat_topic_new')"
                @click="startNewTopic"
              >
                <PhPlus class="icon" />
              </QButton>
            </header>
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
                    <span v-if="topicTime(topic)" class="chat-topic-item-meta workspace-sidebar-item-meta">
                      <time class="chat-topic-item-time">{{ topicTime(topic) }}</time>
                    </span>
                  </span>
                </span>
                <span class="chat-topic-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                  <QBadge v-if="topicIsActive(topic)" dot type="primary" size="sm" />
                </span>
              </button>
              <QButton
                v-if="topicsNextCursor"
                class="plain sm chat-topic-load-older"
                :loading="topicsLoadingOlder"
                @click="loadOlderTopics"
              >
                {{ t('chat_topics_load_older') }}
              </QButton>
            </div>
          </aside>
          <section v-if="showChatPane" :class="chatMainClass" :style="chatMainStyle">
            <header v-if="consoleTopicsEnabled && !showChatPlaceholder" class="chat-desk-head">
              <div class="chat-desk-head-main">
                <div class="chat-desk-copy">
                  <h3 class="chat-desk-title workspace-document-title">{{ deskTitle }}</h3>
                </div>
                <div v-if="workspaceSidebarAvailable" class="chat-desk-tools">
                  <QButton
                    :class="workspaceSidebarOpen ? 'plain sm icon chat-workspace-toggle is-active' : 'plain sm icon chat-workspace-toggle'"
                    :title="workspaceSidebarToggleLabel"
                    :aria-label="workspaceSidebarToggleLabel"
                    @click="toggleWorkspaceSidebar"
                  >
                    <PhSidebarSimple class="icon" />
                  </QButton>
                </div>
              </div>
            </header>
            <section v-if="showChatPlaceholder" class="chat-placeholder">
              <div class="chat-placeholder-copy">
                <h3 class="chat-placeholder-title workspace-document-title">{{ deskTitle }}</h3>
                <p class="chat-placeholder-note">{{ chatPlaceholderHint }}</p>
              </div>
              <ChatComposer
                ref="composerRef"
                v-model="taskInput"
                landing
                :disabled="composerDisabled"
                :placeholder="composerPlaceholder"
                :send-disabled="sendDisabled"
                :sending="sending"
                :stop-mode="composerStopMode"
                :attach-active="composerAttachActive"
                :attach-disabled="composerAddDisabled"
                :add-label="t('chat_composer_add')"
                :attach-label="t('chat_composer_add_workspace')"
                :upload-label="t('chat_composer_upload_files')"
                :uploading="composerUploading"
                :file-items="composerFiles"
                :file-labels="composerFileLabels"
                :send-label="composerActionLabel"
                :submit-on-enter="!mobileMode"
                :input-history="composerInputHistory"
                :commands="composerCommands"
                v-model:llm-profile-value="composerLLMProfile"
                :llm-profile-items="composerLLMProfileItems"
                :llm-profile-label="t('chat_llm_profile_label')"
                :skills="composerSkills"
                :skills-loading="composerSkillsLoading"
                :skills-error="composerSkillsError"
                :suggestion-labels="composerSuggestionLabels"
                @attach="openComposerWorkspaceBrowser"
                @upload="openComposerFilePicker"
                @preview-file="previewComposerFile"
                @remove-file="removeComposerFile"
                @submit="submitTask"
                @stop="stopActiveTask"
                @request-commands="ensureComposerCommandsLoaded"
                @request-skills="ensureComposerSkillsLoaded"
                @height-change="updateComposerHeight"
              />
            </section>
            <template v-else>
              <div
                ref="historyViewport"
                class="chat-history"
                @pointerdown.passive="markHistoryPointerScrollIntent"
                @scroll.passive="handleHistoryScroll"
                @touchmove.passive="markHistoryScrollIntent"
                @wheel.passive="markHistoryScrollIntent"
              >
                <QButton
                  v-if="historyNextCursor"
                  class="plain sm chat-history-load-older"
                  :loading="historyLoadingOlder"
                  @click="loadOlderHistory"
                >
                  {{ t('chat_history_load_older') }}
                </QButton>
                <ChatHistoryList
                  :items="chatHistoryItems"
                  :loading="historyLoading"
                  :loading-text="t('chat_history_loading')"
                  :empty-text="t('chat_empty')"
                  :footer-text="chatDisclaimer"
                  :submit-endpoint-ref="submitEndpointRef"
                  :selected-topic-id="selectedTopicID"
                  :copied-item-id="copiedHistoryItemID"
                  :expanded-state="chatStatusExpandedState"
                  :auto-preview-item-id="autoPreviewHistoryID"
                  :stream-profiler="historyStreamProfilerEnabled"
                  :copy-label="t('action_copy')"
                  :file-preview-label="t('chat_composer_file_preview')"
                  :approval-approve-label="t('chat_approval_approve')"
                  :approval-deny-label="t('chat_approval_deny')"
                  :approval-title="t('chat_approval_title')"
                  @rendered="markHistoryItemRendered"
                  @copy="copyHistoryItem"
                  @preview-file="previewComposerFile"
                  @toggle-status="toggleChatStatus"
                  @time-click="clickHistoryTime"
                  @approval-approve="approveHistoryApproval"
                  @approval-deny="denyHistoryApproval"
                />
              </div>
            </template>
            <ChatComposer
              v-if="!showChatPlaceholder"
              ref="composerRef"
              v-model="taskInput"
              :disabled="composerDisabled"
              :placeholder="composerPlaceholder"
              :send-disabled="sendDisabled"
              :sending="sending"
              :stop-mode="composerStopMode"
              :attach-active="composerAttachActive"
              :attach-disabled="composerAddDisabled"
              :add-label="t('chat_composer_add')"
              :attach-label="t('chat_composer_add_workspace')"
              :upload-label="t('chat_composer_upload_files')"
              :uploading="composerUploading"
              :file-items="composerFiles"
              :file-labels="composerFileLabels"
              :send-label="composerActionLabel"
              :submit-on-enter="!mobileMode"
              :input-history="composerInputHistory"
              :commands="composerCommands"
              v-model:llm-profile-value="composerLLMProfile"
              :llm-profile-items="composerLLMProfileItems"
              :llm-profile-label="t('chat_llm_profile_label')"
              :skills="composerSkills"
              :skills-loading="composerSkillsLoading"
              :skills-error="composerSkillsError"
              :suggestion-labels="composerSuggestionLabels"
              @attach="openComposerWorkspaceBrowser"
              @upload="openComposerFilePicker"
              @preview-file="previewComposerFile"
              @remove-file="removeComposerFile"
              @submit="submitTask"
              @stop="stopActiveTask"
              @request-commands="ensureComposerCommandsLoaded"
              @request-skills="ensureComposerSkillsLoaded"
              @height-change="updateComposerHeight"
            />
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
                <template v-if="workspaceSidebarTabID === 'workspace'">
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
                          <PhPlus class="icon" />
                        </QButton>
                        <QButton
                          v-if="workspaceSource === 'attachment'"
                          class="plain xs icon"
                          :title="t('chat_workspace_action_detach')"
                          :aria-label="t('chat_workspace_action_detach')"
                          :disabled="workspaceDetachDisabled"
                          :loading="workspaceSaving"
                          @click="detachWorkspace"
                        >
                          <PhTrash class="icon" />
                        </QButton>
                      </div>
                    </header>

                    <QFence
                      v-if="workspaceError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="PhXCircle"
                      :text="workspaceError"
                    />

                    <QFence
                      v-if="workspaceTreeError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="PhXCircle"
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
                              <PhPlus class="icon" />
                            </QButton>
                            <QButton
                              class="plain xs icon"
                              :title="t('chat_workspace_action_open')"
                              :aria-label="t('chat_workspace_action_open')"
                              :loading="workspaceOpening"
                              @click="openWorkspaceSelection"
                            >
                              <PhArrowSquareOut class="icon" />
                            </QButton>
                            <QButton
                              class="plain xs icon"
                              :title="t('chat_workspace_action_download')"
                              :aria-label="t('chat_workspace_action_download')"
                              :disabled="workspaceSelectedTreeEntry.is_dir"
                              :loading="workspaceDownloading"
                              @click="downloadWorkspaceSelection"
                            >
                              <PhCloudArrowDown class="icon" />
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
                      icon="PhXCircle"
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
                </template>

                <template v-else-if="workspaceSidebarTabID === 'topic'">
                  <section class="chat-topic-panel">
                    <QFence
                      v-if="topicDeleteError"
                      class="chat-workspace-pane-fence"
                      type="danger"
                      icon="PhXCircle"
                      :text="topicDeleteError"
                    />

                    <section
                      v-if="topicContextProgress"
                      class="chat-topic-context-progress"
                      :title="topicContextProgress.title"
                      :aria-label="topicContextProgress.title"
                    >
                      <div class="chat-topic-context-progress-head">
                        <span>{{ t("chat_topic_context_ratio_label") }}</span>
                        <strong>{{ topicContextProgress.label }}</strong>
                      </div>
                      <QProgress :value="topicContextProgress.value" :max="1" />
                      <div class="chat-topic-context-progress-foot">
                        <span>{{ topicContextProgress.usedInputLabel }}</span>
                        <span>{{ topicContextProgress.windowLabel }}</span>
                      </div>
                    </section>

                    <dl class="chat-topic-property-list">
                      <div v-for="row in topicPropertyRows" :key="row.key" class="chat-topic-property-row">
                        <dt class="chat-topic-property-label">{{ row.label }}</dt>
                        <dd :class="row.code ? 'chat-topic-property-value is-code' : 'chat-topic-property-value'">
                          <code v-if="row.code" :title="row.value">{{ row.value }}</code>
                          <span v-else>{{ row.value }}</span>
                        </dd>
                      </div>
                    </dl>

                    <footer v-if="topicDeleteAvailable" class="chat-topic-danger-zone">
                      <QButton
                        class="danger plain sm chat-topic-danger-action"
                        :loading="topicDeleting"
                        :disabled="topicDeleteDisabled"
                        @click="confirmDeleteTopic"
                      >
                        <PhTrash class="icon" />
                        <span>{{ t("chat_topic_delete_action") }}</span>
                      </QButton>
                    </footer>
                  </section>
                </template>
              </div>
            </div>
          </aside>
        </section>
        <Teleport to="body">
          <Transition name="chat-workspace-mobile">
            <div v-if="mobileWorkspaceSidebarVisible" class="chat-workspace-mobile-layer">
              <div
                class="chat-workspace-mobile-mask"
                aria-hidden="true"
                @click="closeMobileWorkspaceSidebar"
              ></div>
              <aside
                ref="mobileWorkspaceSidebarPanel"
                class="chat-workspace-mobile-panel"
                :aria-label="t('chat_workspace_label')"
                tabindex="-1"
              >
                <div class="chat-workspace-sidebar-shell-mobile">
            <QTabs
              class="chat-workspace-tabs"
              :tabs="workspacePanelTabs"
              :modelValue="selectedWorkspacePanelTab"
              variant="plain"
              @change="onWorkspaceTabChange"
            />

            <div class="chat-workspace-pane ui-track-panel">
              <template v-if="workspaceSidebarTabID === 'workspace'">
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
                        <PhPlus class="icon" />
                      </QButton>
                      <QButton
                        v-if="workspaceSource === 'attachment'"
                        class="plain xs icon"
                        :title="t('chat_workspace_action_detach')"
                        :aria-label="t('chat_workspace_action_detach')"
                        :disabled="workspaceDetachDisabled"
                        :loading="workspaceSaving"
                        @click="detachWorkspace"
                      >
                        <PhTrash class="icon" />
                      </QButton>
                    </div>
                  </header>

                  <QFence
                    v-if="workspaceError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="PhXCircle"
                    :text="workspaceError"
                  />

                  <QFence
                    v-if="workspaceTreeError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="PhXCircle"
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
                            <PhPlus class="icon" />
                          </QButton>
                          <QButton
                            class="plain xs icon"
                            :title="t('chat_workspace_action_open')"
                            :aria-label="t('chat_workspace_action_open')"
                            :loading="workspaceOpening"
                            @click="openWorkspaceSelection"
                          >
                            <PhArrowSquareOut class="icon" />
                          </QButton>
                          <QButton
                            class="plain xs icon"
                            :title="t('chat_workspace_action_download')"
                            :aria-label="t('chat_workspace_action_download')"
                            :disabled="workspaceSelectedTreeEntry.is_dir"
                            :loading="workspaceDownloading"
                            @click="downloadWorkspaceSelection"
                          >
                            <PhCloudArrowDown class="icon" />
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
                    icon="PhXCircle"
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
              </template>

              <template v-else-if="workspaceSidebarTabID === 'topic'">
                <section class="chat-topic-panel">
                  <QFence
                    v-if="topicDeleteError"
                    class="chat-workspace-pane-fence"
                    type="danger"
                    icon="PhXCircle"
                    :text="topicDeleteError"
                  />

                  <section
                    v-if="topicContextProgress"
                    class="chat-topic-context-progress"
                    :title="topicContextProgress.title"
                    :aria-label="topicContextProgress.title"
                  >
                    <div class="chat-topic-context-progress-head">
                      <span>{{ t("chat_topic_context_ratio_label") }}</span>
                      <strong>{{ topicContextProgress.label }}</strong>
                    </div>
                    <QProgress :value="topicContextProgress.value" :max="1" />
                    <div class="chat-topic-context-progress-foot">
                      <span>{{ topicContextProgress.usedInputLabel }}</span>
                      <span>{{ topicContextProgress.windowLabel }}</span>
                    </div>
                  </section>

                  <dl class="chat-topic-property-list">
                    <div v-for="row in topicPropertyRows" :key="row.key" class="chat-topic-property-row">
                      <dt class="chat-topic-property-label">{{ row.label }}</dt>
                      <dd :class="row.code ? 'chat-topic-property-value is-code' : 'chat-topic-property-value'">
                        <code v-if="row.code" :title="row.value">{{ row.value }}</code>
                        <span v-else>{{ row.value }}</span>
                      </dd>
                    </div>
                  </dl>

                  <footer v-if="topicDeleteAvailable" class="chat-topic-danger-zone">
                    <QButton
                      class="danger plain sm chat-topic-danger-action"
                      :loading="topicDeleting"
                      :disabled="topicDeleteDisabled"
                      @click="confirmDeleteTopic"
                    >
                      <PhTrash class="icon" />
                      <span>{{ t("chat_topic_delete_action") }}</span>
                    </QButton>
                  </footer>
                </section>
              </template>
            </div>
                </div>
              </aside>
            </div>
          </Transition>
        </Teleport>
        <AppDialogShell
          v-if="workspaceBrowserOpen"
          :modelValue="workspaceBrowserOpen"
          :title="t('chat_workspace_dialog_title')"
          width="720px"
          :closeDisabled="workspaceSaving || workspaceBrowserCreating"
          @close="closeWorkspaceBrowser"
        >
          <section class="chat-workspace-dialog">
            <QFence
              v-if="workspaceBrowserError"
              class="chat-workspace-pane-fence"
              type="danger"
              icon="PhXCircle"
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
                      :disabled="workspaceBrowserCreating"
                      @click="activateWorkspaceBrowserSource('recent')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_recent") }}</span>
                      </span>
                    </button>
                    <button
                      type="button"
                      :class="workspaceBrowserSourceItemClass('home')"
                      :disabled="workspaceBrowserCreating"
                      @click="activateWorkspaceBrowserSource('home')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_home") }}</span>
                      </span>
                    </button>
                    <button
                      type="button"
                      :class="workspaceBrowserSourceItemClass('system')"
                      :disabled="workspaceBrowserCreating"
                      @click="activateWorkspaceBrowserSource('system')"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ t("chat_workspace_dialog_system") }}</span>
                      </span>
                    </button>
                    <button
                      v-for="item in workspaceBrowserPlaceSourceItems"
                      :key="item.id"
                      type="button"
                      :class="workspaceBrowserSourceItemClass(item.id)"
                      :title="item.path"
                      :disabled="workspaceBrowserCreating"
                      @click="activateWorkspaceBrowserSource(item.id)"
                    >
                      <span class="workspace-sidebar-item-copy">
                        <span class="workspace-sidebar-item-title">{{ item.title }}</span>
                      </span>
                    </button>
                  </div>
                </section>
              </aside>

              <div class="chat-workspace-dialog-main">
                <div class="chat-workspace-browser-toolbar">
                  <span class="chat-workspace-browser-parent">
                    <span class="chat-workspace-browser-parent-label ui-kicker">
                      {{ t("chat_workspace_dialog_create_in") }}
                    </span>
                    <code
                      class="chat-workspace-browser-parent-path"
                      :title="workspaceBrowserCreateParent"
                    >{{ workspaceBrowserCreateParent || t("chat_workspace_dialog_selection_empty") }}</code>
                  </span>
                  <QButton
                    class="plain xs chat-workspace-browser-create-button"
                    :disabled="workspaceBrowserCreateDisabled || workspaceBrowserCreateOpen"
                    @click="openWorkspaceBrowserCreate"
                  >
                    <PhPlus class="icon" />
                    <span>{{ t("chat_workspace_dialog_new_directory") }}</span>
                  </QButton>
                </div>

                <div v-if="workspaceBrowserCreateOpen" class="chat-workspace-browser-create">
                  <div ref="workspaceBrowserCreateField" class="chat-workspace-browser-create-field">
                    <QInput
                      v-model="workspaceBrowserCreateName"
                      :placeholder="t('chat_workspace_dialog_directory_name')"
                      :aria-label="t('chat_workspace_dialog_directory_name')"
                      :disabled="workspaceBrowserCreating"
                      @keydown.enter.prevent="createWorkspaceBrowserDir"
                    />
                  </div>
                  <div class="chat-workspace-browser-create-actions">
                    <QButton
                      class="plain sm"
                      :disabled="workspaceBrowserCreating"
                      @click="cancelWorkspaceBrowserCreate"
                    >
                      {{ t("action_cancel") }}
                    </QButton>
                    <QButton
                      class="primary sm"
                      :loading="workspaceBrowserCreating"
                      :disabled="workspaceBrowserCreateSubmitDisabled"
                      @click="createWorkspaceBrowserDir"
                    >
                      {{ t("chat_workspace_dialog_create_directory") }}
                    </QButton>
                  </div>
                </div>

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
                        :class="workspaceBrowserTreeEntryClass(row)"
                        :disabled="!row.entry.is_dir || workspaceBrowserCreating"
                        :title="row.entry.path"
                        @click="selectWorkspaceBrowserNode(row)"
                      >
                        <span class="chat-workspace-tree-kind" aria-hidden="true">
                          <img class="chat-workspace-tree-icon" :src="workspaceTreeIcon(row.entry, row.expanded)" alt="" />
                        </span>
                        <WorkspaceBrowserRecentItem
                          v-if="row.source === 'recent'"
                          :name="row.entry.name"
                          :path="row.entry.path"
                        />
                        <span v-else class="chat-workspace-tree-name">{{ row.entry.name }}</span>
                      </button>
                    </div>
                  </div>
                  <p v-else class="chat-workspace-tree-status">{{ workspaceBrowserEmptyText }}</p>
                </div>
              </div>

              <div class="chat-workspace-dialog-actions">
                <div class="chat-workspace-dialog-options">
                  <QSwitch
                    :modelValue="workspaceBrowserShowHidden"
                    :disabled="workspaceBrowserLoading || workspaceBrowserCreating"
                    :aria-label="t('chat_workspace_dialog_show_hidden')"
                    @update:modelValue="setWorkspaceBrowserShowHidden"
                  />
                  <span class="chat-workspace-dialog-option-label">{{ t("chat_workspace_dialog_show_hidden") }}</span>
                </div>
                <div class="chat-workspace-dialog-action-buttons">
                  <QButton
                    class="plain sm"
                    :disabled="workspaceSaving || workspaceBrowserCreating"
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
            </div>
          </section>
        </AppDialogShell>
        <AppDialogShell
          v-if="composerFilePreviewOpen"
          v-model="composerFilePreviewOpen"
          :title="composerFilePreviewName"
          width="880px"
          height="min(78vh, 760px)"
          @close="closeComposerFilePreview"
        >
          <section class="chat-composer-file-preview">
            <div class="chat-composer-file-preview-stage">
              <p v-if="composerFilePreviewLoading" class="chat-composer-file-preview-status">
                {{ t("chat_composer_file_preview_loading") }}
              </p>
              <div
                v-else-if="composerFilePreviewKind === 'image' && composerFilePreviewURL"
                class="chat-composer-file-preview-image-scroll"
              >
                <img
                  class="chat-composer-file-preview-image"
                  :src="composerFilePreviewURL"
                  :alt="composerFilePreviewName"
                />
              </div>
              <iframe
                v-else-if="composerFilePreviewKind === 'pdf' && composerFilePreviewURL"
                class="chat-composer-file-preview-pdf"
                :src="composerFilePreviewURL"
                :title="composerFilePreviewName"
                sandbox=""
                referrerpolicy="no-referrer"
              ></iframe>
              <pre
                v-else-if="composerFilePreviewKind === 'text'"
                class="chat-composer-file-preview-text"
              >{{ composerFilePreviewText }}</pre>
              <QFence
                v-else-if="composerFilePreviewError"
                class="chat-composer-file-preview-error"
                type="danger"
                icon="PhXCircle"
                :text="composerFilePreviewError"
              />
              <nav
                v-if="composerFilePreviewItems.length > 1"
                class="chat-composer-file-preview-navigation"
                :aria-label="t('chat_composer_file_preview_navigation')"
              >
                <QButton
                  class="plain sm icon chat-composer-file-preview-nav-button is-previous"
                  :disabled="!composerFilePreviewHasPrevious"
                  :title="t('chat_composer_file_preview_previous')"
                  :aria-label="t('chat_composer_file_preview_previous')"
                  @click="navigateComposerFilePreview(-1)"
                >
                  <PhArrowLeft class="icon" />
                </QButton>
                <QButton
                  class="plain sm icon chat-composer-file-preview-nav-button is-next"
                  :disabled="!composerFilePreviewHasNext"
                  :title="t('chat_composer_file_preview_next')"
                  :aria-label="t('chat_composer_file_preview_next')"
                  @click="navigateComposerFilePreview(1)"
                >
                  <PhArrowRight class="icon" />
                </QButton>
              </nav>
            </div>
          </section>
        </AppDialogShell>
        <RawJsonDialog
          v-if="rawDialogOpen"
          :open="rawDialogOpen"
          :json="rawDialogJSON"
          @close="closeRawDialog"
        />
        <QMessageDialog
          v-model="topicDeleteDialogOpen"
          icon="PhTrash"
          iconColor="red"
          :title="t('chat_topic_delete_action')"
          :text="topicDeleteDialogText"
          :actions="topicDeleteDialogActions"
        />
      </template>
    </AppPage>
  `,
};

export default ChatView;
