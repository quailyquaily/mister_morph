import { useToast } from "quail-ui";
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import "./RuntimeView.css";

import telegramLogo from "../assets/images/channels/telegram.svg";
import slackLogo from "../assets/images/channels/slack.svg";
import lineLogo from "../assets/images/channels/line.svg";
import larkLogo from "../assets/images/channels/lark.svg";
import mixinLogo from "../assets/images/channels/mixin.svg";

import AppDialogShell from "../components/AppDialogShell";
import PokeDialogContent from "../components/PokeDialogContent";
import { onDesktopWindowMessage } from "../core/desktop-runtime";
import { openPokeDesktopWindow } from "../core/desktop-windows";
import { endpointDisplayItem, endpointChannelLabel } from "../core/endpoints";
import {
  endpointState,
  formatBytes,
  formatTime,
  loadEndpoints,
  runtimeApiFetchForEndpoint,
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

function normalizeHealth(value) {
  return String(value || "").trim().toLowerCase();
}

const POKE_BODY_LIMIT = 10 * 1024;

function utf8ByteLength(value) {
  return new TextEncoder().encode(String(value || "")).length;
}

function runtimeRows(t, overview) {
  return [
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
    const loading = ref(true);
    const loadedEndpointRef = ref("");
    const lastUpdated = ref("");
    let loadSequence = 0;
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
      health: "",
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
      endpointChannelLabel((hasData.value && overview.mode) || selectedEndpoint.value?.mode || "", t)
    );
    const heroTitle = computed(() => {
      const name = hasData.value ? String(overview.agent_name || "").trim() : "";
      if (name) {
        return name;
      }
      return endpointMeta.value?.title || t("runtime_title");
    });
    const hasData = computed(() => loadedEndpointRef.value === endpointState.selectedRef && !!lastUpdated.value);
    const statusTone = computed(() => {
      if (!hasData.value) return "unknown";
      if (err.value) return "stale";
      const health = normalizeHealth(overview.health);
      if (["ok", "healthy", "ready"].includes(health)) return "healthy";
      if (["warn", "warning", "degraded"].includes(health)) return "stale";
      return health ? "error" : "unknown";
    });
    const statusLabel = computed(() => {
      if (!hasData.value) return t(loading.value ? "runtime_loading" : "runtime_unavailable");
      if (err.value) return t("runtime_stale");
      if (statusTone.value === "healthy") return t("runtime_healthy");
      return overview.health || t("runtime_unknown");
    });
    const statusRows = computed(() => [
      { key: "uptime", label: t("stat_uptime"), value: formatUptime(overview.uptime_sec) },
      { key: "started", label: t("stat_started"), value: formatTime(overview.started_at) },
      { key: "poke", label: t("runtime_field_last_poke"),
        value: overview.last_poke_at ? formatTime(overview.last_poke_at) : t("runtime_status_never") },
    ]);
    const technicalRows = computed(() => [
      { key: "endpoint", label: t("runtime_field_endpoint"), value: endpointMeta.value?.title || "-" },
      { key: "reference", label: t("runtime_endpoint_reference"), value: endpointState.selectedRef },
      { key: "location", label: t("runtime_field_location"), value: endpointMeta.value?.location || "-" },
      { key: "instance", label: t("runtime_field_instance"), value: stringValue(overview.instance_id) },
      { key: "version", label: t("stat_version"), value: stringValue(overview.version) },
      { key: "go", label: t("stat_go_version"), value: stringValue(overview.runtime_go_version) },
    ]);
    const routeRows = computed(() => [
      { key: "provider", label: t("stat_llm_provider"), value: stringValue(overview.llm_provider) },
      { key: "model", label: t("stat_llm_model"), value: stringValue(overview.llm_model) },
    ]);
    const channelRows = computed(() => [
      {
        key: "telegram",
        logo: telegramLogo,
        title: t("endpoint_channel_telegram"),
        configured: overview.channel_telegram_configured,
        running: overview.channel_running_telegram,
      },
      {
        key: "slack",
        logo: slackLogo,
        title: t("endpoint_channel_slack"),
        configured: overview.channel_slack_configured,
        running: overview.channel_running_slack,
      },
      {
        key: "line",
        logo: lineLogo,
        title: t("endpoint_channel_line"),
        configured: overview.channel_line_configured,
        running: overview.channel_running_line,
      },
      {
        key: "lark",
        logo: larkLogo,
        title: t("endpoint_channel_lark"),
        configured: overview.channel_lark_configured,
        running: overview.channel_running_lark,
      },
      {
        key: "mixin",
        logo: mixinLogo,
        title: t("endpoint_channel_mixin"),
        configured: overview.channel_mixin_configured,
        running: overview.channel_running_mixin,
      },
    ]);
    const configuredChannels = computed(() => channelRows.value.filter(item => item.configured));
    const unconfiguredChannels = computed(() => channelRows.value.filter(item => !item.configured));
    const runtimeMetrics = computed(() => runtimeRows(t, overview));
    const canPoke = computed(() => hasData.value && toBool(overview.poke_enabled, false));
    const awarenessRunning = computed(() => toBool(overview.awareness_running, false));
    const pokeDisabled = computed(() => !hasData.value || !!err.value || poking.value || awarenessRunning.value);
    const pokeBodyBytes = computed(() => utf8ByteLength(pokeBody.value));
    const pokeBodyTooLarge = computed(() => pokeBodyBytes.value > POKE_BODY_LIMIT);
    const pokeSubmitDisabled = computed(() => pokeDisabled.value || !String(pokeBody.value || "").trim() || pokeBodyTooLarge.value);
    async function load() {
      const sequence = ++loadSequence;
      loading.value = true;
      try {
        await loadEndpoints();
        if (sequence !== loadSequence) return;
        const endpointRef = endpointState.selectedRef;
        if (!endpointRef) throw new Error(t("msg_select_endpoint"));
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/overview");
        if (sequence !== loadSequence || endpointRef !== endpointState.selectedRef) return;
        overview.version = data.version || "-";
        overview.started_at = data.started_at || "";
        overview.uptime_sec = toInt(data.uptime_sec, 0);
        overview.health = data.health || "";
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
        loadedEndpointRef.value = endpointRef;
        lastUpdated.value = new Date().toISOString();
        err.value = "";
      } catch (e) {
        if (sequence === loadSequence) err.value = e.message || t("msg_load_failed");
      } finally {
        if (sequence === loadSequence) loading.value = false;
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
      if (pokeDisabled.value) return;
      const endpointRef = endpointState.selectedRef;
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
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/poke", {
          method: "POST",
          headers: { "Content-Type": "text/plain; charset=utf-8" },
          body,
        });
        if (endpointRef !== endpointState.selectedRef) return;
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
        loadedEndpointRef.value = "";
        lastUpdated.value = "";
        err.value = "";
        pokeDialogOpen.value = false;
        pokeError.value = "";
        void load();
      }
    );

    onUnmounted(() => {
      loadSequence += 1;
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
      modeLabel,
      statusRows,
      technicalRows,
      hasData,
      lastUpdated,
      formatTime,
      statusTone,
      statusLabel,
      awarenessRunning,
      routeRows,
      configuredChannels,
      unconfiguredChannels,
      runtimeMetrics,
      canPoke,
      pokeDisabled,
      pokeBodyTooLarge,
      pokeSubmitDisabled,
      load,
      openPokeDialog,
      closePokeDialog,
      updatePokeBody,
      submitPoke,
    };
  },
  template: `
    <div class="runtime-panel">
      <QCard class="runtime-status-card" variant="default">
        <header class="runtime-heading">
          <div class="runtime-heading-copy">
            <h2 class="runtime-title workspace-document-title">{{ heroTitle }}</h2>
            <div class="runtime-heading-meta">
              <span>{{ modeLabel }}</span>
              <span class="runtime-status" :class="'is-' + statusTone" role="status">
                <span class="runtime-status-dot" aria-hidden="true"></span>{{ statusLabel }}
              </span>
            </div>
          </div>
          <div v-if="canPoke" class="runtime-actions">
            <QButton class="outlined runtime-poke-button" :loading="poking" :disabled="pokeDisabled" :aria-describedby="awarenessRunning ? 'runtime-poke-busy' : undefined" @click="openPokeDialog">
              {{ t("runtime_action_poke") }}
            </QButton>
          </div>
        </header>
        <p v-if="canPoke && awarenessRunning" id="runtime-poke-busy" class="runtime-note">{{ t("runtime_poke_busy") }}</p>
        <p class="runtime-update" role="status">
          {{ loading ? t("runtime_loading") : hasData ? t("runtime_updated", { time: formatTime(lastUpdated) }) : t("runtime_no_data") }}
        </p>
        <div v-if="err" class="runtime-error" role="alert">
          <span>{{ err }}</span>
          <span v-if="hasData">{{ t("runtime_stale_hint") }}</span>
        </div>
        <dl v-if="hasData" class="runtime-ledger runtime-status-ledger">
          <div v-for="item in statusRows" :key="item.key" class="runtime-ledger-row">
            <dt>{{ item.label }}</dt><dd>{{ item.value }}</dd>
          </div>
        </dl>
      </QCard>

      <template v-if="hasData">
        <QCard class="runtime-context-card" variant="default">
          <div class="runtime-context-grid">
            <section>
              <h3 class="runtime-section-title">{{ t("runtime_current_model") }}</h3>
              <dl class="runtime-ledger">
                <div v-for="item in routeRows" :key="item.key" class="runtime-ledger-row">
                  <dt>{{ item.label }}</dt><dd>{{ item.value }}</dd>
                </div>
              </dl>
            </section>
            <section>
              <h3 class="runtime-section-title">{{ t("group_channels") }}</h3>
              <ul v-if="configuredChannels.length" class="runtime-channels">
                <li v-for="item in configuredChannels" :key="item.key" class="runtime-channel-row">
                  <span class="runtime-channel-name"><img :src="item.logo" alt="" />{{ item.title }}</span>
                  <span class="runtime-status" :class="item.running ? 'is-healthy' : 'is-unknown'">
                    <span class="runtime-status-dot" aria-hidden="true"></span>
                    {{ item.running ? t("runtime_status_running") : t("runtime_not_running") }}
                  </span>
                </li>
              </ul>
              <details v-if="unconfiguredChannels.length" class="runtime-unconfigured">
                <summary>
                  <span>{{ t(configuredChannels.length ? "runtime_status_not_configured" : "runtime_no_channels") }}</span>
                  <PhCaretDown class="icon" aria-hidden="true" />
                </summary>
                <ul class="runtime-channels">
                  <li v-for="item in unconfiguredChannels" :key="item.key" class="runtime-channel-name">
                    <img :src="item.logo" alt="" />{{ item.title }}
                  </li>
                </ul>
              </details>
            </section>
          </div>
        </QCard>

        <QCard class="runtime-metrics-card" variant="default">
          <h3 class="runtime-section-title">{{ t("group_runtime") }}</h3>
          <dl class="runtime-ledger runtime-metrics">
            <div v-for="item in runtimeMetrics" :key="item.key" class="runtime-ledger-row">
              <dt>{{ item.label }}</dt><dd>{{ item.value }}</dd>
            </div>
          </dl>
        </QCard>

        <QCard class="runtime-technical-card" variant="default">
          <details class="runtime-technical">
            <summary>
              <h3 class="runtime-section-title">{{ t("runtime_technical_details") }}</h3>
              <PhCaretDown class="icon" aria-hidden="true" />
            </summary>
            <dl class="runtime-ledger">
              <div v-for="item in technicalRows" :key="item.key" class="runtime-ledger-row">
                <dt>{{ item.label }}</dt><dd>{{ item.value }}</dd>
              </div>
            </dl>
          </details>
        </QCard>
      </template>

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
          :submitDisabled="pokeSubmitDisabled"
          :submitting="poking"
          @submit="submitPoke"
          @update:body="updatePokeBody"
        />
      </AppDialogShell>
    </div>
  `,
};

export default RuntimePanel;
