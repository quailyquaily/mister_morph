import { computed, onMounted, ref, watch } from "vue";
import "./MemoryView.css";

import AppPage from "../components/AppPage";
import MarkdownEditor from "../components/MarkdownEditor";
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

function todayDayKey() {
  return new Date().toISOString().slice(0, 10);
}

function lineCount(value) {
  const text = String(value || "");
  if (!text) {
    return 0;
  }
  return text.split(/\r?\n/).length;
}

function formatDayLabel(value) {
  const dayKey = String(value || "").trim();
  if (!dayKey) {
    return "";
  }
  const parsed = new Date(`${dayKey}T00:00:00`);
  if (Number.isNaN(parsed.getTime())) {
    return dayKey;
  }
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  }).format(parsed);
}

function formatClockLabel(value) {
  const raw = String(value || "").trim();
  if (!raw) {
    return "";
  }
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(parsed);
}

function compactSessionID(raw) {
  const value = String(raw || "").trim();
  if (!value) {
    return "";
  }
  if (value.length <= 18) {
    return value;
  }
  return `${value.slice(0, 10)}…${value.slice(-6)}`;
}

function inferSessionChannel(t, item) {
  const sessionID = String(item?.sessionID || "").trim();
  const fileName = String(item?.name || "").trim().replace(/\.md$/i, "");
  const raw = (sessionID.split(/[:_-]/)[0] || fileName.split(/[_-]/)[0] || "").trim().toLowerCase();
  switch (raw) {
    case "console":
      return t("endpoint_channel_console");
    case "tg":
    case "telegram":
      return t("endpoint_channel_telegram");
    case "slack":
      return t("endpoint_channel_slack");
    case "line":
      return t("endpoint_channel_line");
    case "lark":
      return t("endpoint_channel_lark");
    default:
      return t("memory_session_unknown");
  }
}

function sessionTitle(t, item) {
  const time = formatClockLabel(item?.modTime);
  if (time) {
    return time;
  }
  const sessionID = compactSessionID(item?.sessionID || String(item?.name || "").replace(/\.md$/i, ""));
  if (sessionID) {
    return sessionID;
  }
  return t("memory_session_unknown");
}

function sessionMeta(t, item) {
  return inferSessionChannel(t, item);
}

function sessionCountLabel(t, count) {
  return t("memory_date_session_count", { count: Number(count || 0) });
}

function compareSessionItems(left, right) {
  const leftTime = Date.parse(String(left?.modTime || ""));
  const rightTime = Date.parse(String(right?.modTime || ""));
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) {
    return rightTime - leftTime;
  }
  return String(left?.name || "").localeCompare(String(right?.name || ""));
}

