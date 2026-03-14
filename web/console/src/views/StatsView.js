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
    { label: t("stats_requests"), value: formatNumber(totals.requests) },
    { label: t("stats_total_tokens"), value: formatNumber(totals.total_tokens) },
    { label: t("stats_input_tokens"), value: formatNumber(totals.input_tokens) },
    { label: t("stats_output_tokens"), value: formatNumber(totals.output_tokens) },
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
    const hostValue = ref("");
    const modelValue = ref("");

    const hostItems = computed(() => {
      const items = [{ title: t("stats_filter_all_hosts"), value: "" }];
      for (const item of Array.isArray(payload.value.api_hosts) ? payload.value.api_hosts : []) {
        if (!item || typeof item.api_host !== "string" || !item.api_host) {
          continue;
        }
        items.push({ title: item.api_host, value: item.api_host });
      }
      return items;
    });

    const globalModelItems = computed(() => {
      const items = [{ title: t("stats_filter_all_models"), value: "" }];
      for (const item of Array.isArray(payload.value.models) ? payload.value.models : []) {
        if (!item || typeof item.model !== "string" || !item.model) {
          continue;
        }
        items.push({ title: item.model, value: item.model });
      }
      return items;
    });

    const selectedHost = computed(() => {
      return (Array.isArray(payload.value.api_hosts) ? payload.value.api_hosts : []).find((item) => item.api_host === hostValue.value) || null;
    });

    const selectedHostItem = computed(() => hostItems.value.find((item) => item.value === hostValue.value) || hostItems.value[0] || null);
    const selectedModelItem = computed(() => globalModelItems.value.find((item) => item.value === modelValue.value) || globalModelItems.value[0] || null);
    const statsTabs = computed(() => [
      { id: "api_hosts", title: t("stats_group_api_hosts") },
      { id: "models", title: t("stats_group_models") },
    ]);
    const selectedStatsTab = computed(() => statsTabs.value.find((item) => item.id === activeTabID.value) || statsTabs.value[0] || null);

    const visibleHosts = computed(() => {
      let hosts = Array.isArray(payload.value.api_hosts) ? payload.value.api_hosts : [];
      if (hostValue.value) {
        hosts = hosts.filter((item) => item.api_host === hostValue.value);
      }
      if (!modelValue.value) {
        return hosts;
      }
      return hosts
        .map((item) => {
          const models = Array.isArray(item.models) ? item.models.filter((model) => model.model === modelValue.value) : [];
          if (models.length === 0) {
            return null;
          }
          return { ...item, ...sumModelTotals(models), models };
        })
        .filter(Boolean);
    });

    const visibleModels = computed(() => {
      if (selectedHost.value) {
        const hostModels = Array.isArray(selectedHost.value.models) ? selectedHost.value.models : [];
        if (!modelValue.value) {
          return hostModels;
        }
        return hostModels.filter((item) => item.model === modelValue.value);
      }
      let models = Array.isArray(payload.value.models) ? payload.value.models : [];
      if (modelValue.value) {
        models = models.filter((item) => item.model === modelValue.value);
      }
      return models;
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

    function onHostChange(item) {
      hostValue.value = item && typeof item.value === "string" ? item.value : "";
    }

    function onModelChange(item) {
      modelValue.value = item && typeof item.value === "string" ? item.value : "";
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
      hostItems,
      globalModelItems,
      selectedHostItem,
      selectedModelItem,
      statsTabs,
      selectedStatsTab,
      visibleHosts,
      visibleModels,
      summaryMetrics,
      load,
      onHostChange,
      onModelChange,
      onTabChange,
      sectionMetrics,
      formatTime,
      formatNumber,
    };
  },
  template: `
    <AppPage :title="t('stats_title')">
      <div class="toolbar wrap">
        <div class="tool-item">
          <QDropdownMenu
            :items="hostItems"
            :initialItem="selectedHostItem"
            :placeholder="t('placeholder_api_host')"
            @change="onHostChange"
          />
        </div>
        <div class="tool-item">
          <QDropdownMenu
            :items="globalModelItems"
            :initialItem="selectedModelItem"
            :placeholder="t('placeholder_model')"
            @change="onModelChange"
          />
        </div>
        <QButton class="outlined" :loading="loading" @click="load">{{ t("action_refresh") }}</QButton>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

      <div class="stats-grid">
        <div class="stat-item stats-card">
          <div class="stats-card-head">
            <h3 class="stats-card-title">{{ t("stats_group_summary") }}</h3>
            <span class="muted">{{ t("stats_updated_at") }}: {{ formatTime(payload.updated_at) }}</span>
          </div>
          <div class="stats-metric-grid">
            <div class="stats-metric">
              <span class="stats-metric-label">{{ t("stats_projected_records") }}</span>
              <span class="stats-metric-value">{{ formatNumber(payload.projected_records) }}</span>
            </div>
            <div class="stats-metric">
              <span class="stats-metric-label">{{ t("stats_skipped_records") }}</span>
              <span class="stats-metric-value">{{ formatNumber(payload.skipped_records) }}</span>
            </div>
            <div v-for="item in summaryMetrics" :key="item.label" class="stats-metric">
              <span class="stats-metric-label">{{ item.label }}</span>
              <span class="stats-metric-value">{{ item.value }}</span>
            </div>
          </div>
        </div>

        <section class="stats-section">
          <QTabs
            class="stats-section-tabs"
            :tabs="statsTabs"
            :modelValue="selectedStatsTab"
            variant="normal"
            @change="onTabChange"
          />

          <div v-if="selectedStatsTab && selectedStatsTab.id === 'api_hosts'" class="stats-section-panel">
            <div v-if="visibleHosts.length === 0" class="stats-empty frame">{{ t("stats_no_data") }}</div>
            <div v-else class="stack">
              <div v-for="host in visibleHosts" :key="host.api_host" class="stats-card stat-item">
                <div class="stats-card-head">
                  <h4 class="stats-card-title">{{ host.api_host }}</h4>
                </div>
                <div class="stats-metric-grid">
                  <div v-for="item in sectionMetrics(host)" :key="host.api_host + ':' + item.label" class="stats-metric">
                    <span class="stats-metric-label">{{ item.label }}</span>
                    <span class="stats-metric-value">{{ item.value }}</span>
                  </div>
                </div>
                <div v-if="Array.isArray(host.models) && host.models.length > 0" class="stats-model-list">
                  <div v-for="model in host.models" :key="host.api_host + ':' + model.model" class="stats-model-row">
                    <div>
                      <span class="stats-model-label">{{ t("stats_model") }}</span>
                      <span class="stats-model-name">{{ model.model }}</span>
                    </div>
                    <div>
                      <span class="stats-model-label">{{ t("stats_requests") }}</span>
                      <span class="stats-model-value">{{ formatNumber(model.requests) }}</span>
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
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div v-else class="stats-section-panel">
            <div v-if="visibleModels.length === 0" class="stats-empty frame">{{ t("stats_no_data") }}</div>
            <div v-else class="stats-model-list">
              <div v-for="model in visibleModels" :key="model.model" class="stats-model-row">
                <div>
                  <span class="stats-model-label">{{ t("stats_model") }}</span>
                  <span class="stats-model-name">{{ model.model }}</span>
                </div>
                <div>
                  <span class="stats-model-label">{{ t("stats_requests") }}</span>
                  <span class="stats-model-value">{{ formatNumber(model.requests) }}</span>
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
              </div>
            </div>
          </div>
        </section>
      </div>
    </AppPage>
  `,
};

export default StatsView;
