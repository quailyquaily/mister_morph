import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./TasksView.css";

import AppPage from "../components/AppPage";
import { TASK_STATUS_META, endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";

const TasksView = {
  components: {
    AppPage,
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
    const limitText = ref("20");
    const items = ref([]);
    const err = ref("");
    const loading = ref(false);
    const emptyTitle = computed(() => (hasStatusFilter.value ? t("tasks_empty_filtered_title") : t("tasks_empty_title")));
    const emptyHint = computed(() => (hasStatusFilter.value ? t("tasks_empty_filtered_hint") : t("tasks_empty_hint")));

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const q = new URLSearchParams();
        const v = statusValue.value || "";
        if (v) {
          q.set("status", v);
        }
        const limit = Math.max(1, Math.min(200, parseInt(limitText.value || "20", 10) || 20));
        q.set("limit", String(limit));
        const data = await runtimeApiFetch(`/tasks?${q.toString()}`);
        items.value = Array.isArray(data.items) ? data.items : [];
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

    function openTask(id) {
      router.push(`/tasks/${id}`);
    }

    function goChat() {
      router.push("/chat");
    }

    function summary(item) {
      const source = item.source || "daemon";
      const status = (item.status || "unknown").toUpperCase();
      return `[${status}] ${item.id} | ${source} | ${item.model || "-"} | ${formatTime(item.created_at)}`;
    }

    onMounted(load);
    watch(
      () => endpointState.selectedRef,
      () => {
        void load();
      }
    );
    return {
      t,
      taskStatusItems,
      statusItem,
      limitText,
      items,
      err,
      loading,
      load,
      onStatusChange,
      openTask,
      goChat,
      summary,
      emptyTitle,
      emptyHint,
    };
  },
  template: `
    <AppPage :title="t('tasks_title')">
      <div class="toolbar wrap">
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
        <QButton class="outlined" :loading="loading" @click="load">{{ t("action_refresh") }}</QButton>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="stack">
        <div v-for="item in items" :key="item.id" class="task-row">
          <code class="task-line">{{ summary(item) }}</code>
          <QButton class="plain" @click="openTask(item.id)">{{ t("task_detail") }}</QButton>
        </div>
        <section v-if="items.length === 0 && !loading" class="task-empty frame">
          <div class="task-empty-copy">
            <code class="task-empty-kicker">{{ t("tasks_title") }}</code>
            <h3 class="task-empty-title">{{ emptyTitle }}</h3>
            <p class="task-empty-hint">{{ emptyHint }}</p>
          </div>
          <QButton class="plain sm" @click="goChat">{{ t("tasks_empty_action") }}</QButton>
        </section>
      </div>
    </AppPage>
  `,
};


export default TasksView;
