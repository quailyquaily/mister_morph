import { computed, onMounted, reactive, ref, watch } from "vue";
import "./AuditView.css";

import AppPage from "../components/AppPage";
import RawJsonDialog from "../components/RawJsonDialog";
import {
  endpointState,
  formatBytes,
  formatTime,
  runtimeApiFetch,
  safeJSON,
  toBool,
  toInt,
  translate,
} from "../core/context";

const AUDIT_ITEMS_PER_PAGE = 50;

function tuiKicker(left, right) {
  const lhs = String(left || "").trim();
  const rhs = String(right || "").trim();
  if (lhs && rhs) {
    return `[ ${lhs.toUpperCase()} // ${rhs.toUpperCase()} ]`;
  }
  return `[ ${(lhs || rhs).toUpperCase()} ]`;
}

function formatAuditStamp(raw) {
  const text = String(raw || "").trim();
  if (!text) {
    return "";
  }
  const value = new Date(text);
  if (Number.isNaN(value.getTime())) {
    return text;
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(value);
}

function normalizeAuditText(value, fallback = "-") {
  if (typeof value === "string") {
    const s = value.trim();
    return s === "" ? fallback : s;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(Math.trunc(value));
  }
  return fallback;
}

function normalizeAuditList(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .map((it) => {
      if (typeof it === "string") {
        return it.trim();
      }
      if (it === null || it === undefined) {
        return "";
      }
      return String(it).trim();
    })
    .filter((it) => it !== "");
}

function humanizeAuditToken(raw) {
  const text = normalizeAuditText(raw, "");
  if (!text) {
    return "-";
  }
  return text.replaceAll("_", " ").replace(/([a-z0-9])([A-Z])/g, "$1 $2");
}

function decisionBadgeType(raw) {
  switch (String(raw || "").trim().toLowerCase()) {
    case "allow":
      return "success";
    case "allow_with_redaction":
      return "warning";
    case "require_approval":
      return "warning";
    case "deny":
      return "danger";
    default:
      return "default";
  }
}

function riskBadgeType(raw) {
  switch (String(raw || "").trim().toLowerCase()) {
    case "low":
      return "success";
    case "medium":
      return "warning";
    case "high":
      return "danger";
    case "critical":
      return "danger";
    default:
      return "default";
  }
}

function decisionLabel(t, raw) {
  switch (String(raw || "").trim().toLowerCase()) {
    case "allow":
      return t("audit_decision_allow");
    case "allow_with_redaction":
      return t("audit_decision_redact");
    case "require_approval":
      return t("audit_decision_require_approval");
    case "deny":
      return t("audit_decision_deny");
    default:
      return humanizeAuditToken(raw);
  }
}

function riskLabel(t, raw) {
  switch (String(raw || "").trim().toLowerCase()) {
    case "low":
      return t("audit_risk_low");
    case "medium":
      return t("audit_risk_medium");
    case "high":
      return t("audit_risk_high");
    case "critical":
      return t("audit_risk_critical");
    default:
      return humanizeAuditToken(raw);
  }
}

function toAuditFileItem(t, item) {
  const name = String(item?.name || "").trim();
  const sizeBytes = toInt(item?.size_bytes, 0);
  const modTime = String(item?.mod_time || "").trim();
  const current = toBool(item?.current, false);
  const metaParts = [];
  if (current) {
    metaParts.push(t("audit_current_file"));
  }
  if (modTime) {
    metaParts.push(formatAuditStamp(modTime));
  }
  metaParts.push(formatBytes(sizeBytes));
  return {
    key: name,
    value: name,
    name,
    sizeBytes,
    modTime,
    current,
    meta: metaParts.filter(Boolean).join(" · "),
  };
}

const AuditView = {
  components: {
    AppPage,
    RawJsonDialog,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const err = ref("");
    const pageValue = ref(1);
    const fileItems = ref([]);
    const selectedFile = ref("");
    const lines = ref([]);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    const meta = reactive({
      path: "",
      exists: false,
      size_bytes: 0,
      limit: AUDIT_ITEMS_PER_PAGE,
      total_lines: 0,
      total_pages: 0,
      current_page: 1,
      before: 0,
      from: 0,
      to: 0,
      has_older: false,
    });

    const selectedFileItem = computed(
      () => fileItems.value.find((item) => item.value === selectedFile.value) || fileItems.value[0] || null
    );
    const selectedFileDropdownItem = computed(() => {
      const item = selectedFileItem.value;
      if (!item) {
        return null;
      }
      return {
        title: item.name,
        value: item.value,
      };
    });
    const lineWindowText = computed(() => {
      const total = Number(meta.total_lines || 0);
      const page = Number(meta.current_page || 0);
      const size = Number(meta.limit || AUDIT_ITEMS_PER_PAGE);
      if (total <= 0 || page <= 0 || size <= 0) {
        return "-";
      }
      const end = Math.max(total - (page - 1) * size, 0);
      const start = Math.max(end - size + 1, 1);
      return `${start}-${end} / ${total}`;
    });
    const pageText = computed(() => {
      if (meta.total_pages <= 0) {
        return "-";
      }
      return `${meta.current_page} / ${meta.total_pages}`;
    });
    const selectedFileTitle = computed(() => String(selectedFileItem.value?.name || "").trim() || t("audit_title"));
    const showFilePicker = computed(() => fileItems.value.length > 1);
    const selectedFileMeta = computed(() => {
      const parts = [];
      if (meta.path) {
        parts.push(meta.path);
      } else if (selectedFileItem.value?.modTime) {
        parts.push(t("audit_updated", { value: formatAuditStamp(selectedFileItem.value.modTime) }));
      }
      if (meta.exists && lineWindowText.value !== "-") {
        parts.push(t("audit_window_meta", { range: lineWindowText.value }));
      }
      return parts.join(" · ");
    });
    const auditSummaryItems = computed(() => {
      if (!meta.exists) {
        return [];
      }
      return [
        { key: "size", label: t("audit_size"), value: formatBytes(meta.size_bytes) },
        { key: "lines", label: t("audit_lines"), value: lineWindowText.value },
        { key: "page", label: t("audit_page"), value: pageText.value },
      ];
    });

    function parseAuditLine(line, idx) {
      const raw = typeof line === "string" ? line : String(line ?? "");
      const parsed = safeJSON(raw, null);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return {
          key: `${meta.from}-${idx}-raw`,
          parsed: false,
          raw,
          rawPretty: raw,
        };
      }

      const eventID = normalizeAuditText(parsed.event_id);
      const tsRaw = normalizeAuditText(parsed.ts);
      const stepText = normalizeAuditText(parsed.step);
      const actionTypeRaw = normalizeAuditText(parsed.action_type);
      const actionType = humanizeAuditToken(actionTypeRaw);
      const toolName = normalizeAuditText(parsed.tool_name);
      const runID = normalizeAuditText(parsed.run_id);
      const actor = normalizeAuditText(parsed.actor);
      const approvalStatus = normalizeAuditText(parsed.approval_status);
      const summary = normalizeAuditText(parsed.action_summary_redacted);
      const reasons = normalizeAuditList(parsed.reasons);
      const reasonsText = reasons.length > 0 ? reasons.join(" | ") : "-";
      const decisionRaw = normalizeAuditText(parsed.decision, "");
      const riskRaw = normalizeAuditText(parsed.risk_level, "");
      const hasTool = toolName !== "-";
      const primaryTitle = hasTool ? toolName : actionType;
      const subtitleParts = [];
      if (hasTool && actionType !== "-") {
        subtitleParts.push(actionType);
      }
      if (stepText !== "-") {
        subtitleParts.push(`${t("audit_step")} ${stepText}`);
      }
      if (actor !== "-") {
        subtitleParts.push(`${t("audit_actor")} ${actor}`);
      }
      const subtitle = subtitleParts.join(" · ");
      const metaTrail = [];
      if (eventID !== "-") {
        metaTrail.push(eventID);
      }
      if (tsRaw !== "-") {
        metaTrail.push(formatTime(tsRaw));
      }
      if (approvalStatus !== "-") {
        metaTrail.push(`${t("audit_approval")} ${humanizeAuditToken(approvalStatus)}`);
      }

      return {
        key: `${meta.from}-${idx}-${eventID}`,
        parsed: true,
        raw,
        rawPretty: JSON.stringify(parsed, null, 2),
        eventID,
        tsText: tsRaw === "-" ? "-" : formatTime(tsRaw),
        actionType,
        toolName,
        runID,
        stepText,
        actor,
        approvalStatus: humanizeAuditToken(approvalStatus),
        summary,
        reasonsText,
        primaryTitle,
        subtitle,
        metaTrail,
        decisionLabel: decisionLabel(t, decisionRaw),
        decisionType: decisionBadgeType(decisionRaw),
        riskLabel: riskLabel(t, riskRaw),
        riskType: riskBadgeType(riskRaw),
      };
    }

    const auditItems = computed(() =>
      lines.value
        .map((line, idx) => parseAuditLine(line, idx))
        .reverse()
    );
    const auditGroups = computed(() => {
      const groups = [];
      const byRunID = new Map();
      for (const item of auditItems.value) {
        const runID = item.parsed ? item.runID : "-";
        const groupKey = `run:${runID}`;
        let group = byRunID.get(groupKey);
        if (!group) {
          group = {
            key: groupKey,
            runID,
            title: runID === "-" ? t("audit_run_unknown") : runID,
            items: [],
            latestTs: "-",
          };
          byRunID.set(groupKey, group);
          groups.push(group);
        }
        group.items.push(item);
        if (group.latestTs === "-" && item.parsed && item.tsText !== "-") {
          group.latestTs = item.tsText;
        }
      }
      return groups;
    });

    function openRawDialog(item) {
      if (!item) {
        return;
      }
      rawDialogJSON.value = String(item.rawPretty || item.raw || "").trim();
      rawDialogOpen.value = true;
    }

    function closeRawDialog() {
      rawDialogOpen.value = false;
    }

    async function loadFiles() {
      const data = await runtimeApiFetch("/audit/files");
      const items = Array.isArray(data.items) ? data.items : [];
      fileItems.value = items
        .map((it) => toAuditFileItem(t, it))
        .filter((it) => it.value !== "");

      const preferred = typeof data.default_file === "string" ? data.default_file.trim() : "";
      if (fileItems.value.length === 0) {
        selectedFile.value = preferred;
        return;
      }
      if (fileItems.value.find((it) => it.value === selectedFile.value)) {
        return;
      }
      if (preferred && fileItems.value.find((it) => it.value === preferred)) {
        selectedFile.value = preferred;
        return;
      }
      selectedFile.value = fileItems.value[0].value;
    }

    async function loadChunk(cursor = null) {
      loading.value = true;
      err.value = "";
      try {
        const q = new URLSearchParams();
        if (selectedFile.value) {
          q.set("file", selectedFile.value);
        }
        q.set("limit", String(AUDIT_ITEMS_PER_PAGE));
        if (cursor !== null && cursor >= 0) {
          q.set("cursor", String(cursor));
        }
        const data = await runtimeApiFetch(`/audit/logs?${q.toString()}`);
        meta.path = data.path || "";
        meta.exists = toBool(data.exists, false);
        meta.size_bytes = toInt(data.size_bytes, 0);
        meta.limit = toInt(data.limit, AUDIT_ITEMS_PER_PAGE);
        meta.total_lines = toInt(data.total_lines, 0);
        meta.total_pages = toInt(data.total_pages, 0);
        meta.current_page = toInt(data.current_page, 1);
        meta.before = toInt(data.before, 0);
        meta.from = toInt(data.from, 0);
        meta.to = toInt(data.to, 0);
        meta.has_older = toBool(data.has_older, false);
        const fetchedLines = Array.isArray(data.lines) ? data.lines : [];
        lines.value = fetchedLines.slice(-AUDIT_ITEMS_PER_PAGE);
        pageValue.value = Math.max(1, meta.current_page || 1);
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function refreshLatest() {
      await loadChunk(0);
    }

    async function goPage(page) {
      if (loading.value) {
        return;
      }
      const totalPages = Math.max(1, meta.total_pages || 1);
      const target = Math.max(1, Math.min(totalPages, toInt(page, 1)));
      const cursor = (target - 1) * AUDIT_ITEMS_PER_PAGE;
      if (target === meta.current_page && lines.value.length > 0) {
        return;
      }
      await loadChunk(cursor);
    }

    async function goPrev() {
      await goPage(pageValue.value - 1);
    }

    async function goNext() {
      await goPage(pageValue.value + 1);
    }

    async function onFileChange(item) {
      if (!item || typeof item !== "object" || typeof item.value !== "string") {
        return;
      }
      if (item.value === selectedFile.value) {
        return;
      }
      selectedFile.value = item.value;
      await refreshLatest();
    }

    async function init() {
      try {
        await loadFiles();
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      }
      await refreshLatest();
    }

    onMounted(init);
    watch(
      () => endpointState.selectedRef,
      () => {
        void init();
      }
    );

      return {
        t,
        loading,
        err,
        fileItems,
        selectedFileItem,
        selectedFileDropdownItem,
        auditGroups,
        selectedFileTitle,
        selectedFileMeta,
        auditSummaryItems,
        meta,
        pageValue,
        pageText,
        showFilePicker,
        refreshLatest,
        goPrev,
        goNext,
        onFileChange,
        tuiKicker,
        rawDialogOpen,
        rawDialogJSON,
        openRawDialog,
        closeRawDialog,
      };
    },
  template: `
    <AppPage :title="t('audit_title')" class="audit-page">
      <section class="audit-ledger">
        <header class="audit-ledger-head">
          <div class="audit-ledger-copy">
            <p class="ui-kicker">{{ tuiKicker(t("audit_title"), selectedFileTitle) }}</p>
            <div class="audit-ledger-title-row">
              <h2 class="audit-ledger-title workspace-document-title">{{ selectedFileTitle }}</h2>
              <QBadge v-if="selectedFileItem && selectedFileItem.current" size="sm" type="primary">
                {{ t("audit_current_file") }}
              </QBadge>
            </div>
            <p v-if="selectedFileMeta" class="audit-ledger-meta">{{ selectedFileMeta }}</p>
          </div>
          <div class="audit-ledger-actions">
            <div v-if="showFilePicker" class="audit-file-picker">
              <QDropdownMenu
                :items="fileItems"
                :initialItem="selectedFileDropdownItem"
                :placeholder="t('placeholder_audit_file')"
                @change="onFileChange"
              />
            </div>
            <QButton
              class="plain sm icon"
              :loading="loading"
              :title="t('action_refresh')"
              :aria-label="t('action_refresh')"
              @click="refreshLatest"
            >
              <QIconRefresh class="icon" />
            </QButton>
            <div v-if="meta.total_pages > 0" class="audit-pagination">
              <QButton
                class="plain sm icon"
                :disabled="pageValue <= 1"
                :title="t('audit_newer')"
                :aria-label="t('audit_newer')"
                @click="goPrev"
              >
                <QIconArrowLeft class="icon" />
              </QButton>
              <code class="audit-page-indicator">{{ pageText }}</code>
              <QButton
                class="plain sm icon"
                :disabled="pageValue >= meta.total_pages"
                :title="t('audit_older')"
                :aria-label="t('audit_older')"
                @click="goNext"
              >
                <QIconArrowRight class="icon" />
              </QButton>
            </div>
          </div>
        </header>

        <div v-if="auditSummaryItems.length > 0" class="audit-summary-strip">
          <div v-for="item in auditSummaryItems" :key="item.key" class="audit-summary-item">
            <span class="audit-summary-strip-label">{{ item.label }}</span>
            <strong class="audit-summary-strip-value">{{ item.value }}</strong>
          </div>
        </div>

        <QProgress v-if="loading" :infinite="true" />
        <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

        <div v-if="meta.exists" class="audit-feed">
          <section v-for="group in auditGroups" :key="group.key" class="audit-group">
            <header class="audit-group-head">
              <div class="audit-group-head-main">
                <span class="audit-group-label">{{ t("audit_run") }}</span>
                <div class="audit-group-heading">
                  <code class="audit-group-run">{{ group.title }}</code>
                  <span v-if="group.latestTs !== '-'" class="audit-group-time">{{ group.latestTs }}</span>
                </div>
              </div>
              <QBadge size="sm" type="default">{{ group.items.length }} {{ t("audit_group_count") }}</QBadge>
            </header>

            <QCard
              v-for="item in group.items"
              :key="item.key"
              class="audit-row audit-item-card clickable"
              variant="default"
              :hoverable="true"
              tabindex="0"
              role="button"
              :aria-label="t('chat_action_show_raw')"
              @click="openRawDialog(item)"
              @keydown.enter.prevent="openRawDialog(item)"
              @keydown.space.prevent="openRawDialog(item)"
            >
              <template v-if="item.parsed">
                <div class="audit-item-head">
                  <div class="audit-item-primary">
                    <h3 class="audit-item-title">{{ item.primaryTitle }}</h3>
                    <p v-if="item.subtitle" class="audit-item-subtitle">{{ item.subtitle }}</p>
                  </div>
                  <div class="audit-item-badges">
                    <QBadge :type="item.decisionType">{{ item.decisionLabel }}</QBadge>
                    <QBadge :type="item.riskType">{{ item.riskLabel }}</QBadge>
                  </div>
                </div>

                <p v-if="item.summary !== '-'" class="audit-item-summary">{{ item.summary }}</p>

                <div v-if="item.reasonsText !== '-'" class="audit-detail-block audit-detail-block-note">
                  <span class="audit-detail-label">{{ t("audit_reasons") }}</span>
                  <p class="audit-detail-copy">{{ item.reasonsText }}</p>
                </div>

                <div v-if="item.metaTrail.length > 0" class="audit-item-meta-trail">
                  <template v-for="(metaItem, index) in item.metaTrail" :key="metaItem">
                    <code v-if="index === 0" class="audit-item-meta-code">{{ metaItem }}</code>
                    <span v-else class="audit-item-meta-text">{{ metaItem }}</span>
                  </template>
                </div>
              </template>

              <template v-else>
                <div class="audit-item-head">
                  <QBadge type="default" size="sm">{{ t("audit_raw") }}</QBadge>
                </div>
                <pre class="audit-line">{{ item.raw }}</pre>
              </template>
            </QCard>
          </section>

          <div v-if="!loading && auditGroups.length === 0" class="audit-empty">
            <p class="ui-kicker">{{ t("audit_title") }}</p>
            <h3 class="audit-empty-title">{{ t("audit_empty_title") }}</h3>
            <p class="audit-empty-copy">{{ t("audit_empty") }}</p>
          </div>
        </div>

        <div v-else-if="!loading" class="audit-empty">
          <p class="ui-kicker">{{ t("audit_title") }}</p>
          <h3 class="audit-empty-title">{{ t("audit_missing_title") }}</h3>
          <p class="audit-empty-copy">{{ t("audit_no_file") }}</p>
        </div>

        <RawJsonDialog
          :open="rawDialogOpen"
          :json="rawDialogJSON"
          @close="closeRawDialog"
        />
      </section>
    </AppPage>
  `,
};

export default AuditView;
