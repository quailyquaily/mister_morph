import { computed, onBeforeUpdate, onUpdated } from "vue";

import { approvalParameterEntries } from "../core/chat-approvals";
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
    const reasoningVisible = computed(() => String(props.item?.reasoning || "").trim() !== "");
    const userFiles = computed(() =>
      role.value === "user" && Array.isArray(props.item?.files) ? props.item.files : []
    );
    const copyButtonClass = computed(() =>
      props.copied ? "chat-history-copy-action is-copied" : "chat-history-copy-action"
    );
    const approvalStatus = computed(() => {
      const value = String(props.item?.approval?.status || "").trim().toLowerCase();
      return value === "denied" || value === "expired" ? value : "pending";
    });
    const approvalVisible = computed(
      () =>
        role.value === "agent" &&
        String(props.item?.approval?.approvalRequestID || "").trim() !== ""
    );
    const approvalPending = computed(() => approvalStatus.value === "pending");
    const approvalMessage = computed(() =>
      String(props.item?.approval?.message || props.item?.text || "").trim()
    );
    const approvalToolName = computed(() => String(props.item?.approval?.toolName || "").trim());
    const approvalReasons = computed(() =>
      Array.isArray(props.item?.approval?.reasons) ? props.item.approval.reasons : []
    );
    const approvalParams = computed(() => approvalParameterEntries(props.item?.approval?.toolParams));
    const approvalMessageVisible = computed(
      () =>
        approvalPending.value &&
        approvalMessage.value !== "" &&
        approvalReasons.value.length === 0 &&
        approvalParams.value.length === 0
    );
    const approvalHeading = computed(() =>
      approvalPending.value
        ? props.approvalTitle
        : String(props.item?.text || approvalMessage.value).trim()
    );
    const approvalPanelClass = computed(() => `chat-approval-panel is-${approvalStatus.value}`);
    const agentBubbleVisible = computed(
      () => !approvalVisible.value && String(props.item?.text || "") !== ""
    );
    const copyAvailable = computed(
      () =>
        !contextCompactNotice.value &&
        !approvalVisible.value &&
        (role.value === "agent" || role.value === "user") &&
        String(props.item?.text || "").trim() !== ""
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
      approvalMessage,
      approvalMessageVisible,
      approvalHeading,
      approvalPanelClass,
      approvalParams,
      approvalPending,
      approvalReasons,
      approvalToolName,
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
            class="chat-history-reasoning"
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
          <div v-if="approvalVisible" :class="surfaceClass" class="chat-history-approval-surface">
            <section :class="approvalPanelClass" role="region" :aria-label="approvalHeading">
              <header class="chat-approval-head">
                <PhShieldCheck class="chat-approval-icon" aria-hidden="true" />
                <div class="chat-approval-title-line">
                  <span class="chat-approval-title">{{ approvalHeading }}</span>
                  <code v-if="approvalToolName" class="chat-approval-tool">{{ approvalToolName }}</code>
                </div>
              </header>
              <p v-if="approvalMessageVisible" class="chat-approval-message">{{ approvalMessage }}</p>
              <ul v-if="approvalReasons.length" class="chat-approval-reasons">
                <li v-for="(reason, index) in approvalReasons" :key="reason + ':' + index">{{ reason }}</li>
              </ul>
              <dl v-if="approvalParams.length" class="chat-approval-params">
                <div
                  v-for="param in approvalParams"
                  :key="param.name"
                  :class="param.command ? 'chat-approval-param is-command' : 'chat-approval-param'"
                >
                  <dt><code>{{ param.name }}</code></dt>
                  <dd>
                    <pre v-if="param.command || param.multiline"><code>{{ param.value }}</code></pre>
                    <code v-else>{{ param.value }}</code>
                  </dd>
                </div>
              </dl>
              <div v-if="item.approvalError" class="chat-approval-error" role="alert">
                <PhInfo class="icon" aria-hidden="true" />
                <span>{{ item.approvalError }}</span>
              </div>
              <div v-if="approvalPending" class="chat-approval-actions">
                <QButton
                  class="plain xs"
                  :disabled="item.approvalBusy"
                  @click.stop="emitApprovalDeny"
                >
                  {{ approvalDenyLabel }}
                </QButton>
                <QButton
                  class="primary xs"
                  :disabled="item.approvalBusy"
                  @click.stop="emitApprovalApprove"
                >
                  {{ approvalApproveLabel }}
                </QButton>
              </div>
            </section>
          </div>
          <button
            v-if="copyAvailable"
            type="button"
            :class="copyButtonClass"
            :title="copyLabel"
            :aria-label="copyLabel"
            @click.stop="emitCopy"
          >
            <PhCopy class="icon" />
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
            <PhPaperclip class="chat-history-file-icon" />
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
          <PhCopy class="icon" />
        </button>
      </template>
    </article>
  `,
};

export default ChatHistoryItem;
