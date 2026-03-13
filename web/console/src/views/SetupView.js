import { computed, onMounted, reactive, ref } from "vue";
import "./SetupView.css";

import { applySetup, applyLanguageChange, fetchSetupStatus, localeState, translate } from "../core/context";
import { isDesktopHostLikely, requestDesktopRestart } from "../core/desktopBridge";

const SetupView = {
  setup() {
    const t = translate;
    const lang = computed(() => localeState.lang);
    const busy = ref(false);
    const err = ref("");
    const ok = ref("");
    const missingFields = ref([]);
    const restarting = ref(false);
    const form = reactive({
      llmProvider: "",
      llmModel: "",
      llmEndpoint: "",
      llmAPIKey: "",
      consolePassword: "",
      endpointName: "",
      endpointURL: "",
      endpointToken: "",
    });

    function trim(value) {
      return String(value || "").trim();
    }

    function hasMissing(field) {
      return missingFields.value.includes(field);
    }

    function requireField(value, label) {
      if (trim(value) === "") {
        return `${label}: required`;
      }
      return "";
    }

    function formatValidationError(e) {
      const payload = e && e.payload && typeof e.payload === "object" ? e.payload : null;
      if (payload && Array.isArray(payload.errors) && payload.errors.length > 0) {
        return payload.errors
          .map((item) => {
            const field = item && typeof item.field === "string" ? item.field : "setup";
            const message = item && typeof item.message === "string" ? item.message : "invalid";
            return `${field}: ${message}`;
          })
          .join("; ");
      }
      return (e && e.message) || t("setup_apply_failed");
    }

    function buildPayload() {
      const payload = {};

      const llm = {};
      if (trim(form.llmProvider) !== "") {
        llm.provider = trim(form.llmProvider);
      }
      if (trim(form.llmModel) !== "") {
        llm.model = trim(form.llmModel);
      }
      if (trim(form.llmEndpoint) !== "") {
        llm.endpoint = trim(form.llmEndpoint);
      }
      if (trim(form.llmAPIKey) !== "") {
        llm.api_key = trim(form.llmAPIKey);
      }
      if (Object.keys(llm).length > 0) {
        payload.llm = llm;
      }

      const consolePayload = {};
      if (trim(form.consolePassword) !== "") {
        consolePayload.password = trim(form.consolePassword);
      }
      const endpoint = {};
      if (trim(form.endpointName) !== "") {
        endpoint.name = trim(form.endpointName);
      }
      if (trim(form.endpointURL) !== "") {
        endpoint.url = trim(form.endpointURL);
      }
      if (trim(form.endpointToken) !== "") {
        endpoint.auth_token = trim(form.endpointToken);
      }
      if (Object.keys(endpoint).length > 0) {
        consolePayload.endpoints = [endpoint];
      }
      if (Object.keys(consolePayload).length > 0) {
        payload.console = consolePayload;
      }

      return payload;
    }

    async function loadStatus() {
      err.value = "";
      try {
        const status = await fetchSetupStatus();
        missingFields.value = Array.isArray(status.missing_fields) ? status.missing_fields : [];
      } catch (e) {
        err.value = e.message || t("setup_status_failed");
      }
    }

    async function submit() {
      if (busy.value) {
        return;
      }
      const localErrors = [];
      if (hasMissing("llm.provider")) {
        const msg = requireField(form.llmProvider, t("setup_llm_provider"));
        if (msg) {
          localErrors.push(msg);
        }
      }
      if (hasMissing("llm.model")) {
        const msg = requireField(form.llmModel, t("setup_llm_model"));
        if (msg) {
          localErrors.push(msg);
        }
      }
      if (hasMissing("llm.api_key")) {
        const msg = requireField(form.llmAPIKey, t("setup_llm_api_key"));
        if (msg) {
          localErrors.push(msg);
        }
      }
      if (hasMissing("console.password_hash")) {
        const msg = requireField(form.consolePassword, t("setup_console_password"));
        if (msg) {
          localErrors.push(msg);
        }
      }
      if (hasMissing("console.endpoints")) {
        const msgA = requireField(form.endpointName, t("setup_endpoint_name"));
        const msgB = requireField(form.endpointURL, t("setup_endpoint_url"));
        const msgC = requireField(form.endpointToken, t("setup_endpoint_token"));
        if (msgA) {
          localErrors.push(msgA);
        }
        if (msgB) {
          localErrors.push(msgB);
        }
        if (msgC) {
          localErrors.push(msgC);
        }
      }
      if (localErrors.length > 0) {
        err.value = localErrors.join("; ");
        return;
      }

      busy.value = true;
      err.value = "";
      ok.value = "";
      try {
        const resp = await applySetup(buildPayload());
        const needsRestart = Boolean(resp && resp.restart_required);
        if (!needsRestart) {
          ok.value = t("setup_apply_success");
          return;
        }

        restarting.value = true;
        const restarted = await requestDesktopRestart();
        if (restarted) {
          ok.value = t("setup_restarting");
          return;
        }
        if (isDesktopHostLikely()) {
          ok.value = t("setup_restart_failed_manual");
          return;
        }
        ok.value = t("setup_apply_success_manual");
      } catch (e) {
        err.value = formatValidationError(e);
      } finally {
        restarting.value = false;
        busy.value = false;
      }
    }

    onMounted(() => {
      void loadStatus();
    });

    return {
      t,
      lang,
      busy,
      restarting,
      err,
      ok,
      form,
      missingFields,
      submit,
      onLanguageChange: applyLanguageChange,
    };
  },
  template: `
    <section class="setup-page">
      <div class="setup-box stack">
        <div class="setup-header">
          <h1 class="setup-title">{{ t("setup_title") }}</h1>
          <p class="setup-subtitle">{{ t("setup_subtitle") }}</p>
          <QLanguageSelector :lang="lang" :presist="true" @change="onLanguageChange" />
        </div>

        <QFence v-if="missingFields.length > 0" type="warning" icon="QIconInfoCircle" :text="t('setup_missing_fields', { fields: missingFields.join(', ') })" />
        <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
        <QFence v-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />

        <div class="stack">
          <QInput v-model="form.llmProvider" :label="t('setup_llm_provider')" :disabled="busy" />
          <QInput v-model="form.llmModel" :label="t('setup_llm_model')" :disabled="busy" />
          <QInput v-model="form.llmEndpoint" :label="t('setup_llm_endpoint')" :disabled="busy" />
          <QInput v-model="form.llmAPIKey" inputType="password" :label="t('setup_llm_api_key')" :disabled="busy" />
          <QInput v-model="form.consolePassword" inputType="password" :label="t('setup_console_password')" :disabled="busy" />
          <QInput v-model="form.endpointName" :label="t('setup_endpoint_name')" :disabled="busy" />
          <QInput v-model="form.endpointURL" :label="t('setup_endpoint_url')" :disabled="busy" />
          <QInput v-model="form.endpointToken" inputType="password" :label="t('setup_endpoint_token')" :disabled="busy" />
          <QButton class="primary" :loading="busy || restarting" @click="submit">{{ t("setup_submit") }}</QButton>
        </div>
      </div>
    </section>
  `,
};

export default SetupView;
