import { computed, onMounted, reactive, ref, watch } from "vue";
import "./AuditView.css";

import AppPage from "../components/AppPage";
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

const AuditView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const err = ref("");
    const pageValue = ref(1);
    const fileItems = ref([]);
    const selectedFile = ref("");
    const lines = ref([]);
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

    const selectedFileItem = computed(() => {
      return fileItems.value.find((item) => item.value === selectedFile.value) || fileItems.value[0] || null;
    });
    const auditItems = computed(() => {
      return lines.value
        .map((line, idx) => parseAuditLine(line, idx))
        .reverse();
    });
    const auditGroups = computed(() => {
      const groups = [];
      const byRunID = new Map();
      for (const item of auditItems.value) {
        const runID = item.parsed ? item.runID : "-";
        const groupKey = `run:${runID}`;
        let group = byRunID.get(groupKey);
        if (!group) {
          group = { key: groupKey, runID, items: [], latestTs: "-" };
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
      return text
        .replaceAll("_", " ")
        .replace(/([a-z0-9])([A-Z])/g, "$1 $2");
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

    function decisionLabel(raw) {
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

    function riskLabel(raw) {
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

    function parseAuditLine(line, idx) {
      const raw = typeof line === "string" ? line : String(line ?? "");
      const parsed = safeJSON(raw, null);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return {
          key: `${meta.from}-${idx}-raw`,
          parsed: false,
          raw,
        };
      }

      const eventID = normalizeAuditText(parsed.event_id);
      const tsRaw = normalizeAuditText(parsed.ts);
      const stepText = normalizeAuditText(parsed.step);
      const actionTypeRaw = normalizeAuditText(parsed.action_type);
      const toolName = normalizeAuditText(parsed.tool_name);
      const runID = normalizeAuditText(parsed.run_id);
      const actor = normalizeAuditText(parsed.actor);
      const approvalStatus = normalizeAuditText(parsed.approval_status);
      const summary = normalizeAuditText(parsed.action_summary_redacted);
      const reasons = normalizeAuditList(parsed.reasons);
      const reasonsText = reasons.length > 0 ? reasons.join(" | ") : "-";
      const decisionRaw = normalizeAuditText(parsed.decision, "");
      const riskRaw = normalizeAuditText(parsed.risk_level, "");

      return {
        key: `${meta.from}-${idx}-${eventID}`,
        parsed: true,
        eventID,
        tsText: tsRaw === "-" ? "-" : formatTime(tsRaw),
        actionType: humanizeAuditToken(actionTypeRaw),
        toolName,
        runID,
        stepText,
        actor,
        approvalStatus: humanizeAuditToken(approvalStatus),
        summary,
        reasonsText,
        decisionLabel: decisionLabel(decisionRaw),
        decisionType: decisionBadgeType(decisionRaw),
        riskLabel: riskLabel(riskRaw),
        riskType: riskBadgeType(riskRaw),
      };
    }

    async function loadFiles() {
      const data = await runtimeApiFetch("/audit/files");
      const items = Array.isArray(data.items) ? data.items : [];
      fileItems.value = items
        .map((it) => {
          const name = typeof it.name === "string" ? it.name.trim() : "";
          return {
            title: `${name} (${formatBytes(it.size_bytes)})`,
            value: name,
          };
        })
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
      auditGroups,
      meta,
      pageValue,
      lineWindowText,
      pageText,
      refreshLatest,
      goPage,
      goPrev,
      goNext,
      onFileChange,
      formatBytes,
    };
  },
  template: `
    <AppPage :title="t('audit_title')">
      <div class="toolbar wrap">
        <div class="tool-item">
          <QDropdownMenu
            :items="fileItems"
            :initialItem="selectedFileItem"
            :placeholder="t('placeholder_audit_file')"
            @change="onFileChange"
          />
        </div>
        <QButton
          class="outlined icon"
          :loading="loading"
          :title="t('action_refresh')"
          :aria-label="t('action_refresh')"
          @click="refreshLatest"
        >
          <QIconRefresh class="icon" />
        </QButton>
        <div
          v-if="meta.total_pages > 0"
          class="audit-pagination"
        >
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
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div v-if="meta.exists" class="audit-meta">
        <div class="audit-meta-item">
          <span class="audit-meta-label">{{ t("audit_size") }}</span>
          <strong class="audit-meta-value">{{ formatBytes(meta.size_bytes) }}</strong>
        </div>
        <div class="audit-meta-item">
          <span class="audit-meta-label">{{ t("audit_lines") }}</span>
          <strong class="audit-meta-value">{{ lineWindowText }}</strong>
        </div>
        <div class="audit-meta-item">
          <span class="audit-meta-label">{{ t("audit_page") }}</span>
          <strong class="audit-meta-value">{{ pageText }}</strong>
        </div>
      </div>
      <div class="audit-list">
        <div v-for="group in auditGroups" :key="group.key" class="audit-group">
          <div class="audit-group-head">
            <div class="audit-group-head-main">
              <span class="audit-group-label">{{ t("audit_run") }}</span>
              <code class="audit-group-run">{{ group.runID }}</code>
              <code v-if="group.latestTs !== '-'" class="audit-group-time">{{ group.latestTs }}</code>
            </div>
            <div class="audit-group-head-count">
              <strong class="audit-group-count-value">{{ group.items.length }}</strong>
              <span class="audit-group-count-label">{{ t("audit_group_count") }}</span>
            </div>
          </div>
          <div v-for="item in group.items" :key="item.key" class="audit-row">
            <template v-if="item.parsed">
              <div class="audit-item-head">
                <code class="audit-item-id">{{ item.eventID }}</code>
                <code class="audit-item-time">{{ t("audit_time") }}: {{ item.tsText }}</code>
                <QBadge :type="item.decisionType" size="sm" variant="filled">{{ item.decisionLabel }}</QBadge>
                <QBadge :type="item.riskType" size="sm" variant="filled">{{ item.riskLabel }}</QBadge>
              </div>
              <div class="audit-item-meta">
                <code>{{ t("audit_action") }}: {{ item.actionType }}</code>
                <code>{{ t("audit_tool") }}: {{ item.toolName }}</code>
                <code>{{ t("audit_step") }}: {{ item.stepText }}</code>
                <code v-if="item.approvalStatus !== '-'">{{ t("audit_approval") }}: {{ item.approvalStatus }}</code>
                <code v-if="item.actor !== '-'">{{ t("audit_actor") }}: {{ item.actor }}</code>
              </div>
              <code v-if="item.summary !== '-'" class="audit-summary">{{ t("audit_summary") }}: {{ item.summary }}</code>
              <code v-if="item.reasonsText !== '-'" class="audit-summary">{{ t("audit_reasons") }}: {{ item.reasonsText }}</code>
            </template>
            <template v-else>
              <div class="audit-item-head">
                <QBadge type="default" size="sm" variant="filled">{{ t("audit_raw") }}</QBadge>
              </div>
              <code class="audit-line">{{ item.raw }}</code>
            </template>
          </div>
        </div>
        <p v-if="!loading && auditGroups.length === 0" class="muted">{{ meta.exists ? t("audit_empty") : t("audit_no_file") }}</p>
      </div>
    </AppPage>
  `,
};


export default AuditView;
