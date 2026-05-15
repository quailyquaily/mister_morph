import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./TasksView.css";

import AppPage from "../components/AppPage";
import RawJsonDialog from "../components/RawJsonDialog";
import { openRawJsonDesktopWindow } from "../core/desktop-window-payload";
import { endpointChannelLabel } from "../core/endpoints";
import {
  TASK_STATUS_META,
  endpointState,
  formatTime,
  runtimeApiFetchFirstForEndpoints,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  taskEndpointRefsForSelection,
  translate,
} from "../core/context";

function taskCreatedAt(task) {
  const value = Date.parse(String(task?.created_at || "").trim());
  return Number.isFinite(value) ? value : 0;
}

function taskTextPreview(task) {
  const text = String(task?.task || "").replace(/\s+/g, " ").trim();
  if (!text) {
    return "";
  }
  if (text.length <= 180) {
    return text;
  }
  return `${text.slice(0, 177)}...`;
}

function normalizeTaskStatus(raw) {
  return String(raw || "").trim().toLowerCase();
}

function shortenTaskID(raw) {
  const value = String(raw || "").trim();
  if (!value) {
    return "-";
  }
  if (value.length <= 18) {
    return value;
  }
  return `${value.slice(0, 8)}...${value.slice(-6)}`;
}

const TASKS_PAGE_SIZE = 20;

