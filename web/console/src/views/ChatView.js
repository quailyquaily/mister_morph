import { onMounted, onUnmounted, ref, watch } from "vue";
import "./ChatView.css";

import AppPage from "../components/AppPage";
import { endpointState, runtimeApiFetch, translate } from "../core/context";

const POLL_INTERVAL_MS = 1200;

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
    const taskInput = ref("");
    const sending = ref(false);
    const err = ref("");
    const pollTimers = new Set();

    function clearPollTimers() {
      for (const timerID of pollTimers) {
        window.clearTimeout(timerID);
      }
      pollTimers.clear();
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

    function statusText(status) {
      switch (normalizeTaskStatus(status)) {
        case "running":
          return t("status_running");
        case "pending":
          return t("status_pending");
        case "done":
          return t("status_done");
        case "failed":
          return t("status_failed");
        case "canceled":
          return t("status_canceled");
        default:
          return t("status_queued");
      }
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

    function pushHistoryItem(partial) {
      const item = {
        id: newHistoryID(),
        role: String(partial?.role || "system"),
        text: String(partial?.text || ""),
        status: String(partial?.status || ""),
        taskId: String(partial?.taskId || ""),
      };
      chatHistoryItems.value = [...chatHistoryItems.value, item];
      return item.id;
    }

    function patchHistoryItem(id, patch) {
      const idx = chatHistoryItems.value.findIndex((item) => item.id === id);
      if (idx < 0) {
        return;
      }
      chatHistoryItems.value[idx] = {
        ...chatHistoryItems.value[idx],
        ...patch,
      };
    }

    function schedulePoll(fn) {
      const timerID = window.setTimeout(async () => {
        pollTimers.delete(timerID);
        await fn();
      }, POLL_INTERVAL_MS);
      pollTimers.add(timerID);
    }

    async function pollTask(taskID, historyID) {
      try {
        const detail = await runtimeApiFetch(`/tasks/${encodeURIComponent(taskID)}`);
        const status = normalizeTaskStatus(detail?.status);
        const hasResult = detail && detail.result !== undefined && detail.result !== null;
        const resultText = stringifyResult(detail?.result);
        const errorText = String(detail?.error || "").trim();
        const lines = [`${t("chat_task_prefix")} ${taskID}`];
        if (hasResult && resultText) {
          lines.push(resultText);
        } else if (errorText) {
          lines.push(errorText);
        } else if (isTerminalStatus(status)) {
          lines.push(t("chat_result_empty"));
        } else {
          lines.push(t("chat_polling_hint"));
        }
        patchHistoryItem(historyID, {
          status,
          text: lines.join("\n\n"),
        });
        if (!isTerminalStatus(status)) {
          schedulePoll(async () => {
            await pollTask(taskID, historyID);
          });
        }
      } catch (e) {
        patchHistoryItem(historyID, {
          status: "failed",
          text: e?.message || t("msg_load_failed"),
        });
      }
    }

    async function submitTask() {
      const task = String(taskInput.value || "").trim();
      if (!task || sending.value) {
        return;
      }
      sending.value = true;
      err.value = "";
      taskInput.value = "";

      pushHistoryItem({
        role: "user",
        text: task,
      });
      const agentHistoryID = pushHistoryItem({
        role: "agent",
        text: t("chat_agent_waiting"),
        status: "queued",
      });

      try {
        const submitted = await runtimeApiFetch("/tasks", {
          method: "POST",
          body: { task },
        });
        const taskID = String(submitted?.id || "").trim();
        const status = normalizeTaskStatus(submitted?.status);
        if (!taskID) {
          throw new Error(t("chat_missing_task_id"));
        }
        patchHistoryItem(agentHistoryID, {
          taskId: taskID,
          status,
          text: `${t("chat_task_prefix")} ${taskID}\n\n${t("chat_polling_hint")}`,
        });
        await pollTask(taskID, agentHistoryID);
      } catch (e) {
        const message = e?.message || t("msg_load_failed");
        err.value = message;
        patchHistoryItem(agentHistoryID, {
          status: "failed",
          text: message,
        });
      } finally {
        sending.value = false;
      }
    }

    onMounted(() => {
      pushHistoryItem({
        role: "system",
        text: t("chat_intro"),
      });
    });
    onUnmounted(() => {
      clearPollTimers();
    });
    watch(
      () => endpointState.selectedRef,
      () => {
        clearPollTimers();
      }
    );

    return {
      t,
      chatHistoryItems,
      taskInput,
      sending,
      err,
      submitTask,
      roleText,
      statusText,
      historyClass,
    };
  },
  template: `
    <AppPage :title="t('chat_title')" class="chat-page">
      <div class="chat-history">
        <article v-for="item in chatHistoryItems" :key="item.id" :class="historyClass(item)">
          <header class="chat-history-head">
            <code class="chat-history-role">{{ roleText(item.role) }}</code>
            <code v-if="item.status" class="chat-history-status">{{ statusText(item.status) }}</code>
          </header>
          <pre class="chat-history-body">{{ item.text }}</pre>
          <code v-if="item.taskId" class="chat-history-task">{{ t("chat_task_prefix") }} {{ item.taskId }}</code>
        </article>
        <p v-if="chatHistoryItems.length === 0" class="muted">{{ t("chat_empty") }}</p>
      </div>
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="chat-composer">
        <QTextarea v-model="taskInput" :rows="4" :placeholder="t('chat_input_placeholder')" />
        <div class="chat-composer-actions">
          <QButton class="primary" :loading="sending" @click="submitTask">{{ t("chat_action_send") }}</QButton>
        </div>
      </div>
    </AppPage>
  `,
};

export default ChatView;
