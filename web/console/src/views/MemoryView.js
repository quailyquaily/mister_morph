import { computed, onMounted, ref } from "vue";

import AppPage from "../components/AppPage";
import { formatBytes, formatTime, runtimeApiFetch, translate } from "../core/context";

const DEFAULT_MEMORY_FILES = [{ id: "index.md", name: "index.md", group: "long_term", exists: false }];

function normalizeGroup(value) {
  return String(value || "").trim().toLowerCase();
}

function groupTitle(t, group) {
  switch (normalizeGroup(group)) {
    case "long_term":
      return t("memory_group_long_term");
    case "short_term":
      return t("memory_group_short_term");
    default:
      return t("files_group_other");
  }
}

function toMemoryItem(t, item) {
  const id = String(item?.id || "").trim();
  const name = String(item?.name || "").trim();
  const group = normalizeGroup(item?.group);
  const date = String(item?.date || "").trim();
  const sessionID = String(item?.session_id || "").trim();
  const sizeBytes = Number.isFinite(Number(item?.size_bytes)) ? Number(item.size_bytes) : 0;
  const modTime = String(item?.mod_time || "").trim();
  const exists = item?.exists !== false;
  const titleParts = [groupTitle(t, group), name];
  if (date) {
    titleParts.push(date);
  }
  return {
    title: titleParts.join(" / "),
    id,
    name,
    group,
    date,
    sessionID,
    sizeBytes,
    modTime,
    exists,
  };
}

function dayKeyFromISO(value) {
  const raw = String(value || "").trim();
  const m = raw.match(/^(\d{4}-\d{2}-\d{2})/);
  if (!m) {
    return "";
  }
  return m[1];
}

function dateValueFromDayKey(dayKey) {
  const key = String(dayKey || "").trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(key)) {
    return "";
  }
  return `${key}T00:00:00`;
}

function todayDayKey() {
  return new Date().toISOString().slice(0, 10);
}