const TasksView = {
  components: {
    AppPage,
    RawJsonDialog,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const selectedEndpoint = computed(() => runtimeEndpointByRef(endpointState.selectedRef));
    const taskFeedEndpointRef = computed(() => {
      const selected = selectedEndpoint.value;
      if (!selected) {
        return "";
      }
      const mapped = String(selected.submit_endpoint_ref || "").trim();
      if (mapped) {
        return mapped;
      }
      return String(selected.endpoint_ref || "").trim();
    });
    const pageIndex = ref(0);
    const pageCursors = ref([""]);
    const nextCursor = ref("");
    const items = ref([]);
    const err = ref("");
    const loading = ref(false);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const emptyTitle = computed(() => t("tasks_empty_title"));
    const emptyHint = computed(() => t("tasks_empty_hint"));
    const tasksPageText = computed(() => `${pageIndex.value + 1}`);
    const taskStatusTitleMap = computed(() => {
      const map = new Map();
      for (const item of TASK_STATUS_META) {
        map.set(item.value, t(item.titleKey));
      }
      return map;
    });

    function resetPagination() {
      pageIndex.value = 0;
      pageCursors.value = [""];
      nextCursor.value = "";
    }

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const endpointRef = String(taskFeedEndpointRef.value || "").trim();
        if (!endpointRef) {
          items.value = [];
          nextCursor.value = "";
          return;
        }
        const q = new URLSearchParams();
        q.set("limit", String(TASKS_PAGE_SIZE));
        const currentCursor = String(pageCursors.value[pageIndex.value] || "").trim();
        if (currentCursor) {
          q.set("cursor", currentCursor);
        }
        const endpoint = runtimeEndpointByRef(endpointRef);
        const data = await runtimeApiFetchForEndpoint(endpointRef, `/tasks?${q.toString()}`);
        const rows = Array.isArray(data?.items) ? data.items : [];
        const sourceLabel = endpointChannelLabel(endpoint?.mode, t);
        items.value = rows.map((item) => ({
          ...item,
          source_label: sourceLabel,
          source_mode: endpoint?.mode || "",
          source_name: String(endpoint?.name || "").trim(),
          source_endpoint_ref: endpointRef,
          task_preview: taskTextPreview(item),
        }));
        nextCursor.value = String(data?.next_cursor || "").trim();
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function prevPage() {
      if (pageIndex.value <= 0) {
        return;
      }
      pageIndex.value -= 1;
      void load();
    }

    function nextPage() {
      const cursor = String(nextCursor.value || "").trim();
      if (!cursor) {
        return;
      }
      const nextPageIndex = pageIndex.value + 1;
      const nextHistory = pageCursors.value.slice(0, nextPageIndex);
      nextHistory[nextPageIndex] = cursor;
      pageCursors.value = nextHistory;
      pageIndex.value = nextPageIndex;
      void load();
    }

    function taskStatusLabel(task) {
      const value = normalizeTaskStatus(task?.status);
      return taskStatusTitleMap.value.get(value) || String(task?.status || "").trim() || "-";
    }

    function taskStatusType(task) {
      switch (normalizeTaskStatus(task?.status)) {
        case "done":
          return "success";
        case "failed":
          return "danger";
        case "running":
          return "primary";
        case "pending":
          return "warning";
        case "queued":
          return "default";
        case "canceled":
          return "default";
        default:
          return "default";
      }
    }

    function taskSourceLabel(task) {
      const current = String(task?.source_label || "").trim();
      if (current) {
        return current;
      }
      const mode = String(task?.source_mode || "").trim();
      if (mode) {
        return endpointChannelLabel(mode, t);
      }
      return t("tasks_runtime_fallback");
    }

    function taskSourceType(task) {
      switch (String(task?.source_mode || "").trim().toLowerCase()) {
        case "console":
          return "primary";
        case "telegram":
          return "info";
        case "slack":
          return "danger";
        case "line":
          return "success";
        case "lark":
          return "warning";
        case "serve":
        default:
          return "default";
      }
    }

    function taskRuntimeMeta(task) {
      const name = String(task?.source_name || "").trim();
      if (name) {
        return name;
      }
      const ref = String(task?.source_endpoint_ref || "").trim();
      if (ref) {
        return ref;
      }
      return taskSourceLabel(task);
    }

    function taskModelMeta(task) {
      const model = String(task?.model || "").trim();
      return model || "default";
    }

    function taskTitle(task) {
      return taskTextPreview(task) || String(task?.task || "").trim() || shortenTaskID(task?.id);
    }

    async function openTask(item) {
      const id = String(item?.id || "").trim();
      if (!id) {
        return;
      }
      err.value = "";
      try {
        let data;
        const endpointRef = String(item?.source_endpoint_ref || "").trim();
        if (endpointRef) {
          data = await runtimeApiFetchForEndpoint(endpointRef, `/tasks/${encodeURIComponent(id)}`);
        } else {
          data = await runtimeApiFetchFirstForEndpoints(
            taskEndpointRefsForSelection(),
            `/tasks/${encodeURIComponent(id)}`
          );
        }
        const json = JSON.stringify(data, null, 2);
        if (await openRawJsonDesktopWindow({ title: "RAW JSON", json }).catch(() => false)) {
          return;
        }
        rawDialogJSON.value = json;
        rawDialogOpen.value = rawDialogJSON.value !== "";
      } catch (e) {
        rawDialogJSON.value = "";
        rawDialogOpen.value = false;
        err.value = e.message || t("msg_load_failed");
      }
    }

    function closeRawDialog() {
      rawDialogOpen.value = false;
    }

    function goChat() {
      router.push("/chat");
    }

    onMounted(load);
    watch(
      () => [taskFeedEndpointRef.value, endpointState.items.length],
      () => {
        resetPagination();
        void load();
      }
    );
    return {
      t,
      pageIndex,
      items,
      err,
      loading,
      load,
      prevPage,
      nextPage,
      openTask,
      goChat,
      taskStatusLabel,
      taskStatusType,
      taskSourceLabel,
      taskSourceType,
      taskRuntimeMeta,
      taskModelMeta,
      taskTitle,
      shortenTaskID,
      tasksPageText,
      formatTime,
      emptyTitle,
      emptyHint,
      hasPrevPage: computed(() => pageIndex.value > 0),
      hasNextPage: computed(() => String(nextCursor.value || "").trim() !== ""),
      rawDialogOpen,
      rawDialogJSON,
      closeRawDialog,
    };
  },
  template: `
    <AppPage :title="t('tasks_title')">
      <section class="tasks-controls">
        <div class="toolbar wrap tasks-toolbar">
          <QButton
            class="plain sm icon"
            :loading="loading"
            :title="t('action_refresh')"
            :aria-label="t('action_refresh')"
            @click="load"
          >
            <QIconRefresh class="icon" />
          </QButton>
          <div class="tasks-limit-control">
            <QButton
              class="plain sm icon"
              :disabled="!hasPrevPage"
              :title="t('audit_newer')"
              :aria-label="t('audit_newer')"
              @click="prevPage"
            >
              <QIconArrowLeft class="icon" />
            </QButton>
            <div class="tasks-limit-indicator">{{ tasksPageText }}</div>
            <QButton
              class="plain sm icon"
              :disabled="!hasNextPage"
              :title="t('audit_older')"
              :aria-label="t('audit_older')"
              @click="nextPage"
            >
              <QIconArrowRight class="icon" />
            </QButton>
          </div>
        </div>
      </section>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="stack tasks-stack">
        <QCard
          v-for="item in items"
          :key="item.id"
          class="task-row clickable"
          variant="default"
          :hoverable="true"
          tabindex="0"
          role="button"
          :aria-label="t('chat_action_show_raw')"
          @click="openTask(item)"
          @keydown.enter.prevent="openTask(item)"
          @keydown.space.prevent="openTask(item)"
        >
          <div class="task-row-head">
            <div class="task-copy">
              <h3 class="task-title">{{ taskTitle(item) }}</h3>
              <div class="task-badges">
                <QBadge :type="taskStatusType(item)" size="sm">{{ taskStatusLabel(item) }}</QBadge>
                <QBadge :type="taskSourceType(item)" size="sm">{{ taskSourceLabel(item) }}</QBadge>
              </div>
            </div>
            <div class="task-row-side">
              <time class="task-time">{{ formatTime(item.created_at) }}</time>
              <span class="task-row-arrow" aria-hidden="true">
                <QIconArrowRight class="icon" />
              </span>
            </div>
          </div>
          <div class="task-meta-grid">
            <div class="task-meta-item">
              <span class="task-meta-label">{{ t("stats_model") }}</span>
              <span class="task-meta-value">{{ taskModelMeta(item) }}</span>
            </div>
            <div class="task-meta-item">
              <span class="task-meta-label">{{ t("tasks_runtime_label") }}</span>
              <span class="task-meta-value">{{ taskRuntimeMeta(item) }}</span>
            </div>
            <div class="task-meta-item task-meta-item-code">
              <span class="task-meta-label">{{ t("tasks_task_id_label") }}</span>
              <code class="task-meta-value task-meta-code" :title="item.id">{{ shortenTaskID(item.id) }}</code>
            </div>
          </div>
        </QCard>
        <QCard v-if="items.length === 0 && !loading" class="task-empty" variant="default">
          <div class="task-empty-copy">
            <code class="task-empty-kicker">{{ t("tasks_title") }}</code>
            <h3 class="task-empty-title">{{ emptyTitle }}</h3>
            <p class="task-empty-hint">{{ emptyHint }}</p>
          </div>
          <template #footer>
            <QButton class="plain sm" @click="goChat">{{ t("tasks_empty_action") }}</QButton>
          </template>
        </QCard>
        <RawJsonDialog
          :open="rawDialogOpen"
          :json="rawDialogJSON"
          @close="closeRawDialog"
        />
      </div>
    </AppPage>
  `,
};


export default TasksView;
