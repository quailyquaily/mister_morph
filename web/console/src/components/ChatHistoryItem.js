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
  emits: ["copy", "rendered", "time-click", "toggle-status"],
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
  },
  setup(props, { emit }) {
    let updateStartedAt = 0;
    const role = computed(() => roleOf(props.item));
    const itemClass = computed(() => {
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
    const copyAvailable = computed(
      () => (role.value === "agent" || role.value === "user") && String(props.item?.text || "").trim() !== ""
    );
    const copyButtonClass = computed(() =>
      props.copied ? "chat-history-copy-action is-copied" : "chat-history-copy-action"
    );
    const streaming = computed(
      () =>
        role.value === "agent" &&
        String(props.item?.taskId || "").trim() !== "" &&
        !isTerminalStatus(normalizeTaskStatus(props.item?.status))
    );

    function emitCopy() {
      emit("copy", props.item);
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
      copyAvailable,
      copyButtonClass,
      emitCopy,
      emitRendered,
      emitTimeClick,
      emitToggle,
      itemClass,
      role,
      streaming,
      surfaceClass,
    };
  },
  template: `
    <article
      :class="itemClass"
      v-memo="[item, copied, expandedPanel, autoPreview, streamProfiler, submitEndpointRef, selectedTopicId]"
    >
      <code
        v-if="item.timeText"
        class="chat-history-status"
        @click="emitTimeClick"
      >
        {{ item.timeText }}
      </code>
      <template v-if="role === 'agent'">
        <div class="chat-history-stack">
          <ChatStatusCard
            v-if="item.plan || item.activity"
            :item-id="item.id"
            :plan="item.plan"
            :activity="item.activity"
            :status="item.status"
            :expanded-panel="expandedPanel"
            @toggle="emitToggle"
          />
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
