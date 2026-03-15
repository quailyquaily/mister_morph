import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import "./ChatView.css";

import AppPage from "../components/AppPage";
import { endpointChannelLabel } from "../core/endpoints";
import {
  currentLocale,
  endpointState,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  translate,
} from "../core/context";

const POLL_INTERVAL_MS = 1200;
const COMPOSER_MAX_ROWS = 5;
const CHAT_HISTORY_LIMIT = 100;
const HEARTBEAT_TOPIC_ID = "_heartbeat";

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
  const summary = String(task?.result?.output || "").trim();
  if (summary) {
    return summary;
  }
  const finalOutput = task?.result?.final?.output;
  if (typeof finalOutput === "string") {
    return finalOutput.trim();
  }
  if (finalOutput !== undefined && finalOutput !== null) {
    return stringifyResult(finalOutput);
  }
  return "";
}

function taskAgentText(task, t) {
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
  return t("chat_polling_hint");
}

function taskHistoryItems(task, t) {
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
    text: taskAgentText(task, t),
    status: normalizeTaskStatus(task?.status),
    timeText: historyTimeLabel(task?.finished_at),
    taskId: taskID,
    rawJSON: taskRawJSON(task),
  });
  return items;
}

function newHistoryID() {
  return `${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

const ChatView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const chatHistoryItems = ref([]);
    const historyLoading = ref(false);
    const topics = ref([]);
    const topicsLoading = ref(false);
    const selectedTopicID = ref("");
    const creatingTopic = ref(false);
    const showSystemTopics = ref(false);
    const taskInput = ref("");
    const sending = ref(false);
    const err = ref("");
    const pollTimers = new Set();
    const composerField = ref(null);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const rawDialogTaskID = ref("");
    const rawRevealItemID = ref("");
    const rawRevealCount = ref(0);
    let rawRevealTimerID = 0;

    const selectedEndpoint = computed(() => runtimeEndpointByRef(endpointState.selectedRef));
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
    const hasSystemTopics = computed(() =>
      topics.value.some((topic) => normalizeTopicID(topic?.id) === HEARTBEAT_TOPIC_ID)
    );
    const visibleTopics = computed(() => {
      return topics.value.filter((topic) => {
        const topicID = normalizeTopicID(topic?.id);
        if (!topicID) {
          return false;
        }
        if (topicID === normalizeTopicID(selectedTopicID.value)) {
          return true;
        }
        if (topicID === HEARTBEAT_TOPIC_ID && !showSystemTopics.value) {
          return false;
        }
        return true;
      });
    });
    const systemTopicToggleText = computed(() =>
      showSystemTopics.value ? t("chat_topic_system_hide") : t("chat_topic_system_show")
    );

    function composerTextarea() {
      const root = composerField.value?.$el || composerField.value;
      if (!root || typeof root.querySelector !== "function") {
        return null;
      }
      return root.querySelector("textarea");
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

    function staticHistoryItem(id, text) {
      return {
        id,
        role: "system",
        text,
        status: "",
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

    function roleText(role) {
      if (role === "user") {
        return t("chat_role_user");
      }
      if (role === "agent") {
        return t("chat_role_agent");
      }
      return t("chat_role_system");
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
      if (normalizeTopicID(topic?.id) === "default") {
        return t("chat_topic_default");
      }
      return "";
    }

    function topicItemClass(topic) {
      const classes = ["chat-topic-item"];
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
        patchHistoryItem(historyID, {
          status,
          text: taskAgentText(detail, t),
          timeText: historyTimeLabel(detail?.finished_at),
          rawJSON: taskRawJSON(detail),
        });
        if (!isTerminalStatus(status)) {
          schedulePoll(async () => {
            await pollTask(taskID, historyID, endpointRef);
          });
        }
      } catch (e) {
        patchHistoryItem(historyID, {
          status: "failed",
          text: e?.message || t("msg_load_failed"),
          rawJSON: "",
        });
      }
    }

    function pickInitialTopic(items) {
      return items.find((topic) => !isSystemTopic(topic)) || items[0] || null;
    }

    function resetTopicState() {
      topics.value = [];
      topicsLoading.value = false;
      selectedTopicID.value = "";
      creatingTopic.value = false;
      showSystemTopics.value = false;
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
          return true;
        }
        if (preserveDraft && creatingTopic.value) {
          return true;
        }
        const currentID = normalizeTopicID(selectedTopicID.value);
        if (currentID && items.some((topic) => normalizeTopicID(topic?.id) === currentID)) {
          creatingTopic.value = false;
          return true;
        }
        const fallback = pickInitialTopic(items);
        if (fallback) {
          selectedTopicID.value = normalizeTopicID(fallback.id);
          creatingTopic.value = false;
        } else if (!preserveSelection) {
          selectedTopicID.value = "";
          creatingTopic.value = true;
        }
        return true;
      } catch (e) {
        err.value = e?.message || t("msg_load_failed");
        if (!preserveSelection) {
          selectedTopicID.value = "";
          creatingTopic.value = true;
        }
        return false;
      } finally {
        topicsLoading.value = false;
      }
    }

    async function loadHistory(options = {}) {
      clearPollTimers();
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
            chatHistoryItems.value = [emptyHistoryItem()];
            return true;
          }
          const topicID = normalizeTopicID(selectedTopicID.value);
          if (!topicID) {
            chatHistoryItems.value = [emptyHistoryItem()];
            return true;
          }
          path = `/tasks?limit=${CHAT_HISTORY_LIMIT}&topic_id=${encodeURIComponent(topicID)}`;
        }

        const data = await runtimeApiFetchForEndpoint(endpointRef, path);
        const tasks = Array.isArray(data?.items) ? [...data.items] : [];
        tasks.sort((left, right) => taskCreatedAt(left) - taskCreatedAt(right));
        const nextItems = tasks.flatMap((task) => taskHistoryItems(task, t));
        chatHistoryItems.value = nextItems.length > 0 ? nextItems : [emptyHistoryItem()];
        for (const item of chatHistoryItems.value) {
          if (item.role === "agent" && item.taskId && !isTerminalStatus(item.status)) {
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

    function openRawDialog(item) {
      resetRawReveal();
      rawDialogTaskID.value = String(item?.taskId || "").trim();
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

    function selectTopic(topicID) {
      const normalized = normalizeTopicID(topicID);
      if (!normalized) {
        return;
      }
      creatingTopic.value = false;
      selectedTopicID.value = normalized;
      void loadHistory();
    }

    function startNewTopic() {
      creatingTopic.value = true;
      selectedTopicID.value = "";
      err.value = "";
      void loadHistory();
      syncComposerHeight();
    }

    function toggleSystemTopics() {
      showSystemTopics.value = !showSystemTopics.value;
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

      pushHistoryItem({
        role: "user",
        text: task,
        timeText: historyTimeLabel(new Date().toISOString()),
      });
      const agentHistoryID = pushHistoryItem({
        role: "agent",
        text: t("chat_polling_hint"),
        status: "queued",
        timeText: "",
      });

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
        patchHistoryItem(agentHistoryID, {
          taskId: taskID,
          status,
          text: t("chat_polling_hint"),
          rawJSON: "",
        });

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
          const reloaded = await loadHistory({ preserveCurrent: true });
          if (!reloaded) {
            await pollTask(taskID, agentHistoryID, endpointRef);
          }
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
      }
    }

    onMounted(() => {
      void refreshChatData();
      syncComposerHeight();
    });
    onUnmounted(() => {
      clearPollTimers();
      resetRawReveal();
    });
    watch(
      () => [endpointState.selectedRef, submitEndpointRef.value],
      () => {
        resetTopicState();
        void refreshChatData();
        syncComposerHeight();
      }
    );
    watch(taskInput, () => {
      syncComposerHeight();
    });

    return {
      t,
      chatHistoryItems,
      historyLoading,
      topics,
      topicsLoading,
      visibleTopics,
      hasSystemTopics,
      showSystemTopics,
      systemTopicToggleText,
      creatingTopic,
      taskInput,
      sending,
      err,
      composerField,
      submitBlockedMessage,
      chatReadonly,
      readonlyTitle,
      readonlyReason,
      composerDisabled,
      sendDisabled,
      consoleTopicsEnabled,
      submitTask,
      selectTopic,
      startNewTopic,
      toggleSystemTopics,
      topicTitle,
      topicTime,
      topicBadgeText,
      topicItemClass,
      topicIsActive,
      roleText,
      historyClass,
      clickHistoryTime,
      openRawDialog,
      closeRawDialog,
      rawDialogOpen,
      rawDialogJSON,
      rawDialogTaskID,
    };
  },
  template: `
    <AppPage :title="t('chat_title')" :class="consoleTopicsEnabled ? 'chat-page chat-page-topics' : 'chat-page'">
      <template v-if="consoleTopicsEnabled" #leading>
        <div class="chat-page-bar-sidebar">
          <h2 class="title page-bar-title">{{ t("chat_title") }}</h2>
          <QButton
            class="outlined xs icon chat-page-bar-new"
            :title="t('chat_topic_new')"
            :aria-label="t('chat_topic_new')"
            @click="startNewTopic"
          >
            <QIconPlus class="icon" />
          </QButton>
        </div>
      </template>
      <template v-if="consoleTopicsEnabled" #actions>
        <div class="chat-page-bar-main" aria-hidden="true"></div>
      </template>
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <section v-if="chatReadonly" class="chat-readonly frame">
        <h3 class="chat-readonly-title">{{ readonlyTitle }}</h3>
        <p class="chat-readonly-text">{{ readonlyReason }}</p>
      </section>
      <template v-else>
        <section :class="consoleTopicsEnabled ? 'chat-shell has-sidebar' : 'chat-shell'">
          <aside v-if="consoleTopicsEnabled" class="chat-topic-sidebar">
            <div v-if="hasSystemTopics || topicsLoading" class="chat-topic-sidebar-actions">
              <p v-if="topicsLoading" class="muted chat-topic-loading">{{ t("chat_topics_loading") }}</p>
              <QButton
                v-if="hasSystemTopics"
                :class="showSystemTopics ? 'plain xs chat-topic-filter is-active' : 'plain xs chat-topic-filter'"
                @click="toggleSystemTopics"
              >
                {{ systemTopicToggleText }}
              </QButton>
            </div>
            <div :class="topicsLoading ? 'chat-topic-list is-busy' : 'chat-topic-list'">
              <div
                v-for="topic in visibleTopics"
                :key="topic.id"
                :class="topicItemClass(topic)"
                role="button"
                tabindex="0"
                :aria-current="topicIsActive(topic) ? 'page' : undefined"
                @click="selectTopic(topic.id)"
                @keydown.enter.prevent="selectTopic(topic.id)"
                @keydown.space.prevent="selectTopic(topic.id)"
              >
                <span class="chat-topic-item-copy">
                  <span class="chat-topic-item-main">
                    <span class="chat-topic-item-title">{{ topicTitle(topic) }}</span>
                    <span v-if="topicBadgeText(topic)" class="chat-topic-item-badge">{{ topicBadgeText(topic) }}</span>
                  </span>
                  <time class="chat-topic-item-time">{{ topicTime(topic) }}</time>
                </span>
              </div>
            </div>
          </aside>
          <section class="chat-main">
            <div class="chat-history">
              <p v-if="historyLoading" class="muted">{{ t("chat_history_loading") }}</p>
              <article v-for="item in chatHistoryItems" :key="item.id" :class="historyClass(item)">
                <div class="chat-history-bubble">
                  <header class="chat-history-head">
                    <code class="chat-history-role">{{ roleText(item.role) }}</code>
                    <code
                      v-if="item.timeText"
                      class="chat-history-status"
                      @click="clickHistoryTime(item)"
                    >
                      {{ item.timeText }}
                    </code>
                  </header>
                  <div class="chat-history-body">{{ item.text }}</div>
                </div>
              </article>
              <p v-if="chatHistoryItems.length === 0 && !historyLoading" class="muted">{{ t("chat_empty") }}</p>
            </div>
            <div class="chat-composer">
              <QTextarea
                ref="composerField"
                v-model="taskInput"
                :rows="1"
                :disabled="composerDisabled"
                :placeholder="t('chat_input_placeholder')"
                @keydown.enter.exact.prevent="submitTask"
              >
                <template #append>
                  <div class="chat-composer-append">
                    <QButton
                      class="primary sm icon chat-composer-send"
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
        <div v-if="rawDialogOpen" class="chat-raw-overlay" @click.self="closeRawDialog">
          <section class="chat-raw-dialog frame">
            <header class="chat-raw-head">
              <div class="chat-raw-copy">
                <code class="chat-raw-kicker">{{ t("chat_task_prefix") }} {{ rawDialogTaskID || "-" }}</code>
                <h3 class="chat-raw-title">{{ t("chat_raw_title") }}</h3>
              </div>
              <QButton class="plain sm" @click="closeRawDialog">{{ t("action_close") }}</QButton>
            </header>
            <pre class="chat-raw-body">{{ rawDialogJSON }}</pre>
          </section>
        </div>
      </template>
    </AppPage>
  `,
};

export default ChatView;