const MemoryView = {
  components: {
    AppPage,
    MarkdownEditor,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const ok = ref("");

    const rawMemoryItems = ref(DEFAULT_MEMORY_FILES.map((item) => toMemoryItem(t, item)));
    const modeTabs = computed(() => [
      { id: "long_term", title: t("memory_mode_long_term") },
      { id: "short_term", title: t("memory_mode_short_term") },
    ]);
    const modeValue = ref("short_term");
    const selectedModeTab = computed(() => modeTabs.value.find((item) => item.id === modeValue.value) || modeTabs.value[0] || null);

    const selectedDateKey = ref(todayDayKey());
    const selectedSessionID = ref("");

    const content = ref("");
    const loadedContent = ref("");

    const selectedDateLabel = computed(() => formatDayLabel(selectedDateKey.value));
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
    const dateGroups = computed(() =>
      shortTermDates.value.map((dayKey) => ({
        dayKey,
        title: formatDayLabel(dayKey),
        meta: sessionCountLabel(
          t,
          rawMemoryItems.value.filter((item) => item.group === "short_term" && item.date === dayKey).length
        ),
        sessions: rawMemoryItems.value
          .filter((item) => item.group === "short_term" && item.date === dayKey)
          .sort(compareSessionItems),
      }))
    );
    const sessionItems = computed(() =>
      rawMemoryItems.value
        .filter((item) => item.group === "short_term" && item.date === selectedDateKey.value)
        .sort(compareSessionItems)
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
    const dayRailTitle = computed(() => t("memory_index_days"));
    const dayRailMeta = computed(() =>
      t("memory_index_days_meta", {
        count: dateGroups.value.length,
      })
    );
    const indexTitle = computed(() =>
      modeValue.value === "long_term" ? t("memory_doc_core") : t("memory_index_sessions")
    );
    const indexMeta = computed(() => {
      if (modeValue.value === "long_term") {
        return t("memory_index_long_meta");
      }
      return t("memory_index_short_meta", {
        count: sessionItems.value.length,
        date: selectedDateLabel.value || selectedDateKey.value || t("memory_meta_date"),
      });
    });
    const editorTitle = computed(() => {
      if (modeValue.value === "long_term") {
        return t("memory_doc_core");
      }
      const item = selectedMemory.value;
      if (!item) {
        return t("memory_title");
      }
      const label = inferSessionChannel(t, item);
      const time = formatClockLabel(item.modTime);
      return time ? `${label} · ${time}` : label;
    });
    const editorMeta = computed(() => {
      const parts = [];
      const item = selectedMemory.value;
      parts.push(groupTitle(t, modeValue.value));
      if (modeValue.value === "short_term" && selectedDateLabel.value) {
        parts.push(selectedDateLabel.value);
      }
      if (item?.sessionID && modeValue.value === "short_term") {
        parts.push(compactSessionID(item.sessionID));
      } else if (item?.name) {
        parts.push(item.name);
      }
      if (item?.modTime) {
        parts.push(`${t("memory_meta_updated")}: ${formatTime(item.modTime)}`);
      }
      parts.push(
        t("files_editor_meta", {
          lines: lineCount(content.value),
          chars: content.value.length,
        })
      );
      return parts.join(" · ");
    });
    const saveDisabled = computed(
      () => saving.value || loading.value || !selectedMemory.value || content.value === loadedContent.value
    );

    function isSelectedDate(dayKey) {
      return String(dayKey || "") === String(selectedDateKey.value || "");
    }

    function isSelectedItem(item) {
      return String(item?.id || "") === String(selectedMemory.value?.id || "");
    }

    function dateClass(dayKey) {
      const classes = ["memory-date-item", "workspace-sidebar-item"];
      if (isSelectedDate(dayKey)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function itemClass(item) {
      const classes = ["memory-index-item", "workspace-sidebar-item"];
      if (isSelectedItem(item)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

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

    async function onModeChange(detail) {
      const nextMode = String(detail?.tab?.id || "").trim();
      if (!nextMode) {
        return;
      }
      modeValue.value = nextMode;
      if (modeValue.value === "short_term") {
        const current = selectedDateKey.value;
        const nextDate = shortTermDates.value.includes(current) ? current : shortTermDates.value[0] || "";
        selectedDateKey.value = nextDate;
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

    async function onDateSelect(dayKey) {
      const nextDayKey = String(dayKey || "").trim();
      if (!nextDayKey || nextDayKey === selectedDateKey.value) {
        return;
      }
      selectedDateKey.value = nextDayKey;
      err.value = "";
      ok.value = "";
      if (!selectedSession.value) {
        content.value = "";
        loadedContent.value = "";
        return;
      }
      await loadContent(selectedSession.value.id);
    }

    async function onSessionSelect(item) {
      if (!item || typeof item !== "object" || !item.id) {
        return;
      }
      if (String(item.id) === String(selectedSessionID.value || "").trim()) {
        return;
      }
      selectedSessionID.value = String(item.id || "").trim();
      await loadContent(item.id);
    }

    async function onLongTermSelect() {
      if (modeValue.value !== "long_term") {
        modeValue.value = "long_term";
      }
      const target = longTermItem.value;
      if (target && target.id) {
        await loadContent(target.id);
      }
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
        if (nextDate !== current) {
          selectedDateKey.value = nextDate;
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
      modeTabs,
      selectedModeTab,
      modeValue,
      selectedDateLabel,
      dateGroups,
      dayRailTitle,
      dayRailMeta,
      sessionItems,
      selectedMemory,
      content,
      indexTitle,
      indexMeta,
      editorTitle,
      editorMeta,
      saveDisabled,
      isSelectedDate,
      isSelectedItem,
      dateClass,
      itemClass,
      sessionTitle,
      sessionMeta,
      onModeChange,
      onDateSelect,
      onSessionSelect,
      onLongTermSelect,
      save,
    };
  },
  template: `
    <AppPage :title="t('memory_title')" class="memory-page" :hideDesktopBar="true">
      <div class="memory-workbench">
        <aside class="memory-index workspace-sidebar-section" :aria-label="t('memory_title')">
          <QTabs
            class="memory-index-tabs"
            :tabs="modeTabs"
            :modelValue="selectedModeTab"
            variant="plain"
            @change="onModeChange"
          />

          <div class="memory-index-head workspace-sidebar-head">
            <p class="ui-kicker">{{ modeValue === "long_term" ? t("memory_group_long_term") : t("memory_group_short_term") }}</p>
            <h3 class="memory-index-title workspace-section-title">{{ indexTitle }}</h3>
            <p class="memory-index-meta">{{ indexMeta }}</p>
          </div>

          <div class="memory-index-rail">
            <section v-if="modeValue === 'short_term'" class="memory-index-group">
              <div class="memory-index-group-head">
                <h4 class="memory-index-group-title">{{ dayRailTitle }}</h4>
                <p class="memory-index-group-meta">{{ dayRailMeta }}</p>
              </div>
              <div class="memory-date-items workspace-sidebar-list">
                <section
                  v-for="group in dateGroups"
                  :key="group.dayKey"
                  class="memory-date-group"
                >
                  <button
                    type="button"
                    :class="dateClass(group.dayKey)"
                    @click="onDateSelect(group.dayKey)"
                  >
                    <span class="memory-date-item-copy workspace-sidebar-item-copy">
                      <span class="memory-date-item-name workspace-sidebar-item-title">{{ group.title }}</span>
                      <span class="memory-date-item-meta workspace-sidebar-item-meta">{{ group.meta }}</span>
                    </span>
                    <span class="memory-date-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                      <QBadge v-if="isSelectedDate(group.dayKey)" dot type="primary" size="sm" />
                    </span>
                  </button>

                  <div v-if="isSelectedDate(group.dayKey)" class="memory-date-sessions">
                    <button
                      v-for="item in group.sessions"
                      :key="item.id"
                      type="button"
                      :class="itemClass(item)"
                      @click="onSessionSelect(item)"
                    >
                      <span class="memory-index-item-copy workspace-sidebar-item-copy">
                        <span class="memory-index-item-name workspace-sidebar-item-title">{{ sessionTitle(t, item) }}</span>
                        <span class="memory-index-item-meta workspace-sidebar-item-meta">{{ sessionMeta(t, item) }}</span>
                      </span>
                      <span class="memory-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                        <QBadge v-if="isSelectedItem(item)" dot type="primary" size="sm" />
                      </span>
                    </button>
                  </div>
                </section>
              </div>
            </section>

            <section class="memory-index-group">
              <div v-if="modeValue === 'long_term'" class="memory-index-items workspace-sidebar-list">
                <button type="button" :class="itemClass(selectedMemory)" @click="onLongTermSelect">
                  <span class="memory-index-item-copy workspace-sidebar-item-copy">
                    <span class="memory-index-item-name workspace-sidebar-item-title">{{ t("memory_doc_core") }}</span>
                    <span class="memory-index-item-meta workspace-sidebar-item-meta">index.md</span>
                  </span>
                  <span class="memory-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                    <QBadge v-if="selectedMemory" dot type="primary" size="sm" />
                  </span>
                </button>
              </div>

              <div v-else-if="sessionItems.length === 0" class="memory-index-empty">
                <p class="memory-index-empty-title">{{ t("memory_empty_short_title") }}</p>
                <p class="memory-index-empty-copy">{{ t("memory_empty_short_hint", { date: selectedDateLabel || t("memory_meta_date") }) }}</p>
              </div>
            </section>
          </div>
        </aside>

        <QCard class="memory-editor-card" variant="default">
          <div class="memory-editor-shell">
            <header class="memory-editor-head">
              <div class="memory-editor-copy">
                <div class="memory-editor-kickers">
                  <QBadge size="sm">{{ modeValue === "long_term" ? t("memory_group_long_term") : t("memory_group_short_term") }}</QBadge>
                </div>
                <h3 class="memory-editor-title workspace-document-title">{{ editorTitle }}</h3>
                <p class="memory-editor-meta">{{ editorMeta }}</p>
              </div>
              <div class="memory-editor-actions">
                <QButton class="primary" :loading="saving" :disabled="saveDisabled" @click="save">
                  {{ t("action_save") }}
                </QButton>
              </div>
            </header>

            <div class="memory-editor-notices">
              <QProgress v-if="loading" :infinite="true" />
              <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
              <QFence v-else-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
            </div>

            <div v-if="selectedMemory" class="memory-editor-surface">
              <MarkdownEditor
                v-model="content"
                height="100%"
                :aria-label="editorTitle"
                :disabled="loading"
              />
            </div>

            <div v-else class="memory-empty-state">
              <h4 class="memory-empty-state-title">{{ t("memory_empty_short_title") }}</h4>
              <p class="memory-empty-state-copy">{{ t("memory_empty_short_hint", { date: selectedDateLabel || t("memory_meta_date") }) }}</p>
            </div>
          </div>
        </QCard>
      </div>
    </AppPage>
  `,
};

export default MemoryView;
