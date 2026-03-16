import { computed, onMounted, ref, watch } from "vue";
import "./MemoryView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";

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

function normalizePickerDayKey(value) {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return value.toISOString().slice(0, 10);
  }
  const raw = String(value || "").trim();
  const direct = dayKeyFromISO(raw);
  if (direct) {
    return direct;
  }
  const parsed = new Date(raw);
  if (!Number.isNaN(parsed.getTime())) {
    return parsed.toISOString().slice(0, 10);
  }
  return "";
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
    const selectedSessionID = ref("");

    const content = ref("");
    const loadedContent = ref("");

    const selectedDateKey = computed(() => dayKeyFromISO(selectedDateValue.value));
    const longTermItem = computed(
      () => rawMemoryItems.value.find((item) => item.id === "index.md") || toMemoryItem(t, DEFAULT_MEMORY_FILES[0])
    );
    const shortTermDates = computed(() => {
      const dates = new Set(
        rawMemoryItems.value
          .filter((item) => item.group === "short_term" && item.date)
          .map((item) => item.date)
      );
      return Array.from(dates).sort((a, b) => b.localeCompare(a));
    });
    const sessionItems = computed(() =>
      rawMemoryItems.value
        .filter((item) => item.group === "short_term" && item.date === selectedDateKey.value)
        .sort((a, b) => a.name.localeCompare(b.name))
    );
    const selectedSession = computed(() => {
      const currentID = String(selectedSessionID.value || "").trim();
      if (!currentID) {
        return sessionItems.value[0] || null;
      }
      return sessionItems.value.find((item) => item.id === currentID) || sessionItems.value[0] || null;
    });
    const selectedMemory = computed(() => {
      if (modeValue.value === "long_term") {
        return longTermItem.value;
      }
      return selectedSession.value;
    });

    async function loadFiles() {
      const data = await runtimeApiFetch("/memory/files");
      const items = Array.isArray(data.items) ? data.items : [];
      const mapped = items.map((item) => toMemoryItem(t, item)).filter((item) => item.id !== "");
      rawMemoryItems.value = mapped.length > 0 ? mapped : DEFAULT_MEMORY_FILES.map((item) => toMemoryItem(t, item));
      if (!rawMemoryItems.value.some((item) => item.group === "short_term")) {
        modeValue.value = "long_term";
      }
    }

    async function loadContent(id) {
      if (!id) {
        content.value = "";
        loadedContent.value = "";
        return;
      }
      loading.value = true;
      err.value = "";
      ok.value = "";
      try {
        const data = await runtimeApiFetch(`/memory/files/${encodeURIComponent(id)}`);
        const nextContent = data.content || "";
        content.value = nextContent;
        loadedContent.value = nextContent;
      } catch (e) {
        if (e && e.status === 404) {
          content.value = "";
          loadedContent.value = "";
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
        loadedContent.value = content.value;
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
        loadedContent.value = "";
      }
    }

    async function onModeChange(item) {
      if (!item || typeof item !== "object" || typeof item.value !== "string") {
        return;
      }
      modeValue.value = item.value;
      if (modeValue.value === "short_term") {
        const current = selectedDateKey.value;
        const nextDate = shortTermDates.value.includes(current) ? current : shortTermDates.value[0] || "";
        selectedDateValue.value = nextDate ? dateValueFromDayKey(nextDate) : "";
      }
      err.value = "";
      ok.value = "";
      const target = selectedMemory.value;
      if (target && target.id) {
        await loadContent(target.id);
      } else {
        content.value = "";
        loadedContent.value = "";
      }
    }

    async function onDateChange(value) {
      const nextDayKey = normalizePickerDayKey(value);
      selectedDateValue.value = nextDayKey ? dateValueFromDayKey(nextDayKey) : "";
      err.value = "";
      ok.value = "";
      if (!selectedSession.value) {
        content.value = "";
        loadedContent.value = "";
        return;
      }
      await loadContent(selectedSession.value.id);
    }

    async function onSessionChange(item) {
      if (!item || typeof item !== "object" || !item.id) {
        return;
      }
      selectedSessionID.value = String(item.id || "").trim();
      await loadContent(item.id);
    }

    const memoryHint = computed(() => {
      const item = selectedMemory.value;
      if (!item || !item.modTime) {
        return "";
      }
      return `${t("memory_meta_updated")}: ${formatTime(item.modTime)}`;
    });
    const saveDisabled = computed(
      () => saving.value || !selectedMemory.value || content.value === loadedContent.value
    );

    function sessionPickerKey() {
      return `${selectedDateKey.value}:${sessionItems.value.map((item) => item.id).join("|")}`;
    }

    async function init() {
      await refresh();
    }

    onMounted(init);
    watch(
      () => endpointState.selectedRef,
      () => {
        void init();
      }
    );
    watch(
      shortTermDates,
      (dates) => {
        if (modeValue.value !== "short_term") {
          return;
        }
        const current = selectedDateKey.value;
        const nextDate = dates.includes(current) ? current : dates[0] || "";
        const nextValue = nextDate ? dateValueFromDayKey(nextDate) : "";
        if (nextValue !== selectedDateValue.value) {
          selectedDateValue.value = nextValue;
        }
      },
      { immediate: true }
    );
    watch(
      sessionItems,
      (items) => {
        const current = String(selectedSessionID.value || "").trim();
        const next = items.find((item) => item.id === current)?.id || items[0]?.id || "";
        if (next !== current) {
          selectedSessionID.value = next;
        }
      },
      { immediate: true }
    );
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
      memoryHint,
      saveDisabled,
      refresh,
      save,
      onModeChange,
      onDateChange,
      onSessionChange,
      sessionPickerKey,
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
        <div v-if="modeValue === 'short_term' && sessionItems.length > 0" class="tool-item">
          <QDropdownMenu
            :key="sessionPickerKey()"
            :items="sessionItems"
            :initialItem="selectedSession"
            :placeholder="t('placeholder_select_memory_session')"
            @change="onSessionChange"
          />
        </div>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <QFence v-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
      <p v-if="!selectedMemory && modeValue === 'short_term'" class="muted">{{ t("memory_no_sessions_for_date") }}</p>
      <p v-else-if="!selectedMemory" class="muted">{{ t("memory_no_files") }}</p>
      <div v-if="selectedMemory" class="memory-editor">
        <QTextarea
          v-model="content"
          :rows="22"
          :disabled="!selectedMemory"
          :hint="memoryHint"
        />
        <div class="memory-editor-actions">
          <QButton class="primary" :loading="saving" :disabled="saveDisabled" @click="save">{{ t("action_save") }}</QButton>
        </div>
      </div>
    </AppPage>
  `,
};

export default MemoryView;
