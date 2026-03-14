import { onMounted, onUnmounted, reactive, ref, watch } from "vue";
import "./DashboardView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatBytes, formatTime, loadEndpoints, runtimeApiFetch, toBool, toInt, translate } from "../core/context";

const DashboardView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const err = ref("");
    const loading = ref(false);
    let refreshTimer = null;
    const overview = reactive({
      version: "-",
      started_at: "-",
      uptime_sec: 0,
      health: "-",
      llm_provider: "-",
      llm_model: "-",
      channel_telegram_configured: false,
      channel_slack_configured: false,
      channel_running_telegram: false,
      channel_running_slack: false,
      runtime_go_version: "-",
      runtime_goroutines: 0,
      runtime_heap_alloc_bytes: 0,
      runtime_heap_sys_bytes: 0,
      runtime_heap_objects: 0,
      runtime_gc_cycles: 0,
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
        overview.started_at = data.started_at || "-";
        overview.uptime_sec = toInt(data.uptime_sec, 0);
        overview.health = data.health || "-";
        const llm = data && typeof data.llm === "object" ? data.llm : {};
        overview.llm_provider = llm.provider || "-";
        overview.llm_model = llm.model || "-";
        const channel = data && typeof data.channel === "object" ? data.channel : {};
        overview.channel_telegram_configured = toBool(channel.telegram_configured, false);
        overview.channel_slack_configured = toBool(channel.slack_configured, false);
        overview.channel_running_telegram = toBool(channel.telegram_running, false);
        overview.channel_running_slack = toBool(channel.slack_running, false);
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

    onMounted(() => {
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
    });
    return { t, err, loading, overview, formatTime, formatBytes };
  },
  template: `
    <AppPage :title="t('runtime_title')">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="stat-groups">
        <section class="stat-group">
          <h3 class="stat-group-title">{{ t("group_basic") }}</h3>
          <div class="stat-list">
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_version") }}</span>
              <code class="stat-value">{{ overview.version }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_started") }}</span>
              <code class="stat-value">{{ formatTime(overview.started_at) }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_uptime") }}</span>
              <code class="stat-value">{{ overview.uptime_sec }}s</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_health") }}</span>
              <code class="stat-value">{{ overview.health }}</code>
            </div>
          </div>
        </section>
        <section class="stat-group">
          <h3 class="stat-group-title">{{ t("group_model") }}</h3>
          <div class="stat-list">
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_llm_provider") }}</span>
              <code class="stat-value">{{ overview.llm_provider }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_llm_model") }}</span>
              <code class="stat-value">{{ overview.llm_model }}</code>
            </div>
          </div>
        </section>
        <section class="stat-group">
          <h3 class="stat-group-title">{{ t("group_channels") }}</h3>
          <div class="stat-list">
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_channels") }}</span>
              <div class="channel-runtime-list">
                <div :class="overview.channel_telegram_configured ? 'channel-runtime-item' : 'channel-runtime-item is-disabled'">
                  <span class="channel-runtime-dot">
                    <QBadge
                      :type="overview.channel_running_telegram ? 'success' : 'default'"
                      size="md"
                      variant="filled"
                      :dot="true"
                    />
                  </span>
                  <span class="channel-runtime-label">Telegram</span>
                </div>
                <div :class="overview.channel_slack_configured ? 'channel-runtime-item' : 'channel-runtime-item is-disabled'">
                  <span class="channel-runtime-dot">
                    <QBadge
                      :type="overview.channel_running_slack ? 'success' : 'default'"
                      size="md"
                      variant="filled"
                      :dot="true"
                    />
                  </span>
                  <span class="channel-runtime-label">Slack</span>
                </div>
              </div>
            </div>
          </div>
        </section>
        <section class="stat-group">
          <h3 class="stat-group-title">{{ t("group_runtime") }}</h3>
          <div class="stat-list">
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_go_version") }}</span>
              <code class="stat-value">{{ overview.runtime_go_version }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_goroutines") }}</span>
              <code class="stat-value">{{ overview.runtime_goroutines }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_heap_alloc") }}</span>
              <code class="stat-value">{{ formatBytes(overview.runtime_heap_alloc_bytes) }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_heap_sys") }}</span>
              <code class="stat-value">{{ formatBytes(overview.runtime_heap_sys_bytes) }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_heap_objects") }}</span>
              <code class="stat-value">{{ overview.runtime_heap_objects }}</code>
            </div>
            <div class="stat-item">
              <span class="stat-key">{{ t("stat_gc_cycles") }}</span>
              <code class="stat-value">{{ overview.runtime_gc_cycles }}</code>
            </div>
          </div>
        </section>
      </div>
    </AppPage>
  `,
};


export default DashboardView;
