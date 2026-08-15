import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";

import defaultEndpointAvatarURL from "../assets/images/app_logo_current.svg";
import { approvalDetailsByID, taskApprovalState } from "../core/chat-approvals";
import { chatDraft, clearChatDraft, rememberChatDraft } from "../core/chat-draft-memory";
import { lastTopicID, rememberLastTopicID } from "../core/chat-topic-memory";
import {
  buildPollingHint,
  chatApprovalReasonText,
  historyPendingSeed,
  historyTimeLabel,
  isContextCompactCommand,
  isTerminalStatus,
  normalizeActivity,
  normalizePlan,
  normalizeReasoning,
  normalizeTaskStatus,
  normalizeTopicID,
  taskListHistoryItems,
} from "../core/chat-task-history";
import {
  buildConsoleStreamURL,
  createConsoleStreamTicket,
  currentLocale,
  runtimeApiFetchForEndpoint,
  safeJSON,
  supportsConsoleTaskStream,
  translate,
} from "../core/context";
import AppDialogShell from "./AppDialogShell";
import ChatComposer from "./ChatComposer";
import ChatHistoryList from "./ChatHistoryList";
import "../views/ChatView.css";
import "./AgentChatPane.css";

const POLL_INTERVAL_MS = 1200;
const HISTORY_LIMIT = 60;
const TOPIC_LIMIT = 60;
const AWARENESS_TOPIC_ID = "_awareness";

function cleanText(value) {
  return String(value || "").trim();
}

function topicUpdatedAt(topic) {
  const value = Date.parse(cleanText(topic?.updated_at || topic?.created_at));
  return Number.isFinite(value) ? value : 0;
}

