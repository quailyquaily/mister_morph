import { useToast } from "quail-ui";
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import "./RuntimeView.css";

import AppDialogShell from "../components/AppDialogShell";
import PokeDialogContent from "../components/PokeDialogContent";
import { onDesktopWindowMessage } from "../core/desktop-runtime";
import { openPokeDesktopWindow } from "../core/desktop-windows";
import { endpointDisplayItem, endpointChannelLabel } from "../core/endpoints";
import {
  currentLocale,
  endpointState,
  formatBytes,
  formatTime,
  loadEndpoints,
  runtimeApiFetch,
  runtimeEndpointByRef,
  toBool,
  toInt,
  translate,
} from "../core/context";

function stringValue(value, fallback = "-") {
  const text = String(value || "").trim();
  return text || fallback;
}

function formatUptime(seconds) {
  const total = Number(seconds || 0);
  if (!Number.isFinite(total) || total < 0) {
    return "-";
  }
  const whole = Math.trunc(total);
  const days = Math.floor(whole / 86400);
  const hours = Math.floor((whole % 86400) / 3600);
  const minutes = Math.floor((whole % 3600) / 60);
  const secs = whole % 60;
  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${secs}s`;
  }
  return `${secs}s`;
}

function formatGlanceTimestamp(ts, fallback = "-") {
  const raw = String(ts || "").trim();
  if (!raw) {
    return fallback;
  }
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) {
    return fallback;
  }
  try {
    return new Intl.DateTimeFormat(currentLocale(), {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }).format(d);
  } catch {
    return formatTime(raw);
  }
}

function normalizeHealth(value) {
  return String(value || "").trim().toLowerCase();
}

const POKE_BODY_LIMIT = 10 * 1024;

function utf8ByteLength(value) {
  return new TextEncoder().encode(String(value || "")).length;
}

function healthBadgeType(value) {
  switch (normalizeHealth(value)) {
    case "":
    case "ok":
    case "healthy":
    case "ready":
      return "success";
    case "warn":
    case "warning":
    case "degraded":
      return "warning";
    default:
      return "danger";
  }
}

function channelStatusType(configured, running) {
  if (running) {
    return "success";
  }
  if (configured) {
    return "primary";
  }
  return "default";
}

function runtimeRows(t, overview) {
  return [
    { key: "go", label: t("stat_go_version"), value: stringValue(overview.runtime_go_version) },
    { key: "goroutines", label: t("stat_goroutines"), value: String(overview.runtime_goroutines || 0) },
    { key: "heap_alloc", label: t("stat_heap_alloc"), value: formatBytes(overview.runtime_heap_alloc_bytes) },
    { key: "heap_sys", label: t("stat_heap_sys"), value: formatBytes(overview.runtime_heap_sys_bytes) },
    { key: "heap_objects", label: t("stat_heap_objects"), value: String(overview.runtime_heap_objects || 0) },
    { key: "gc", label: t("stat_gc_cycles"), value: String(overview.runtime_gc_cycles || 0) },
  ];
}

const RuntimePanel = {
  components: {
    AppDialogShell,
    PokeDialogContent,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const err = ref("");
    const loading = ref(false);
    const poking = ref(false);
    const pokeDialogOpen = ref(false);
    const pokeBody = ref("");
    const pokeError = ref("");
    let refreshTimer = null;
    let removeDesktopWindowMessage = null;

    const overview = reactive({
      version: "-",
      started_at: "",
      uptime_sec: 0,
      health: "ok",
      mode: "",
      agent_name: "",
      poke_enabled: false,
      awareness_running: false,
      instance_id: "",
      last_poke_at: "",
      llm_provider: "-",
      llm_model: "-",
      channel_telegram_configured: false,
      channel_slack_configured: false,
      channel_line_configured: false,
      channel_lark_configured: false,
	  channel_mixin_configured: false,
      channel_running_telegram: false,
      channel_running_slack: false,
      channel_running_line: false,
      channel_running_lark: false,
	  channel_running_mixin: false,
      runtime_go_version: "-",
      runtime_goroutines: 0,
      runtime_heap_alloc_bytes: 0,
      runtime_heap_sys_bytes: 0,
      runtime_heap_objects: 0,
      runtime_gc_cycles: 0,
    });

    const selectedEndpoint = computed(() => runtimeEndpointByRef(endpointState.selectedRef));
    const endpointMeta = computed(() => {
      const item = selectedEndpoint.value;
      return item ? endpointDisplayItem(item, t) : null;
    });
    const modeLabel = computed(() =>
      endpointChannelLabel(overview.mode || selectedEndpoint.value?.mode || "", t)
    );
    const heroTitle = computed(() => {
      const name = String(overview.agent_name || "").trim();
      if (name) {
        return name;
      }
      return endpointMeta.value?.title || t("runtime_title");
    });
    const heroMeta = computed(() => {
      const parts = [];
      if (selectedEndpoint.value?.endpoint_ref) {
        parts.push(selectedEndpoint.value.endpoint_ref);
      }
      if (endpointMeta.value?.location) {
        parts.push(endpointMeta.value.location);
      }
      return parts.join(" · ");
    });
    const glanceItems = computed(() => [
      { key: "uptime", label: t("stat_uptime"), value: formatUptime(overview.uptime_sec) },
      {
        key: "started",
        label: t("stat_started"),
        value: formatGlanceTimestamp(overview.started_at),
      },
      {
        key: "poke",
        label: t("runtime_field_last_poke"),
        value: overview.last_poke_at
          ? formatGlanceTimestamp(overview.last_poke_at)
          : t("runtime_status_never"),
      },
    ]);
    const basicRows = computed(() => [
      { key: "endpoint", label: t("runtime_field_endpoint"), value: endpointMeta.value?.title || "-" },
      { key: "agent", label: t("runtime_field_agent"), value: stringValue(overview.agent_name) },
      { key: "location", label: t("runtime_field_location"), value: endpointMeta.value?.location || "-" },
      { key: "mode", label: t("endpoint_label_mode"), value: modeLabel.value },
      { key: "health", label: t("stat_health"), value: stringValue(overview.health, "ok"), tone: healthBadgeType(overview.health) },
      { key: "version", label: t("stat_version"), value: stringValue(overview.version) },
      { key: "instance", label: t("runtime_field_instance"), value: stringValue(overview.instance_id) },
      { key: "started", label: t("stat_started"), value: formatTime(overview.started_at) },
      { key: "uptime", label: t("stat_uptime"), value: formatUptime(overview.uptime_sec) },
      {
        key: "last_poke",
        label: t("runtime_field_last_poke"),
        value: overview.last_poke_at ? formatTime(overview.last_poke_at) : t("runtime_status_never"),
      },
    ]);
    const routeRows = computed(() => [
      { key: "provider", label: t("stat_llm_provider"), value: stringValue(overview.llm_provider) },
      { key: "model", label: t("stat_llm_model"), value: stringValue(overview.llm_model) },
    ]);
    const channelRows = computed(() => [
      {
        key: "telegram",
        title: t("endpoint_channel_telegram"),
        configured: overview.channel_telegram_configured,
        running: overview.channel_running_telegram,
      },
      {
        key: "slack",
        title: t("endpoint_channel_slack"),
        configured: overview.channel_slack_configured,
        running: overview.channel_running_slack,
      },
      {
        key: "line",
        title: t("endpoint_channel_line"),
        configured: overview.channel_line_configured,
        running: overview.channel_running_line,
      },
      {
        key: "lark",
        title: t("endpoint_channel_lark"),
        configured: overview.channel_lark_configured,
        running: overview.channel_running_lark,
      },
	  {
		key: "mixin",
		title: t("endpoint_channel_mixin"),
		configured: overview.channel_mixin_configured,
		running: overview.channel_running_mixin,
	  },
    ]);
    const runtimeMetrics = computed(() => runtimeRows(t, overview));
    const canPoke = computed(() => toBool(overview.poke_enabled, false));
    const awarenessRunning = computed(() => toBool(overview.awareness_running, false));
    const pokeDisabled = computed(() => poking.value || awarenessRunning.value);
    const pokeBodyBytes = computed(() => utf8ByteLength(pokeBody.value));
    const pokeBodyTooLarge = computed(() => pokeBodyBytes.value > POKE_BODY_LIMIT);
    const pokeSubmitDisabled = computed(() => pokeDisabled.value || !String(pokeBody.value || "").trim() || pokeBodyTooLarge.value);
    const pokeSizeLabel = computed(() =>
      t("runtime_poke_size", { used: formatBytes(pokeBodyBytes.value), limit: formatBytes(POKE_BODY_LIMIT) })
    );
    const pokeHelperText = computed(() => {
      if (pokeError.value) {
        return pokeError.value;
      }
      if (pokeBodyTooLarge.value) {
        return t("runtime_poke_too_large");
      }
      return t("runtime_poke_limit", { limit: formatBytes(POKE_BODY_LIMIT) });
    });

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        await loadEndpoints();
        if (!endpointState.selectedRef) {
          return;
        }
        const data = await runtimeApiFetch("/overview");
        overview.version = data.version || "-";
        overview.started_at = data.started_at || "";
        overview.uptime_sec = toInt(data.uptime_sec, 0);
        overview.health = data.health || "ok";
        overview.mode = data.mode || "";
        overview.agent_name = data.agent_name || "";
        overview.poke_enabled = toBool(data.poke_enabled, false);
        overview.awareness_running = toBool(data.awareness_running, false);
        overview.instance_id = data.instance_id || "";
        overview.last_poke_at = data.last_poke_at || "";
        const llm = data && typeof data.llm === "object" ? data.llm : {};
        overview.llm_provider = llm.provider || "-";
        overview.llm_model = llm.model || "-";
        const channel = data && typeof data.channel === "object" ? data.channel : {};
        const runningChannel = String(channel.running || "").trim();
        const telegramRunning = toBool(channel.telegram_running, false) || runningChannel === "telegram";
        const slackRunning = toBool(channel.slack_running, false) || runningChannel === "slack";
        const lineRunning = toBool(channel.line_running, false) || runningChannel === "line";
        const larkRunning = toBool(channel.lark_running, false) || runningChannel === "lark";
		const mixinRunning = toBool(channel.mixin_running, false) || runningChannel === "mixin";
        overview.channel_running_telegram = telegramRunning;
        overview.channel_running_slack = slackRunning;
        overview.channel_running_line = lineRunning;
        overview.channel_running_lark = larkRunning;
		overview.channel_running_mixin = mixinRunning;
        overview.channel_telegram_configured = toBool(channel.telegram_configured, false) || telegramRunning;
        overview.channel_slack_configured = toBool(channel.slack_configured, false) || slackRunning;
        overview.channel_line_configured = toBool(channel.line_configured, false) || lineRunning;
        overview.channel_lark_configured = toBool(channel.lark_configured, false) || larkRunning;
		overview.channel_mixin_configured = toBool(channel.mixin_configured, false) || mixinRunning;
        const rt = data && typeof data.runtime === "object" ? data.runtime : {};
        overview.runtime_go_version = rt.go_version || "-";
        overview.runtime_goroutines = toInt(rt.goroutines, 0);
        overview.runtime_heap_alloc_bytes = toInt(rt.heap_alloc_bytes, 0);
        overview.runtime_heap_sys_bytes = toInt(rt.heap_sys_bytes, 0);
        overview.runtime_heap_objects = toInt(rt.heap_objects, 0);
        overview.runtime_gc_cycles = toInt(rt.gc_cycles, 0);
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function openPokeDialog() {
      if (pokeDisabled.value) {
        return;
      }
      if (await openPokeDesktopWindow({ title: t("runtime_poke_dialog_title") }).catch(() => false)) {
        return;
      }
      pokeBody.value = "";
      pokeError.value = "";
      pokeDialogOpen.value = true;
    }

    function closePokeDialog() {
      if (poking.value) {
        return;
      }
      pokeDialogOpen.value = false;
      pokeError.value = "";
    }

    function updatePokeBody(value) {
      pokeBody.value = String(value || "");
      if (pokeError.value) {
        pokeError.value = "";
      }
    }

    async function submitPoke() {
      const body = String(pokeBody.value || "").trim();
      if (!body) {
        pokeError.value = t("runtime_poke_empty");
        return;
      }
      if (utf8ByteLength(body) > POKE_BODY_LIMIT) {
        pokeError.value = t("runtime_poke_too_large");
        return;
      }
      poking.value = true;
      try {
        const data = await runtimeApiFetch("/poke", {
          method: "POST",
          headers: { "Content-Type": "text/plain; charset=utf-8" },
          body,
        });
        overview.last_poke_at = typeof data?.poked_at === "string" ? data.poked_at : overview.last_poke_at;
        pokeDialogOpen.value = false;
        pokeBody.value = "";
        pokeError.value = "";
        toast.success(t("runtime_poke_ok"));
      } catch (e) {
        const message = e.message || t("msg_load_failed");
        if (e?.status === 400 || e?.status === 413) {
          pokeError.value = message;
        } else {
          toast.error(message);
        }
      } finally {
        poking.value = false;
      }
    }

    function handleDesktopWindowMessage(message) {
      if (message?.type !== "runtime:poke-submitted") {
        return;
      }
      if (typeof message?.payload?.poked_at === "string" && message.payload.poked_at) {
        overview.last_poke_at = message.payload.poked_at;
      }
    }

    onMounted(() => {
      removeDesktopWindowMessage = onDesktopWindowMessage(handleDesktopWindowMessage);
      void load();
      refreshTimer = window.setInterval(() => {
        void load();
      }, 60000);
    });

    watch(
      () => endpointState.selectedRef,
      () => {
        void load();
      }
    );

    onUnmounted(() => {
      if (refreshTimer !== null) {
        window.clearInterval(refreshTimer);
        refreshTimer = null;
      }
      if (removeDesktopWindowMessage) {
        removeDesktopWindowMessage();
        removeDesktopWindowMessage = null;
      }
    });

    return {
      t,
      err,
      loading,
      poking,
      pokeDialogOpen,
      pokeBody,
      pokeError,
      overview,
      heroTitle,
      heroMeta,
      modeLabel,
      glanceItems,
      basicRows,
      routeRows,
      channelRows,
      runtimeMetrics,
      canPoke,
      pokeDisabled,
      pokeBodyTooLarge,
      pokeSubmitDisabled,
      pokeSizeLabel,
      pokeHelperText,
      load,
      openPokeDialog,
      closePokeDialog,
      updatePokeBody,
      submitPoke,
      healthBadgeType,
      channelStatusType,
    };
  },
  template: `
    <div class="runtime-panel">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="PhXCircle" :text="err" />

      <section class="runtime-page">
        <header class="runtime-hero block-default">
          <div class="runtime-hero-copy">
            <p v-if="heroMeta" class="runtime-hero-meta">{{ heroMeta }}</p>
          </div>

          <section class="runtime-hero-spotlight">
            <span class="runtime-hero-primary-label">{{ modeLabel }}</span>
            <div class="runtime-hero-title-row">
              <h2 class="runtime-hero-title workspace-document-title">{{ heroTitle }}</h2>
              <QBadge :type="healthBadgeType(overview.health)" size="sm">{{ overview.health || "ok" }}</QBadge>
            </div>
          </section>

          <div class="runtime-hero-aside">
            <div v-if="canPoke" class="runtime-hero-actions">
              <QButton class="plain sm runtime-poke-button" :loading="poking" :disabled="pokeDisabled" @click="openPokeDialog">
                {{ t("runtime_action_poke") }}
              </QButton>
            </div>
            <div class="runtime-hero-rail">
              <article v-for="item in glanceItems" :key="item.key" class="runtime-glance-item">
                <span class="runtime-glance-label">{{ item.label }}</span>
                <span class="runtime-glance-value">{{ item.value }}</span>
              </article>
            </div>
          </div>
        </header>

        <div class="runtime-grid">
          <div class="runtime-dossier-stack">
            <QCard class="runtime-dossier-card" variant="default">
              <template #header>
                <div class="runtime-card-head">
                  <h3 class="ui-kicker">{{ t("group_basic") }}</h3>
                </div>
              </template>

              <section class="runtime-ledger-section">
                <div v-for="item in basicRows" :key="item.key" class="runtime-ledger-row">
                  <span class="runtime-ledger-label">{{ item.label }}</span>
                  <div class="runtime-ledger-value">
                    <QBadge v-if="item.tone" :type="item.tone" size="sm">{{ item.value }}</QBadge>
                    <span v-else>{{ item.value }}</span>
                  </div>
                </div>
              </section>
            </QCard>

            <QCard class="runtime-dossier-card" variant="default">
              <template #header>
                <div class="runtime-card-head">
                  <h3 class="ui-kicker">{{ t("group_model") }}</h3>
                </div>
              </template>

              <section class="runtime-ledger-section">
                <div v-for="item in routeRows" :key="item.key" class="runtime-ledger-row">
                  <span class="runtime-ledger-label">{{ item.label }}</span>
                  <div class="runtime-ledger-value">
                    <QBadge v-if="item.tone" :type="item.tone" size="sm">{{ item.value }}</QBadge>
                    <span v-else>{{ item.value }}</span>
                  </div>
                </div>
              </section>
            </QCard>

            <QCard class="runtime-dossier-card" variant="default">
              <template #header>
                <div class="runtime-card-head">
                  <h3 class="ui-kicker">{{ t("group_channels") }}</h3>
                </div>
              </template>

              <section class="runtime-ledger-section">
                <div v-for="item in channelRows" :key="item.key" class="runtime-channel-row">
                  <span class="runtime-ledger-label">{{ item.title }}</span>
                  <div class="runtime-channel-badges">
                    <QBadge :type="item.configured ? 'primary' : 'default'" size="sm">
                      {{ item.configured ? t("runtime_status_configured") : t("runtime_status_not_configured") }}
                    </QBadge>
                    <QBadge :type="channelStatusType(item.configured, item.running)" size="sm">
                      {{ item.running ? t("runtime_status_running") : t("runtime_status_idle") }}
                    </QBadge>
                  </div>
                </div>
              </section>
            </QCard>
          </div>

          <QCard class="runtime-metrics-card" variant="default">
            <template #header>
              <div class="runtime-card-head">
                <h3 class="ui-kicker">{{ t("group_runtime") }}</h3>
              </div>
            </template>
            <section class="runtime-ledger-section">
              <div v-for="item in runtimeMetrics" :key="item.key" class="runtime-ledger-row">
                <span class="runtime-ledger-label">{{ item.label }}</span>
                <div class="runtime-ledger-value">
                  <span>{{ item.value }}</span>
                </div>
              </div>
            </section>
          </QCard>
        </div>
      </section>

      <AppDialogShell
        :modelValue="pokeDialogOpen"
        :title="t('runtime_poke_dialog_title')"
        width="640px"
        :closeDisabled="poking"
        @close="closePokeDialog"
      >
        <PokeDialogContent
          :body="pokeBody"
          :bodyTooLarge="pokeBodyTooLarge"
          :disabled="poking"
          :error="pokeError"
          :helperText="pokeHelperText"
          :sizeLabel="pokeSizeLabel"
          :submitDisabled="pokeSubmitDisabled"
          :submitting="poking"
          @cancel="closePokeDialog"
          @submit="submitPoke"
          @update:body="updatePokeBody"
        />
      </AppDialogShell>
    </div>
  `,
};

export default RuntimePanel;
