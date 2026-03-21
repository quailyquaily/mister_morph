import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./TasksView.css";

import AppPage from "../components/AppPage";
import RawJsonDialog from "../components/RawJsonDialog";
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

const TasksView = {
  components: {
    AppPage,
    RawJsonDialog,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const taskStatusItems = computed(() =>
      TASK_STATUS_META.map((item) => ({
        title: t(item.titleKey),
        value: item.value,
      }))
    );
    const statusValue = ref(TASK_STATUS_META[0].value);
    const statusItem = computed(() => {
      return taskStatusItems.value.find((item) => item.value === statusValue.value) || taskStatusItems.value[0] || null;
    });
    const hasStatusFilter = computed(() => statusValue.value !== "");
    const normalizedLimit = computed(() => Math.max(1, Math.min(200, parseInt(limitText.value || "20", 10) || 20)));
    const limitText = ref("20");
    const items = ref([]);
    const err = ref("");
    const loading = ref(false);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const emptyTitle = computed(() => (hasStatusFilter.value ? t("tasks_empty_filtered_title") : t("tasks_empty_title")));
    const emptyHint = computed(() => (hasStatusFilter.value ? t("tasks_empty_filtered_hint") : t("tasks_empty_hint")));
    const activeEndpointScope = computed(() => {
      const refs = taskEndpointRefsForSelection();
      const names = refs
        .map((endpointRef) => runtimeEndpointByRef(endpointRef))
        .filter(Boolean)
        .map((endpoint) => String(endpoint.name || "").trim() || endpointChannelLabel(endpoint.mode, t))
        .filter(Boolean);
      return names.length > 0 ? names.join(" + ") : t("tasks_runtime_fallback");
    });
    const tasksShowingText = computed(() => t("tasks_showing", { count: items.value.length }));
    const tasksScopeText = computed(() => t("tasks_scope", { value: activeEndpointScope.value }));
    const tasksLimitText = computed(() => t("tasks_limit_label", { count: normalizedLimit.value }));
    const taskStatusTitleMap = computed(() => {
      const map = new Map();
      for (const item of TASK_STATUS_META) {
        map.set(item.value, t(item.titleKey));
      }
      return map;
    });

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const q = new URLSearchParams();
        const v = statusValue.value || "";
        if (v) {
          q.set("status", v);
        }
        q.set("limit", String(normalizedLimit.value));
        const endpointRefs = taskEndpointRefsForSelection();
        const settled = await Promise.allSettled(
          endpointRefs.map(async (endpointRef) => {
            const endpoint = runtimeEndpointByRef(endpointRef);
            const data = await runtimeApiFetchForEndpoint(endpointRef, `/tasks?${q.toString()}`);
            return {
              endpointRef,
              endpoint,
              items: Array.isArray(data?.items) ? data.items : [],
            };
          })
        );
        const failures = settled.filter((entry) => entry.status === "rejected");
        const successes = settled.filter((entry) => entry.status === "fulfilled");
        if (successes.length === 0) {
          throw failures[0]?.reason || new Error(t("msg_load_failed"));
        }
        const merged = new Map();
        for (const entry of successes) {
          const { endpoint, endpointRef, items: rows } = entry.value;
          const sourceLabel = endpointChannelLabel(endpoint?.mode, t);
          for (const item of rows) {
            const id = String(item?.id || "").trim();
            if (!id) {
              continue;
            }
            const nextItem = {
              ...item,
              source_label: sourceLabel,
              source_mode: endpoint?.mode || "",
              source_name: String(endpoint?.name || "").trim(),
              source_endpoint_ref: endpointRef,
              task_preview: taskTextPreview(item),
            };
            const current = merged.get(id);
            if (!current || taskCreatedAt(nextItem) >= taskCreatedAt(current)) {
              merged.set(id, nextItem);
            }
          }
        }
        items.value = Array.from(merged.values()).sort((left, right) => taskCreatedAt(right) - taskCreatedAt(left));
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function onStatusChange(item) {
      if (item && typeof item === "object") {
        statusValue.value = typeof item.value === "string" ? item.value : "";
      }
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

    function taskStatusTypeForValue(value) {
      return taskStatusType({ status: value });
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
        rawDialogJSON.value = JSON.stringify(data, null, 2);
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
      () => [endpointState.selectedRef, endpointState.items.length],
      () => {
        void load();
      }
    );
    return {
      t,
      taskStatusItems,
      statusItem,
      hasStatusFilter,
      limitText,
      normalizedLimit,
      items,
      err,
      loading,
      load,
      onStatusChange,
      openTask,
      goChat,
      taskStatusLabel,
      taskStatusType,
      taskStatusTypeForValue,
      taskSourceLabel,
      taskSourceType,
      taskRuntimeMeta,
      taskModelMeta,
      taskTitle,
      shortenTaskID,
      activeEndpointScope,
      tasksShowingText,
      tasksScopeText,
      tasksLimitText,
      formatTime,
      emptyTitle,
      emptyHint,
      rawDialogOpen,
      rawDialogJSON,
      closeRawDialog,
    };
  },
  template: `
    <AppPage :title="t('tasks_title')">
      <section class="tasks-controls">
        <div class="toolbar wrap tasks-toolbar">
          <div class="tool-item">
            <QDropdownMenu
              :items="taskStatusItems"
              :initialItem="statusItem"
              :placeholder="t('placeholder_status')"
              @change="onStatusChange"
            />
          </div>
          <div class="tool-item">
            <QInput v-model="limitText" inputType="number" :placeholder="t('placeholder_limit')" />
          </div>
          <QButton
            class="outlined icon"
            :loading="loading"
            :title="t('action_refresh')"
            :aria-label="t('action_refresh')"
            @click="load"
          >
            <QIconRefresh class="icon" />
          </QButton>
        </div>
        <div class="tasks-overview">
          <div class="tasks-overview-copy">
            <p class="tasks-overview-title">{{ tasksShowingText }}</p>
            <p class="tasks-overview-meta">{{ tasksScopeText }}</p>
          </div>
          <div class="tasks-overview-badges">
            <QBadge v-if="hasStatusFilter" :type="taskStatusTypeForValue(statusItem && statusItem.value)" size="sm">
              {{ statusItem ? statusItem.title : t("status_all") }}
            </QBadge>
            <QBadge type="default" size="sm">{{ tasksLimitText }}</QBadge>
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
                <time class="task-time">{{ formatTime(item.created_at) }}</time>
              </div>
            </div>
            <span class="task-row-arrow" aria-hidden="true">
              <QIconArrowRight class="icon" />
            </span>
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