function newHistoryID() {
  return `${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

const AgentChatPane = {
  name: "AgentChatPane",
  components: {
    AppDialogShell,
    ChatComposer,
    ChatHistoryList,
  },
  props: {
    paneId: {
      type: String,
      required: true,
    },
    endpoint: {
      type: Object,
      required: true,
    },
    endpointOptions: {
      type: Array,
      default: () => [],
    },
    initialTopicId: {
      type: String,
      default: "",
    },
    canClose: {
      type: Boolean,
      default: true,
    },
  },
  emits: [
    "activate",
    "close",
    "endpoint-change",
    "open-full",
    "split",
    "topic-change",
    "topic-missing",
  ],
  setup(props, { emit }) {
    const t = translate;
    const topics = ref([]);
    const topicsSupported = ref(true);
    const selectedTopicID = ref("");
    const creatingTopic = ref(false);
    const topicDialogOpen = ref(false);
    const topicFilter = ref("");
    const historyItems = ref([]);
    const historyLoading = ref(false);
    const taskInput = ref("");
    const sending = ref(false);
    const error = ref("");
    const historyViewport = ref(null);
    const copiedItemID = ref("");
    const expandedState = ref({});
    const approvalDetailAttempts = new Set();
    const pollTimers = new Map();
    const pollInFlight = new Set();
    const streamSockets = new Map();
    let alive = true;
    let loadVersion = 0;
    let copiedTimerID = 0;
    let historyAutoStick = true;
    let skipNextDraftPersist = false;

    const endpointRef = computed(() => cleanText(props.endpoint?.endpoint_ref));
    let watchedEndpointRef = endpointRef.value;
    const submitEndpointRef = computed(() => {
      const mapped = cleanText(props.endpoint?.submit_endpoint_ref);
      if (mapped) {
        return mapped;
      }
      return props.endpoint?.can_submit === true ? endpointRef.value : "";
    });
    const available = computed(
      () => props.endpoint?.connected === true && submitEndpointRef.value !== ""
    );
    const agentName = computed(
      () =>
        cleanText(props.endpoint?.agent_name) ||
        cleanText(props.endpoint?.name) ||
        endpointRef.value ||
        t("chat_agent_name_fallback")
    );
    const avatarURL = computed(
      () => cleanText(props.endpoint?.avatar_url) || defaultEndpointAvatarURL
    );
    const selectedEndpointOption = computed(
      () =>
        props.endpointOptions.find((item) => cleanText(item?.value) === endpointRef.value) || {
          id: endpointRef.value,
          value: endpointRef.value,
          title: agentName.value,
          image: avatarURL.value,
        }
    );
    const topicOptions = computed(() =>
      topics.value.map((topic) => ({
        id: normalizeTopicID(topic?.id),
        value: normalizeTopicID(topic?.id),
        title: topicTitle(topic),
      }))
    );
    const filteredTopicOptions = computed(() => {
      const query = cleanText(topicFilter.value).toLowerCase();
      if (!query) {
        return topicOptions.value;
      }
      return topicOptions.value.filter((item) =>
        cleanText(item?.title).toLowerCase().includes(query)
      );
    });
    const topicLabel = computed(() => {
      if (creatingTopic.value || !selectedTopicID.value) {
        return t("agent_desk_new_topic");
      }
      return (
        topicOptions.value.find((item) => item.value === selectedTopicID.value)?.title ||
        t("chat_topic_untitled")
      );
    });
    const activeTaskItem = computed(() => {
      for (let index = historyItems.value.length - 1; index >= 0; index -= 1) {
        const item = historyItems.value[index];
        if (
          cleanText(item?.role) === "agent" &&
          cleanText(item?.taskId) &&
          !isTerminalStatus(normalizeTaskStatus(item?.status))
        ) {
          return item;
        }
      }
      return null;
    });
    const composerStopMode = computed(
      () => Boolean(activeTaskItem.value) && cleanText(taskInput.value) === ""
    );
    const sendDisabled = computed(
      () => !available.value || sending.value || (!composerStopMode.value && !cleanText(taskInput.value))
    );
    const composerInputHistory = computed(() => {
      const values = [];
      for (let index = historyItems.value.length - 1; index >= 0; index -= 1) {
        const item = historyItems.value[index];
        if (cleanText(item?.role) === "user" && cleanText(item?.text)) {
          values.push(cleanText(item.text));
        }
      }
      return values;
    });

    function topicTitle(topic) {
      const title = cleanText(topic?.title);
      if (title) {
        return title;
      }
      return normalizeTopicID(topic?.id) === "default"
        ? t("chat_topic_default")
        : t("chat_topic_untitled");
    }

    function sortTopics(items) {
      return (Array.isArray(items) ? [...items] : [])
        .filter((topic) => {
          const id = normalizeTopicID(topic?.id);
          return id && id !== AWARENESS_TOPIC_ID;
        })
        .sort((left, right) => topicUpdatedAt(right) - topicUpdatedAt(left));
    }

    function draftTopicID() {
      return creatingTopic.value ? "" : normalizeTopicID(selectedTopicID.value);
    }

    function persistDraft() {
      rememberChatDraft(submitEndpointRef.value, draftTopicID(), taskInput.value);
    }

    function restoreDraft() {
      taskInput.value = chatDraft(submitEndpointRef.value, draftTopicID());
    }

    function patchHistoryItem(itemID, patch) {
      const index = historyItems.value.findIndex((item) => cleanText(item?.id) === cleanText(itemID));
      if (index < 0) {
        return;
      }
      const next = historyItems.value.slice();
      next[index] = { ...next[index], ...patch };
      historyItems.value = next;
    }

    function taskHistoryItem(taskID) {
      const key = cleanText(taskID);
      return (
        historyItems.value.find(
          (item) => cleanText(item?.role) === "agent" && cleanText(item?.taskId) === key
        ) || null
      );
    }

    function handleHistoryScroll() {
      const viewport = historyViewport.value;
      if (!viewport) {
        return;
      }
      const distance = viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop;
      historyAutoStick = distance <= 32;
    }

    async function scrollToBottom(force = false) {
      if (!force && !historyAutoStick) {
        return;
      }
      await nextTick();
      if (historyViewport.value) {
        historyViewport.value.scrollTop = historyViewport.value.scrollHeight;
        historyAutoStick = true;
      }
    }

    function clearPoll(taskID) {
      const key = cleanText(taskID);
      const timerID = pollTimers.get(key);
      if (timerID) {
        window.clearTimeout(timerID);
      }
      pollTimers.delete(key);
    }

    function closeStream(taskID) {
      const key = cleanText(taskID);
      const entry = streamSockets.get(key);
      if (!entry) {
        return;
      }
      try {
        entry.socket.close();
      } catch {
        // The authoritative task state still comes from polling.
      }
      streamSockets.delete(key);
    }

    function clearTracking() {
      for (const taskID of pollTimers.keys()) {
        clearPoll(taskID);
      }
      pollInFlight.clear();
      for (const taskID of streamSockets.keys()) {
        closeStream(taskID);
      }
    }

    function schedulePoll(taskID) {
      const key = cleanText(taskID);
      if (!key || !alive) {
        return;
      }
      clearPoll(key);
      const timerID = window.setTimeout(() => {
        pollTimers.delete(key);
        void pollTask(key);
      }, POLL_INTERVAL_MS);
      pollTimers.set(key, timerID);
    }

    function applyTaskDetail(detail) {
      const mapped = taskListHistoryItems([detail], t, {
        agentName: agentName.value,
        endpointRef: submitEndpointRef.value,
        locale: currentLocale(),
      });
      let next = historyItems.value.slice();
      for (const item of mapped) {
        const index = next.findIndex((existing) => cleanText(existing?.id) === cleanText(item?.id));
        if (index >= 0) {
          next[index] = { ...next[index], ...item };
        } else {
          next.push(item);
        }
      }
      historyItems.value = next;
    }

    async function pollTask(taskID) {
      const key = cleanText(taskID);
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      if (!key || !targetEndpointRef || !alive || pollInFlight.has(key)) {
        return;
      }
      pollInFlight.add(key);
      try {
        const detail = await runtimeApiFetchForEndpoint(
          targetEndpointRef,
          `/tasks/${encodeURIComponent(key)}`
        );
        if (
          !alive ||
          targetEndpointRef !== submitEndpointRef.value ||
          targetVersion !== loadVersion
        ) {
          return;
        }
        applyTaskDetail(detail);
        const status = normalizeTaskStatus(detail?.status);
        if (taskApprovalState(detail)) {
          void loadApprovalDetails();
        }
        if (isTerminalStatus(status)) {
          clearPoll(key);
          closeStream(key);
        } else {
          schedulePoll(key);
        }
        error.value = "";
        void scrollToBottom();
      } catch (cause) {
        if (alive && targetVersion === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
          schedulePoll(key);
        }
      } finally {
        pollInFlight.delete(key);
      }
    }

    async function startTaskStream(taskID) {
      const key = cleanText(taskID);
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      if (!key || !supportsConsoleTaskStream(targetEndpointRef) || streamSockets.has(key)) {
        return;
      }
      let ticketPayload;
      try {
        ticketPayload = await createConsoleStreamTicket();
      } catch {
        return;
      }
      if (
        !alive ||
        targetEndpointRef !== submitEndpointRef.value ||
        targetVersion !== loadVersion ||
        isTerminalStatus(normalizeTaskStatus(taskHistoryItem(key)?.status))
      ) {
        return;
      }
      const url = buildConsoleStreamURL(cleanText(ticketPayload?.ticket), key, targetEndpointRef);
      if (!url) {
        return;
      }
      const socket = new WebSocket(url);
      const entry = { socket };
      streamSockets.set(key, entry);
      socket.onmessage = (event) => {
        if (
          !alive ||
          targetEndpointRef !== submitEndpointRef.value ||
          targetVersion !== loadVersion ||
          streamSockets.get(key) !== entry
        ) {
          return;
        }
        const frame = safeJSON(event.data, null);
        const existing = taskHistoryItem(key);
        if (!frame || !existing) {
          return;
        }
        const patch = {};
        if (frame.plan && typeof frame.plan === "object") {
          patch.plan = normalizePlan(frame.plan || existing.plan);
        }
        if (frame.activity && typeof frame.activity === "object") {
          patch.activity = normalizeActivity(frame.activity || existing.activity);
        }
        if (typeof frame.reasoning === "string" && frame.reasoning.trim()) {
          patch.reasoning = normalizeReasoning(frame.reasoning);
        }
        if (frame.preview !== true && typeof frame.text === "string" && frame.text) {
          patch.text = frame.text;
        } else if (frame.preview !== true && typeof frame.error === "string" && frame.error) {
          patch.text = frame.error;
        }
        if (typeof frame.status === "string" && frame.status) {
          patch.status = normalizeTaskStatus(frame.status);
        }
        if (Object.keys(patch).length > 0) {
          patchHistoryItem(existing.id, patch);
          void scrollToBottom();
        }
        if (frame.done) {
          closeStream(key);
        }
      };
      socket.onclose = () => {
        if (streamSockets.get(key) === entry) {
          streamSockets.delete(key);
        }
      };
      socket.onerror = () => {
        // Polling remains active.
      };
    }

    function trackTask(taskID, immediate = false) {
      const key = cleanText(taskID);
      if (!key || isTerminalStatus(normalizeTaskStatus(taskHistoryItem(key)?.status))) {
        return;
      }
      void startTaskStream(key);
      if (immediate) {
        void pollTask(key);
      } else {
        schedulePoll(key);
      }
    }

    async function loadApprovalDetails() {
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      const requestItems = historyItems.value
        .map((item) => ({
          requestID: cleanText(item?.approval?.approvalRequestID),
          status: cleanText(item?.approval?.status || "pending").toLowerCase(),
        }))
        .filter((item) => item.requestID);
      const pending = requestItems.filter(
        (item) => !approvalDetailAttempts.has(`${item.status}:${item.requestID}`)
      );
      if (pending.length === 0 || !targetEndpointRef) {
        return;
      }
      for (const item of pending) {
        approvalDetailAttempts.add(`${item.status}:${item.requestID}`);
      }
      const requests = [];
      if (pending.some((item) => item.status === "pending")) {
        requests.push(
          runtimeApiFetchForEndpoint(targetEndpointRef, "/approvals?status=pending&limit=200")
            .then((payload) => (Array.isArray(payload?.items) ? payload.items : []))
            .catch(() => [])
        );
      }
      for (const item of pending) {
        if (item.status !== "pending") {
          requests.push(
            runtimeApiFetchForEndpoint(
              targetEndpointRef,
              `/approvals/${encodeURIComponent(item.requestID)}`
            )
              .then((payload) => [payload])
              .catch(() => [])
          );
        }
      }
      const details = approvalDetailsByID({ items: (await Promise.all(requests)).flat() });
      if (
        !alive ||
        targetEndpointRef !== submitEndpointRef.value ||
        targetVersion !== loadVersion ||
        details.size === 0
      ) {
        return;
      }
      historyItems.value = historyItems.value.map((item) => {
        const requestID = cleanText(item?.approval?.approvalRequestID);
        const detail = details.get(requestID);
        if (!detail) {
          return item;
        }
        const currentStatus = cleanText(item?.approval?.status).toLowerCase();
        return {
          ...item,
          approval: {
            ...item.approval,
            ...detail,
            status:
              currentStatus === "denied" || currentStatus === "expired"
                ? currentStatus
                : detail.status || currentStatus || "pending",
            reasons: detail.reasons.map((reason) => chatApprovalReasonText(reason, t)),
          },
        };
      });
    }

    async function fetchHistory(version) {
      if (!submitEndpointRef.value || (topicsSupported.value && creatingTopic.value)) {
        historyItems.value = [];
        return;
      }
      let path = `/tasks?limit=${HISTORY_LIMIT}`;
      if (topicsSupported.value && selectedTopicID.value) {
        path += `&topic_id=${encodeURIComponent(selectedTopicID.value)}`;
      }
      const payload = await runtimeApiFetchForEndpoint(submitEndpointRef.value, path);
      if (!alive || version !== loadVersion) {
        return;
      }
      historyItems.value = taskListHistoryItems(
        Array.isArray(payload?.items) ? payload.items : [],
        t,
        {
          agentName: agentName.value,
          endpointRef: submitEndpointRef.value,
          locale: currentLocale(),
        }
      );
      approvalDetailAttempts.clear();
      void loadApprovalDetails();
      for (const item of historyItems.value) {
        if (
          cleanText(item?.role) === "agent" &&
          cleanText(item?.taskId) &&
          !isTerminalStatus(normalizeTaskStatus(item?.status))
        ) {
          trackTask(item.taskId);
        }
      }
      void scrollToBottom();
    }

    async function fetchTopics(preferredTopicID = "", removeIfMissing = false) {
      const payload = await runtimeApiFetchForEndpoint(
        submitEndpointRef.value,
        `/topics?limit=${TOPIC_LIMIT}`
      );
      topicsSupported.value = true;
      const preferred = normalizeTopicID(preferredTopicID);
      const remembered = normalizeTopicID(lastTopicID(submitEndpointRef.value));
      const candidates = [preferred, selectedTopicID.value, remembered]
        .map((value) => normalizeTopicID(value))
        .filter(Boolean);
      const items = Array.isArray(payload?.items) ? [...payload.items] : [];
      const targetTopicID = candidates[0] || "";
      if (
        targetTopicID &&
        !items.some((topic) => normalizeTopicID(topic?.id) === targetTopicID)
      ) {
        try {
          const target = await runtimeApiFetchForEndpoint(
            submitEndpointRef.value,
            `/topics/${encodeURIComponent(targetTopicID)}`
          );
          if (normalizeTopicID(target?.id) === targetTopicID) {
            items.push(target);
          }
        } catch (cause) {
          if (cause?.status !== 404) {
            throw cause;
          }
          if (removeIfMissing && preferred && targetTopicID === preferred) {
            return { missingTopicID: preferred };
          }
        }
      }
      topics.value = sortTopics(items);
      const nextTopicID = candidates.find((value) =>
        topics.value.some((topic) => normalizeTopicID(topic?.id) === value)
      );
      selectedTopicID.value = nextTopicID || normalizeTopicID(topics.value[0]?.id);
      creatingTopic.value = selectedTopicID.value === "";
      if (selectedTopicID.value) {
        rememberLastTopicID(submitEndpointRef.value, selectedTopicID.value);
      }
      return { missingTopicID: "" };
    }

    async function loadPane(preferredTopicID = "", removeIfMissing = false) {
      const version = loadVersion + 1;
      loadVersion = version;
      historyAutoStick = true;
      clearTracking();
      historyLoading.value = true;
      error.value = "";
      if (!available.value) {
        historyItems.value = [];
        historyLoading.value = false;
        return;
      }
      try {
        try {
          const topicResult = await fetchTopics(preferredTopicID, removeIfMissing);
          if (!alive || version !== loadVersion) {
            return;
          }
          if (topicResult?.missingTopicID) {
            emit("topic-missing", {
              paneID: props.paneId,
              topicID: topicResult.missingTopicID,
            });
            return;
          }
        } catch (cause) {
          if (cause?.status !== 404) {
            throw cause;
          }
          topicsSupported.value = false;
          topics.value = [];
          selectedTopicID.value = "";
          creatingTopic.value = false;
        }
        if (!alive || version !== loadVersion) {
          return;
        }
        emit("topic-change", {
          paneID: props.paneId,
          topicID: normalizeTopicID(selectedTopicID.value),
        });
        restoreDraft();
        await fetchHistory(version);
      } catch (cause) {
        if (alive && version === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
          historyItems.value = [];
        }
      } finally {
        if (alive && version === loadVersion) {
          historyLoading.value = false;
        }
      }
    }

    async function selectTopic(item) {
      topicDialogOpen.value = false;
      const topicID = normalizeTopicID(item?.value);
      if (!topicID || topicID === selectedTopicID.value) {
        return;
      }
      persistDraft();
      selectedTopicID.value = topicID;
      creatingTopic.value = false;
      rememberLastTopicID(submitEndpointRef.value, topicID);
      emit("topic-change", { paneID: props.paneId, topicID });
      restoreDraft();
      const version = loadVersion + 1;
      loadVersion = version;
      clearTracking();
      historyAutoStick = true;
      historyLoading.value = true;
      error.value = "";
      try {
        await fetchHistory(version);
      } catch (cause) {
        if (alive && version === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
        }
      } finally {
        if (alive && version === loadVersion) {
          historyLoading.value = false;
        }
      }
    }

    function startNewTopic() {
      topicDialogOpen.value = false;
      persistDraft();
      loadVersion += 1;
      clearTracking();
      historyAutoStick = true;
      selectedTopicID.value = "";
      creatingTopic.value = true;
      emit("topic-change", { paneID: props.paneId, topicID: "" });
      historyItems.value = [];
      error.value = "";
      restoreDraft();
      void nextTick(() => emit("activate", props.paneId));
    }

    function openTopicDialog() {
      if (!available.value || historyLoading.value) {
        return;
      }
      topicFilter.value = "";
      topicDialogOpen.value = true;
    }

    async function submitTask() {
      const task = cleanText(taskInput.value);
      if (!task || sending.value || !available.value) {
        return;
      }
      const submittedTopicID = draftTopicID();
      const requestBody = { task };
      if (topicsSupported.value && submittedTopicID) {
        requestBody.topic_id = submittedTopicID;
      }
      const provisionalUserID = `provisional:${newHistoryID()}:user`;
      const provisionalAgentID = `provisional:${newHistoryID()}:agent`;
      historyItems.value = [
        ...historyItems.value,
        {
          id: provisionalUserID,
          role: "user",
          text: task,
          endpointRef: submitEndpointRef.value,
          topicID: submittedTopicID,
          timeText: historyTimeLabel(new Date().toISOString(), currentLocale()),
        },
        {
          id: provisionalAgentID,
          role: "agent",
          text: buildPollingHint(agentName.value, t, provisionalAgentID),
          status: "queued",
          taskId: "",
          pendingSeed: provisionalAgentID,
          presentation: isContextCompactCommand(task) ? "context-compact" : "",
        },
      ];
      sending.value = true;
      error.value = "";
      historyAutoStick = true;
      taskInput.value = "";
      clearChatDraft(submitEndpointRef.value, submittedTopicID);
      void scrollToBottom();
      try {
        const submitted = await runtimeApiFetchForEndpoint(submitEndpointRef.value, "/tasks", {
          method: "POST",
          body: requestBody,
        });
        const taskID = cleanText(submitted?.id);
        const trackedTaskID = cleanText(submitted?.steer_target_task_id) || taskID;
        if (!taskID || !trackedTaskID) {
          throw new Error(t("chat_missing_task_id"));
        }
        const createdTopicID = normalizeTopicID(submitted?.topic_id || submittedTopicID);
        if (topicsSupported.value && !createdTopicID) {
          throw new Error(t("chat_missing_topic_id"));
        }
        if (createdTopicID) {
          selectedTopicID.value = createdTopicID;
          creatingTopic.value = false;
          rememberLastTopicID(submitEndpointRef.value, createdTopicID);
          emit("topic-change", { paneID: props.paneId, topicID: createdTopicID });
        }
        await loadPane(createdTopicID);
        trackTask(trackedTaskID, true);
      } catch (cause) {
        const message = cause?.message || t("msg_load_failed");
        error.value = message;
        taskInput.value = task;
        rememberChatDraft(submitEndpointRef.value, submittedTopicID, task);
        patchHistoryItem(provisionalAgentID, {
          status: "failed",
          text: message,
        });
      } finally {
        sending.value = false;
      }
    }

    async function stopActiveTask() {
      const taskID = cleanText(activeTaskItem.value?.taskId);
      if (!taskID || sending.value || !submitEndpointRef.value) {
        return;
      }
      sending.value = true;
      error.value = "";
      try {
        await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/tasks/${encodeURIComponent(taskID)}/stop`,
          { method: "POST" }
        );
        await pollTask(taskID);
      } catch (cause) {
        error.value = cause?.message || t("msg_load_failed");
      } finally {
        sending.value = false;
      }
    }

    async function decideApproval(item, decision) {
      const requestID = cleanText(item?.approval?.approvalRequestID);
      const taskID = cleanText(item?.taskId);
      const action = cleanText(decision).toLowerCase();
      if (!requestID || !taskID || (action !== "approve" && action !== "deny")) {
        return;
      }
      patchHistoryItem(item.id, { approvalBusy: true, approvalError: "" });
      try {
        const result = await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/approvals/${encodeURIComponent(requestID)}/${action}`,
          { method: "POST", body: { actor: "console:user" } }
        );
        const decisionError = cleanText(result?.error);
        if (action === "approve" && result?.resumed === false && decisionError) {
          patchHistoryItem(item.id, {
            approval: null,
            approvalBusy: false,
            status: "failed",
            text: decisionError,
          });
          return;
        }
        if (action === "deny") {
          patchHistoryItem(item.id, {
            approval: { ...item.approval, status: "denied" },
            approvalBusy: false,
            status: "canceled",
            text: t("chat_approval_denied"),
          });
        } else {
          patchHistoryItem(item.id, {
            approval: null,
            approvalBusy: false,
            status: "queued",
            text: buildPollingHint(agentName.value, t, historyPendingSeed(item, taskID)),
          });
          trackTask(taskID, true);
        }
        await pollTask(taskID);
      } catch (cause) {
        patchHistoryItem(item.id, {
          approvalBusy: false,
          approvalError: cause?.message || t("msg_load_failed"),
        });
      }
    }

    async function copyHistoryItem(item) {
      const text = String(item?.text || "");
      if (
        !text ||
        typeof navigator === "undefined" ||
        typeof navigator.clipboard?.writeText !== "function"
      ) {
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        copiedItemID.value = cleanText(item?.id);
        if (copiedTimerID) {
          window.clearTimeout(copiedTimerID);
        }
        copiedTimerID = window.setTimeout(() => {
          copiedItemID.value = "";
          copiedTimerID = 0;
        }, 1200);
      } catch {
        // Clipboard availability depends on the browser security context.
      }
    }

    function toggleStatus(itemID, panel) {
      const key = cleanText(itemID);
      const value = cleanText(panel);
      if (!key || !["plan", "activity", "reasoning"].includes(value)) {
        return;
      }
      const next = { ...expandedState.value };
      if (next[key] === value) {
        delete next[key];
      } else {
        next[key] = value;
      }
      expandedState.value = next;
    }

    function toggleDuration(item) {
      if (!cleanText(item?.durationText)) {
        return;
      }
      patchHistoryItem(item.id, {
        durationVisible: item?.durationVisible !== true,
        durationVisibleManual: true,
      });
    }

    function openFullChat() {
      persistDraft();
      emit("open-full", {
        paneID: props.paneId,
        endpointRef: endpointRef.value,
        topicID: normalizeTopicID(selectedTopicID.value),
      });
    }

    function selectEndpoint(item) {
      if (!cleanText(item?.value) || item?.disabled === true) {
        return;
      }
      emit("endpoint-change", { paneID: props.paneId, item });
    }

    function splitPane(direction) {
      emit("split", { paneID: props.paneId, direction });
    }

    watch(taskInput, () => {
      if (skipNextDraftPersist) {
        skipNextDraftPersist = false;
        return;
      }
      persistDraft();
    });

    watch(
      () =>
        `${endpointRef.value}\u0000${submitEndpointRef.value}\u0000${
          props.endpoint?.connected === true ? "1" : "0"
        }`,
      () => {
        const endpointChanged = watchedEndpointRef !== endpointRef.value;
        watchedEndpointRef = endpointRef.value;
        if (endpointChanged) {
          topicDialogOpen.value = false;
          topicFilter.value = "";
          topics.value = [];
          topicsSupported.value = true;
          selectedTopicID.value = "";
          creatingTopic.value = false;
          historyItems.value = [];
          if (taskInput.value !== "") {
            skipNextDraftPersist = true;
            taskInput.value = "";
          }
          error.value = "";
          copiedItemID.value = "";
          expandedState.value = {};
          approvalDetailAttempts.clear();
        }
        const preferredTopicID = endpointChanged ? "" : selectedTopicID.value;
        const removeIfMissing =
          Boolean(preferredTopicID) &&
          preferredTopicID === normalizeTopicID(props.initialTopicId);
        void loadPane(preferredTopicID, removeIfMissing);
      }
    );

    onMounted(() => {
      const initialTopicID = normalizeTopicID(props.initialTopicId);
      selectedTopicID.value = initialTopicID;
      void loadPane(initialTopicID, Boolean(initialTopicID));
    });

    onUnmounted(() => {
      alive = false;
      loadVersion += 1;
      persistDraft();
      clearTracking();
      if (copiedTimerID) {
        window.clearTimeout(copiedTimerID);
      }
    });

    return {
      t,
      agentName,
      available,
      avatarURL,
      composerInputHistory,
      composerStopMode,
      copiedItemID,
      creatingTopic,
      decideApproval,
      error,
      expandedState,
      filteredTopicOptions,
      historyItems,
      historyLoading,
      historyViewport,
      handleHistoryScroll,
      openFullChat,
      openTopicDialog,
      copyHistoryItem,
      selectedEndpointOption,
      selectedTopicID,
      selectEndpoint,
      selectTopic,
      sendDisabled,
      sending,
      scrollToBottom,
      startNewTopic,
      stopActiveTask,
      splitPane,
      submitEndpointRef,
      submitTask,
      taskInput,
      toggleDuration,
      toggleStatus,
      topicDialogOpen,
      topicFilter,
      topicLabel,
      topicOptions,
      topicsSupported,
    };
  },
  template: `
    <article
      class="agent-chat-pane"
      :class="{ 'is-unavailable': !available }"
      :data-pane-id="paneId"
      :data-endpoint-ref="endpoint.endpoint_ref"
      :aria-label="agentName"
      tabindex="-1"
      @pointerdown="$emit('activate', paneId)"
    >
      <header class="agent-chat-pane-head">
        <div class="agent-chat-pane-identity">
          <QDropdownMenu
            :key="paneId + ':' + endpoint.endpoint_ref"
            class="agent-chat-pane-endpoint-menu"
            :class="available ? 'is-online' : 'is-offline'"
            :items="endpointOptions"
            :initialItem="selectedEndpointOption"
            :placeholder="agentName"
            :useFilter="endpointOptions.length > 8"
            useDialog="always"
            :scrollHeight="Math.min(320, Math.max(60, endpointOptions.length * 44 + 16)) + 'px'"
            hideSelected
            hideActionLabel
            :disabled="sending"
            :title="endpoint.endpoint_ref"
            variant="plain"
            @change="selectEndpoint"
          >
            <img
              class="agent-chat-pane-endpoint-avatar"
              :src="avatarURL"
              :alt="agentName"
            />
          </QDropdownMenu>
          <span
            class="agent-chat-pane-status"
            :class="available ? 'is-online' : 'is-offline'"
            aria-hidden="true"
          ></span>
        </div>

        <div class="agent-chat-pane-topicbar">
          <button
            v-if="topicsSupported"
            type="button"
            class="agent-chat-pane-topic-trigger"
            :disabled="!available || historyLoading"
            :title="topicLabel"
            aria-haspopup="dialog"
            :aria-expanded="topicDialogOpen ? 'true' : 'false'"
            @click.stop="openTopicDialog"
          >
            <span class="agent-chat-pane-topic-label">{{ topicLabel }}</span>
            <QIconChevronDown class="icon agent-chat-pane-topic-chevron" aria-hidden="true" />
          </button>
          <span v-else class="agent-chat-pane-topic-label">{{ topicLabel }}</span>
        </div>

        <div class="agent-chat-pane-actions">
          <QButton
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_split_right')"
            :aria-label="t('agent_desk_split_right')"
            @click.stop="splitPane('row')"
          >
            <QIconLayoutRight class="icon" />
          </QButton>
          <QButton
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_split_down')"
            :aria-label="t('agent_desk_split_down')"
            @click.stop="splitPane('column')"
          >
            <QIconLayoutRight class="icon agent-chat-pane-split-down-icon" />
          </QButton>
          <QButton
            v-if="canClose"
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_close_pane')"
            :aria-label="t('agent_desk_close_pane')"
            @click.stop="$emit('close', paneId)"
          >
            <QIconCloseCircle class="icon" />
          </QButton>
        </div>
      </header>

      <section v-if="!available" class="agent-chat-pane-unavailable">
        <span class="agent-chat-pane-unavailable-mark" aria-hidden="true"></span>
        <h3>{{ t('agent_desk_endpoint_unavailable') }}</h3>
        <p>{{ t('agent_desk_endpoint_unavailable_hint') }}</p>
      </section>
      <div
        v-else
        ref="historyViewport"
        class="agent-chat-pane-history chat-history"
        @scroll="handleHistoryScroll"
      >
        <div v-if="historyLoading" class="agent-chat-pane-skeleton" aria-hidden="true">
          <QSkeleton width="42%" height="16px" />
          <QSkeleton width="88%" height="72px" />
          <QSkeleton width="56%" height="16px" />
          <QSkeleton width="76%" height="92px" />
        </div>
        <ChatHistoryList
          v-else
          :items="historyItems"
          :loading="false"
          :emptyText="creatingTopic ? t('chat_new_topic_intro') : t('chat_topic_empty')"
          :submitEndpointRef="submitEndpointRef"
          :selectedTopicId="selectedTopicID"
          :copiedItemId="copiedItemID"
          :expandedState="expandedState"
          :copyLabel="t('action_copy')"
          :filePreviewLabel="t('agent_desk_open_full_chat')"
          :approvalApproveLabel="t('chat_approval_approve')"
          :approvalDenyLabel="t('chat_approval_deny')"
          :approvalTitle="t('chat_approval_title')"
          @copy="copyHistoryItem"
          @rendered="scrollToBottom()"
          @preview-file="openFullChat"
          @toggle-status="toggleStatus"
          @time-click="toggleDuration"
          @approval-approve="decideApproval($event, 'approve')"
          @approval-deny="decideApproval($event, 'deny')"
        />
      </div>

      <p v-if="error && available" class="agent-chat-pane-error" role="alert">{{ error }}</p>
      <ChatComposer
        v-if="available"
        :modelValue="taskInput"
        :disabled="sending"
        :sendDisabled="sendDisabled"
        :sending="sending"
        :stopMode="composerStopMode"
        :placeholder="t('chat_input_placeholder', { name: agentName })"
        :sendLabel="composerStopMode ? t('chat_action_stop') : t('chat_action_send')"
        :inputHistory="composerInputHistory"
        :maxRows="8"
        :showAddActions="false"
        @update:modelValue="taskInput = $event"
        @submit="submitTask"
        @stop="stopActiveTask"
      />

      <Teleport to="body">
        <AppDialogShell
          v-if="topicsSupported"
          :modelValue="topicDialogOpen"
          :title="t('agent_desk_topic')"
          width="420px"
          @update:modelValue="topicDialogOpen = $event"
          @close="topicDialogOpen = false"
        >
          <section class="agent-chat-topic-dialog">
            <div class="agent-chat-topic-dialog-filterbar">
              <QInput
                v-model="topicFilter"
                class="agent-chat-topic-dialog-filter"
                :placeholder="t('agent_desk_topic_filter_placeholder')"
              />
              <QButton
                class="plain icon agent-chat-pane-new-topic"
                :class="{ 'is-active': creatingTopic }"
                :disabled="!available || historyLoading"
                :title="t('agent_desk_new_topic')"
                :aria-label="t('agent_desk_new_topic')"
                @click="startNewTopic"
              >
                <QIconPlus class="icon" />
              </QButton>
            </div>

            <div class="agent-chat-topic-dialog-list" role="listbox" :aria-label="t('agent_desk_topic')">
              <button
                v-for="item in filteredTopicOptions"
                :key="item.value"
                type="button"
                class="agent-chat-topic-dialog-option"
                :class="{ 'is-selected': item.value === selectedTopicID }"
                role="option"
                :aria-selected="item.value === selectedTopicID ? 'true' : 'false'"
                @click="selectTopic(item)"
              >
                <span>{{ item.title }}</span>
                <QIconCheckCircle
                  v-if="item.value === selectedTopicID"
                  class="icon agent-chat-topic-dialog-check"
                  aria-hidden="true"
                />
              </button>
              <p v-if="filteredTopicOptions.length === 0" class="agent-chat-topic-dialog-empty">
                {{ t('agent_desk_topic_filter_empty') }}
              </p>
            </div>
          </section>
        </AppDialogShell>
      </Teleport>
    </article>
  `,
};

export default AgentChatPane;
