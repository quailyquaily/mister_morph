import { computed, nextTick, onBeforeUpdate, onMounted, onUnmounted, onUpdated, ref, watch } from "vue";

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

function estimateSkeletonHeight(text) {
  const value = String(text || "");
  const explicitLines = value.split(/\r?\n/).length;
  const estimatedWrappedLines = Math.ceil(value.length / 88);
  const lines = Math.max(3, Math.min(18, explicitLines + estimatedWrappedLines));
  return `${lines * 22 + 20}px`;
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
    let visibilityObserver = null;
    const rootEl = ref(null);
    const role = computed(() => roleOf(props.item));
    const renderReady = ref(role.value !== "agent");
    const richContentAllowed = ref(role.value !== "agent");
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
    const skeletonVisible = computed(
      () => role.value === "agent" && agentBubbleVisible.value && (!richContentAllowed.value || !renderReady.value)
    );
    const skeletonStyle = computed(() => ({
      minHeight: estimateSkeletonHeight(props.item?.text),
    }));
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
    const shouldMountRichContent = computed(() => role.value !== "agent" || richContentAllowed.value || streaming.value);

    function stopVisibilityObserver() {
      if (!visibilityObserver) {
        return;
      }
      visibilityObserver.disconnect();
      visibilityObserver = null;
    }

    function allowRichContent() {
      richContentAllowed.value = true;
      stopVisibilityObserver();
    }

    function startVisibilityObserver() {
      stopVisibilityObserver();
      if (role.value !== "agent" || richContentAllowed.value || streaming.value || !agentBubbleVisible.value) {
        return;
      }
      if (typeof window === "undefined" || typeof window.IntersectionObserver !== "function") {
        allowRichContent();
        return;
      }
      const node = rootEl.value;
      if (!node) {
        return;
      }
      visibilityObserver = new window.IntersectionObserver(
        (entries) => {
          if (entries.some((entry) => entry.isIntersecting || entry.intersectionRatio > 0)) {
            allowRichContent();
          }
        },
        { root: null, rootMargin: "720px 0px", threshold: 0 }
      );
      visibilityObserver.observe(node);
    }

    function resetRenderState() {
      stopVisibilityObserver();
      renderReady.value = role.value !== "agent";
      richContentAllowed.value = role.value !== "agent" || streaming.value;
      if (richContentAllowed.value) {
        return;
      }
      nextTick(startVisibilityObserver);
    }

    function syncVisibilityState() {
      if (role.value !== "agent") {
        richContentAllowed.value = true;
        stopVisibilityObserver();
        return;
      }
      if (streaming.value) {
        allowRichContent();
        return;
      }
      if (!richContentAllowed.value) {
        nextTick(startVisibilityObserver);
      }
    }

    function emitCopy() {
      emit("copy", props.item);
    }

    function emitRendered() {
      if (role.value === "agent" && renderReady.value !== true) {
        renderReady.value = true;
      }
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

    onMounted(() => {
      syncVisibilityState();
    });

    onUnmounted(() => {
      stopVisibilityObserver();
    });

    watch(
      () => [props.item?.id, role.value],
      () => {
        resetRenderState();
      }
    );

    watch(
      () => [streaming.value, agentBubbleVisible.value],
      () => {
        syncVisibilityState();
      },
      { flush: "post" }
    );

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
      rootEl,
      shouldMountRichContent,
      skeletonStyle,
      skeletonVisible,
      streaming,
      surfaceClass,
    };
  },
  template: `
    <article
      ref="rootEl"
      :class="itemClass"
      v-memo="[item, copied, expandedPanel, autoPreview, streamProfiler, submitEndpointRef, selectedTopicId, skeletonVisible, shouldMountRichContent]"
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
            <div v-if="skeletonVisible" class="chat-history-skeleton" :style="skeletonStyle" aria-hidden="true">
              <QSkeleton variant="text" width="92%" />
              <QSkeleton variant="text" width="100%" />
              <QSkeleton variant="text" width="68%" />
            </div>
            <ChatRichContent
              v-if="shouldMountRichContent"
              :class="skeletonVisible ? 'chat-history-markdown is-render-pending' : 'chat-history-markdown'"
              :source="item.text"
              :endpoint-ref="submitEndpointRef"
              :fallback-topic-id="selectedTopicId"
              :auto-preview="autoPreview"
              :streaming="streaming"
              stream-mode="balanced"
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
