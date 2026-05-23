import { computed, onMounted, ref, watch } from "vue";
import "./StatsView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";
import modelVendorPrefixes from "../core/model-vendor-prefixes.json";
import anthropicIcon from "../assets/model-vendors/anthropic.png";
import deepseekIcon from "../assets/model-vendors/deepseek.png";
import geminiIcon from "../assets/model-vendors/gemini.png";
import groqIcon from "../assets/model-vendors/groq.svg";
import mistralIcon from "../assets/model-vendors/mistral.svg";
import openaiIcon from "../assets/model-vendors/openai.svg";
import xaiIcon from "../assets/model-vendors/xai.svg";

const MODEL_VENDOR_ICONS = {
  anthropic: anthropicIcon,
  deepseek: deepseekIcon,
  gemini: geminiIcon,
  groq: groqIcon,
  mistral: mistralIcon,
  openai: openaiIcon,
  xai: xaiIcon,
};

const MODEL_VENDOR_LABELS = {
  anthropic: "Anthropic",
  deepseek: "DeepSeek",
  gemini: "Gemini",
  groq: "Groq",
  mistral: "Mistral",
  openai: "OpenAI",
  xai: "xAI",
};

const MODEL_VENDOR_RULES = Array.isArray(modelVendorPrefixes)
  ? [...modelVendorPrefixes]
      .filter((item) => item && typeof item.prefix === "string" && typeof item.vendor === "string")
      .sort((a, b) => b.prefix.length - a.prefix.length)
  : [];

function hasMetricValue(totals, key) {
  return Boolean(totals) && Object.prototype.hasOwnProperty.call(totals, key);
}

function formatNumber(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n)) {
    return "0";
  }
  return Math.trunc(n).toLocaleString();
}

function toFiniteNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function formatCost(value, currency = "USD") {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return "-";
  }
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: String(currency || "USD").toUpperCase(),
      minimumFractionDigits: Math.abs(n) > 0 && Math.abs(n) < 1 ? 4 : 2,
      maximumFractionDigits: 6,
    }).format(n);
  } catch {
    return `${String(currency || "USD").toUpperCase()} ${n.toFixed(4)}`;
  }
}

function formatFixedCost(value, currency = "USD", fractionDigits = 6) {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return "-";
  }
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: String(currency || "USD").toUpperCase(),
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(n);
  } catch {
    return `${String(currency || "USD").toUpperCase()} ${n.toFixed(fractionDigits)}`;
  }
}

function formatSignedFixedCost(value, currency = "USD", fractionDigits = 6) {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return "-";
  }
  const rounded = Number(n.toFixed(fractionDigits));
  if (!Number.isFinite(rounded)) {
    return "-";
  }
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: String(currency || "USD").toUpperCase(),
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
      signDisplay: "exceptZero",
    }).format(rounded);
  } catch {
    const sign = rounded > 0 ? "+" : rounded < 0 ? "-" : "";
    return `${sign}${String(currency || "USD").toUpperCase()} ${Math.abs(rounded).toFixed(fractionDigits)}`;
  }
}

