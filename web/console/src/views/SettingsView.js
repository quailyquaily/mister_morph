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
  const lhs = String(left || "").trim();
  const rhs = String(right || "").trim();
  if (lhs && rhs) {
    return `[ ${lhs.toUpperCase()} // ${rhs.toUpperCase()} ]`;
  }
  return `[ ${(lhs || rhs).toUpperCase()} ]`;
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
    const selectedSectionID = ref("agent");

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

    const settingsSections = computed(() => {
      const items = [
        {
          id: "agent",
          title: t("settings_agent_block_title"),
          meta: t("settings_section_agent_meta"),
          groupTitle: t("settings_agent_title"),
          saveKind: "agent",
        },
        {
          id: "inputs",
          title: t("settings_multimodal_title"),
          meta: t("settings_section_inputs_meta"),
          groupTitle: t("settings_agent_title"),
          saveKind: "agent",
        },
        {
          id: "tools",
          title: t("settings_tools_title"),
          meta: t("settings_section_tools_meta"),
          groupTitle: t("settings_agent_title"),
          saveKind: "agent",
        },
      ];
      if (showConsoleManagedSettings.value) {
        items.push({
          id: "runtimes",
          title: t("settings_console_runtime_title"),
          meta: t("settings_section_runtimes_meta"),
          groupTitle: t("settings_console_title"),
          saveKind: "console",
        });
      }
      items.push({
        id: "console",
        title: t("settings_console_title"),
        meta: t("settings_section_console_meta"),
        groupTitle: t("settings_console_title"),
        saveKind: "",
      });
      return items;
    });

    const selectedSection = computed(
      () => settingsSections.value.find((item) => item.id === selectedSectionID.value) || settingsSections.value[0] || null
    );
    const activeSaveKind = computed(() => String(selectedSection.value?.saveKind || ""));
    const panelKicker = computed(() => {
      if (!selectedSection.value) {
        return "";
      }
      return tuiKicker(selectedSection.value.groupTitle, selectedSection.value.title);
    });
    const panelHint = computed(() => {
      switch (selectedSection.value?.id) {
        case "agent":
          return t("settings_agent_llm_hint", { path: llmConfigPath.value || "config.yaml" });
        case "inputs":
          return t("settings_multimodal_hint");
        case "tools":
          return t("settings_tools_hint");
        case "runtimes":
          return t("settings_console_runtime_hint", { path: consoleConfigPath.value || "config.yaml" });
        case "console":
          return t("settings_console_preferences_hint");
        default:
          return "";
      }
    });

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

    function selectSection(id) {
      selectedSectionID.value = String(id || "").trim();
    }

    function isSelectedSection(item) {
      return String(item?.id || "") === selectedSectionID.value;
    }

    function sectionClass(item) {
      const classes = ["settings-index-item", "workspace-sidebar-item"];
      if (isSelectedSection(item)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    onMounted(() => {
      void loadAgentSettings();
    });

    watch(
      settingsSections,
      (items) => {
        if (!items.some((item) => item.id === selectedSectionID.value)) {
          selectedSectionID.value = items[0]?.id || "";
        }
      },
      { immediate: true }
    );

    watch(
      showConsoleManagedSettings,
      (enabled) => {
        consoleErr.value = "";
        consoleOk.value = "";
        if (enabled) {
          void loadConsoleSettings();
          return;
        }
        if (selectedSectionID.value === "runtimes") {
          selectedSectionID.value = "console";
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
      consoleLoading,
      consoleSaving,
      consoleErr,
      consoleOk,
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
      settingsSections,
      selectedSection,
      panelKicker,
      panelHint,
      activeSaveKind,
      agentSaveDisabled,
      consoleSaveDisabled,
      logout,
      saveAgentSettings,
      saveConsoleSettings,
      onProviderChange,
      onReasoningEffortChange,
      onToolsEmulationChange,
      setMultimodalSource,
      setToolEnabled,
      setManagedRuntimeEnabled,
      selectSection,
      isSelectedSection,
      sectionClass,
      tuiKicker,
      onLanguageChange: applyLanguageChange,
    };
  },
  template: `
    <AppPage :title="t('settings_title')" class="settings-page">
      <div class="settings-workbench">
        <aside class="settings-index workspace-sidebar-section">
          <div class="settings-index-items workspace-sidebar-list">
            <button
              v-for="item in settingsSections"
              :key="item.id"
              type="button"
              :class="sectionClass(item)"
              :aria-current="isSelectedSection(item) ? 'page' : undefined"
              @click="selectSection(item.id)"
            >
              <span class="workspace-sidebar-item-copy">
                <span class="workspace-sidebar-item-title">{{ item.title }}</span>
                <span class="workspace-sidebar-item-meta">{{ item.meta }}</span>
              </span>
              <span class="workspace-sidebar-item-marker">
                <QBadge v-if="isSelectedSection(item)" dot type="primary" size="sm" />
              </span>
            </button>
          </div>
        </aside>

        <QCard v-if="selectedSection" class="settings-panel-card" variant="default">
          <div class="settings-panel-shell">
            <header class="settings-panel-head">
              <div class="settings-panel-copy">
                <p class="ui-kicker">{{ panelKicker }}</p>
                <h3 class="settings-panel-title workspace-document-title">{{ selectedSection.title }}</h3>
                <p class="settings-panel-meta">{{ panelHint }}</p>
              </div>
              <div class="settings-panel-actions">
                <QButton
                  v-if="activeSaveKind === 'agent'"
                  class="primary"
                  :loading="agentSaving"
                  :disabled="agentSaveDisabled"
                  @click="saveAgentSettings"
                >
                  {{ t("action_save") }}
                </QButton>
                <QButton
                  v-else-if="activeSaveKind === 'console'"
                  class="primary"
                  :loading="consoleSaving"
                  :disabled="consoleSaveDisabled"
                  @click="saveConsoleSettings"
                >
                  {{ t("action_save") }}
                </QButton>
              </div>
            </header>

            <div class="settings-panel-notices">
              <QFence v-if="activeSaveKind === 'agent' && agentErr" type="danger" icon="QIconCloseCircle" :text="agentErr" />
              <QFence v-if="activeSaveKind === 'agent' && agentOk" type="success" icon="QIconCheckCircle" :text="agentOk" />
              <QFence
                v-if="activeSaveKind === 'console' && consoleErr"
                type="danger"
                icon="QIconCloseCircle"
                :text="consoleErr"
              />
              <QFence
                v-if="activeSaveKind === 'console' && consoleOk"
                type="success"
                icon="QIconCheckCircle"
                :text="consoleOk"
              />
            </div>

            <div class="settings-panel-body">
              <div v-if="selectedSection.id === 'agent'" class="settings-form-grid">
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

              <div v-else-if="selectedSection.id === 'inputs'" class="settings-toggle-list">
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

              <div v-else-if="selectedSection.id === 'tools'" class="settings-toggle-list">
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

              <div v-else-if="selectedSection.id === 'runtimes'" class="settings-toggle-list">
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

              <div v-else class="settings-console-list">
                <div class="settings-console-row">
                  <div class="settings-card-copy">
                    <h4 class="settings-card-title">{{ t("settings_language_title") }}</h4>
                    <p class="settings-card-note">{{ t("settings_language_hint") }}</p>
                  </div>
                  <QLanguageSelector class="settings-console-control" :lang="lang" :presist="true" @change="onLanguageChange" />
                </div>
                <div class="settings-console-row settings-console-row-end">
                  <div class="settings-card-copy">
                    <h4 class="settings-card-title">{{ t("settings_session_title") }}</h4>
                    <p class="settings-card-note">{{ t("settings_session_hint") }}</p>
                  </div>
                  <QButton class="danger settings-console-control" :loading="loggingOut" @click="logout">
                    {{ t("action_logout") }}
                  </QButton>
                </div>
              </div>
            </div>
          </div>
        </QCard>
      </div>
    </AppPage>
  `,
};

export default SettingsView;
