import { taskApprovalState } from "./chat-approvals.js";
import { placeSteeredAgentsAfterUsers } from "./chat-history-steering.js";

const AWARENESS_TOPIC_ID = "_awareness";
const LEGACY_HEARTBEAT_TOPIC_ID = "_heartbeat";
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

function isTerminalStatus(status) {
  return status === "done" || status === "failed" || status === "canceled";
}

function normalizeTopicID(raw) {
  const topicID = String(raw || "").trim();
  return topicID === LEGACY_HEARTBEAT_TOPIC_ID ? AWARENESS_TOPIC_ID : topicID;
}

function normalizeHistoryFileReferences(rawItems) {
  if (!Array.isArray(rawItems)) {
    return [];
  }
  return rawItems
    .map((item, index) => {
      const dirName = String(item?.dir_name || item?.dirName || "").trim();
      const path = String(item?.path || "").trim();
      if (!path || (dirName !== "workspace_dir" && dirName !== "file_cache_dir")) {
        return null;
      }
      const pathParts = path.split(/[\\/]/u).filter(Boolean);
      const name = String(item?.name || "").trim() || pathParts[pathParts.length - 1] || path;
      return {
        id: `history-file-${dirName}-${path}-${index}`,
        name,
        dirName,
        path,
        status: "ready",
        sourceFile: item?.sourceFile || null,
      };
    })
    .filter(Boolean);
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

function historyTimeLabel(raw, locale = "en-US", now = new Date()) {
  const text = String(raw || "").trim();
  if (!text) {
    return "";
  }
  const date = new Date(text);
  if (Number.isNaN(date.getTime())) {
    return text;
  }
  const current = now instanceof Date ? now : new Date(now);
  const sameDay =
    !Number.isNaN(current.getTime()) &&
    current.getFullYear() === date.getFullYear() &&
    current.getMonth() === date.getMonth() &&
    current.getDate() === date.getDate();
  const timeLabel = date.toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
  });
  if (sameDay) {
    return timeLabel;
  }
  const dayLabel = date.toLocaleDateString(locale, {
    month: "short",
    day: "numeric",
  });
  return `${dayLabel} ${timeLabel}`;
}

function timestampMs(raw) {
  const text = String(raw || "").trim();
  if (!text) {
    return 0;
  }
  const value = Date.parse(text);
  return Number.isFinite(value) ? value : 0;
}

function taskDurationMs(task, now = Date.now()) {
  const status = normalizeTaskStatus(task?.status);
  const finishedMs = timestampMs(task?.finished_at);
  const startedMs = timestampMs(task?.started_at) || (finishedMs > 0 ? timestampMs(task?.created_at) : 0);
  if (startedMs <= 0) {
    return 0;
  }
  const currentMs = now instanceof Date ? now.getTime() : Number(now);
  const endMs = finishedMs > 0 ? finishedMs : isTerminalStatus(status) ? 0 : currentMs;
  if (!Number.isFinite(endMs) || endMs <= startedMs) {
    return 0;
  }
  return endMs - startedMs;
}