function formatPercent(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return "-";
  }
  const clamped = Math.min(Math.max(n, 0), 1);
  return new Intl.NumberFormat(undefined, {
    style: "percent",
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(clamped);
}

function modelCacheBaseInputTokens(row) {
  const inputTokens = toFiniteNumber(row?.input_tokens);
  const cachedInputTokens = toFiniteNumber(row?.cached_input_tokens);
  const cacheCreationInputTokens = toFiniteNumber(row?.cache_creation_input_tokens);
  return Math.max(0, inputTokens - cachedInputTokens - cacheCreationInputTokens);
}

function modelCacheRate(row) {
  const inputTokens = toFiniteNumber(row?.input_tokens);
  if (inputTokens <= 0) {
    return null;
  }
  const cachedInputTokens = Math.min(toFiniteNumber(row?.cached_input_tokens), inputTokens);
  return Math.max(0, cachedInputTokens / inputTokens);
}

function modelCacheCostDelta(row) {
  const inputTokens = toFiniteNumber(row?.input_tokens);
  if (inputTokens <= 0) {
    return null;
  }

  const cachedInputTokens = toFiniteNumber(row?.cached_input_tokens);
  const cacheCreationInputTokens = toFiniteNumber(row?.cache_creation_input_tokens);
  if (cachedInputTokens <= 0 && cacheCreationInputTokens <= 0) {
    return 0;
  }

  const baseInputTokens = modelCacheBaseInputTokens(row);
  if (baseInputTokens <= 0 || !hasMetricValue(row, "input_cost")) {
    return null;
  }

  const inputCost = Number(row?.input_cost);
  if (!Number.isFinite(inputCost)) {
    return null;
  }

  const baseInputCostPerToken = inputCost / baseInputTokens;
  if (!Number.isFinite(baseInputCostPerToken)) {
    return null;
  }

  const actualInputCost =
    inputCost + toFiniteNumber(row?.cached_input_cost) + toFiniteNumber(row?.cache_creation_input_cost);
  const baselineInputCostWithoutCache = baseInputCostPerToken * inputTokens;
  return actualInputCost - baselineInputCostWithoutCache;
}

function summaryHeroMetric(t, totals, key) {
  const costCurrency = typeof totals?.cost_currency === "string" ? totals.cost_currency : "USD";
  switch (key) {
    case "total_cost":
      return {
        key,
        label: t("stats_total_cost"),
        value: hasMetricValue(totals, key) ? formatCost(totals[key], costCurrency) : "-",
        unavailable: !hasMetricValue(totals, key),
      };
    case "total_tokens":
      return {
        key,
        label: t("stats_total_tokens"),
        value: hasMetricValue(totals, key) ? formatNumber(totals[key]) : "-",
        unavailable: !hasMetricValue(totals, key),
      };
    case "requests":
      return {
        key,
        label: t("stats_requests"),
        value: hasMetricValue(totals, key) ? formatNumber(totals[key]) : "-",
        unavailable: !hasMetricValue(totals, key),
      };
    default:
      return {
        key,
        label: key,
        value: "-",
        unavailable: true,
      };
  }
}

function summaryHeroMetrics(t, totals) {
  const orderedKeys = ["total_cost", "total_tokens", "requests"];
  const primaryKey = orderedKeys.find((key) => hasMetricValue(totals, key)) || orderedKeys[0];
  return {
    primary: summaryHeroMetric(t, totals, primaryKey),
    secondary: orderedKeys.filter((key) => key !== primaryKey).map((key) => summaryHeroMetric(t, totals, key)),
  };
}

function summaryCostMetrics(t, totals, includeTotal = true) {
  const costCurrency = typeof totals?.cost_currency === "string" ? totals.cost_currency : "USD";
  return [
    {
      key: "total_cost",
      label: t("stats_total_cost"),
      value: hasMetricValue(totals, "total_cost") ? formatCost(totals?.total_cost, costCurrency) : "-",
      unavailable: !hasMetricValue(totals, "total_cost"),
    },
    {
      key: "input_cost",
      label: t("stats_input_cost"),
      value: formatCost(totals?.input_cost, costCurrency),
      unavailable: !hasMetricValue(totals, "input_cost"),
    },
    {
      key: "output_cost",
      label: t("stats_output_cost"),
      value: formatCost(totals?.output_cost, costCurrency),
      unavailable: !hasMetricValue(totals, "output_cost"),
    },
    {
      key: "cached_input_cost",
      label: t("stats_cached_input_cost"),
      value: formatCost(totals?.cached_input_cost, costCurrency),
      unavailable: !hasMetricValue(totals, "cached_input_cost"),
    },
    {
      key: "cache_creation_input_cost",
      label: t("stats_cache_creation_input_cost"),
      value: formatCost(totals?.cache_creation_input_cost, costCurrency),
      unavailable: !hasMetricValue(totals, "cache_creation_input_cost"),
    },
  ].filter((item) => (includeTotal || item.key !== "total_cost") && !item.unavailable);
}

function summaryTokenMetrics(t, totals, includeTotal = true) {
  return [
    {
      key: "total_tokens",
      label: t("stats_total_tokens"),
      value: hasMetricValue(totals, "total_tokens") ? formatNumber(totals?.total_tokens) : "-",
      unavailable: !hasMetricValue(totals, "total_tokens"),
    },
    {
      key: "input_tokens",
      label: t("stats_input_tokens"),
      value: hasMetricValue(totals, "input_tokens") ? formatNumber(totals?.input_tokens) : "-",
      unavailable: !hasMetricValue(totals, "input_tokens"),
    },
    {
      key: "output_tokens",
      label: t("stats_output_tokens"),
      value: hasMetricValue(totals, "output_tokens") ? formatNumber(totals?.output_tokens) : "-",
      unavailable: !hasMetricValue(totals, "output_tokens"),
    },
    {
      key: "cached_input_tokens",
      label: t("stats_cached_input_tokens"),
      value: hasMetricValue(totals, "cached_input_tokens") ? formatNumber(totals?.cached_input_tokens) : "-",
      unavailable: !hasMetricValue(totals, "cached_input_tokens"),
    },
    {
      key: "cache_creation_input_tokens",
      label: t("stats_cache_creation_input_tokens"),
      value: hasMetricValue(totals, "cache_creation_input_tokens")
        ? formatNumber(totals?.cache_creation_input_tokens)
        : "-",
      unavailable: !hasMetricValue(totals, "cache_creation_input_tokens"),
    },
  ].filter((item) => (includeTotal || item.key !== "total_tokens") && !item.unavailable);
}

function costMetrics(t, totals) {
  const costCurrency = typeof totals?.cost_currency === "string" ? totals.cost_currency : "USD";
  return [
    {
      key: "total_cost",
      label: t("stats_total_cost"),
      value: hasMetricValue(totals, "total_cost") ? formatCost(totals.total_cost, costCurrency) : "-",
      unavailable: !hasMetricValue(totals, "total_cost"),
    },
    {
      key: "input_cost",
      label: t("stats_input_cost"),
      value: hasMetricValue(totals, "input_cost") ? formatCost(totals.input_cost, costCurrency) : "-",
      unavailable: !hasMetricValue(totals, "input_cost"),
    },
    {
      key: "output_cost",
      label: t("stats_output_cost"),
      value: hasMetricValue(totals, "output_cost") ? formatCost(totals.output_cost, costCurrency) : "-",
      unavailable: !hasMetricValue(totals, "output_cost"),
    },
    {
      key: "cached_input_cost",
      label: t("stats_cached_input_cost"),
      value: hasMetricValue(totals, "cached_input_cost") ? formatCost(totals.cached_input_cost, costCurrency) : "-",
      unavailable: !hasMetricValue(totals, "cached_input_cost"),
    },
    {
      key: "cache_creation_input_cost",
      label: t("stats_cache_creation_input_cost"),
      value: hasMetricValue(totals, "cache_creation_input_cost")
        ? formatCost(totals.cache_creation_input_cost, costCurrency)
        : "-",
      unavailable: !hasMetricValue(totals, "cache_creation_input_cost"),
    },
  ];
}

function tokenMetrics(t, totals) {
  return [
    {
      key: "total_tokens",
      label: t("stats_total_tokens"),
      value: hasMetricValue(totals, "total_tokens") ? formatNumber(totals.total_tokens) : "-",
      unavailable: !hasMetricValue(totals, "total_tokens"),
    },
    {
      key: "input_tokens",
      label: t("stats_input_tokens"),
      value: hasMetricValue(totals, "input_tokens") ? formatNumber(totals.input_tokens) : "-",
      unavailable: !hasMetricValue(totals, "input_tokens"),
    },
    {
      key: "output_tokens",
      label: t("stats_output_tokens"),
      value: hasMetricValue(totals, "output_tokens") ? formatNumber(totals.output_tokens) : "-",
      unavailable: !hasMetricValue(totals, "output_tokens"),
    },
    {
      key: "cached_input_tokens",
      label: t("stats_cached_input_tokens"),
      value: hasMetricValue(totals, "cached_input_tokens") ? formatNumber(totals.cached_input_tokens) : "-",
      unavailable: !hasMetricValue(totals, "cached_input_tokens"),
    },
    {
      key: "cache_creation_input_tokens",
      label: t("stats_cache_creation_input_tokens"),
      value: hasMetricValue(totals, "cache_creation_input_tokens")
        ? formatNumber(totals.cache_creation_input_tokens)
        : "-",
      unavailable: !hasMetricValue(totals, "cache_creation_input_tokens"),
    },
  ];
}

function visibleModelCostColumns(t) {
  const columns = [
    { key: "total_cost", label: t("stats_total_cost"), kind: "cost" },
    { key: "input_cost", label: t("stats_input_cost"), kind: "cost" },
    { key: "output_cost", label: t("stats_output_cost"), kind: "cost" },
    { key: "cached_input_cost", label: t("stats_cached_input_cost"), kind: "cost" },
    { key: "cache_creation_input_cost", label: t("stats_cache_creation_input_cost"), kind: "cost" },
    { key: "cache_cost_delta", label: t("stats_cache_cost_delta"), kind: "cache_cost_delta" },
  ];
  return columns;
}

function visibleModelTokenColumns(t) {
  const columns = [
    { key: "total_tokens", label: t("stats_total_tokens"), kind: "token" },
    { key: "input_tokens", label: t("stats_input_tokens"), kind: "token" },
    { key: "output_tokens", label: t("stats_output_tokens"), kind: "token" },
    { key: "cached_input_tokens", label: t("stats_cached_input_tokens"), kind: "token" },
    { key: "cache_creation_input_tokens", label: t("stats_cache_creation_input_tokens"), kind: "token" },
    { key: "cache_rate", label: t("stats_cache_rate"), kind: "cache_rate" },
  ];
  return columns;
}

function formatModelLedgerValue(row, column) {
  if (column.kind === "cost") {
    const currency = typeof row?.cost_currency === "string" ? row.cost_currency : "USD";
    return hasMetricValue(row, column.key) ? formatFixedCost(row[column.key], currency) : "-";
  }
  if (column.kind === "cache_cost_delta") {
    const currency = typeof row?.cost_currency === "string" ? row.cost_currency : "USD";
    const delta = modelCacheCostDelta(row);
    return delta === null ? "-" : formatSignedFixedCost(delta, currency);
  }
  if (column.kind === "cache_rate") {
    const rate = modelCacheRate(row);
    return rate === null ? "-" : formatPercent(rate);
  }
  return hasMetricValue(row, column.key) ? formatNumber(row[column.key]) : "-";
}

function isModelLedgerValueUnavailable(row, column) {
  if (column.kind === "cache_cost_delta") {
    return modelCacheCostDelta(row) === null;
  }
  if (column.kind === "cache_rate") {
    return modelCacheRate(row) === null;
  }
  return !hasMetricValue(row, column.key);
}

function modelLedgerValueToneClass(row, column) {
  if (column.kind !== "cache_cost_delta") {
    return "";
  }
  const delta = modelCacheCostDelta(row);
  if (delta === null || Math.abs(delta) < 1e-12) {
    return "";
  }
  return delta > 0 ? "stats-model-ledger-value-cell-cost-up" : "stats-model-ledger-value-cell-cost-down";
}

function normalizeModelName(value) {
  return String(value || "").trim().toLowerCase();
}

function modelMatchCandidates(modelName) {
  const normalized = normalizeModelName(modelName);
  if (!normalized) {
    return [];
  }
  const candidates = [normalized];
  const slashIndex = normalized.lastIndexOf("/");
  if (slashIndex >= 0 && slashIndex < normalized.length - 1) {
    candidates.push(normalized.slice(slashIndex + 1));
  }
  return [...new Set(candidates)];
}

function modelVendorMeta(modelName) {
  const candidates = modelMatchCandidates(modelName);
  if (candidates.length === 0) {
    return {
      vendor: "",
      icon: "",
      label: "",
    };
  }
  const matchedRule = candidates
    .flatMap((candidate) => MODEL_VENDOR_RULES.filter((item) => candidate.startsWith(item.prefix)))
    .sort((a, b) => b.prefix.length - a.prefix.length)[0];
  const vendor = matchedRule?.vendor || "";
  return {
    vendor,
    icon: vendor ? MODEL_VENDOR_ICONS[vendor] || "" : "",
    label: vendor ? MODEL_VENDOR_LABELS[vendor] || vendor : "",
  };
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

    const visibleHosts = computed(() => (Array.isArray(payload.value.api_hosts) ? payload.value.api_hosts : []));
    const visibleModels = computed(() => (Array.isArray(payload.value.models) ? payload.value.models : []));
    const heroSummaryMetrics = computed(() => summaryHeroMetrics(t, payload.value.summary || {}));
    const summaryCosts = computed(() => summaryCostMetrics(t, payload.value.summary || {}, false));
    const summaryTokens = computed(() => summaryTokenMetrics(t, payload.value.summary || {}, false));
    const summaryMetaItems = computed(() => {
      const items = [];
      if (payload.value.updated_at) {
        items.push(`${t("stats_updated_at")}: ${formatTime(payload.value.updated_at)}`);
      }
      items.push(`${t("stats_projected_records")}: ${formatNumber(payload.value.projected_records)}`);
      if (Number(payload.value.skipped_records || 0) > 0) {
        items.push(`${t("stats_skipped_records")}: ${formatNumber(payload.value.skipped_records)}`);
      }
      return items;
    });
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

    function hostCostMetrics(item) {
      return costMetrics(t, item || {});
    }

    function hostTokenMetrics(item) {
      return tokenMetrics(t, item || {});
    }

    function modelLedgerCostColumns(items) {
      return visibleModelCostColumns(t, Array.isArray(items) ? items : []);
    }

    function modelLedgerTokenColumns(items) {
      return visibleModelTokenColumns(t, Array.isArray(items) ? items : []);
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
      heroSummaryMetrics,
      summaryCosts,
      summaryTokens,
      summaryMetaItems,
      onTabChange,
      hostCostMetrics,
      hostTokenMetrics,
      modelLedgerCostColumns,
      modelLedgerTokenColumns,
      formatModelLedgerValue,
      isModelLedgerValueUnavailable,
      modelLedgerValueToneClass,
      modelVendorMeta,
      formatNumber,
    };
  },
  template: `
    <AppPage :title="t('stats_title')">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

      <section class="stats-page">
        <header class="stats-hero block-default">
          <div class="stats-hero-copy">
            <p v-if="summaryMetaItems.length > 0" class="stats-hero-meta">
              <span v-for="item in summaryMetaItems" :key="item" class="stats-hero-meta-item">{{ item }}</span>
            </p>
          </div>

          <section class="stats-hero-spotlight">
            <span class="stats-hero-primary-label">{{ heroSummaryMetrics.primary.label }}</span>
            <span class="stats-hero-primary-value" :class="{ 'stats-hero-primary-value-unavailable': heroSummaryMetrics.primary.unavailable }">
              {{ heroSummaryMetrics.primary.value }}
            </span>
          </section>

          <div class="stats-hero-side">
            <div class="stats-hero-secondary-grid">
              <article v-for="item in heroSummaryMetrics.secondary" :key="item.key" class="stats-hero-secondary-item">
                <span class="stats-hero-secondary-label">{{ item.label }}</span>
                <span class="stats-hero-secondary-value" :class="{ 'stats-hero-secondary-value-unavailable': item.unavailable }">
                  {{ item.value }}
                </span>
              </article>
            </div>

            <div v-if="summaryCosts.length > 0 || summaryTokens.length > 0" class="stats-hero-detail-groups">
              <div v-if="summaryCosts.length > 0" class="stats-inline-meta stats-inline-meta-summary">
                <div v-for="item in summaryCosts" :key="'summary:cost:' + item.key" class="stats-inline-meta-item">
                  <span class="stats-inline-meta-label">{{ item.label }}</span>
                  <span class="stats-inline-meta-value">{{ item.value }}</span>
                </div>
              </div>

              <div v-if="summaryTokens.length > 0" class="stats-inline-meta stats-inline-meta-summary">
                <div v-for="item in summaryTokens" :key="'summary:token:' + item.key" class="stats-inline-meta-item">
                  <span class="stats-inline-meta-label">{{ item.label }}</span>
                  <span class="stats-inline-meta-value">{{ item.value }}</span>
                </div>
              </div>
            </div>
          </div>
        </header>

        <section class="stats-section">
          <QTabs
            class="stats-section-tabs"
            :tabs="statsTabs"
            :modelValue="selectedStatsTab"
            variant="plain"
            @change="onTabChange"
          />

          <div v-if="selectedStatsTab && selectedStatsTab.id === 'api_hosts'" class="stats-section-panel">
            <div v-if="visibleHosts.length === 0" class="stats-empty">{{ t("stats_no_data") }}</div>
            <div v-else class="stats-host-list">
              <article v-for="host in visibleHosts" :key="host.api_host" class="stats-host-block">
                <header class="stats-host-head">
                  <div class="stats-host-ident">
                    <span class="stats-host-eyebrow">{{ t("stats_api_host") }}</span>
                    <code class="stats-host-name">{{ host.api_host }}</code>
                  </div>
                  <div class="stats-request-pill">
                    <span class="stats-request-pill-label">{{ t("stats_requests") }}</span>
                    <span class="stats-request-pill-value">{{ formatNumber(host.requests) }}</span>
                  </div>
                </header>

                <section class="stats-band stats-band-cost">
                  <header class="stats-band-head">
                    <QIconWallet class="stats-band-icon icon" />
                    <span class="stats-band-title">{{ t("stats_costs") }}</span>
                  </header>
                  <div class="stats-band-grid">
                    <div v-for="item in hostCostMetrics(host)" :key="host.api_host + ':cost:' + item.key" class="stats-band-cell">
                      <span class="stats-ledger-label">{{ item.label }}</span>
                      <span class="stats-ledger-value" :class="{ 'stats-ledger-value-unavailable': item.unavailable }">{{ item.value }}</span>
                    </div>
                  </div>
                </section>

                <section class="stats-band stats-band-token">
                  <header class="stats-band-head">
                    <QIconBarChart class="stats-band-icon icon" />
                    <span class="stats-band-title">{{ t("stats_tokens") }}</span>
                  </header>
                  <div class="stats-band-grid">
                    <div v-for="item in hostTokenMetrics(host)" :key="host.api_host + ':token:' + item.key" class="stats-band-cell">
                      <span class="stats-ledger-label">{{ item.label }}</span>
                      <span class="stats-ledger-value" :class="{ 'stats-ledger-value-unavailable': item.unavailable }">{{ item.value }}</span>
                    </div>
                  </div>
                </section>

                <div v-if="Array.isArray(host.models) && host.models.length > 0" class="stats-model-table">
                  <div class="stats-model-ledger-scroll">
                    <table class="stats-model-ledger-table">
                      <thead>
                        <tr class="stats-model-ledger-group-row">
                          <th rowspan="2" class="stats-model-ledger-stub">{{ t("stats_model") }}</th>
                          <th rowspan="2" class="stats-model-ledger-stub stats-model-ledger-stub-requests">{{ t("stats_requests") }}</th>
                          <th
                            v-if="modelLedgerCostColumns(host.models).length > 0"
                            :colspan="modelLedgerCostColumns(host.models).length"
                            class="stats-model-ledger-group"
                          >
                            <span class="stats-model-ledger-group-copy">
                              <QIconWallet class="stats-model-ledger-group-icon icon" />
                              <span>{{ t("stats_costs") }}</span>
                            </span>
                          </th>
                          <th :colspan="modelLedgerTokenColumns(host.models).length" class="stats-model-ledger-group">
                            <span class="stats-model-ledger-group-copy">
                              <QIconBarChart class="stats-model-ledger-group-icon icon" />
                              <span>{{ t("stats_tokens") }}</span>
                            </span>
                          </th>
                        </tr>
                        <tr class="stats-model-ledger-column-row">
                          <th
                            v-for="column in modelLedgerCostColumns(host.models)"
                            :key="host.api_host + ':head:cost:' + column.key"
                            class="stats-model-ledger-column"
                          >
                            {{ column.label }}
                          </th>
                          <th
                            v-for="column in modelLedgerTokenColumns(host.models)"
                            :key="host.api_host + ':head:token:' + column.key"
                            class="stats-model-ledger-column"
                          >
                            {{ column.label }}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="model in host.models" :key="host.api_host + ':' + model.model" class="stats-model-ledger-row">
                          <th scope="row" class="stats-model-ledger-model">
                            <div class="stats-model-ident">
                              <span class="stats-model-vendor-badge" :class="{ 'stats-model-vendor-badge-fallback': !modelVendorMeta(model.model).icon }">
                                <img
                                  v-if="modelVendorMeta(model.model).icon"
                                  :src="modelVendorMeta(model.model).icon"
                                  :alt="modelVendorMeta(model.model).label"
                                  class="stats-model-vendor-image"
                                />
                                <QIconCpuChip v-else class="stats-model-vendor-fallback icon" />
                              </span>
                              <code class="stats-model-name">{{ model.model }}</code>
                            </div>
                          </th>
                          <td class="stats-model-ledger-value-cell stats-model-ledger-requests">{{ formatNumber(model.requests) }}</td>
                          <td
                            v-for="column in modelLedgerCostColumns(host.models)"
                            :key="host.api_host + ':' + model.model + ':cost:' + column.key"
                            class="stats-model-ledger-value-cell"
                            :class="[
                              { 'stats-model-ledger-value-cell-unavailable': isModelLedgerValueUnavailable(model, column) },
                              modelLedgerValueToneClass(model, column),
                            ]"
                          >
                            {{ formatModelLedgerValue(model, column) }}
                          </td>
                          <td
                            v-for="column in modelLedgerTokenColumns(host.models)"
                            :key="host.api_host + ':' + model.model + ':token:' + column.key"
                            class="stats-model-ledger-value-cell"
                            :class="{ 'stats-model-ledger-value-cell-unavailable': isModelLedgerValueUnavailable(model, column) }"
                          >
                            {{ formatModelLedgerValue(model, column) }}
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </article>
            </div>
          </div>

          <div v-else class="stats-section-panel">
            <div v-if="visibleModels.length === 0" class="stats-empty">{{ t("stats_no_data") }}</div>
            <div v-else class="stats-host-list">
              <section class="stats-host-block">
                <header class="stats-host-head">
                  <div class="stats-host-ident">
                    <span class="stats-host-eyebrow">{{ t("stats_group_models") }}</span>
                    <span class="stats-host-name">{{ t("stats_model") }}</span>
                  </div>
                </header>

                <div class="stats-model-table">
                  <div class="stats-model-ledger-scroll">
                    <table class="stats-model-ledger-table">
                      <thead>
                        <tr class="stats-model-ledger-group-row">
                          <th rowspan="2" class="stats-model-ledger-stub">{{ t("stats_model") }}</th>
                          <th rowspan="2" class="stats-model-ledger-stub stats-model-ledger-stub-requests">{{ t("stats_requests") }}</th>
                          <th
                            v-if="modelLedgerCostColumns(visibleModels).length > 0"
                            :colspan="modelLedgerCostColumns(visibleModels).length"
                            class="stats-model-ledger-group"
                          >
                            <span class="stats-model-ledger-group-copy">
                              <QIconWallet class="stats-model-ledger-group-icon icon" />
                              <span>{{ t("stats_costs") }}</span>
                            </span>
                          </th>
                          <th :colspan="modelLedgerTokenColumns(visibleModels).length" class="stats-model-ledger-group">
                            <span class="stats-model-ledger-group-copy">
                              <QIconBarChart class="stats-model-ledger-group-icon icon" />
                              <span>{{ t("stats_tokens") }}</span>
                            </span>
                          </th>
                        </tr>
                        <tr class="stats-model-ledger-column-row">
                          <th
                            v-for="column in modelLedgerCostColumns(visibleModels)"
                            :key="'models:head:cost:' + column.key"
                            class="stats-model-ledger-column"
                          >
                            {{ column.label }}
                          </th>
                          <th
                            v-for="column in modelLedgerTokenColumns(visibleModels)"
                            :key="'models:head:token:' + column.key"
                            class="stats-model-ledger-column"
                          >
                            {{ column.label }}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="model in visibleModels" :key="model.model" class="stats-model-ledger-row">
                          <th scope="row" class="stats-model-ledger-model">
                            <div class="stats-model-ident">
                              <span class="stats-model-vendor-badge" :class="{ 'stats-model-vendor-badge-fallback': !modelVendorMeta(model.model).icon }">
                                <img
                                  v-if="modelVendorMeta(model.model).icon"
                                  :src="modelVendorMeta(model.model).icon"
                                  :alt="modelVendorMeta(model.model).label"
                                  class="stats-model-vendor-image"
                                />
                                <QIconCpuChip v-else class="stats-model-vendor-fallback icon" />
                              </span>
                              <code class="stats-model-name">{{ model.model }}</code>
                            </div>
                          </th>
                          <td class="stats-model-ledger-value-cell stats-model-ledger-requests">{{ formatNumber(model.requests) }}</td>
                          <td
                            v-for="column in modelLedgerCostColumns(visibleModels)"
                            :key="model.model + ':cost:' + column.key"
                            class="stats-model-ledger-value-cell"
                            :class="[
                              { 'stats-model-ledger-value-cell-unavailable': isModelLedgerValueUnavailable(model, column) },
                              modelLedgerValueToneClass(model, column),
                            ]"
                          >
                            {{ formatModelLedgerValue(model, column) }}
                          </td>
                          <td
                            v-for="column in modelLedgerTokenColumns(visibleModels)"
                            :key="model.model + ':token:' + column.key"
                            class="stats-model-ledger-value-cell"
                            :class="{ 'stats-model-ledger-value-cell-unavailable': isModelLedgerValueUnavailable(model, column) }"
                          >
                            {{ formatModelLedgerValue(model, column) }}
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </section>
            </div>
          </div>
        </section>
      </section>
    </AppPage>
  `,
};

export default StatsView;
