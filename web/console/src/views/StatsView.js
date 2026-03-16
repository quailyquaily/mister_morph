import { computed, onMounted, ref, watch } from "vue";
import "./StatsView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";

function formatNumber(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n)) {
    return "0";
  }
  return Math.trunc(n).toLocaleString();
}

function metricItems(t, totals) {
  return [
    { key: "total_tokens", label: t("stats_total_tokens"), value: formatNumber(totals.total_tokens), density: "primary" },
    { key: "input_tokens", label: t("stats_input_tokens"), value: formatNumber(totals.input_tokens), density: "secondary" },
    { key: "output_tokens", label: t("stats_output_tokens"), value: formatNumber(totals.output_tokens), density: "secondary" },
    { key: "requests", label: t("stats_requests"), value: formatNumber(totals.requests), density: "compact" },
  ];
}

function sumModelTotals(models) {
  const totals = { requests: 0, total_tokens: 0, input_tokens: 0, output_tokens: 0 };
  for (const item of Array.isArray(models) ? models : []) {
    totals.requests += Number(item.requests || 0);
    totals.total_tokens += Number(item.total_tokens || 0);
    totals.input_tokens += Number(item.input_tokens || 0);
    totals.output_tokens += Number(item.output_tokens || 0);
  }
  return totals;
}

function tuiKicker(left, right) {
  const lhs = String(left || "").trim();
  const rhs = String(right || "").trim();
  if (lhs && rhs) {
    return `[ ${lhs} // ${rhs} ]`;
  }
  return `[ ${lhs || rhs} ]`;
}

const StatsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const err = ref("");
    const activeTabID = ref("api_hosts");
    const payload = ref({
      updated_at: "",
      projected_records: 0,
      skipped_records: 0,
      summary: {},
      api_hosts: [],
      models: [],
    });
    const statsTabs = computed(() => [
      { id: "api_hosts", title: t("stats_group_api_hosts") },
      { id: "models", title: t("stats_group_models") },
    ]);
    const selectedStatsTab = computed(() => statsTabs.value.find((item) => item.id === activeTabID.value) || statsTabs.value[0] || null);

    const visibleHosts = computed(() => {
      return Array.isArray(payload.value.api_hosts) ? payload.value.api_hosts : [];
    });

    const visibleModels = computed(() => {
      return Array.isArray(payload.value.models) ? payload.value.models : [];
    });

    const summaryMetrics = computed(() => metricItems(t, payload.value.summary || {}));

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetch("/stats/llm/usage");
        payload.value = {
          updated_at: typeof data.updated_at === "string" ? data.updated_at : "",
          projected_records: Number(data.projected_records || 0),
          skipped_records: Number(data.skipped_records || 0),
          summary: data.summary && typeof data.summary === "object" ? data.summary : {},
          api_hosts: Array.isArray(data.api_hosts) ? data.api_hosts : [],
          models: Array.isArray(data.models) ? data.models : [],
        };
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function sectionMetrics(item) {
      return metricItems(t, item || {});
    }

    function onTabChange(detail) {
      const nextID = String(detail?.tab?.id || "").trim();
      activeTabID.value = nextID || "api_hosts";
    }

    onMounted(load);
    watch(
      () => endpointState.selectedRef,
      () => {
        void load();
      }
    );

    return {
      t,
      loading,
      err,
      payload,
      statsTabs,
      selectedStatsTab,
      visibleHosts,
      visibleModels,
      summaryMetrics,
      tuiKicker,
      load,
      onTabChange,
      sectionMetrics,
      formatTime,
      formatNumber,
    };
  },
  template: `
    <AppPage :title="t('stats_title')">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

      <div class="stats-grid">
        <section class="stats-summary-board ui-track-panel">
          <div class="stats-summary-head">
            <h3 class="ui-kicker">{{ tuiKicker("LLM", t("stats_group_summary")) }}</h3>
            <p class="stats-summary-meta">{{ t("stats_updated_at") }}: {{ formatTime(payload.updated_at) }}</p>
          </div>
          <div class="stats-summary-grid">
            <article
              v-for="item in summaryMetrics"
              :key="item.key"
              class="stats-summary-metric"
              :class="'is-' + item.density"
            >
              <span class="stats-summary-label">{{ item.label }}</span>
              <span class="stats-summary-value">{{ item.value }}</span>
            </article>
          </div>
        </section>

        <section class="stats-section">
          <QTabs
            class="stats-section-tabs"
            :tabs="statsTabs"
            :modelValue="selectedStatsTab"
            variant="normal"
            @change="onTabChange"
          />

          <div v-if="selectedStatsTab && selectedStatsTab.id === 'api_hosts'" class="stats-section-panel ui-track-panel">
            <div v-if="visibleHosts.length === 0" class="stats-empty">{{ t("stats_no_data") }}</div>
            <div v-else class="stats-host-list">
              <section v-for="host in visibleHosts" :key="host.api_host" class="stats-host-card">
                <div class="stats-host-head">
                  <h4 class="ui-kicker">{{ tuiKicker(t("stats_api_host"), host.api_host) }}</h4>
                </div>
                <div class="stats-host-metrics">
                  <article
                    v-for="item in sectionMetrics(host)"
                    :key="host.api_host + ':' + item.key"
                    class="stats-host-metric"
                    :class="'is-' + item.density"
                  >
                    <span class="stats-host-metric-label">{{ item.label }}</span>
                    <span class="stats-host-metric-value">{{ item.value }}</span>
                  </article>
                </div>
                <div v-if="Array.isArray(host.models) && host.models.length > 0" class="stats-model-list">
                  <div v-for="model in host.models" :key="host.api_host + ':' + model.model" class="stats-model-row">
                    <div>
                      <span class="stats-model-label">{{ t("stats_model") }}</span>
                      <span class="stats-model-name">{{ model.model }}</span>
                    </div>
                    <div>
                      <span class="stats-model-label">{{ t("stats_total_tokens") }}</span>
                      <span class="stats-model-value">{{ formatNumber(model.total_tokens) }}</span>
                    </div>
                    <div>
                      <span class="stats-model-label">{{ t("stats_input_tokens") }}</span>
                      <span class="stats-model-value">{{ formatNumber(model.input_tokens) }}</span>
                    </div>
                    <div>
                      <span class="stats-model-label">{{ t("stats_output_tokens") }}</span>
                      <span class="stats-model-value">{{ formatNumber(model.output_tokens) }}</span>
                    </div>
                    <div>
                      <span class="stats-model-label">{{ t("stats_requests") }}</span>
                      <span class="stats-model-value">{{ formatNumber(model.requests) }}</span>
                    </div>
                  </div>
                </div>
              </section>
            </div>
          </div>

          <div v-else class="stats-section-panel ui-track-panel">
            <div v-if="visibleModels.length === 0" class="stats-empty">{{ t("stats_no_data") }}</div>
            <div v-else class="stats-model-list">
              <div v-for="model in visibleModels" :key="model.model" class="stats-model-row">
                <div>
                  <span class="stats-model-label">{{ t("stats_model") }}</span>
                  <span class="stats-model-name">{{ model.model }}</span>
                </div>
                <div>
                  <span class="stats-model-label">{{ t("stats_total_tokens") }}</span>
                  <span class="stats-model-value">{{ formatNumber(model.total_tokens) }}</span>
                </div>
                <div>
                  <span class="stats-model-label">{{ t("stats_input_tokens") }}</span>
                  <span class="stats-model-value">{{ formatNumber(model.input_tokens) }}</span>
                </div>
                <div>
                  <span class="stats-model-label">{{ t("stats_output_tokens") }}</span>
                  <span class="stats-model-value">{{ formatNumber(model.output_tokens) }}</span>
                </div>
                <div>
                  <span class="stats-model-label">{{ t("stats_requests") }}</span>
                  <span class="stats-model-value">{{ formatNumber(model.requests) }}</span>
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </AppPage>
  `,
};

export default StatsView;
