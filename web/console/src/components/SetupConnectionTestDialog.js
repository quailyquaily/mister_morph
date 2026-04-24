import { computed, ref, watch } from "vue";
import { translate } from "../core/context";
import "./SetupConnectionTestDialog.css";

const TEST_CONNECTION_BENCHMARK_IDS = ["text_reply", "json_response", "tool_calling"];

const SetupConnectionTestDialog = {
  props: {
    modelValue: Boolean,
    loading: Boolean,
    error: {
      type: String,
      default: "",
    },
    benchmarks: {
      type: Array,
      default: () => [],
    },
    provider: {
      type: String,
      default: "",
    },
    apiBase: {
      type: String,
      default: "",
    },
    model: {
      type: String,
      default: "",
    },
    showIntro: {
      type: Boolean,
      default: true,
    },
  },
  emits: ["update:modelValue", "retry"],
  setup(props, { emit }) {
    const t = translate;
    const expandedRawBenchmarkId = ref("");
    const targetHost = computed(() => {
      const value = String(props.apiBase || "").trim();
      if (value === "") {
        return "";
      }
      try {
        return String(new URL(value).host || "").trim() || value;
      } catch {
        return value;
      }
    });
    const targetModel = computed(() => String(props.model || "").trim());
    const showTarget = computed(() => targetHost.value !== "" || targetModel.value !== "");

    const hasBenchmarks = computed(() => Array.isArray(props.benchmarks) && props.benchmarks.length > 0);
    const visibleBenchmarks = computed(() => {
      if (props.loading && !hasBenchmarks.value) {
        return TEST_CONNECTION_BENCHMARK_IDS.map((id) => ({
          id,
          ok: false,
          running: true,
          duration_ms: 0,
          detail: "",
          error: "",
          raw_response: "",
        }));
      }
      return (Array.isArray(props.benchmarks) ? props.benchmarks : []).map((item) => ({
        ...item,
        running: false,
      }));
    });
    const showBenchmarks = computed(() => props.loading || hasBenchmarks.value);

    function close() {
      expandedRawBenchmarkId.value = "";
      emit("update:modelValue", false);
    }

    function retry() {
      expandedRawBenchmarkId.value = "";
      emit("retry");
    }

    function isRawExpanded(item) {
      return String(item?.id || "").trim() !== "" && expandedRawBenchmarkId.value === String(item.id).trim();
    }

    function toggleRawResponse(item) {
      const id = String(item?.id || "").trim();
      if (!id) {
        return;
      }
      expandedRawBenchmarkId.value = expandedRawBenchmarkId.value === id ? "" : id;
    }

    function rawResponseText(item) {
      const raw = String(item?.raw_response || "").trim();
      return raw === "" ? t("setup_llm_test_raw_empty") : raw;
    }

    watch(
      () => props.modelValue,
      (open) => {
        if (!open) {
          expandedRawBenchmarkId.value = "";
        }
      },
    );

    watch(
      () => props.loading,
      (loading) => {
        if (loading) {
          expandedRawBenchmarkId.value = "";
        }
      },
    );

    watch(
      () => props.benchmarks,
      (benchmarks) => {
        if (!Array.isArray(benchmarks) || benchmarks.every((item) => String(item?.id || "").trim() !== expandedRawBenchmarkId.value)) {
          expandedRawBenchmarkId.value = "";
        }
      },
      { deep: true },
    );

    function benchmarkStatusClass(item) {
      if (item?.running) {
        return "is-loading";
      }
      return item?.ok ? "is-ok" : "is-failed";
    }

    function benchmarkDetailToggleClass(item) {
      return item?.ok ? "connection-test-benchmark-detail" : "connection-test-benchmark-error";
    }

    function benchmarkSummaryText(item) {
      const id = String(item?.id || "").trim();
      if (!id) {
        return item?.ok ? String(item?.detail || "").trim() : String(item?.error || "").trim();
      }
      const key = `setup_llm_test_benchmark_${id}_${item?.ok ? "ok" : "failed"}`;
      const translated = t(key);
      if (translated && translated !== key) {
        return translated;
      }
      return item?.ok ? String(item?.detail || "").trim() : String(item?.error || "").trim();
    }

    function formatBenchmarkSeconds(durationMS) {
      const ms = Number(durationMS || 0);
      if (!Number.isFinite(ms) || ms <= 0) {
        return "0s";
      }
      const seconds = ms / 1000;
      const digits = seconds >= 10 ? 1 : 2;
      return `${seconds
        .toFixed(digits)
        .replace(/\.0+$/, "")
        .replace(/(\.\d*[1-9])0+$/, "$1")}s`;
    }

    return {
      t,
      targetHost,
      targetModel,
      showTarget,
      visibleBenchmarks,
      showBenchmarks,
      close,
      retry,
      isRawExpanded,
      toggleRawResponse,
      rawResponseText,
      benchmarkStatusClass,
      benchmarkDetailToggleClass,
      benchmarkSummaryText,
      formatBenchmarkSeconds,
    };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      @close="close"
    >
      <template #header>
        <header class="app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ t("setup_llm_test_title") }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
      </template>

      <section class="connection-test-dialog">
        <div v-if="showTarget || showIntro" class="connection-test-context">
          <div v-if="showTarget" class="connection-test-intro">
            <div v-if="targetHost" class="connection-test-target-row">
              <QIconCompass class="connection-test-target-icon icon" />
              <span class="connection-test-target-text">{{ targetHost }}</span>
            </div>
            <div v-if="targetModel" class="connection-test-target-row">
              <QIconCpuChip class="connection-test-target-icon icon" />
              <span class="connection-test-target-text">{{ targetModel }}</span>
            </div>
          </div>
          <p v-else class="connection-test-intro">{{ t("setup_llm_test_intro") }}</p>
        </div>

        <QFence
          v-if="error"
          type="danger"
          icon="QIconCloseCircle"
          :text="error"
        />

        <div v-if="showBenchmarks" class="connection-test-result">
          <div class="connection-test-benchmark-list">
            <article
              v-for="item in visibleBenchmarks"
              :key="item.id"
              :class="['connection-test-benchmark', { 'is-ok': item.ok, 'is-failed': !item.ok, 'is-loading': item.running }]"
            >
              <div class="connection-test-benchmark-summary">
                <div class="connection-test-benchmark-main">
                  <p class="connection-test-benchmark-title">{{ t('setup_llm_test_benchmark_' + item.id) }}</p>
                  <p v-if="item.running" class="connection-test-benchmark-detail">{{ t("setup_llm_test_running") }}</p>
                  <div
                    v-else-if="(item.ok && item.detail) || item.error"
                    role="button"
                    tabindex="0"
                    :aria-expanded="isRawExpanded(item) ? 'true' : 'false'"
                    :class="['connection-test-benchmark-toggle', benchmarkDetailToggleClass(item)]"
                    @click="toggleRawResponse(item)"
                    @keydown.enter.prevent="toggleRawResponse(item)"
                    @keydown.space.prevent="toggleRawResponse(item)"
                  >
                    <span class="connection-test-benchmark-toggle-text">{{ benchmarkSummaryText(item) }}</span>
                    <QIconArrowRight :class="['connection-test-benchmark-toggle-icon', { 'is-open': isRawExpanded(item) }]" />
                  </div>
                </div>
                <div class="connection-test-benchmark-side">
                  <div class="connection-test-benchmark-status-row">
                    <span v-if="item.running" class="connection-test-benchmark-spinner" aria-hidden="true"></span>
                    <strong v-else :class="['connection-test-benchmark-status', benchmarkStatusClass(item)]">
                      {{ item.ok ? t("setup_llm_test_status_ok") : t("setup_llm_test_status_failed") }}
                    </strong>
                  </div>
                  <span v-if="!item.running" class="connection-test-benchmark-time">{{ formatBenchmarkSeconds(item.duration_ms) }}</span>
                </div>
              </div>
              <pre v-if="!item.running && isRawExpanded(item)" class="connection-test-benchmark-raw-output">{{ rawResponseText(item) }}</pre>
            </article>
          </div>
        </div>

        <div class="connection-test-actions">
          <QButton class="primary" :loading="loading" @click="retry">
            {{ loading ? t("setup_llm_test_running") : t("setup_llm_test_retry") }}
          </QButton>
          <QButton class="outlined" @click="close">{{ t("action_close") }}</QButton>
        </div>
      </section>
    </QDialog>
  `,
};

export default SetupConnectionTestDialog;