const MemoryView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const ok = ref("");

    const rawMemoryItems = ref(DEFAULT_MEMORY_FILES.map((item) => toMemoryItem(t, item)));
    const modeItems = computed(() => [
      { title: t("memory_mode_long_term"), value: "long_term" },
      { title: t("memory_mode_short_term"), value: "short_term" },
    ]);
    const modeValue = ref("short_term");
    const modeItem = computed(() => modeItems.value.find((item) => item.value === modeValue.value) || modeItems.value[0] || null);

    const selectedDateValue = ref(dateValueFromDayKey(todayDayKey()));
    const sessionItems = ref([]);
    const selectedSession = ref(null);
    const longTermItem = ref(toMemoryItem(t, DEFAULT_MEMORY_FILES[0]));

    const content = ref("");

    const selectedDateKey = computed(() => dayKeyFromISO(selectedDateValue.value));
    const selectedMemory = computed(() => {
      if (modeValue.value === "long_term") {
        return longTermItem.value;
      }
      return selectedSession.value;
    });

    function syncDateAndSessionSelection(preferredDateKey = "") {
      const nextDateSet = new Set(
        rawMemoryItems.value
          .filter((item) => item.group === "short_term" && item.date)
          .map((item) => item.date)
      );
      const sortedDates = Array.from(nextDateSet).sort((a, b) => b.localeCompare(a));

      const currentDate = preferredDateKey || selectedDateKey.value;
      const fallbackDate = sortedDates[0] || dayKeyFromISO(selectedDateValue.value) || todayDayKey();
      const nextDate = sortedDates.includes(currentDate) ? currentDate : fallbackDate;
      selectedDateValue.value = dateValueFromDayKey(nextDate);

      const list = rawMemoryItems.value
        .filter((item) => item.group === "short_term" && item.date === nextDate)
        .sort((a, b) => a.name.localeCompare(b.name));
      sessionItems.value = list;

      const keepID = String(selectedSession.value?.id || "").trim();
      selectedSession.value = list.find((item) => item.id === keepID) || list[0] || null;
    }

    async function loadFiles() {
      const data = await runtimeApiFetch("/memory/files");
      const items = Array.isArray(data.items) ? data.items : [];
      const mapped = items.map((item) => toMemoryItem(t, item)).filter((item) => item.id !== "");
      rawMemoryItems.value = mapped.length > 0 ? mapped : DEFAULT_MEMORY_FILES.map((item) => toMemoryItem(t, item));
      longTermItem.value = rawMemoryItems.value.find((item) => item.id === "index.md") || toMemoryItem(t, DEFAULT_MEMORY_FILES[0]);
      if (!rawMemoryItems.value.some((item) => item.group === "short_term")) {
        modeValue.value = "long_term";
      }
      syncDateAndSessionSelection();
    }

    async function loadContent(id) {
      if (!id) {
        content.value = "";
        return;
      }
      loading.value = true;
      err.value = "";
      ok.value = "";
      try {
        const data = await runtimeApiFetch(`/memory/files/${encodeURIComponent(id)}`);
        content.value = data.content || "";
      } catch (e) {
        if (e && e.status === 404) {
          content.value = "";
          ok.value = t("msg_file_missing_create");
          return;
        }
        err.value = e.message || t("msg_read_failed");
      } finally {
        loading.value = false;
      }
    }

    async function save() {
      const target = selectedMemory.value;
      if (!target || !target.id) {
        return;
      }
      saving.value = true;
      err.value = "";
      ok.value = "";
      try {
        await runtimeApiFetch(`/memory/files/${encodeURIComponent(target.id)}`, {
          method: "PUT",
          body: { content: content.value },
        });
        ok.value = t("msg_save_success");
        await loadFiles();
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    async function refresh() {
      err.value = "";
      ok.value = "";
      await loadFiles();
      const target = selectedMemory.value;
      if (target && target.id) {
        await loadContent(target.id);
      } else {
        content.value = "";
      }
    }

    async function onModeChange(item) {
      if (!item || typeof item !== "object" || typeof item.value !== "string") {
        return;
      }
      modeValue.value = item.value;
      err.value = "";
      ok.value = "";
      const target = selectedMemory.value;
      if (target && target.id) {
        await loadContent(target.id);
      } else {
        content.value = "";
      }
    }

    async function onDateChange(value) {
      selectedDateValue.value = String(value || "").trim();
      syncDateAndSessionSelection(selectedDateKey.value);
      err.value = "";
      ok.value = "";
      if (!selectedSession.value) {
        content.value = "";
        return;
      }
      await loadContent(selectedSession.value.id);
    }

    async function onSessionChange(item) {
      if (!item || typeof item !== "object" || !item.id) {
        return;
      }
      selectedSession.value = item;
      await loadContent(item.id);
    }

    function metaText(item) {
      if (!item) {
        return "";
      }
      const parts = [groupTitle(t, item.group)];
      if (item.date) {
        parts.push(item.date);
      }
      if (item.sizeBytes > 0) {
        parts.push(formatBytes(item.sizeBytes));
      }
      if (item.modTime) {
        parts.push(formatTime(item.modTime));
      }
      return parts.join(" | ");
    }

    async function init() {
      await refresh();
    }

    onMounted(init);
    return {
      t,
      loading,
      saving,
      err,
      ok,
      modeItems,
      modeItem,
      modeValue,
      selectedDateValue,
      sessionItems,
      selectedSession,
      selectedMemory,
      content,
      refresh,
      save,
      onModeChange,
      onDateChange,
      onSessionChange,
      metaText,
    };
  },
  template: `
    <AppPage :title="t('memory_title')">
      <div class="toolbar wrap">
        <div class="tool-item">
          <QDropdownMenu
            :items="modeItems"
            :initialItem="modeItem"
            :placeholder="t('memory_label_mode')"
            @change="onModeChange"
          />
        </div>
        <div v-if="modeValue === 'short_term'" class="tool-item">
          <QDatetimePicker
            v-model="selectedDateValue"
            accept="date"
            @change="onDateChange"
          />
        </div>
        <div v-if="modeValue === 'short_term'" class="tool-item">
          <QDropdownMenu
            :items="sessionItems"
            :initialItem="selectedSession"
            :placeholder="t('placeholder_select_memory_session')"
            @change="onSessionChange"
          />
        </div>
        <QButton class="outlined" :loading="loading" @click="refresh">{{ t("action_refresh") }}</QButton>
        <QButton class="primary" :loading="saving" @click="save">{{ t("action_save") }}</QButton>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <QFence v-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
      <p v-if="selectedMemory" class="muted">{{ metaText(selectedMemory) }}</p>
      <p v-else-if="modeValue === 'short_term'" class="muted">{{ t("memory_no_sessions_for_date") }}</p>
      <p v-else class="muted">{{ t("memory_no_files") }}</p>
      <QTextarea v-model="content" :rows="22" :disabled="!selectedMemory" />
    </AppPage>
  `,
};

export default MemoryView;
