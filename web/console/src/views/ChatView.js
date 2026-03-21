import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./ChatView.css";

import AppPage from "../components/AppPage";
import MarkdownContent from "../components/MarkdownContent";
import RawJsonDialog from "../components/RawJsonDialog";
import { endpointChannelLabel } from "../core/endpoints";
import {
  buildConsoleStreamURL,
  createConsoleStreamTicket,
  currentLocale,
  endpointState,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  safeJSON,
  translate,
} from "../core/context";

const POLL_INTERVAL_MS = 1200;
const COMPOSER_MAX_ROWS = 5;
const CHAT_HISTORY_LIMIT = 100;
const HEARTBEAT_TOPIC_ID = "_heartbeat";
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

function isTerminalStatus(status) {
  return status === "done" || status === "failed" || status === "canceled";
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

function tuiKicker(left, right) {
  const lhs = String(left || "").trim();
  const rhs = String(right || "").trim();
  if (lhs && rhs) {
    return `[ ${lhs} // ${rhs} ]`;
  }
  return `[ ${lhs || rhs} ]`;
}

const ChatView = {
  components: {
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
    const readonlyKicker = computed(() => {
      return tuiKicker(endpointChannelLabel(selectedEndpoint.value?.mode, t), t("chat_readonly_kicker"));
    });
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
      if (mobileTopicView.value === "topics") {
        return t("chat_topics_title");
      }
      if (creatingTopic.value) {
        return t("chat_topic_new");
      }
      return selectedTopic.value ? topicTitle(selectedTopic.value) : t("chat_title");
    });
    const mobileShowBack = computed(() => mobileTopicSplitEnabled.value && mobileTopicView.value === "chat");
    const showTopicSidebar = computed(() => {
      if (!consoleTopicsEnabled.value) {
        return false;
      }
      if (!mobileTopicSplitEnabled.value) {
        return true;
      }
      return mobileTopicView.value === "topics";
    });
    const showChatPane = computed(() => {
      if (!mobileTopicSplitEnabled.value) {
        return true;
      }
      return mobileTopicView.value === "chat";
    });
    const shellClass = computed(() => {
      if (!consoleTopicsEnabled.value) {
        return "chat-shell";
      }
      if (!mobileTopicSplitEnabled.value) {
        return "chat-shell has-sidebar";
      }
      return mobileTopicView.value === "topics" ? "chat-shell is-mobile-topics" : "chat-shell is-mobile-chat";
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
    const chatPlaceholderHint = computed(() => {
      if (visibleTopics.value.length > 0) {
        return t("chat_placeholder_choose_topic");
      }
      return chatPlaceholderText.value;
    });

    function syncMobileTopicView(options = {}) {
      if (!mobileTopicSplitEnabled.value) {
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
      chatHistoryItems.value = [...chatHistoryItems.value, item];
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
      chatHistoryItems.value = next;
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
        chatHistoryItems.value = [];
        return true;
      }
      historyLoading.value = true;
      const preserveCurrent = Boolean(options.preserveCurrent);
      try {
        let path = `/tasks?limit=${CHAT_HISTORY_LIMIT}`;
        if (consoleTopicsEnabled.value) {
          if (creatingTopic.value) {
            chatHistoryItems.value = [];
            historyAutoStick.value = true;
            return true;
          }
          const topicID = normalizeTopicID(selectedTopicID.value);
          if (!topicID) {
            chatHistoryItems.value = [];
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
        chatHistoryItems.value = nextItems.length > 0 ? nextItems : [emptyHistoryItem()];
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
          chatHistoryItems.value = [];
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
      composerField,
      submitBlockedMessage,
      chatReadonly,
      readonlyTitle,
      readonlyKicker,
      readonlyReason,
      handleComposerPointerDown,
      pageClass,
      showChatPlaceholder,
      chatPlaceholderText,
      composerDisabled,
      sendDisabled,
      composerPlaceholder,
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
      submitTask,
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
      historyClass,
      historySurfaceClass,
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
          <h3 class="chat-readonly-title ui-kicker">{{ readonlyKicker }}</h3>
          <p class="chat-readonly-text">{{ readonlyReason }}</p>
        </section>
      </section>
      <template v-else>
        <section :class="shellClass">
          <aside v-if="showTopicSidebar" class="chat-topic-sidebar workspace-sidebar-section">
            <header class="chat-topic-sidebar-head workspace-sidebar-head">
              <div class="chat-topic-sidebar-copy">
                <p class="ui-kicker">{{ topicSidebarKicker }}</p>
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
              <div class="chat-desk-copy">
                <p v-if="deskMeta" class="chat-desk-meta">{{ deskMeta }}</p>
                <h3 class="chat-desk-title workspace-document-title">{{ deskTitle }}</h3>
              </div>
            </header>
            <section v-if="showChatPlaceholder" class="chat-placeholder">
              <div class="chat-placeholder-copy">
                <p v-if="deskMeta" class="chat-placeholder-meta">{{ deskMeta }}</p>
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
                >
                  <template #append>
                    <div class="chat-composer-append">
                      <QButton
                        class="outlined sm icon chat-composer-send"
                        :loading="sending"
                        :disabled="sendDisabled"
                        :title="t('chat_action_send')"
                        :aria-label="t('chat_action_send')"
                        @click="submitTask"
                      >
                        <QIconSend class="icon" />
                      </QButton>
                    </div>
                  </template>
                </QTextarea>
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
                    <MarkdownContent
                      v-if="item.role === 'agent'"
                      class="chat-history-markdown"
                      :source="item.text"
                      format="auto"
                      theme="blueprint"
                      @rendered="handleMarkdownRendered"
                    />
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
                  <div class="chat-composer-append">
                    <QButton
                      class="outlined sm icon chat-composer-send"
                      :loading="sending"
                      :disabled="sendDisabled"
                      :title="t('chat_action_send')"
                      :aria-label="t('chat_action_send')"
                      @click="submitTask"
                    >
                      <QIconSend class="icon" />
                    </QButton>
                  </div>
                </template>
              </QTextarea>
            </div>
          </section>
        </section>
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
