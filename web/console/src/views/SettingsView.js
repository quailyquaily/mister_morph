import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./SettingsView.css";

import AppPage from "../components/AppPage";
import {
  apiFetch,
  applyLanguageChange,
  clearAuth,
  endpointState,
  loadEndpoints,
  localeState,
  runtimeEndpointByRef,
  translate,
} from "../core/context";

function tuiKicker(left, right) {
  return `[ ${String(left || "").trim().toUpperCase()} // ${String(right || "").trim().toUpperCase()} ]`;
}

const PROVIDER_OPTIONS = [
  { title: "OpenAI", value: "openai" },
  { title: "Anthropic", value: "anthropic" },
  { title: "Gemini", value: "gemini" },
  { title: "xAI", value: "xai" },
  { title: "DeepSeek", value: "deepseek" },
  { title: "Azure", value: "azure" },
  { title: "Bedrock", value: "bedrock" },
  { title: "Cloudflare", value: "cloudflare" },
  { title: "OpenAI Compatible", value: "openai_custom" },
  { title: "Susanoo", value: "susanoo" },
];

const MULTIMODAL_SOURCES = [
  { id: "telegram", titleKey: "settings_multimodal_source_telegram", noteKey: "settings_multimodal_note_telegram" },
  { id: "slack", titleKey: "settings_multimodal_source_slack", noteKey: "settings_multimodal_note_slack" },
  { id: "line", titleKey: "settings_multimodal_source_line", noteKey: "settings_multimodal_note_line" },
  {
    id: "remote_download",
    titleKey: "settings_multimodal_source_remote_download",
    noteKey: "settings_multimodal_note_remote_download",
  },
];

const TOOL_ITEMS = [
  { id: "write_file", titleKey: "settings_tool_write_file", noteKey: "settings_tool_note_write_file" },
  { id: "contacts_send", titleKey: "settings_tool_contacts_send", noteKey: "settings_tool_note_contacts_send" },
  { id: "todo_update", titleKey: "settings_tool_todo_update", noteKey: "settings_tool_note_todo_update" },
  { id: "plan_create", titleKey: "settings_tool_plan_create", noteKey: "settings_tool_note_plan_create" },
  { id: "url_fetch", titleKey: "settings_tool_url_fetch", noteKey: "settings_tool_note_url_fetch" },
  { id: "web_search", titleKey: "settings_tool_web_search", noteKey: "settings_tool_note_web_search" },
  { id: "bash", titleKey: "settings_tool_bash", noteKey: "settings_tool_note_bash" },
];

const MANAGED_RUNTIME_ITEMS = [
  { id: "telegram", titleKey: "settings_console_runtime_telegram", noteKey: "settings_console_runtime_note_telegram" },
  { id: "slack", titleKey: "settings_console_runtime_slack", noteKey: "settings_console_runtime_note_slack" },
];

const LOCAL_CONSOLE_ENDPOINT_REF = "ep_console_local";

function buildAgentSnapshot(state) {
  return JSON.stringify({
    llm: {
      provider: String(state.llm.provider || "").trim(),
      endpoint: String(state.llm.endpoint || "").trim(),
      model: String(state.llm.model || "").trim(),
      api_key: String(state.llm.api_key || "").trim(),
      reasoning_effort: String(state.llm.reasoning_effort || "").trim(),
      tools_emulation_mode: String(state.llm.tools_emulation_mode || "").trim(),
    },
    multimodal: {
      telegram: !!state.multimodal.telegram,
      slack: !!state.multimodal.slack,
      line: !!state.multimodal.line,
      remote_download: !!state.multimodal.remote_download,
    },
    tools: {
      write_file: !!state.tools.write_file,
      contacts_send: !!state.tools.contacts_send,
      todo_update: !!state.tools.todo_update,
      plan_create: !!state.tools.plan_create,
      url_fetch: !!state.tools.url_fetch,
      web_search: !!state.tools.web_search,
      bash: !!state.tools.bash,
    },
  });
}

function buildConsoleSnapshot(state) {
  return JSON.stringify({
    managed_runtimes: {
      telegram: !!state.managedRuntimes.telegram,
      slack: !!state.managedRuntimes.slack,
    },
  });
}

const SettingsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const lang = computed(() => localeState.lang);
    const loggingOut = ref(false);
    const agentLoading = ref(false);
    const agentSaving = ref(false);
    const agentErr = ref("");
    const agentOk = ref("");
    const llmConfigPath = ref("");
    const loadedSnapshot = ref("");
    const consoleLoading = ref(false);
    const consoleSaving = ref(false);
    const consoleErr = ref("");
    const consoleOk = ref("");
    const consoleConfigPath = ref("");
    const loadedConsoleSnapshot = ref("");

    const state = reactive({
      llm: {
        provider: "",
        endpoint: "",
        model: "",
        api_key: "",
        reasoning_effort: "",
        tools_emulation_mode: "",
      },
      multimodal: {
        telegram: false,
        slack: false,
        line: false,
        remote_download: false,
      },
      tools: {
        write_file: true,
        contacts_send: true,
        todo_update: true,
        plan_create: true,
        url_fetch: true,
        web_search: true,
        bash: true,
      },
      managedRuntimes: {
        telegram: false,
        slack: false,
      },
    });

    const providerItems = computed(() => PROVIDER_OPTIONS);
    const providerItem = computed(
      () => providerItems.value.find((item) => item.value === state.llm.provider) || null
    );
    const reasoningEffortItems = computed(() => [
      { title: t("settings_llm_reasoning_none"), value: "" },
      { title: t("settings_llm_reasoning_minimal"), value: "minimal" },
      { title: t("settings_llm_reasoning_low"), value: "low" },
      { title: t("settings_llm_reasoning_medium"), value: "medium" },
      { title: t("settings_llm_reasoning_high"), value: "high" },
      { title: t("settings_llm_reasoning_max"), value: "max" },
      { title: t("settings_llm_reasoning_xhigh"), value: "xhigh" },
    ]);
    const reasoningEffortItem = computed(
      () => reasoningEffortItems.value.find((item) => item.value === state.llm.reasoning_effort) || reasoningEffortItems.value[0]
    );
    const toolsEmulationItems = computed(() => [
      { title: t("settings_llm_tools_emulation_off"), value: "off" },
      { title: t("settings_llm_tools_emulation_fallback"), value: "fallback" },
      { title: t("settings_llm_tools_emulation_force"), value: "force" },
    ]);
    const toolsEmulationItem = computed(
      () =>
        toolsEmulationItems.value.find((item) => item.value === state.llm.tools_emulation_mode) ||
        toolsEmulationItems.value[0]
    );

    const multimodalItems = computed(() => MULTIMODAL_SOURCES);
    const toolItems = computed(() => TOOL_ITEMS);
    const managedRuntimeItems = computed(() => MANAGED_RUNTIME_ITEMS);
    const selectedEndpoint = computed(() => runtimeEndpointByRef(endpointState.selectedRef));
    const showConsoleManagedSettings = computed(
      () => String(selectedEndpoint.value?.endpoint_ref || "").trim() === LOCAL_CONSOLE_ENDPOINT_REF
    );

    const agentDirty = computed(() => buildAgentSnapshot(state) !== loadedSnapshot.value);
    const agentSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || !String(state.llm.provider || "").trim() || !agentDirty.value
    );
    const consoleDirty = computed(() => buildConsoleSnapshot(state) !== loadedConsoleSnapshot.value);
    const consoleSaveDisabled = computed(() => consoleLoading.value || consoleSaving.value || !consoleDirty.value);

    function applyPayload(data) {
      const llm = data?.llm && typeof data.llm === "object" ? data.llm : {};
      const multimodal = data?.multimodal && typeof data.multimodal === "object" ? data.multimodal : {};
      const tools = data?.tools && typeof data.tools === "object" ? data.tools : {};
      const imageSources = Array.isArray(multimodal.image_sources) ? multimodal.image_sources : [];

      state.llm.provider = typeof llm.provider === "string" ? llm.provider : "";
      state.llm.endpoint = typeof llm.endpoint === "string" ? llm.endpoint : "";
      state.llm.model = typeof llm.model === "string" ? llm.model : "";
      state.llm.api_key = typeof llm.api_key === "string" ? llm.api_key : "";
      state.llm.reasoning_effort = typeof llm.reasoning_effort === "string" ? llm.reasoning_effort : "";
      state.llm.tools_emulation_mode =
        typeof llm.tools_emulation_mode === "string" ? llm.tools_emulation_mode : "off";

      for (const item of MULTIMODAL_SOURCES) {
        state.multimodal[item.id] = imageSources.includes(item.id);
      }
      state.tools.write_file = !!tools.write_file_enabled;
      state.tools.contacts_send = !!tools.contacts_send_enabled;
      state.tools.todo_update = !!tools.todo_update_enabled;
      state.tools.plan_create = !!tools.plan_create_enabled;
      state.tools.url_fetch = !!tools.url_fetch_enabled;
      state.tools.web_search = !!tools.web_search_enabled;
      state.tools.bash = !!tools.bash_enabled;

      loadedSnapshot.value = buildAgentSnapshot(state);
    }

    async function loadAgentSettings() {
      agentLoading.value = true;
      agentErr.value = "";
      agentOk.value = "";
      try {
        const data = await apiFetch("/settings/agent");
        llmConfigPath.value = typeof data.config_path === "string" ? data.config_path : "";
        applyPayload(data);
      } catch (e) {
        agentErr.value = e.message || t("msg_load_failed");
      } finally {
        agentLoading.value = false;
      }
    }

    function applyConsolePayload(data) {
      const values = Array.isArray(data?.managed_runtimes) ? data.managed_runtimes : [];
      for (const item of MANAGED_RUNTIME_ITEMS) {
        state.managedRuntimes[item.id] = values.includes(item.id);
      }
      loadedConsoleSnapshot.value = buildConsoleSnapshot(state);
    }

    async function loadConsoleSettings() {
      if (!showConsoleManagedSettings.value) {
        return;
      }
      consoleLoading.value = true;
      consoleErr.value = "";
      consoleOk.value = "";
      try {
        const data = await apiFetch("/settings/console");
        consoleConfigPath.value = typeof data.config_path === "string" ? data.config_path : "";
        applyConsolePayload(data);
      } catch (e) {
        consoleErr.value = e.message || t("msg_load_failed");
      } finally {
        consoleLoading.value = false;
      }
    }

    function buildSavePayload() {
      return {
        llm: {
          provider: state.llm.provider,
          endpoint: state.llm.endpoint,
          model: state.llm.model,
          api_key: state.llm.api_key,
          reasoning_effort: state.llm.reasoning_effort,
          tools_emulation_mode: state.llm.tools_emulation_mode,
        },
        multimodal: {
          image_sources: MULTIMODAL_SOURCES.filter((item) => state.multimodal[item.id]).map((item) => item.id),
        },
        tools: {
          write_file_enabled: state.tools.write_file,
          contacts_send_enabled: state.tools.contacts_send,
          todo_update_enabled: state.tools.todo_update,
          plan_create_enabled: state.tools.plan_create,
          url_fetch_enabled: state.tools.url_fetch,
          web_search_enabled: state.tools.web_search,
          bash_enabled: state.tools.bash,
        },
      };
    }

    function buildConsoleSavePayload() {
      return {
        managed_runtimes: MANAGED_RUNTIME_ITEMS.filter((item) => state.managedRuntimes[item.id]).map((item) => item.id),
      };
    }

    async function saveAgentSettings() {
      if (agentSaveDisabled.value) {
        return;
      }
      agentSaving.value = true;
      agentErr.value = "";
      agentOk.value = "";
      try {
        const payload = await apiFetch("/settings/agent", {
          method: "PUT",
          body: buildSavePayload(),
        });
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        applyPayload(payload);
        await loadEndpoints();
        agentOk.value = t("msg_save_success");
      } catch (e) {
        agentErr.value = e.message || t("msg_save_failed");
      } finally {
        agentSaving.value = false;
      }
    }

    async function saveConsoleSettings() {
      if (consoleSaveDisabled.value || !showConsoleManagedSettings.value) {
        return;
      }
      consoleSaving.value = true;
      consoleErr.value = "";
      consoleOk.value = "";
      try {
        const payload = await apiFetch("/settings/console", {
          method: "PUT",
          body: buildConsoleSavePayload(),
        });
        consoleConfigPath.value =
          typeof payload.config_path === "string" ? payload.config_path : consoleConfigPath.value;
        applyConsolePayload(payload);
        consoleOk.value = t("msg_save_success");
      } catch (e) {
        consoleErr.value = e.message || t("msg_save_failed");
      } finally {
        consoleSaving.value = false;
      }
    }

    async function logout() {
      loggingOut.value = true;
      try {
        await apiFetch("/auth/logout", { method: "POST" });
      } catch {
        // ignore logout failure
      } finally {
        clearAuth();
        router.replace("/login");
        loggingOut.value = false;
      }
    }

    function onProviderChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      state.llm.provider = String(item.value || "").trim();
    }

    function onReasoningEffortChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      state.llm.reasoning_effort = String(item.value || "").trim();
    }

    function onToolsEmulationChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      state.llm.tools_emulation_mode = String(item.value || "").trim();
    }

    function setMultimodalSource(id, value) {
      if (!Object.prototype.hasOwnProperty.call(state.multimodal, id)) {
        return;
      }
      state.multimodal[id] = !!value;
    }

    function setToolEnabled(id, value) {
      if (!Object.prototype.hasOwnProperty.call(state.tools, id)) {
        return;
      }
      state.tools[id] = !!value;
    }

    function setManagedRuntimeEnabled(id, value) {
      if (!Object.prototype.hasOwnProperty.call(state.managedRuntimes, id)) {
        return;
      }
      state.managedRuntimes[id] = !!value;
    }

    onMounted(() => {
      void loadAgentSettings();
    });

    watch(
      showConsoleManagedSettings,
      (enabled) => {
        consoleErr.value = "";
        consoleOk.value = "";
        if (enabled) {
          void loadConsoleSettings();
        }
      },
      { immediate: true }
    );

    return {
      t,
      lang,
      loggingOut,
      agentLoading,
      agentSaving,
      agentErr,
      agentOk,
      llmConfigPath,
      consoleLoading,
      consoleSaving,
      consoleErr,
      consoleOk,
      consoleConfigPath,
      state,
      providerItems,
      providerItem,
      reasoningEffortItems,
      reasoningEffortItem,
      toolsEmulationItems,
      toolsEmulationItem,
      multimodalItems,
      toolItems,
      managedRuntimeItems,
      agentSaveDisabled,
      consoleSaveDisabled,
      showConsoleManagedSettings,
      logout,
      saveAgentSettings,
      saveConsoleSettings,
      onProviderChange,
      onReasoningEffortChange,
      onToolsEmulationChange,
      setMultimodalSource,
      setToolEnabled,
      setManagedRuntimeEnabled,
      tuiKicker,
      onLanguageChange: applyLanguageChange,
    };
  },
  template: `
    <AppPage :title="t('settings_title')">
      <div class="settings-grid">
        <section class="settings-section">
          <h2 class="ui-kicker">{{ tuiKicker(t("settings_agent_title"), t("settings_agent_block_title")) }}</h2>
          <QFence v-if="agentErr" type="danger" icon="QIconCloseCircle" :text="agentErr" />
          <QFence v-if="agentOk" type="success" icon="QIconCheckCircle" :text="agentOk" />

          <article class="ui-track-panel settings-card">
            <div class="settings-card-copy">
              <h3 class="settings-card-title">{{ t("settings_agent_block_title") }}</h3>
              <p class="settings-card-note">{{ t("settings_agent_llm_hint", { path: llmConfigPath || "config.yaml" }) }}</p>
            </div>
            <div class="settings-form-grid">
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_agent_provider_label") }}</span>
                <QDropdownMenu
                  :key="state.llm.provider || 'provider'"
                  :items="providerItems"
                  :initialItem="providerItem"
                  :placeholder="t('settings_agent_provider_placeholder')"
                  @change="onProviderChange"
                />
              </label>
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_agent_endpoint_label") }}</span>
                <QInput
                  v-model="state.llm.endpoint"
                  :placeholder="t('settings_agent_endpoint_placeholder')"
                  :disabled="agentLoading || agentSaving"
                />
              </label>
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_agent_model_label") }}</span>
                <QInput
                  v-model="state.llm.model"
                  :placeholder="t('settings_agent_model_placeholder')"
                  :disabled="agentLoading || agentSaving"
                />
              </label>
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_agent_api_key_label") }}</span>
                <QInput
                  v-model="state.llm.api_key"
                  inputType="password"
                  :placeholder="t('settings_agent_api_key_placeholder')"
                  :disabled="agentLoading || agentSaving"
                />
              </label>
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_llm_reasoning_label") }}</span>
                <QDropdownMenu
                  :key="state.llm.reasoning_effort || 'reasoning'"
                  :items="reasoningEffortItems"
                  :initialItem="reasoningEffortItem"
                  :placeholder="t('settings_llm_reasoning_placeholder')"
                  @change="onReasoningEffortChange"
                />
              </label>
              <label class="settings-field">
                <span class="settings-field-label">{{ t("settings_llm_tools_emulation_label") }}</span>
                <QDropdownMenu
                  :key="state.llm.tools_emulation_mode || 'tools-emulation'"
                  :items="toolsEmulationItems"
                  :initialItem="toolsEmulationItem"
                  :placeholder="t('settings_llm_tools_emulation_placeholder')"
                  @change="onToolsEmulationChange"
                />
              </label>
            </div>
          </article>

          <article class="ui-track-panel settings-card">
            <div class="settings-card-copy">
              <h3 class="settings-card-title">{{ t("settings_multimodal_title") }}</h3>
              <p class="settings-card-note">{{ t("settings_multimodal_hint") }}</p>
            </div>
            <div class="settings-toggle-grid">
              <div v-for="item in multimodalItems" :key="item.id" class="settings-toggle-row">
                <div class="settings-toggle-copy">
                  <strong class="settings-toggle-title">{{ t(item.titleKey) }}</strong>
                  <span class="settings-toggle-note">{{ t(item.noteKey) }}</span>
                </div>
                <QSwitch
                  :modelValue="state.multimodal[item.id]"
                  :disabled="agentLoading || agentSaving"
                  @update:modelValue="setMultimodalSource(item.id, $event)"
                />
              </div>
            </div>
          </article>

          <article class="ui-track-panel settings-card">
            <div class="settings-card-copy">
              <h3 class="settings-card-title">{{ t("settings_tools_title") }}</h3>
              <p class="settings-card-note">{{ t("settings_tools_hint") }}</p>
            </div>
            <div class="settings-toggle-grid">
              <div v-for="item in toolItems" :key="item.id" class="settings-toggle-row">
                <div class="settings-toggle-copy">
                  <strong class="settings-toggle-title">{{ t(item.titleKey) }}</strong>
                  <span class="settings-toggle-note">{{ t(item.noteKey) }}</span>
                </div>
                <QSwitch
                  :modelValue="state.tools[item.id]"
                  :disabled="agentLoading || agentSaving"
                  @update:modelValue="setToolEnabled(item.id, $event)"
                />
              </div>
            </div>
            <div class="settings-card-actions">
              <QButton class="primary" :loading="agentSaving" :disabled="agentSaveDisabled" @click="saveAgentSettings">
                {{ t("action_save") }}
              </QButton>
            </div>
          </article>
        </section>

        <section class="settings-section">
          <h2 class="ui-kicker">{{ tuiKicker(t("settings_console_title"), t("settings_session_title")) }}</h2>
          <QFence v-if="showConsoleManagedSettings && consoleErr" type="danger" icon="QIconCloseCircle" :text="consoleErr" />
          <QFence v-if="showConsoleManagedSettings && consoleOk" type="success" icon="QIconCheckCircle" :text="consoleOk" />

          <article v-if="showConsoleManagedSettings" class="ui-track-panel settings-card">
            <div class="settings-card-copy">
              <h3 class="settings-card-title">{{ t("settings_console_runtime_title") }}</h3>
              <p class="settings-card-note">
                {{ t("settings_console_runtime_hint", { path: consoleConfigPath || "config.yaml" }) }}
              </p>
            </div>
            <div class="settings-toggle-grid">
              <div v-for="item in managedRuntimeItems" :key="item.id" class="settings-toggle-row">
                <div class="settings-toggle-copy">
                  <strong class="settings-toggle-title">{{ t(item.titleKey) }}</strong>
                  <span class="settings-toggle-note">{{ t(item.noteKey) }}</span>
                </div>
                <QSwitch
                  :modelValue="state.managedRuntimes[item.id]"
                  :disabled="consoleLoading || consoleSaving"
                  @update:modelValue="setManagedRuntimeEnabled(item.id, $event)"
                />
              </div>
            </div>
            <div class="settings-card-actions">
              <QButton class="primary" :loading="consoleSaving" :disabled="consoleSaveDisabled" @click="saveConsoleSettings">
                {{ t("action_save") }}
              </QButton>
            </div>
          </article>

          <article class="ui-track-panel settings-card">
            <div class="settings-console-row">
              <div class="settings-card-copy">
                <h3 class="settings-card-title">{{ t("settings_language_title") }}</h3>
                <p class="settings-card-note">{{ t("settings_language_hint") }}</p>
              </div>
              <QLanguageSelector :lang="lang" :presist="true" @change="onLanguageChange" />
            </div>
            <div class="settings-console-row settings-console-row-end">
              <div class="settings-card-copy">
                <h3 class="settings-card-title">{{ t("settings_session_title") }}</h3>
                <p class="settings-card-note">{{ t("settings_session_hint") }}</p>
              </div>
              <QButton class="danger" :loading="loggingOut" @click="logout">{{ t("action_logout") }}</QButton>
            </div>
          </article>
        </section>
      </div>
    </AppPage>
  `,
};

export default SettingsView;
