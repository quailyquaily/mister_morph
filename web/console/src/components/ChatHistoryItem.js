import { computed, onBeforeUpdate, onUpdated } from "vue";

import { recordComponentUpdate } from "../core/performance";
import ChatRichContent from "./ChatRichContent";
import ChatStatusCard from "./ChatStatusCard";

function roleOf(item) {
  return String(item?.role || "").trim().toLowerCase();
}

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

const RECORD_COMPONENT_PERF = import.meta.env.DEV === true;

const ChatHistoryItem = {
  components: {
    ChatRichContent,
    ChatStatusCard,
  },
  emits: ["approval-approve", "approval-deny", "copy", "preview-file", "rendered", "time-click", "toggle-status"],
  props: {
    item: {
      type: Object,
      required: true,
    },
    submitEndpointRef: {
      type: String,
      default: "",
    },
    selectedTopicId: {
      type: String,
      default: "",
    },
    copied: {
      type: Boolean,
      default: false,
    },
    expandedPanel: {
      type: String,
      default: "",
    },
    autoPreview: {
      type: Boolean,
      default: false,
    },
    streamProfiler: {
      type: Boolean,
      default: false,
    },
    copyLabel: {
      type: String,
      default: "Copy",
    },
    filePreviewLabel: {
      type: String,
      default: "Preview file",
    },
    approvalApproveLabel: {
      type: String,
      default: "Approve",
    },
    approvalDenyLabel: {
      type: String,
      default: "Deny",
    },
    approvalTitle: {
      type: String,
      default: "Approval required",
    },
  },
  setup(props, { emit }) {
    let updateStartedAt = 0;
    const role = computed(() => roleOf(props.item));
    const contextCompactNotice = computed(
      () => String(props.item?.presentation || "").trim().toLowerCase() === "context-compact"
    );
    const contextCompactNoticeClass = computed(
      () => `chat-history-context-notice is-${normalizeTaskStatus(props.item?.status)}`
    );
    const itemClass = computed(() => {
      if (contextCompactNotice.value) {
        return "chat-history-item chat-history-context-compact";
      }
      if (role.value === "user") {
        return "chat-history-item chat-history-user";
      }
      if (role.value === "agent") {
        return "chat-history-item chat-history-agent";
      }
      return "chat-history-item chat-history-system";
    });
    const surfaceClass = computed(() => (role.value === "agent" ? "chat-history-copy" : "chat-history-bubble"));
    const agentBubbleVisible = computed(() => String(props.item?.text || "") !== "");
    const reasoningVisible = computed(() => String(props.item?.reasoning || "").trim() !== "");
    const userFiles = computed(() =>
      role.value === "user" && Array.isArray(props.item?.files) ? props.item.files : []
    );
    const copyAvailable = computed(
      () =>
        !contextCompactNotice.value &&
        (role.value === "agent" || role.value === "user") &&
        String(props.item?.text || "").trim() !== ""
    );
    const copyButtonClass = computed(() =>
      props.copied ? "chat-history-copy-action is-copied" : "chat-history-copy-action"
    );
    const approvalVisible = computed(
      () =>
        role.value === "agent" &&
        String(props.item?.approval?.approvalRequestID || "").trim() !== "" &&
        normalizeTaskStatus(props.item?.status) === "pending"
    );
    const streaming = computed(
      () =>
        role.value === "agent" &&
        String(props.item?.taskId || "").trim() !== "" &&
        !isTerminalStatus(normalizeTaskStatus(props.item?.status))
    );
    const statusText = computed(() => {
      const durationText = String(props.item?.durationText || "").trim();
      if (props.item?.durationVisible === true && durationText) {
        return durationText;
      }
      return String(props.item?.timeText || "").trim();
    });
    const statusInteractive = computed(
      () => role.value === "agent" && String(props.item?.durationText || props.item?.rawJSON || "").trim() !== ""
    );

    function emitCopy() {
      emit("copy", props.item);
    }

    function emitPreviewFile(file) {
      const endpointRef = String(props.item?.endpointRef || "").trim();
      const topicID = String(props.item?.topicID || "").trim();
      const previewItems = userFiles.value.map((item) => ({
        ...item,
        endpointRef,
        topicID,
        status: "ready",
      }));
      const selectedID = String(file?.id || "").trim();
      const previewIndex = Math.max(
        0,
        previewItems.findIndex((item) => String(item?.id || "").trim() === selectedID)
      );
      emit("preview-file", {
        ...previewItems[previewIndex],
        previewItems,
      });
    }

    function emitRendered() {
      emit("rendered", props.item?.id || "");
    }

    function emitTimeClick() {
      emit("time-click", props.item);
    }

    function emitToggle(panel) {
      emit("toggle-status", props.item?.id || "", panel);
    }

    function emitApprovalApprove() {
      emit("approval-approve", props.item);
    }

    function emitApprovalDeny() {
      emit("approval-deny", props.item);
    }

    onBeforeUpdate(() => {
      if (RECORD_COMPONENT_PERF) {
        updateStartedAt = performance.now();
      }
    });

    onUpdated(() => {
      if (RECORD_COMPONENT_PERF) {
        recordComponentUpdate(
          "chat.history_item",
          updateStartedAt ? performance.now() - updateStartedAt : 0
        );
        updateStartedAt = 0;
      }
    });

    return {
      agentBubbleVisible,
      approvalVisible,
      copyAvailable,
      copyButtonClass,
      contextCompactNotice,
      contextCompactNoticeClass,
      emitApprovalApprove,
      emitApprovalDeny,
      emitCopy,
      emitPreviewFile,
      emitRendered,
      emitTimeClick,
      emitToggle,
      itemClass,
      role,
      reasoningVisible,
      statusInteractive,
      statusText,
      streaming,
      surfaceClass,
      userFiles,
    };
  },
  template: `
    <article
      :class="itemClass"
      v-memo="[item, copied, expandedPanel, autoPreview, streamProfiler, submitEndpointRef, selectedTopicId, approvalApproveLabel, approvalDenyLabel, approvalTitle]"
    >
      <span
        v-if="statusText && role !== 'agent'"
        :class="statusInteractive ? 'chat-history-status is-clickable' : 'chat-history-status'"
        :role="statusInteractive ? 'button' : null"
        :tabindex="statusInteractive ? 0 : null"
        @click="emitTimeClick"
        @keydown.enter.prevent="emitTimeClick"
        @keydown.space.prevent="emitTimeClick"
      >
        {{ statusText }}
      </span>
      <template v-if="contextCompactNotice">
        <div :class="contextCompactNoticeClass" role="status" aria-atomic="true">
          <span class="chat-history-context-notice-text">{{ item.text }}</span>
        </div>
      </template>
      <template v-else-if="role === 'agent'">
        <div class="chat-history-stack">
          <ChatStatusCard
            v-if="item.plan || item.activity || reasoningVisible"
            :item-id="item.id"
            :plan="item.plan"
            :activity="item.activity"
            :has-reasoning="reasoningVisible"
            :status="item.status"
            :expanded-panel="expandedPanel"
            @toggle="emitToggle"
          >
            <template #summary-prefix>
              <span
                v-if="statusText"
                :class="statusInteractive ? 'chat-history-status is-clickable' : 'chat-history-status'"
                :role="statusInteractive ? 'button' : null"
                :tabindex="statusInteractive ? 0 : null"
                @click="emitTimeClick"
                @keydown.enter.prevent="emitTimeClick"
                @keydown.space.prevent="emitTimeClick"
              >
                {{ statusText }}
              </span>
            </template>
          </ChatStatusCard>
          <span
            v-else-if="statusText"
            :class="statusInteractive ? 'chat-history-status is-clickable' : 'chat-history-status'"
            :role="statusInteractive ? 'button' : null"
            :tabindex="statusInteractive ? 0 : null"
            @click="emitTimeClick"
            @keydown.enter.prevent="emitTimeClick"
            @keydown.space.prevent="emitTimeClick"
          >
            {{ statusText }}
          </span>
          <div
            v-if="reasoningVisible && expandedPanel === 'reasoning'"
            :class="surfaceClass"
          >
            <ChatRichContent
              class="chat-history-markdown"
              :source="item.reasoning"
              :endpoint-ref="submitEndpointRef"
              :fallback-topic-id="selectedTopicId"
              :auto-preview="autoPreview"
              :streaming="streaming"
              stream-mode="typewriter"
              :stream-profiler="streamProfiler"
              format="auto"
              theme="blueprint"
              @rendered="emitRendered"
            />
          </div>
          <div v-if="agentBubbleVisible" :class="surfaceClass">
            <ChatRichContent
              class="chat-history-markdown"
              :source="item.text"
              :endpoint-ref="submitEndpointRef"
              :fallback-topic-id="selectedTopicId"
              :auto-preview="autoPreview"
              :streaming="streaming"
              stream-mode="typewriter"
              :stream-profiler="streamProfiler"
              format="auto"
              theme="blueprint"
              @rendered="emitRendered"
            />
          </div>
          <div v-if="approvalVisible" class="chat-approval-panel">
            <div class="chat-approval-title">{{ approvalTitle }}</div>
            <div v-if="item.approvalError" class="chat-approval-error">{{ item.approvalError }}</div>
            <div class="chat-approval-actions">
              <QButton
                class="primary xs"
                :disabled="item.approvalBusy"
                @click.stop="emitApprovalApprove"
              >
                {{ approvalApproveLabel }}
              </QButton>
              <QButton
                class="plain xs"
                :disabled="item.approvalBusy"
                @click.stop="emitApprovalDeny"
              >
                {{ approvalDenyLabel }}
              </QButton>
            </div>
          </div>
          <button
            v-if="copyAvailable"
            type="button"
            :class="copyButtonClass"
            :title="copyLabel"
            :aria-label="copyLabel"
            @click.stop="emitCopy"
          >
            <QIconCopy class="icon" />
          </button>
        </div>
      </template>
      <template v-else>
        <div v-if="userFiles.length" class="chat-history-files">
          <button
            v-for="file in userFiles"
            :key="file.id"
            type="button"
            class="chat-history-file"
            :title="filePreviewLabel + ': ' + file.name"
            :aria-label="filePreviewLabel + ': ' + file.name"
            @click="emitPreviewFile(file)"
          >
            <QIconPaperclip class="chat-history-file-icon" />
            <span class="chat-history-file-name">{{ file.name }}</span>
          </button>
        </div>
        <div :class="surfaceClass">
          <div class="chat-history-body">{{ item.text }}</div>
        </div>
        <button
          v-if="copyAvailable"
          type="button"
          :class="copyButtonClass"
          :title="copyLabel"
          :aria-label="copyLabel"
          @click.stop="emitCopy"
        >
          <QIconCopy class="icon" />
        </button>
      </template>
    </article>
  `,
};

export default ChatHistoryItem;
