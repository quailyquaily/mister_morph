import { computed } from "vue";
import { translate } from "../core/context";
import "./ChatStatusCard.css";

const PANEL_PLAN = "plan";
const PANEL_ACTIVITY = "activity";

function cleanText(value) {
  return String(value || "").trim();
}

function normalizeTaskStatus(raw) {
  const value = cleanText(raw).toLowerCase();
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

function normalizePlanStatus(raw) {
  const value = cleanText(raw).toLowerCase();
  switch (value) {
    case "completed":
    case "in_progress":
    case "pending":
      return value;
    default:
      return "pending";
  }
}

function normalizeActivityKind(raw) {
  const value = cleanText(raw).toLowerCase();
  switch (value) {
    case "tool":
    case "subtask":
      return value;
    default:
      return "";
  }
}

function activityCurrentEntry(activity) {
  if (!activity) {
    return null;
  }
  const history = Array.isArray(activity.history) ? activity.history : [];
  return activity.current || history[history.length - 1] || null;
}

function activityHistoryEntries(activity) {
  if (!Array.isArray(activity?.history) || activity.history.length <= 1) {
    return [];
  }
  return activity.history.slice(0, -1).reverse();
}

function activityBlockState(activity, taskStatus) {
  const rawTaskStatus = cleanText(taskStatus);
  if (rawTaskStatus) {
    return normalizeTaskStatus(rawTaskStatus);
  }
  return normalizeTaskStatus(activityCurrentEntry(activity)?.status);
}

function activityStateClass(activity, taskStatus) {
  return `chat-activity-state is-${activityBlockState(activity, taskStatus).replaceAll("_", "-")}`;
}

function activityEntryClass(entry) {
  return `chat-activity-entry is-${normalizeTaskStatus(entry?.status).replaceAll("_", "-")}`;
}

function activityBlockStatusLabel(activity, taskStatus, t) {
  return t(`status_${activityBlockState(activity, taskStatus)}`);
}

function activityKindLabel(entry, t) {
  switch (normalizeActivityKind(entry?.kind)) {
    case "tool":
      return t("chat_activity_kind_tool");
    case "subtask":
      return t("chat_activity_kind_subtask");
    default:
      return "";
  }
}

function activityEntryTitle(entry) {
  const name = cleanText(entry?.name);
  if (name) {
    return name;
  }
  return normalizeActivityKind(entry?.kind) || "activity";
}

function activityEntryTimeText(entry) {
  const raw = cleanText(
    entry?.at ||
      entry?.time ||
      entry?.timestamp ||
      entry?.updated_at ||
      entry?.updatedAt ||
      entry?.created_at ||
      entry?.createdAt
  );
  if (!raw) {
    return "";
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const pad = (value) => String(value).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function activityParamValueText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value.trim();
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function truncateActivityParamValue(raw) {
  const text = cleanText(raw);
  if (text.length <= 120) {
    return text;
  }
  return `${text.slice(0, 117)}...`;
}

function activityParams(entry) {
  const items = [];
  if (entry?.args && typeof entry.args === "object" && !Array.isArray(entry.args)) {
    for (const key of Object.keys(entry.args).sort()) {
      const value = truncateActivityParamValue(activityParamValueText(entry.args[key]));
      if (!value) {
        continue;
      }
      items.push({ key, value });
    }
  }
  if (normalizeActivityKind(entry?.kind) === "subtask") {
    const extras = [
      ["task_id", entry?.taskId],
      ["mode", entry?.mode],
      ["profile", entry?.profile],
      ["output", entry?.outputKind],
    ];
    for (const [key, rawValue] of extras) {
      const value = truncateActivityParamValue(activityParamValueText(rawValue));
      if (!value) {
        continue;
      }
      items.push({ key, value });
    }
  }
  return items;
}

function activityEntryNote(entry) {
  const errorText = cleanText(entry?.error);
  if (errorText) {
    return errorText;
  }
  return cleanText(entry?.summary);
}

function planCompletedCount(plan) {
  if (!Array.isArray(plan?.steps)) {
    return 0;
  }
  return plan.steps.filter((step) => step.status === "completed").length;
}

function planTotalCount(plan) {
  return Array.isArray(plan?.steps) ? plan.steps.length : 0;
}

function planProgressText(plan) {
  return `${planCompletedCount(plan)}/${planTotalCount(plan)}`;
}

function planStepClass(step) {
  return `chat-plan-step is-${normalizePlanStatus(step?.status).replaceAll("_", "-")}`;
}

function normalizePanel(raw) {
  const value = cleanText(raw);
  return value === PANEL_PLAN || value === PANEL_ACTIVITY ? value : "";
}

const ChatStatusCard = {
  emits: ["toggle"],
  props: {
    itemId: {
      type: [String, Number],
      default: "",
    },
    plan: {
      type: Object,
      default: null,
    },
    activity: {
      type: Object,
      default: null,
    },
    status: {
      type: String,
      default: "",
    },
    expandedPanel: {
      type: String,
      default: "",
    },
  },
  setup(props, { emit }) {
    const t = translate;
    const activePanel = computed(() => normalizePanel(props.expandedPanel));
    const planSteps = computed(() => (Array.isArray(props.plan?.steps) ? props.plan.steps : []));
    const currentActivityEntry = computed(() => activityCurrentEntry(props.activity));
    const activityHistory = computed(() => activityHistoryEntries(props.activity));

    function isExpanded(panel) {
      return activePanel.value === panel;
    }

    function toggle(panel) {
      emit("toggle", panel);
    }

    return {
      t,
      PANEL_PLAN,
      PANEL_ACTIVITY,
      activePanel,
      planSteps,
      currentActivityEntry,
      activityHistory,
      isExpanded,
      toggle,
      activityEntryClass,
      activityEntryNote,
      activityEntryTimeText,
      activityEntryTitle,
      activityKindLabel,
      activityParams,
      activityStateClass,
      activityBlockStatusLabel,
      planProgressText,
      planStepClass,
    };
  },
  template: `
    <section v-if="plan || activity" class="chat-status-card">
      <div class="chat-status-summary">
        <span
          v-if="plan"
          :class="['chat-status-column', { 'is-expanded': isExpanded(PANEL_PLAN) }]"
          role="button"
          tabindex="0"
          :aria-expanded="isExpanded(PANEL_PLAN)"
          @click="toggle(PANEL_PLAN)"
          @keydown.enter.prevent="toggle(PANEL_PLAN)"
          @keydown.space.prevent="toggle(PANEL_PLAN)"
        >
          <span class="chat-status-summary-label">{{ t("chat_plan_title") }}</span>
          <span class="chat-status-summary-value">({{ planProgressText(plan) }})</span>
          <QIconChevronDown class="chat-status-column-icon icon" aria-hidden="true" />
        </span>

        <span
          v-if="activity"
          :class="['chat-status-column', { 'is-expanded': isExpanded(PANEL_ACTIVITY) }]"
          role="button"
          tabindex="0"
          :aria-expanded="isExpanded(PANEL_ACTIVITY)"
          @click="toggle(PANEL_ACTIVITY)"
          @keydown.enter.prevent="toggle(PANEL_ACTIVITY)"
          @keydown.space.prevent="toggle(PANEL_ACTIVITY)"
        >
          <span class="chat-status-summary-label">{{ t("chat_activity_title") }}</span>
          <span :class="activityStateClass(activity, status)">
            {{ activityBlockStatusLabel(activity, status, t) }}
          </span>
          <QIconChevronDown class="chat-status-column-icon icon" aria-hidden="true" />
        </span>
      </div>

      <Transition name="chat-status-crack">
        <div
          v-if="(isExpanded(PANEL_PLAN) && plan) || (isExpanded(PANEL_ACTIVITY) && activity)"
          class="chat-status-details-shell"
        >
          <div class="chat-status-details">
            <section v-if="isExpanded(PANEL_PLAN) && plan" class="chat-status-detail">
              <ol class="chat-plan-list">
                <li
                  v-for="(step, stepIndex) in planSteps"
                  :key="itemId + ':plan:' + stepIndex"
                  :class="planStepClass(step)"
                >
                  <span class="chat-plan-step-dot" aria-hidden="true"></span>
                  <div class="chat-plan-step-copy">
                    <p class="chat-plan-step-text">{{ step.step }}</p>
                  </div>
                </li>
              </ol>
            </section>

            <section v-if="isExpanded(PANEL_ACTIVITY) && activity" class="chat-status-detail">
              <div
                v-if="currentActivityEntry"
                :class="activityEntryClass(currentActivityEntry)"
              >
                <span class="chat-activity-dot" aria-hidden="true"></span>
                <div class="chat-activity-copy">
                  <div class="chat-activity-line">
                    <span class="chat-activity-kind">{{ activityKindLabel(currentActivityEntry, t) }}</span>
                    <span class="chat-activity-name">{{ activityEntryTitle(currentActivityEntry) }}</span>
                    <time v-if="activityEntryTimeText(currentActivityEntry)" class="chat-activity-time">
                      {{ activityEntryTimeText(currentActivityEntry) }}
                    </time>
                  </div>
                  <div v-if="activityParams(currentActivityEntry).length > 0" class="chat-activity-params">
                    <span
                      v-for="(param, paramIndex) in activityParams(currentActivityEntry)"
                      :key="itemId + ':activity:param:' + paramIndex"
                      class="chat-activity-param"
                    >
                      <span class="chat-activity-param-key">{{ param.key }}</span>
                      <span class="chat-activity-param-value">{{ param.value }}</span>
                    </span>
                  </div>
                  <p v-if="activityEntryNote(currentActivityEntry)" class="chat-activity-note">
                    {{ activityEntryNote(currentActivityEntry) }}
                  </p>
                </div>
              </div>

              <ol v-if="activityHistory.length > 0" class="chat-activity-list">
                <li
                  v-for="(entry, historyIndex) in activityHistory"
                  :key="itemId + ':activity:history:' + historyIndex"
                  :class="activityEntryClass(entry)"
                >
                  <span class="chat-activity-dot" aria-hidden="true"></span>
                  <div class="chat-activity-copy">
                    <div class="chat-activity-line">
                      <span class="chat-activity-kind">{{ activityKindLabel(entry, t) }}</span>
                      <span class="chat-activity-name">{{ activityEntryTitle(entry) }}</span>
                      <time v-if="activityEntryTimeText(entry)" class="chat-activity-time">
                        {{ activityEntryTimeText(entry) }}
                      </time>
                    </div>
                    <div v-if="activityParams(entry).length > 0" class="chat-activity-params">
                      <span
                        v-for="(param, paramIndex) in activityParams(entry)"
                        :key="itemId + ':activity:history:param:' + historyIndex + ':' + paramIndex"
                        class="chat-activity-param"
                      >
                        <span class="chat-activity-param-key">{{ param.key }}</span>
                        <span class="chat-activity-param-value">{{ param.value }}</span>
                      </span>
                    </div>
                    <p v-if="activityEntryNote(entry)" class="chat-activity-note">{{ activityEntryNote(entry) }}</p>
                  </div>
                </li>
              </ol>
            </section>
          </div>
        </div>
      </Transition>
    </section>
  `,
};

export default ChatStatusCard;
