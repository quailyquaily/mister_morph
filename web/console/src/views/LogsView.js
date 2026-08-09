import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import "./LogsView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatBytes, formatTime, runtimeApiFetch, safeJSON, translate } from "../core/context";

const LIMIT_OPTIONS = [100, 300, 1000];
const AUTO_REFRESH_MS = 4000;

let entrySeq = 0;

function logLevelType(level) {
  switch (String(level || "").trim().toLowerCase()) {
    case "debug":
      return "default";
    case "warn":
    case "warning":
      return "warning";
    case "error":
      return "danger";
    default:
      return "primary";
  }
}

function formatLogStamp(raw) {
  const text = String(raw || "").trim();
  if (!text) {
    return "";
  }
  return formatTime(text);
}

function toLogEntry(file, line) {
  const parsed = safeJSON(line, null);
  const level = typeof parsed?.level === "string" ? parsed.level : "";
  const time = typeof parsed?.time === "string" ? parsed.time : "";
  const msg = typeof parsed?.msg === "string" ? parsed.msg : "";
  entrySeq += 1;
  return {
    id: `${file || "log"}:${entrySeq}`,
    file: String(file || "").trim(),
    line: String(line || ""),
    level,
    time,
    msg,
  };
}

const LogsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const err = ref("");
    const unsupported = ref(false);
    const loading = ref(false);
    const loadingOlder = ref(false);
    const limit = ref(300);
    const hasNewer = ref(false);
    const entries = ref([]);
    const currentFile = ref("");
    const modTime = ref("");
    const sizeBytes = ref(0);
    const hasNext = ref(false);
    const nextCursor = ref("");
    const logPane = ref(null);
    let refreshTimer = null;

    const metaText = computed(() => {
      const parts = [];
      if (modTime.value) {
        parts.push(t("logs_updated", { value: formatTime(modTime.value) }));
      }
      if (sizeBytes.value > 0) {
        parts.push(t("logs_size", { value: formatBytes(sizeBytes.value) }));
      }
      return parts.join(" · ");
    });

    const emptyText = computed(() => {
      if (unsupported.value) {
        return t("logs_unsupported");
      }
      if (!endpointState.selectedRef) {
        return t("msg_select_endpoint");
      }
      return t("logs_empty");
    });

    function isNearBottom() {
      const el = logPane.value;
      if (!el) {
        return true;
      }
      return el.scrollHeight - el.scrollTop - el.clientHeight < 48;
    }

    async function scrollToBottom() {
      await nextTick();
      const el = logPane.value;
      if (el) {
        el.scrollTop = el.scrollHeight;
      }
    }

    function applyLatestPayload(payload) {
      const file = String(payload?.file || "").trim();
      currentFile.value = file;
      modTime.value = String(payload?.mod_time || "").trim();
      sizeBytes.value = Number(payload?.size_bytes || 0);
      hasNext.value = Boolean(payload?.has_next);
      nextCursor.value = String(payload?.next_cursor || "").trim();
      entries.value = Array.isArray(payload?.items) ? payload.items.map((line) => toLogEntry(file, line)) : [];
    }

    async function loadLatest({ keepPosition = false } = {}) {
      if (loading.value) {
        return;
      }
      if (!endpointState.selectedRef) {
        err.value = t("msg_select_endpoint");
        return;
      }
      const shouldStick = !keepPosition && isNearBottom();
      loading.value = true;
      err.value = "";
      unsupported.value = false;
      try {
        const data = await runtimeApiFetch(`/logs/latest?limit=${encodeURIComponent(limit.value)}`);
        applyLatestPayload(data);
        hasNewer.value = false;
        if (shouldStick) {
          await scrollToBottom();
        }
      } catch (e) {
        if (e?.status === 404) {
          unsupported.value = true;
          entries.value = [];
          err.value = "";
          return;
        }
        err.value = e?.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function loadOlder() {
      const cursor = String(nextCursor.value || "").trim();
      if (!cursor || loadingOlder.value) {
        return;
      }
      const el = logPane.value;
      const previousHeight = el ? el.scrollHeight : 0;
      const previousTop = el ? el.scrollTop : 0;
      loadingOlder.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetch(
          `/logs/latest?limit=${encodeURIComponent(limit.value)}&cursor=${encodeURIComponent(cursor)}`
        );
        const file = String(data?.file || "").trim();
        const olderEntries = Array.isArray(data?.items) ? data.items.map((line) => toLogEntry(file, line)) : [];
        entries.value = olderEntries.concat(entries.value);
        hasNext.value = Boolean(data?.has_next);
        nextCursor.value = String(data?.next_cursor || "").trim();
        await nextTick();
        if (el) {
          el.scrollTop = el.scrollHeight - previousHeight + previousTop;
        }
      } catch (e) {
        err.value = e?.message || t("msg_load_failed");
      } finally {
        loadingOlder.value = false;
      }
    }

    function onScroll() {
      const el = logPane.value;
      if (!el || loadingOlder.value) {
        return;
      }
      if (el.scrollTop <= 8 && hasNext.value && nextCursor.value) {
        loadOlder();
      }
    }

    function onLimitSelect(value) {
      limit.value = Number(value || 300);
      loadLatest();
    }

    function startAutoRefresh() {
      stopAutoRefresh();
      refreshTimer = window.setInterval(() => {
        if (isNearBottom()) {
          loadLatest();
        } else {
          hasNewer.value = true;
        }
      }, AUTO_REFRESH_MS);
    }

    function stopAutoRefresh() {
      if (refreshTimer) {
        window.clearInterval(refreshTimer);
        refreshTimer = null;
      }
    }

    function showFileMarker(item, index) {
      if (!item?.file) {
        return false;
      }
      if (index === 0) {
        return true;
      }
      return entries.value[index - 1]?.file !== item.file;
    }

    watch(
      () => endpointState.selectedRef,
      () => {
        entries.value = [];
        currentFile.value = "";
        nextCursor.value = "";
        hasNext.value = false;
        loadLatest();
      }
    );

    onMounted(() => {
      loadLatest();
      startAutoRefresh();
    });

    onUnmounted(() => {
      stopAutoRefresh();
    });

    return {
      t,
      err,
      unsupported,
      loading,
      loadingOlder,
      limit,
      limits: LIMIT_OPTIONS,
      hasNewer,
      entries,
      currentFile,
      hasNext,
      nextCursor,
      logPane,
      metaText,
      emptyText,
      logLevelType,
      formatLogStamp,
      loadLatest,
      loadOlder,
      onScroll,
      onLimitSelect,
      showFileMarker,
    };
  },
  template: `
    <AppPage :title="t('logs_title')" class="logs-page">
      <section class="logs-shell">
        <header class="logs-head">
          <div class="logs-title-block">
            <h3 class="logs-current-file">{{ currentFile || t("logs_unknown_file") }}</h3>
            <p class="logs-meta">{{ metaText || t("logs_meta_empty") }}</p>
          </div>
          <div class="logs-limit-group" :aria-label="t('logs_line_count')">
            <QButton
              v-for="item in limits"
              :key="item"
              :class="['plain sm logs-limit-button', { 'is-active': limit === item }]"
              @click="onLimitSelect(item)"
            >
              {{ item }}
            </QButton>
          </div>
        </header>

        <QProgress v-if="loading && entries.length === 0" :infinite="true" />
        <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

        <div v-if="hasNewer" class="logs-newer-note">
          <span>{{ t("logs_new_available") }}</span>
          <QButton class="plain xs" @click="loadLatest">{{ t("logs_latest") }}</QButton>
        </div>

        <div v-if="entries.length === 0 && !loading" class="logs-empty">
          <p class="logs-empty-title">{{ emptyText }}</p>
        </div>

        <div
          v-else
          ref="logPane"
          class="logs-stream"
          role="log"
          aria-live="polite"
          @scroll.passive="onScroll"
        >
          <div class="logs-older-row">
            <QButton
              class="plain sm"
              :disabled="!hasNext || !nextCursor"
              :loading="loadingOlder"
              @click="loadOlder"
            >
              {{ hasNext ? t("logs_load_older") : t("logs_no_older") }}
            </QButton>
          </div>

          <div
            v-for="(item, index) in entries"
            :key="item.id"
            class="logs-line-row"
          >
            <div v-if="showFileMarker(item, index)" class="logs-file-marker">
              <span>{{ item.file }}</span>
            </div>
            <div class="logs-line-meta">
              <QBadge :type="logLevelType(item.level)" size="sm">{{ item.level || "INFO" }}</QBadge>
              <time v-if="item.time">{{ formatLogStamp(item.time) }}</time>
              <span v-if="item.msg" class="logs-line-msg">{{ item.msg }}</span>
            </div>
            <pre class="logs-line"><code>{{ item.line }}</code></pre>
          </div>
        </div>
      </section>
    </AppPage>
  `,
};

export default LogsView;