function durationPartsLabel(durationMs, t) {
  const totalSeconds = Math.max(1, Math.round(Number(durationMs || 0) / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const parts = [];
  if (hours > 0) {
    parts.push(t(hours === 1 ? "chat_duration_hour" : "chat_duration_hours", { value: hours }));
  }
  if (minutes > 0) {
    parts.push(t("chat_duration_minute", { value: minutes }));
  }
  if (seconds > 0 || parts.length === 0) {
    parts.push(t("chat_duration_second", { value: seconds }));
  }
  return parts.join(" ");
}

function taskDurationLabel(task, t, now = Date.now()) {
  const durationMs = taskDurationMs(task, now);
  if (durationMs <= 0) {
    return "";
  }
  return t("chat_task_duration_thought", {
    duration: durationPartsLabel(durationMs, t),
  });
}

function taskRawJSON(task) {
  return task ? stringifyResult(task) : "";
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

function isContextCompactCommand(raw) {
  return /^\/ctx(?:@\S+)?\s+compact$/iu.test(String(raw || "").trim());
}

function normalizePlanStatus(raw) {
  const value = String(raw || "").trim().toLowerCase();
  switch (value) {
    case "completed":
    case "in_progress":
    case "pending":
      return value;
    default:
      return "pending";
  }
}

function normalizePlan(raw) {
  const steps = Array.isArray(raw?.steps)
    ? raw.steps
        .map((step) => ({
          step: String(step?.step || "").trim(),
          status: normalizePlanStatus(step?.status),
        }))
        .filter((step) => step.step)
    : [];
  return steps.length > 0 ? { steps } : null;
}

function taskPlan(task) {
  return normalizePlan(task?.result?.plan || task?.result?.final?.plan);
}

function normalizeActivityKind(raw) {
  const value = String(raw || "").trim().toLowerCase();
  return value === "tool" || value === "subtask" ? value : "";
}

function normalizeActivityEntry(raw) {
  const id = String(raw?.id || "").trim();
  const kind = normalizeActivityKind(raw?.kind);
  if (!id || !kind) {
    return null;
  }
  const args =
    raw?.args && typeof raw.args === "object" && !Array.isArray(raw.args)
      ? Object.fromEntries(
          Object.entries(raw.args)
            .map(([key, value]) => [String(key || "").trim(), value])
            .filter(([key]) => key)
        )
      : null;
  return {
    id,
    kind,
    name: String(raw?.name || "").trim(),
    status: normalizeTaskStatus(raw?.status),
    at: String(raw?.at || "").trim(),
    stream: String(raw?.stream || "").trim(),
    output: String(raw?.output || ""),
    args: args && Object.keys(args).length > 0 ? args : null,
    summary: String(raw?.summary || "").trim(),
    error: String(raw?.error || "").trim(),
    taskId: String(raw?.task_id || "").trim(),
    mode: String(raw?.mode || "").trim(),
    profile: String(raw?.profile || "").trim(),
    outputKind: String(raw?.output_kind || "").trim(),
  };
}

function normalizeActivity(raw) {
  const history = Array.isArray(raw?.history)
    ? raw.history.map((entry) => normalizeActivityEntry(entry)).filter(Boolean)
    : [];
  const current = normalizeActivityEntry(raw?.current) || history[history.length - 1] || null;
  return current || history.length > 0 ? { current, history } : null;
}

function taskActivity(task) {
  return normalizeActivity(task?.result?.activity);
}

function normalizeReasoning(raw) {
  return typeof raw === "string" ? raw.trim() : "";
}

function taskReasoning(task) {
  return normalizeReasoning(task?.result?.reasoning);
}

function chatApprovalReasonText(raw, t) {
  const reason = String(raw || "").trim().toLowerCase();
  switch (reason) {
    case "bash_requires_approval":
      return t("audit_reason_bash_requires_approval");
    case "powershell_requires_approval":
      return t("chat_approval_reason_powershell");
    default:
      return reason.replaceAll("_", " ");
  }
}

function stableHash(raw) {
  const text = String(raw || "");
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function agentDisplayName(agentName, t) {
  const value = String(agentName || "").trim();
  return value || t("chat_agent_name_fallback");
}

function buildPollingHint(agentName, t, seed) {
  const actionKey = POLLING_ACTION_KEYS[stableHash(seed || "agent") % POLLING_ACTION_KEYS.length];
  return t("chat_polling_hint", {
    name: agentDisplayName(agentName, t),
    action: t(actionKey),
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
  const approval = taskApprovalState(task);
  if (approval?.status === "pending") {
    return approval.message || t("chat_approval_waiting_text");
  }
  if (approval?.status === "denied") {
    return t("chat_approval_denied");
  }
  if (approval?.status === "expired") {
    return t("chat_approval_expired");
  }
  const output = taskOutputText(task);
  if (output) {
    return output;
  }
  const errorText = String(task?.error || "").trim();
  if (errorText) {
    if (errorText === "Approval denied. Task canceled.") {
      return t("chat_approval_denied");
    }
    if (errorText === "Approval expired. Task canceled.") {
      return t("chat_approval_expired");
    }
    return errorText;
  }
  if (isTerminalStatus(normalizeTaskStatus(task?.status))) {
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
  const presentation = isContextCompactCommand(userText) ? "context-compact" : "";
  const locale = String(options.locale || "en-US");
  const now = options.now || new Date();
  if (userText) {
    items.push({
      id: `${taskID}:user`,
      role: "user",
      text: userText,
      files: normalizeHistoryFileReferences(task?.file_references),
      endpointRef: String(options.endpointRef || "").trim(),
      topicID: normalizeTopicID(task?.topic_id),
      status: "",
      timeText: historyTimeLabel(task?.created_at, locale, now),
      durationText: "",
      durationVisible: false,
      durationVisibleManual: false,
      taskId: "",
      rawJSON: "",
      steerTargetTaskID: String(task?.steer_target_task_id || "").trim(),
    });
  }
  if (options.includeAgent !== false) {
    items.push({
      id: `${taskID}:agent`,
      role: "agent",
      text: taskAgentText(task, t, {
        agentName: options.agentName,
        pendingSeed: taskID,
      }),
      plan: taskPlan(task),
      activity: taskActivity(task),
      reasoning: taskReasoning(task),
      approval: taskApprovalState(task),
      approvalBusy: false,
      approvalError: "",
      status: normalizeTaskStatus(task?.status),
      timeText: historyTimeLabel(task?.finished_at || task?.started_at || task?.created_at, locale, now),
      durationText: taskDurationLabel(task, t, now),
      durationVisible: false,
      durationVisibleManual: false,
      taskId: taskID,
      rawJSON: taskRawJSON(task),
      pendingSeed: taskID,
      presentation,
    });
  }
  return items;
}

function taskListHistoryItems(tasks, t, options = {}) {
  const sortedTasks = Array.isArray(tasks) ? [...tasks] : [];
  sortedTasks.sort((left, right) => {
    const leftTime = Date.parse(String(left?.created_at || "").trim());
    const rightTime = Date.parse(String(right?.created_at || "").trim());
    return (Number.isFinite(leftTime) ? leftTime : 0) - (Number.isFinite(rightTime) ? rightTime : 0);
  });

  const items = [];
  for (const task of sortedTasks) {
    const targetTaskID = String(task?.steer_target_task_id || "").trim();
    items.push(
      ...taskHistoryItems(task, t, {
        ...options,
        includeAgent: !targetTaskID,
      })
    );
  }
  return placeSteeredAgentsAfterUsers(items);
}

export {
  agentDisplayName,
  buildPollingHint,
  chatApprovalReasonText,
  historyPendingSeed,
  historyTimeLabel,
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
};
