import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./AuditView.css";

import AppPage from "../components/AppPage";
import RawJsonDialog from "../components/RawJsonDialog";
import { openRawJsonDesktopWindow } from "../core/desktop-windows";
import { endpointChannelLabel } from "../core/endpoints";
import { endpointRoutePath } from "../core/endpoint-routes";
import { loadResource, resourceKey, useResource } from "../core/resources";
import {
  TASK_STATUS_META,
  endpointState,
  formatTime,
  runtimeApiFetchFirstForEndpoints,
  runtimeApiFetchForEndpoint,
  runtimeEndpointByRef,
  safeJSON,
  taskEndpointRefsForSelection,
  toBool,
  toInt,
  translate,
} from "../core/context";

const AUDIT_ITEMS_PER_PAGE = 50;
const TASKS_PAGE_SIZE = 20;
const AUDIT_STREAM_VALUE = "audit";
const TASKS_STREAM_VALUE = "tasks";

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

function formatAuditStepMarker(value) {
  const text = normalizeAuditText(value, "");
  if (!text) {
    return "";
  }
  if (/^\d+$/.test(text)) {
    return text.padStart(2, "0");
  }
  return text;
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

function auditReasonLabel(t, raw) {
  const text = String(raw || "").trim().toLowerCase();
  switch (text) {
    case "bash_requires_approval":
      return t("audit_reason_bash_requires_approval");
    case "url_fetch_not_allowlisted":
      return t("audit_reason_url_fetch_not_allowlisted");
    case "invalid_url":
      return t("audit_reason_invalid_url");
    case "private_ip":
      return t("audit_reason_private_ip");
    case "non_allowlisted_domain":
      return t("audit_reason_non_allowlisted_domain");
    case "sensitive_content_redacted":
      return t("audit_reason_sensitive_content_redacted");
    case "redacted_private_key_block":
      return t("audit_reason_redacted_private_key_block");
    case "redacted_jwt":
      return t("audit_reason_redacted_jwt");
    case "redacted_bearer_token":
      return t("audit_reason_redacted_bearer_token");
    case "redacted_mister_morph_env":
      return t("audit_reason_redacted_mister_morph_env");
    case "redacted_secret_value":
      return t("audit_reason_redacted_secret_value");
    case "redacted_custom_pattern":
      return t("audit_reason_redacted_custom_pattern");
    default:
      if (text.startsWith("redacted_custom_pattern_")) {
        return t("audit_reason_redacted_custom_pattern_named", {
          name: humanizeAuditToken(text.slice("redacted_custom_pattern_".length)),
        });
      }
      return humanizeAuditToken(raw);
  }
}

function isOutputPublishSummaryPlaceholder(actionTypeRaw, summary) {
  return (
    String(actionTypeRaw || "").trim().toLowerCase() === "outputpublish" &&
    String(summary || "").trim() === "OutputPublish content=[redacted_summary]"
  );
}

function isBodyOmittedFromAudit(parsed, actionTypeRaw, summary) {
  return (
    toBool(parsed?.body_omitted_from_audit, false) ||
    isOutputPublishSummaryPlaceholder(actionTypeRaw, summary)
  );
}

function auditFamilyTitle(t, name) {
  const value = String(name || "").trim();
  if (!value) {
    return t("audit_stream_other");
  }
  if (value.startsWith("guard_audit.allow_with_redaction.jsonl")) {
    return t("audit_stream_allow_with_redaction");
  }
  if (value.startsWith("guard_audit.require_approval.jsonl")) {
    return t("audit_stream_require_approval");
  }
  if (value.startsWith("guard_audit.deny.jsonl")) {
    return t("audit_stream_deny");
  }
  if (value.startsWith("guard_audit.jsonl")) {
    return t("audit_stream_all");
  }
  return t("audit_stream_other");
}

function auditFamilyOrder(name) {
  const value = String(name || "").trim();
  if (value.startsWith("guard_audit.jsonl")) {
    return 0;
  }
  if (value.startsWith("guard_audit.require_approval.jsonl")) {
    return 1;
  }
  if (value.startsWith("guard_audit.allow_with_redaction.jsonl")) {
    return 2;
  }
  if (value.startsWith("guard_audit.deny.jsonl")) {
    return 3;
  }
  return 4;
}

function toAuditFileItem(t, item) {
  const name = String(item?.name || "").trim();
  return {
    key: name,
    value: name,
    name,
    title: auditFamilyTitle(t, name),
  };
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

const AuditView = {
  components: {
    AppPage,
    RawJsonDialog,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const loading = ref(false);
    const err = ref("");
    const isMobile = ref(false);
    const mobileLedgerVisible = ref(false);
    const selectedStream = ref(AUDIT_STREAM_VALUE);
    const pageValue = ref(1);
    const auditPageCursors = ref([""]);
    const fileItems = ref([]);
    const selectedFile = ref("");
    const lines = ref([]);
    const rawDialogOpen = ref(false);
    const rawDialogJSON = ref("");
    let initEndpointRef = "";
    let initPromise = null;
    let initToken = null;
    const meta = reactive({
      path: "",
      exists: false,
      size_bytes: 0,
      limit: AUDIT_ITEMS_PER_PAGE,
      has_next: false,
      next_cursor: "",
    });

    const selectedFileItem = computed(
      () => fileItems.value.find((item) => item.value === selectedFile.value) || fileItems.value[0] || null
    );
    const isTasksStreamSelected = computed(() => selectedStream.value === TASKS_STREAM_VALUE);
    const pageText = computed(() => {
      return `${pageValue.value}`;
    });
    const selectedFileTitle = computed(() => String(selectedFileItem.value?.title || "").trim() || t("audit_title"));
    const showIndexPane = computed(() => !isMobile.value || !mobileLedgerVisible.value);
    const showLedgerPane = computed(() => !isMobile.value || mobileLedgerVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobileLedgerVisible.value);
    const mobileBarTitle = computed(() => {
      if (!mobileShowBack.value) {
        return t("audit_title");
      }
      return isTasksStreamSelected.value ? t("tasks_title") : selectedFileTitle.value || t("audit_title");
    });
    const pageClass = computed(() => (isMobile.value ? "audit-page audit-page-mobile-split" : "audit-page"));
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
    const taskPageIndex = ref(0);
    const taskPageCursors = ref([""]);
    const taskNextCursor = ref("");
    const taskItems = ref([]);
    const taskErr = ref("");
    const tasksPageText = computed(() => `${taskPageIndex.value + 1}`);
    const currentTaskCursor = computed(() => String(taskPageCursors.value[taskPageIndex.value] || "").trim());
    const taskStatusTitleMap = computed(() => {
      const map = new Map();
      for (const item of TASK_STATUS_META) {
        map.set(item.value, t(item.titleKey));
      }
      return map;
    });
    function refreshMobileMode() {
      isMobile.value = typeof window !== "undefined" && window.innerWidth <= 920;
    }

    function showIndexView() {
      mobileLedgerVisible.value = false;
    }

    function isSelectedFileItem(item) {
      return (
        !isMobile.value &&
        selectedStream.value === AUDIT_STREAM_VALUE &&
        String(item?.value || "") === selectedFile.value
      );
    }

    function auditFileClass(item) {
      const classes = ["audit-index-item", "workspace-sidebar-item"];
      if (isSelectedFileItem(item)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function taskStreamClass() {
      const classes = ["audit-index-item", "workspace-sidebar-item"];
      if (!isMobile.value && isTasksStreamSelected.value) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function parseAuditLine(line, idx) {
      const raw = typeof line === "string" ? line : String(line ?? "");
      const parsed = safeJSON(raw, null);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return {
          key: `${idx}-raw`,
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
      const summaryRaw = normalizeAuditText(parsed.action_summary_redacted);
      const reasons = normalizeAuditList(parsed.reasons);
      const decisionRaw = normalizeAuditText(parsed.decision, "");
      const riskRaw = normalizeAuditText(parsed.risk_level, "");
      const bodyOmittedFromAudit = isBodyOmittedFromAudit(parsed, actionTypeRaw, summaryRaw);
      const summary = summaryRaw;
      let reasonsText = reasons.length > 0 ? reasons.map((reason) => auditReasonLabel(t, reason)).join(" | ") : "-";
      if (bodyOmittedFromAudit && reasonsText === "-") {
        reasonsText = t("audit_output_publish_reason");
      }
      const hasTool = toolName !== "-";
      const primaryTitle = hasTool ? toolName : actionType;
      const subtitleParts = [];
      if (hasTool && actionType !== "-") {
        subtitleParts.push(actionType);
      }
      if (actor !== "-") {
        subtitleParts.push(`${t("audit_actor")} ${actor}`);
      }
      const subtitle = subtitleParts.join(" · ");
      const formattedStep = formatAuditStepMarker(stepText);
      const stepMarker = formattedStep ? `${primaryTitle} / ${formattedStep}` : "";
      const metaTrail = [];
      if (approvalStatus !== "-") {
        metaTrail.push(`${t("audit_approval")} ${humanizeAuditToken(approvalStatus)}`);
      }

    return {
        key: `${idx}-${eventID}`,
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
        stepMarker,
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

    async function openRawDialog(item) {
      if (!item) {
        return;
      }
      const json = String(item.rawPretty || item.raw || "").trim();
      if (await openRawJsonDesktopWindow({ title: "RAW JSON", json }).catch(() => false)) {
        return;
      }
      rawDialogJSON.value = json;
      rawDialogOpen.value = true;
    }

    function closeRawDialog() {
      rawDialogOpen.value = false;
    }

    function currentEndpointRef() {
      return String(endpointState.selectedRef || "").trim();
    }

    function acceptsAuditLoad(token) {
      return !token || initToken === token;
    }

    function resetTaskPagination() {
      taskPageIndex.value = 0;
      taskPageCursors.value = [""];
      taskNextCursor.value = "";
    }

    watch(
      () => taskFeedEndpointRef.value,
      () => {
        resetTaskPagination();
        taskItems.value = [];
        taskErr.value = "";
      },
      { flush: "sync" }
    );

    const taskListResource = useResource({
      key: computed(() => resourceKey("tasks", "list", taskFeedEndpointRef.value, currentTaskCursor.value)),
      enabled: computed(() => isTasksStreamSelected.value && Boolean(taskFeedEndpointRef.value)),
      initialData: null,
      load: async () => {
        const endpointRef = String(taskFeedEndpointRef.value || "").trim();
        const cursor = currentTaskCursor.value;
        const q = new URLSearchParams();
        q.set("limit", String(TASKS_PAGE_SIZE));
        if (cursor) {
          q.set("cursor", cursor);
        }
        const endpoint = runtimeEndpointByRef(endpointRef);
        const data = await runtimeApiFetchForEndpoint(endpointRef, `/tasks?${q.toString()}`);
        return {
          endpoint,
          endpointRef,
          items: Array.isArray(data?.items) ? data.items : [],
          nextCursor: String(data?.next_cursor || "").trim(),
        };
      },
    });
    const taskLoading = taskListResource.loading;

    watch(
      () => taskListResource.data.value,
      (payload) => {
        if (!payload) {
          if (!taskFeedEndpointRef.value) {
            taskItems.value = [];
            taskNextCursor.value = "";
          }
          return;
        }
        const endpoint = payload.endpoint;
        const endpointRef = String(payload.endpointRef || "").trim();
        const sourceLabel = endpointChannelLabel(endpoint?.mode, t);
        taskItems.value = payload.items.map((item) => ({
          ...item,
          source_label: sourceLabel,
          source_mode: endpoint?.mode || "",
          source_name: String(endpoint?.name || "").trim(),
          source_endpoint_ref: endpointRef,
        }));
        taskNextCursor.value = payload.nextCursor;
      },
      { immediate: true }
    );

    watch(
      () => taskListResource.error.value,
      (error) => {
        if (error) {
          taskErr.value = error.message || t("msg_load_failed");
        }
      }
    );

    function selectTaskStream() {
      selectedStream.value = TASKS_STREAM_VALUE;
      if (isMobile.value) {
        mobileLedgerVisible.value = true;
      }
    }

    async function loadTaskStream() {
      taskErr.value = "";
      return taskListResource.refresh({ force: true });
    }

    function prevTaskPage() {
      if (taskPageIndex.value <= 0) {
        return;
      }
      taskPageIndex.value -= 1;
    }

    function nextTaskPage() {
      const cursor = String(taskNextCursor.value || "").trim();
      if (!cursor) {
        return;
      }
      const nextPageIndex = taskPageIndex.value + 1;
      const nextHistory = taskPageCursors.value.slice(0, nextPageIndex);
      nextHistory[nextPageIndex] = cursor;
      taskPageCursors.value = nextHistory;
      taskPageIndex.value = nextPageIndex;
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
      taskErr.value = "";
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
        taskErr.value = e.message || t("msg_load_failed");
      }
    }

    function goChat() {
      router.push(endpointRoutePath(endpointState.selectedRef, "/chat"));
    }

    async function loadFiles(endpointRef = currentEndpointRef(), token = null) {
      const data = await loadResource(resourceKey("audit", "files", endpointRef), () =>
        runtimeApiFetchForEndpoint(endpointRef, "/audit/files")
      );
      if (!acceptsAuditLoad(token)) {
        return false;
      }
      const items = Array.isArray(data.items) ? data.items : [];
      fileItems.value = items
        .map((it) => toAuditFileItem(t, it))
        .filter((it) => it.value !== "")
        .sort((left, right) => auditFamilyOrder(left.name) - auditFamilyOrder(right.name));

      const preferred = typeof data.default_file === "string" ? data.default_file.trim() : "";
      if (fileItems.value.length === 0) {
        selectedFile.value = preferred;
        return true;
      }
      if (fileItems.value.find((it) => it.value === selectedFile.value)) {
        return true;
      }
      if (preferred && fileItems.value.find((it) => it.value === preferred)) {
        selectedFile.value = preferred;
        return true;
      }
      selectedFile.value = fileItems.value[0].value;
      return true;
    }

    async function loadChunk(cursor = "", endpointRef = currentEndpointRef(), token = null) {
      loading.value = true;
      err.value = "";
      try {
        const q = new URLSearchParams();
        if (selectedFile.value) {
          q.set("file", selectedFile.value);
        }
        q.set("limit", String(AUDIT_ITEMS_PER_PAGE));
        const normalizedCursor = String(cursor || "").trim();
        if (normalizedCursor) {
          q.set("cursor", normalizedCursor);
        }
        const path = `/audit/logs?${q.toString()}`;
        const data = await loadResource(resourceKey("audit", "logs", endpointRef, path), () =>
          runtimeApiFetchForEndpoint(endpointRef, path)
        );
        if (!acceptsAuditLoad(token)) {
          return;
        }
        meta.path = data.path || "";
        meta.exists = toBool(data.exists, false);
        meta.size_bytes = toInt(data.size_bytes, 0);
        meta.limit = toInt(data.limit, AUDIT_ITEMS_PER_PAGE);
        meta.has_next = toBool(data.has_next, false);
        meta.next_cursor = String(data.next_cursor || "").trim();
        const fetchedLines = Array.isArray(data.items) ? data.items : [];
        lines.value = fetchedLines.slice(-AUDIT_ITEMS_PER_PAGE);
        return true;
      } catch (e) {
        if (acceptsAuditLoad(token)) {
          err.value = e.message || t("msg_load_failed");
        }
        return false;
      } finally {
        if (acceptsAuditLoad(token)) {
          loading.value = false;
        }
      }
    }

    async function refreshLatest(endpointRef = currentEndpointRef(), token = null) {
      if (await loadChunk("", endpointRef, token)) {
        auditPageCursors.value = [""];
        pageValue.value = 1;
      }
    }

    async function goPrev() {
      if (loading.value || pageValue.value <= 1) {
        return;
      }
      const target = pageValue.value - 1;
      const cursor = auditPageCursors.value[target - 1] || "";
      if (await loadChunk(cursor)) {
        pageValue.value = target;
      }
    }

    async function goNext() {
      const cursor = String(meta.next_cursor || "").trim();
      if (loading.value || !meta.has_next || !cursor) {
        return;
      }
      if (await loadChunk(cursor)) {
        auditPageCursors.value = auditPageCursors.value.slice(0, pageValue.value);
        auditPageCursors.value.push(cursor);
        pageValue.value += 1;
      }
    }

    async function onFileChange(item) {
      if (!item || typeof item !== "object" || typeof item.value !== "string") {
        return;
      }
      selectedStream.value = AUDIT_STREAM_VALUE;
      if (item.value === selectedFile.value) {
        if (isMobile.value) {
          mobileLedgerVisible.value = true;
        }
        return;
      }
      selectedFile.value = item.value;
      if (isMobile.value) {
        mobileLedgerVisible.value = true;
      }
      await refreshLatest();
    }

    async function init() {
      const endpointRef = currentEndpointRef();
      if (initPromise && initEndpointRef === endpointRef) {
        return initPromise;
      }
      initEndpointRef = endpointRef;
      const token = {};
      initToken = token;
      const promise = (async () => {
        try {
          const loaded = await loadFiles(endpointRef, token);
          if (!loaded) {
            return;
          }
        } catch (e) {
          if (acceptsAuditLoad(token)) {
            err.value = e.message || t("msg_load_failed");
          }
        }
        await refreshLatest(endpointRef, token);
      })();
      initPromise = promise;
      try {
        return await promise;
      } finally {
        if (initPromise === promise) {
          initPromise = null;
        }
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      void init();
    });
    onUnmounted(() => {
      initToken = {};
      window.removeEventListener("resize", refreshMobileMode);
    });
    watch(
      () => endpointState.selectedRef,
      () => {
        void init();
      }
    );

      return {
        t,
        formatTime,
        loading,
        err,
        isMobile,
        mobileShowBack,
        mobileBarTitle,
        pageClass,
        fileItems,
        selectedFileItem,
        isTasksStreamSelected,
        auditGroups,
        selectedFileTitle,
        meta,
        pageValue,
        pageText,
        showIndexPane,
        showLedgerPane,
        isSelectedFileItem,
        auditFileClass,
        taskStreamClass,
        selectTaskStream,
        showIndexView,
        refreshLatest,
        goPrev,
        goNext,
        onFileChange,
        taskItems,
        taskErr,
        taskLoading,
        loadTaskStream,
        prevTaskPage,
        nextTaskPage,
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
        hasPrevTaskPage: computed(() => taskPageIndex.value > 0),
        hasNextTaskPage: computed(() => String(taskNextCursor.value || "").trim() !== ""),
        rawDialogOpen,
        rawDialogJSON,
        openRawDialog,
        closeRawDialog,
      };
  },
  template: `
    <AppPage
      :title="t('audit_title')"
      :class="pageClass"
      :hideDesktopBar="true"
      :hideMobileBar="showIndexPane"
    >
      <template #leading>
        <div class="audit-page-bar">
          <QButton
            v-if="mobileShowBack"
            class="plain xs icon audit-page-bar-back"
            :title="t('audit_title')"
            :aria-label="t('audit_title')"
            @click="showIndexView"
          >
            <QIconArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ mobileBarTitle }}</h2>
        </div>
      </template>
      <div class="audit-workbench">
        <aside v-if="showIndexPane" class="audit-index workspace-sidebar-section" :aria-label="t('audit_title')">
          <div class="audit-index-head workspace-sidebar-head">
            <h3 class="audit-index-title workspace-section-title">{{ t("audit_title") }}</h3>
          </div>
          <div class="audit-index-scroll">
            <section class="audit-index-group">
              <div class="audit-index-items workspace-sidebar-list">
                <button
                  v-for="item in fileItems"
                  :key="item.key"
                  type="button"
                  :class="auditFileClass(item)"
                  @click="onFileChange(item)"
                >
                  <span class="workspace-sidebar-item-copy">
                    <span class="audit-index-item-name workspace-sidebar-item-title">{{ item.title }}</span>
                  </span>
                  <span class="workspace-sidebar-item-marker" aria-hidden="true">
                    <QBadge v-if="isSelectedFileItem(item)" dot type="primary" size="sm" />
                  </span>
                </button>
              </div>
            </section>
            <section class="audit-index-group">
              <div class="audit-index-items workspace-sidebar-list">
                <button
                  type="button"
                  :class="taskStreamClass()"
                  @click="selectTaskStream"
                >
                  <span class="workspace-sidebar-item-copy">
                    <span class="audit-index-item-name workspace-sidebar-item-title">{{ t("tasks_title") }}</span>
                  </span>
                  <span class="workspace-sidebar-item-marker" aria-hidden="true">
                    <QBadge v-if="!isMobile && isTasksStreamSelected" dot type="primary" size="sm" />
                  </span>
                </button>
              </div>
            </section>
          </div>
        </aside>

        <section v-if="showLedgerPane && !isTasksStreamSelected" class="audit-ledger">
        <header class="audit-ledger-head">
          <div class="audit-ledger-copy">
            <h3 class="audit-ledger-title workspace-document-title">{{ selectedFileTitle }}</h3>
          </div>
          <div class="audit-ledger-actions">
            <QButton
              class="plain sm icon"
              :loading="loading"
              :title="t('action_refresh')"
              :aria-label="t('action_refresh')"
              @click="refreshLatest"
            >
              <QIconRefresh class="icon" />
            </QButton>
            <div v-if="meta.exists && (auditGroups.length > 0 || pageValue > 1)" class="audit-pagination">
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
                :disabled="!meta.has_next || !meta.next_cursor"
                :title="t('audit_older')"
                :aria-label="t('audit_older')"
                @click="goNext"
              >
                <QIconArrowRight class="icon" />
              </QButton>
            </div>
          </div>
        </header>

        <QProgress v-if="loading" :infinite="true" />
        <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

        <div v-if="meta.exists" class="audit-feed">
          <section v-for="group in auditGroups" :key="group.key" class="audit-group">
            <QDivider class="audit-group-divider" :label="group.latestTs !== '-' ? group.latestTs : ''" />
            <div class="audit-group-meta">
              <code class="audit-group-run-id">{{ group.title }}</code>
              <span class="audit-group-count">{{ group.items.length }} {{ t("audit_group_count") }}</span>
            </div>

            <QCard
              v-for="item in group.items"
              :key="item.key"
              class="audit-row audit-item-card clickable"
              :variant="item.parsed ? 'annotated' : 'default'"
              :marker="item.parsed ? item.stepMarker : ''"
              marker-style="plate"
              :hoverable="true"
              tabindex="0"
              role="button"
              :aria-label="t('chat_action_show_raw')"
              @click="openRawDialog(item)"
              @keydown.enter.prevent="openRawDialog(item)"
              @keydown.space.prevent="openRawDialog(item)"
            >
              <template #header>
                <div class="audit-item-head" v-if="item.parsed">
                  <code v-if="item.eventID !== '-'" class="audit-item-event-id">{{ item.eventID }}</code>
                  <p v-if="item.actionType !== '-'" class="audit-item-action-type">{{ item.actionType }}</p>
                  <span v-if="item.tsText !== '-'" class="audit-item-time">{{ item.tsText }}</span>
                </div>

                <div class="audit-item-head" v-else>
                  <p class="audit-item-action-type">{{ t("audit_raw") }}</p>
                </div>
              </template>

              <template v-if="item.parsed">
                <p v-if="item.summary !== '-'" class="audit-item-summary">{{ item.summary }}</p>

                <div class="audit-item-footer">
                  <p class="audit-item-reasons" :class="{ 'is-empty': item.reasonsText === '-' }">
                    {{ item.reasonsText === '-' ? '\u00A0' : item.reasonsText }}
                  </p>
                  <div class="audit-item-badges">
                    <QBadge :type="item.decisionType">{{ item.decisionLabel }}</QBadge>
                    <QBadge :type="item.riskType">{{ item.riskLabel }}</QBadge>
                  </div>
                </div>
              </template>

              <template v-else>
                <pre class="audit-line">{{ item.raw }}</pre>
              </template>
            </QCard>
          </section>

          <div v-if="!loading && auditGroups.length === 0" class="audit-empty">
            <h3 class="audit-empty-title">{{ t("audit_empty_title") }}</h3>
            <p class="audit-empty-copy">{{ t("audit_empty") }}</p>
          </div>
        </div>

        <div v-else-if="!loading" class="audit-empty">
          <h3 class="audit-empty-title">{{ t("audit_missing_title") }}</h3>
          <p class="audit-empty-copy">{{ t("audit_no_file") }}</p>
        </div>
        </section>

        <section v-if="showLedgerPane && isTasksStreamSelected" class="audit-ledger">
          <header class="audit-ledger-head">
            <div class="audit-ledger-copy">
              <h3 class="audit-ledger-title workspace-document-title">{{ t("tasks_title") }}</h3>
            </div>
            <div class="audit-ledger-actions">
              <QButton
                class="plain sm icon"
                :loading="taskLoading"
                :title="t('action_refresh')"
                :aria-label="t('action_refresh')"
                @click="loadTaskStream"
              >
                <QIconRefresh class="icon" />
              </QButton>
              <div class="audit-pagination">
                <QButton
                  class="plain sm icon"
                  :disabled="!hasPrevTaskPage"
                  :title="t('audit_newer')"
                  :aria-label="t('audit_newer')"
                  @click="prevTaskPage"
                >
                  <QIconArrowLeft class="icon" />
                </QButton>
                <code class="audit-page-indicator">{{ tasksPageText }}</code>
                <QButton
                  class="plain sm icon"
                  :disabled="!hasNextTaskPage"
                  :title="t('audit_older')"
                  :aria-label="t('audit_older')"
                  @click="nextTaskPage"
                >
                  <QIconArrowRight class="icon" />
                </QButton>
              </div>
            </div>
          </header>

          <QProgress v-if="taskLoading" :infinite="true" />
          <QFence v-if="taskErr" type="danger" icon="QIconCloseCircle" :text="taskErr" />

          <div class="stack audit-task-stream">
            <QCard
              v-for="item in taskItems"
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
            <QCard v-if="taskItems.length === 0 && !taskLoading" class="task-empty" variant="default">
              <div class="task-empty-copy">
                <code class="task-empty-kicker">{{ t("tasks_title") }}</code>
                <h3 class="task-empty-title">{{ t("tasks_empty_title") }}</h3>
                <p class="task-empty-hint">{{ t("tasks_empty_hint") }}</p>
              </div>
              <template #footer>
                <QButton class="plain sm" @click="goChat">{{ t("tasks_empty_action") }}</QButton>
              </template>
            </QCard>
          </div>
        </section>

        <RawJsonDialog
          :open="rawDialogOpen"
          :json="rawDialogJSON"
          @close="closeRawDialog"
        />
      </div>
    </AppPage>
  `,
};

export default AuditView;
