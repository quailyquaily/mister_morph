import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import "./SettingsView.css";

import AppKicker from "../components/AppKicker";
import AppPage from "../components/AppPage";
import LLMConfigForm from "../components/LLMConfigForm";
import SetupConnectionTestDialog from "../components/SetupConnectionTestDialog";
import SetupPickerDialog from "../components/SetupPickerDialog";
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
import {
  hasLLMFieldValue,
  isLLMFieldEnvManaged,
  llmFieldEnvRawValue,
  llmFieldValue,
} from "../core/llm-env-managed";
import {
  defaultEndpointForSetupProvider,
  OPENAI_COMPATIBLE_API_BASE_OPTIONS,
  normalizeSetupProviderChoice,
  normalizeSetupProviderForSave,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_OPTIONS,
  setupProviderRequiresAPIKey,
} from "../core/setup-contract";

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
  { id: "spawn", titleKey: "settings_tool_spawn", noteKey: "settings_tool_note_spawn" },
  { id: "contacts_send", titleKey: "settings_tool_contacts_send", noteKey: "settings_tool_note_contacts_send" },
  { id: "todo_update", titleKey: "settings_tool_todo_update", noteKey: "settings_tool_note_todo_update" },
  { id: "plan_create", titleKey: "settings_tool_plan_create", noteKey: "settings_tool_note_plan_create" },
  { id: "url_fetch", titleKey: "settings_tool_url_fetch", noteKey: "settings_tool_note_url_fetch" },
  { id: "web_search", titleKey: "settings_tool_web_search", noteKey: "settings_tool_note_web_search" },
  { id: "bash", titleKey: "settings_tool_bash", noteKey: "settings_tool_note_bash" },
  { id: "powershell", titleKey: "settings_tool_powershell", noteKey: "settings_tool_note_powershell" },
];

const MANAGED_RUNTIME_ITEMS = [
  { id: "telegram", titleKey: "settings_console_runtime_telegram", noteKey: "settings_console_runtime_note_telegram" },
  { id: "slack", titleKey: "settings_console_runtime_slack", noteKey: "settings_console_runtime_note_slack" },
];

const CHANNEL_GROUP_TRIGGER_VALUES = ["smart", "strict", "talkative"];
const LOCAL_CONSOLE_ENDPOINT_REF = "ep_console_local";
let llmProfileKeySeed = 0;

function buildEmptyLLMForm() {
  return {
    provider: "",
    endpoint: "",
    model: "",
    api_key: "",
    cloudflare_api_token: "",
    cloudflare_account_id: "",
    reasoning_effort: "",
    tools_emulation_mode: "",
  };
}

function buildEmptyTelegramConsoleState() {
  return {
    bot_token: "",
    allowed_chat_ids_text: "",
    group_trigger_mode: "smart",
  };
}

function buildEmptySlackConsoleState() {
  return {
    bot_token: "",
    app_token: "",
    allowed_team_ids_text: "",
    allowed_channel_ids_text: "",
    group_trigger_mode: "smart",
  };
}

function buildEmptyGuardConsoleState() {
  return {
    enabled: true,
    url_fetch_allowed_url_prefixes_text: "https://",
    deny_private_ips: true,
    follow_redirects: false,
    allow_proxy: false,
    redaction_enabled: true,
    approvals_enabled: false,
  };
}

function nextLLMProfileKey() {
  llmProfileKeySeed += 1;
  return `llm-profile-${Date.now()}-${llmProfileKeySeed}`;
}

function buildLLMProfileState(data = {}) {
  return {
    _key: nextLLMProfileKey(),
    _envManaged: {},
    name: "",
    ...buildEmptyLLMForm(),
    ...(data && typeof data === "object" ? data : {}),
  };
}

function trimText(value) {
  return String(value || "").trim();
}

function normalizeNamedList(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  const out = [];
  const seen = new Set();
  for (const value of values) {
    const name = trimText(value);
    if (!name) {
      continue;
    }
    const key = name.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(name);
  }
  return out;
}

function normalizeConsoleGroupTriggerMode(value) {
  const next = String(value || "").trim().toLowerCase();
  return CHANNEL_GROUP_TRIGGER_VALUES.includes(next) ? next : "smart";
}

function parseConfigListText(value) {
  return normalizeNamedList(String(value || "").split(/\r?\n|,/));
}

function formatConfigList(values) {
  return normalizeNamedList(Array.isArray(values) ? values : []).join("\n");
}

function toolEnabledValue(entry) {
  return !!(entry && typeof entry === "object" && entry.enabled === true);
}

function serializeLLMProfile(profile) {
  return {
    name: trimText(profile?.name),
    provider: trimText(profile?.provider),
    endpoint: trimText(profile?.endpoint),
    model: trimText(profile?.model),
    api_key: trimText(profile?.api_key),
    cloudflare_api_token: trimText(profile?.cloudflare_api_token),
    cloudflare_account_id: trimText(profile?.cloudflare_account_id),
    reasoning_effort: trimText(profile?.reasoning_effort),
    tools_emulation_mode: trimText(profile?.tools_emulation_mode),
  };
}

function buildLLMSnapshot(state) {
  return JSON.stringify({
    llm: {
      provider: trimText(state.llm.provider),
      endpoint: trimText(state.llm.endpoint),
      model: trimText(state.llm.model),
      api_key: trimText(state.llm.api_key),
      cloudflare_api_token: trimText(state.llm.cloudflare_api_token),
      cloudflare_account_id: trimText(state.llm.cloudflare_account_id),
      reasoning_effort: trimText(state.llm.reasoning_effort),
      tools_emulation_mode: trimText(state.llm.tools_emulation_mode),
      profiles: Array.isArray(state.llm.profiles) ? state.llm.profiles.map((profile) => serializeLLMProfile(profile)) : [],
      fallback_profiles: normalizeNamedList(state.llm.fallback_profiles),
    },
  });
}

function buildMultimodalSnapshot(state) {
  return JSON.stringify({
    multimodal: {
      telegram: !!state.multimodal.telegram,
      slack: !!state.multimodal.slack,
      line: !!state.multimodal.line,
      remote_download: !!state.multimodal.remote_download,
    },
  });
}

function buildToolsSnapshot(state) {
  return JSON.stringify({
    tools: {
      write_file: !!state.tools.write_file,
      spawn: !!state.tools.spawn,
      contacts_send: !!state.tools.contacts_send,
      todo_update: !!state.tools.todo_update,
      plan_create: !!state.tools.plan_create,
      url_fetch: !!state.tools.url_fetch,
      web_search: !!state.tools.web_search,
      bash: !!state.tools.bash,
      powershell: !!state.tools.powershell,
    },
  });
}

function buildAgentSnapshot(state) {
  return JSON.stringify({
    llm: JSON.parse(buildLLMSnapshot(state)).llm,
    multimodal: JSON.parse(buildMultimodalSnapshot(state)).multimodal,
    tools: JSON.parse(buildToolsSnapshot(state)).tools,
  });
}

function buildConsoleSnapshot(state) {
  return JSON.stringify({
    managed_runtimes: JSON.parse(buildConsoleManagedRuntimeSnapshot(state)),
    telegram: JSON.parse(buildConsoleTelegramSnapshot(state)),
    slack: JSON.parse(buildConsoleSlackSnapshot(state)),
    guard: JSON.parse(buildConsoleGuardSnapshot(state)),
  });
}

function buildConsoleManagedRuntimeSnapshot(state) {
  return JSON.stringify({
    telegram: !!state.managedRuntimes.telegram,
    slack: !!state.managedRuntimes.slack,
  });
}

function buildConsoleTelegramSnapshot(state) {
  return JSON.stringify({
    bot_token: trimText(state.telegram.bot_token),
    allowed_chat_ids: parseConfigListText(state.telegram.allowed_chat_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.telegram.group_trigger_mode),
  });
}

function buildConsoleSlackSnapshot(state) {
  return JSON.stringify({
    bot_token: trimText(state.slack.bot_token),
    app_token: trimText(state.slack.app_token),
    allowed_team_ids: parseConfigListText(state.slack.allowed_team_ids_text),
    allowed_channel_ids: parseConfigListText(state.slack.allowed_channel_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.slack.group_trigger_mode),
  });
}

function buildConsoleGuardSnapshot(state) {
  return JSON.stringify({
    enabled: !!state.guard.enabled,
    network: {
      url_fetch: {
        allowed_url_prefixes: parseConfigListText(state.guard.url_fetch_allowed_url_prefixes_text),
        deny_private_ips: !!state.guard.deny_private_ips,
        follow_redirects: !!state.guard.follow_redirects,
        allow_proxy: !!state.guard.allow_proxy,
      },
    },
    redaction: {
      enabled: !!state.guard.redaction_enabled,
    },
    approvals: {
      enabled: !!state.guard.approvals_enabled,
    },
  });
}

const SettingsView = {
  components: {
    AppKicker,
    AppPage,
    LLMConfigForm,
    SetupConnectionTestDialog,
    SetupPickerDialog,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const lang = computed(() => localeState.lang);
    const loggingOut = ref(false);
    const agentLoading = ref(false);
    const agentSaving = ref(false);
    const agentSavingTarget = ref("");
    const agentNoticeTarget = ref("");
    const agentErr = ref("");
    const agentOk = ref("");
    const agentValidationVisible = ref(false);
    const deleteProfileDialogOpen = ref(false);
    const deleteProfileTargetKey = ref("");
    const llmConfigPath = ref("");
    const loadedLLMSnapshot = ref("");
    const loadedMultimodalSnapshot = ref("");
    const loadedToolsSnapshot = ref("");
    const llmEnvManaged = ref({});
    const consoleLoading = ref(false);
    const consoleSaving = ref(false);
    const consoleSavingTarget = ref("");
    const consoleNoticeTarget = ref("");
    const consoleErr = ref("");
    const consoleOk = ref("");
    const consoleConfigPath = ref("");
    const loadedConsoleSnapshot = ref("");
    const loadedConsoleManagedSnapshot = ref("");
    const loadedConsoleTelegramSnapshot = ref("");
    const loadedConsoleSlackSnapshot = ref("");
    const loadedConsoleGuardSnapshot = ref("");
    const consoleEnvManaged = ref({});
    const selectedSectionID = ref("agent");
    const isMobile = ref(false);
    const mobilePanelVisible = ref(false);
    const apiBasePickerOpen = ref(false);
    const modelPickerOpen = ref(false);
    const modelPickerLoading = ref(false);
    const modelPickerError = ref("");
    const modelPickerItems = ref([]);
    const testConnectionOpen = ref(false);
    const testConnectionLoading = ref(false);
    const testConnectionError = ref("");
    const testConnectionBenchmarks = ref([]);
    const testConnectionMeta = reactive({
      provider: "",
      apiBase: "",
      model: "",
    });
    const testConnectionTargetProfileKey = ref("");

    const state = reactive({
      llm: {
        ...buildEmptyLLMForm(),
        profiles: [],
        fallback_profiles: [],
      },
      multimodal: {
        telegram: false,
        slack: false,
        line: false,
        remote_download: false,
      },
      tools: {
        write_file: true,
        spawn: true,
        contacts_send: true,
        todo_update: true,
        plan_create: true,
        url_fetch: true,
        web_search: true,
        bash: true,
        powershell: false,
      },
      managedRuntimes: {
        telegram: false,
        slack: false,
      },
      telegram: buildEmptyTelegramConsoleState(),
      slack: buildEmptySlackConsoleState(),
      guard: buildEmptyGuardConsoleState(),
    });

    const defaultProviderItems = computed(() => SETUP_PROVIDER_OPTIONS);
    const profileProviderItems = computed(() => [
      { title: t("settings_agent_provider_inherit"), value: "" },
      ...SETUP_PROVIDER_OPTIONS,
    ]);
    const apiBasePickerItems = computed(() =>
      OPENAI_COMPATIBLE_API_BASE_OPTIONS.map((item) => ({
        id: item.id,
        title: item.title,
        value: item.baseURL,
        note: "",
      }))
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
    const profileReasoningEffortItems = computed(() => [
      { title: t("settings_agent_provider_inherit"), value: "" },
      ...reasoningEffortItems.value.filter((item) => item.value !== ""),
    ]);
    const toolsEmulationItems = computed(() => [
      { title: t("settings_llm_tools_emulation_off"), value: "off" },
      { title: t("settings_llm_tools_emulation_fallback"), value: "fallback" },
      { title: t("settings_llm_tools_emulation_force"), value: "force" },
    ]);
    const profileToolsEmulationItems = computed(() => [
      { title: t("settings_agent_provider_inherit"), value: "" },
      ...toolsEmulationItems.value,
    ]);
    const multimodalItems = computed(() => MULTIMODAL_SOURCES);
    const toolItems = computed(() => TOOL_ITEMS);
    const managedRuntimeItems = computed(() => MANAGED_RUNTIME_ITEMS);
    const groupTriggerItems = computed(() => [
      { title: t("settings_console_group_trigger_smart"), value: "smart" },
      { title: t("settings_console_group_trigger_strict"), value: "strict" },
      { title: t("settings_console_group_trigger_talkative"), value: "talkative" },
    ]);
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
          kickerLeft: "Agent",
          kickerRight: "LLM Config",
          saveKind: "agent",
        },
        {
          id: "tools",
          title: t("settings_tools_title"),
          meta: t("settings_section_tools_meta"),
          kickerLeft: "Agent",
          kickerRight: "Tools",
          saveKind: "agent",
        },
      ];
      if (showConsoleManagedSettings.value) {
        items.push({
          id: "channels",
          title: t("settings_console_channels_title"),
          meta: t("settings_section_channels_meta"),
          kickerLeft: "Console",
          kickerRight: "Channels",
          saveKind: "console",
        });
        items.push({
          id: "runtimes",
          title: t("settings_console_runtime_title"),
          meta: t("settings_section_runtimes_meta"),
          kickerLeft: "Console",
          kickerRight: "Managed Runtimes",
          saveKind: "console",
        });
        items.push({
          id: "guard",
          title: t("settings_console_guard_title"),
          meta: t("settings_section_guard_meta"),
          kickerLeft: "Console",
          kickerRight: "Guard",
          saveKind: "console",
        });
      }
      items.push({
        id: "console",
        title: t("settings_console_title"),
        meta: t("settings_section_console_meta"),
        kickerLeft: "Console",
        kickerRight: "Console",
        saveKind: "",
      });
      return items;
    });

    const selectedSection = computed(
      () => settingsSections.value.find((item) => item.id === selectedSectionID.value) || settingsSections.value[0] || null
    );
    const activeSaveKind = computed(() => String(selectedSection.value?.saveKind || ""));
    const panelHint = computed(() => {
      switch (selectedSection.value?.id) {
        case "agent":
          return t("settings_agent_llm_hint", { path: llmConfigPath.value || "config.yaml" });
        case "tools":
          return t("settings_tools_hint");
        case "runtimes":
          return t("settings_console_runtime_hint", { path: consoleConfigPath.value || "config.yaml" });
        case "channels":
          return t("settings_console_channels_hint", { path: consoleConfigPath.value || "config.yaml" });
        case "guard":
          return t("settings_console_guard_hint", { path: consoleConfigPath.value || "config.yaml" });
        case "console":
          return t("settings_console_preferences_hint");
        default:
          return "";
      }
    });
    const showIndexPane = computed(() => !isMobile.value || !mobilePanelVisible.value);
    const showPanelPane = computed(() => !isMobile.value || mobilePanelVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobilePanelVisible.value);
    const mobileBarTitle = computed(() =>
      mobileShowBack.value ? selectedSection.value?.title || t("settings_title") : t("settings_title")
    );
    const pageClass = computed(() => (isMobile.value ? "settings-page settings-page-mobile-split" : "settings-page"));
    const profileBaseProvider = computed(() => llmFieldValue(state.llm, llmEnvManaged.value, "provider"));
    const defaultProviderChoice = computed(() =>
      normalizeSetupProviderChoice(profileBaseProvider.value, { allowEmpty: true })
    );
    const defaultShowCloudflareAccountField = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_CLOUDFLARE);
    const defaultCredentialFieldName = computed(() =>
      defaultShowCloudflareAccountField.value ? "cloudflare_api_token" : "api_key"
    );
    const profileOptions = computed(() =>
      state.llm.profiles
        .map((profile) => ({
          id: profile._key,
          title: trimText(profile.name) || t("settings_agent_profile_placeholder"),
          value: trimText(profile.name),
          note: trimText(profile.model),
        }))
        .filter((item) => item.value !== "")
    );
    const agentValidationError = computed(() => {
      if (!hasLLMFieldValue(state.llm, llmEnvManaged.value, "provider")) {
        return "";
      }
      const seen = new Set();
      for (const profile of state.llm.profiles) {
        const name = trimText(profile.name);
        if (!name) {
          return t("settings_agent_profile_name_required");
        }
        const key = name.toLowerCase();
        if (key === "default") {
          return t("settings_agent_profile_name_reserved");
        }
        if (seen.has(key)) {
          return t("settings_agent_profile_name_duplicate", { name });
        }
        seen.add(key);
      }
      for (const fallback of state.llm.fallback_profiles) {
        const name = trimText(fallback);
        if (!name) {
          return t("settings_agent_fallback_required");
        }
        if (!seen.has(name.toLowerCase())) {
          return t("settings_agent_fallback_unknown", { name });
        }
      }
      return "";
    });
    const deleteProfileTarget = computed(() =>
      state.llm.profiles.find((item) => item._key === deleteProfileTargetKey.value) || null
    );
    const deleteProfileDialogText = computed(() =>
      t("settings_agent_profile_delete_confirm", {
        name: trimText(deleteProfileTarget.value?.name) || t("settings_agent_profile_placeholder"),
      })
    );
    const deleteProfileDialogActions = computed(() => [
      {
        name: "cancel",
        label: t("action_cancel"),
        class: "outlined",
        action: closeDeleteProfileDialog,
      },
      {
        name: "delete",
        label: t("action_delete"),
        class: "danger",
        action: deleteLLMProfile,
      },
    ]);
    const testConnectionDisabled = computed(
      () =>
        testConnectionLoading.value ||
        agentLoading.value ||
        agentSaving.value ||
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "provider") ||
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "model") ||
        (setupProviderRequiresAPIKey(defaultProviderChoice.value) &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, defaultCredentialFieldName.value)) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_api_token")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_account_id"))
    );
    const currentTestTargetProfile = computed(() =>
      state.llm.profiles.find((item) => item._key === testConnectionTargetProfileKey.value) || null
    );
    const llmDirty = computed(() => buildLLMSnapshot(state) !== loadedLLMSnapshot.value);
    const multimodalDirty = computed(() => buildMultimodalSnapshot(state) !== loadedMultimodalSnapshot.value);
    const toolsDirty = computed(() => buildToolsSnapshot(state) !== loadedToolsSnapshot.value);
    const llmSaveDisabled = computed(
      () =>
        agentLoading.value ||
        agentSaving.value ||
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "provider") ||
        !llmDirty.value ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_api_token")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_account_id"))
    );
    const multimodalSaveDisabled = computed(() => agentLoading.value || agentSaving.value || !multimodalDirty.value);
    const toolsSaveDisabled = computed(() => agentLoading.value || agentSaving.value || !toolsDirty.value);
    const consoleDirty = computed(() => buildConsoleSnapshot(state) !== loadedConsoleSnapshot.value);
    const consoleManagedDirty = computed(
      () => buildConsoleManagedRuntimeSnapshot(state) !== loadedConsoleManagedSnapshot.value
    );
    const consoleTelegramDirty = computed(
      () => buildConsoleTelegramSnapshot(state) !== loadedConsoleTelegramSnapshot.value
    );
    const consoleSlackDirty = computed(
      () => buildConsoleSlackSnapshot(state) !== loadedConsoleSlackSnapshot.value
    );
    const consoleGuardDirty = computed(
      () => buildConsoleGuardSnapshot(state) !== loadedConsoleGuardSnapshot.value
    );
    const consoleSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleManagedDirty.value
    );
    const telegramSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleTelegramDirty.value
    );
    const slackSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleSlackDirty.value
    );
    const guardSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleGuardDirty.value
    );

    function applyPayload(data) {
      const llm = data?.llm && typeof data.llm === "object" ? data.llm : {};
      const envManagedPayload = data?.env_managed && typeof data.env_managed === "object" ? data.env_managed : {};
      const llmEnvManagedPayload =
        envManagedPayload?.llm && typeof envManagedPayload.llm === "object" ? envManagedPayload.llm : {};
      const llmProfileEnvManagedPayload =
        envManagedPayload?.llm_profiles && typeof envManagedPayload.llm_profiles === "object"
          ? envManagedPayload.llm_profiles
          : {};
      const multimodal = data?.multimodal && typeof data.multimodal === "object" ? data.multimodal : {};
      const tools = data?.tools && typeof data.tools === "object" ? data.tools : {};
      const imageSources = Array.isArray(multimodal.image_sources) ? multimodal.image_sources : [];
      const profiles = Array.isArray(llm.profiles) ? llm.profiles : [];

      state.llm.provider = normalizeSetupProviderChoice(llm.provider, { allowEmpty: true });
      state.llm.endpoint = typeof llm.endpoint === "string" ? llm.endpoint : "";
      state.llm.model = typeof llm.model === "string" ? llm.model : "";
      state.llm.api_key = typeof llm.api_key === "string" ? llm.api_key : "";
      state.llm.cloudflare_api_token = typeof llm.cloudflare_api_token === "string" ? llm.cloudflare_api_token : "";
      state.llm.cloudflare_account_id = typeof llm.cloudflare_account_id === "string" ? llm.cloudflare_account_id : "";
      state.llm.reasoning_effort = typeof llm.reasoning_effort === "string" ? llm.reasoning_effort : "";
      state.llm.tools_emulation_mode = typeof llm.tools_emulation_mode === "string" ? llm.tools_emulation_mode : "off";
      state.llm.profiles = profiles.map((profile) =>
        buildLLMProfileState({
          name: trimText(profile?.name),
          _envManaged:
            llmProfileEnvManagedPayload?.[trimText(profile?.name)] &&
            typeof llmProfileEnvManagedPayload[trimText(profile?.name)] === "object"
              ? llmProfileEnvManagedPayload[trimText(profile?.name)]
              : {},
          provider: normalizeSetupProviderChoice(profile?.provider, { allowEmpty: true }),
          endpoint: typeof profile?.endpoint === "string" ? profile.endpoint : "",
          model: typeof profile?.model === "string" ? profile.model : "",
          api_key: typeof profile?.api_key === "string" ? profile.api_key : "",
          cloudflare_api_token:
            typeof profile?.cloudflare_api_token === "string" ? profile.cloudflare_api_token : "",
          cloudflare_account_id:
            typeof profile?.cloudflare_account_id === "string" ? profile.cloudflare_account_id : "",
          reasoning_effort: typeof profile?.reasoning_effort === "string" ? profile.reasoning_effort : "",
          tools_emulation_mode:
            typeof profile?.tools_emulation_mode === "string" ? profile.tools_emulation_mode : "",
        }),
      );
      state.llm.fallback_profiles = normalizeNamedList(llm.fallback_profiles);
      for (const item of MULTIMODAL_SOURCES) {
        state.multimodal[item.id] = imageSources.includes(item.id);
      }
      state.tools.write_file = toolEnabledValue(tools.write_file);
      state.tools.spawn = toolEnabledValue(tools.spawn);
      state.tools.contacts_send = toolEnabledValue(tools.contacts_send);
      state.tools.todo_update = toolEnabledValue(tools.todo_update);
      state.tools.plan_create = toolEnabledValue(tools.plan_create);
      state.tools.url_fetch = toolEnabledValue(tools.url_fetch);
      state.tools.web_search = toolEnabledValue(tools.web_search);
      state.tools.bash = toolEnabledValue(tools.bash);
      state.tools.powershell = toolEnabledValue(tools.powershell);
      llmEnvManaged.value = llmEnvManagedPayload;

      agentValidationVisible.value = false;
      loadedLLMSnapshot.value = buildLLMSnapshot(state);
      loadedMultimodalSnapshot.value = buildMultimodalSnapshot(state);
      loadedToolsSnapshot.value = buildToolsSnapshot(state);
    }

    function llmProfileEnvManaged(profile) {
      return profile?._envManaged && typeof profile._envManaged === "object" ? profile._envManaged : {};
    }

    function normalizeProviderForSave(choice, endpoint, allowEmpty = false) {
      const provider = normalizeSetupProviderChoice(choice, { allowEmpty });
      if (provider === "" && allowEmpty) {
        return "";
      }
      return normalizeSetupProviderForSave(choice, endpoint);
    }

    function updateDefaultLLMField({ field, value }) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.llm, key)) {
        return;
      }
      state.llm[key] = String(value || "");
    }

    function updateProfileField(profileKey, { field, value }) {
      const profile = state.llm.profiles.find((item) => item._key === profileKey);
      const key = String(field || "").trim();
      if (!profile || !key || !Object.prototype.hasOwnProperty.call(profile, key)) {
        return;
      }
      const previousName = trimText(profile.name);
      profile[key] = String(value || "");
      if (key !== "name") {
        return;
      }
      const nextName = trimText(profile.name);
      if (!previousName || previousName === nextName) {
        return;
      }
      state.llm.fallback_profiles = state.llm.fallback_profiles.map((item) =>
        trimText(item) === previousName ? nextName : item,
      );
    }

    function addLLMProfile() {
      state.llm.profiles.push(buildLLMProfileState());
    }

    function confirmRemoveLLMProfile(profileKey) {
      deleteProfileTargetKey.value = String(profileKey || "").trim();
      deleteProfileDialogOpen.value = deleteProfileTargetKey.value !== "";
    }

    function closeDeleteProfileDialog() {
      deleteProfileDialogOpen.value = false;
      deleteProfileTargetKey.value = "";
    }

    function removeLLMProfile(profileKey) {
      const index = state.llm.profiles.findIndex((item) => item._key === profileKey);
      if (index < 0) {
        return;
      }
      const [removed] = state.llm.profiles.splice(index, 1);
      const removedName = trimText(removed?.name);
      if (!removedName) {
        return;
      }
      state.llm.fallback_profiles = state.llm.fallback_profiles.filter((item) => trimText(item) !== removedName);
    }

    function deleteLLMProfile() {
      const profileKey = deleteProfileTargetKey.value;
      closeDeleteProfileDialog();
      if (!profileKey) {
        return;
      }
      removeLLMProfile(profileKey);
    }

    function addFallbackProfile() {
      const firstProfile = profileOptions.value[0]?.value || "";
      if (!firstProfile) {
        return;
      }
      state.llm.fallback_profiles.push(firstProfile);
    }

    function updateFallbackProfile(index, item) {
      if (index < 0 || index >= state.llm.fallback_profiles.length) {
        return;
      }
      state.llm.fallback_profiles[index] = trimText(item?.value);
    }

    function removeFallbackProfile(index) {
      if (index < 0 || index >= state.llm.fallback_profiles.length) {
        return;
      }
      state.llm.fallback_profiles.splice(index, 1);
    }

    function moveFallbackProfile(index, delta) {
      const nextIndex = index + delta;
      if (index < 0 || index >= state.llm.fallback_profiles.length || nextIndex < 0 || nextIndex >= state.llm.fallback_profiles.length) {
        return;
      }
      const items = [...state.llm.fallback_profiles];
      const [current] = items.splice(index, 1);
      items.splice(nextIndex, 0, current);
      state.llm.fallback_profiles = items;
    }

    function buildProfilePayload(profile) {
      const envManaged = llmProfileEnvManaged(profile);
      const explicitProvider = normalizeSetupProviderChoice(llmFieldValue(profile, envManaged, "provider"), {
        allowEmpty: true,
      });
      const effectiveProvider = explicitProvider || defaultProviderChoice.value;
      const payload = {
        name: trimText(profile.name),
        provider:
          llmFieldEnvRawValue(envManaged, "provider") ||
          normalizeProviderForSave(profile.provider, profile.endpoint, true),
        endpoint: llmFieldEnvRawValue(envManaged, "endpoint") || trimText(profile.endpoint),
        model: llmFieldEnvRawValue(envManaged, "model") || trimText(profile.model),
        reasoning_effort:
          llmFieldEnvRawValue(envManaged, "reasoning_effort") || trimText(profile.reasoning_effort),
        tools_emulation_mode:
          llmFieldEnvRawValue(envManaged, "tools_emulation_mode") || trimText(profile.tools_emulation_mode),
      };
      if (effectiveProvider === SETUP_PROVIDER_CLOUDFLARE) {
        payload.cloudflare_api_token =
          llmFieldEnvRawValue(envManaged, "cloudflare_api_token") || trimText(profile.cloudflare_api_token);
        payload.cloudflare_account_id =
          llmFieldEnvRawValue(envManaged, "cloudflare_account_id") || trimText(profile.cloudflare_account_id);
        payload.api_key = "";
      } else {
        payload.api_key = llmFieldEnvRawValue(envManaged, "api_key") || trimText(profile.api_key);
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
      }
      return payload;
    }

    function buildDefaultLLMTestPayload() {
      const payload = {};
      const provider = normalizeSetupProviderChoice(llmFieldValue(state.llm, llmEnvManaged.value, "provider"), { allowEmpty: true });
      const providerRaw = llmFieldEnvRawValue(llmEnvManaged.value, "provider");
      if (providerRaw !== "") {
        payload.provider = providerRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "provider") && provider !== "") {
        payload.provider = normalizeSetupProviderForSave(state.llm.provider, state.llm.endpoint);
      }
      const endpointRaw = llmFieldEnvRawValue(llmEnvManaged.value, "endpoint");
      if (endpointRaw !== "") {
        payload.endpoint = endpointRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "endpoint")) {
        const endpoint = trimText(state.llm.endpoint);
        if (endpoint !== "") {
          payload.endpoint = endpoint;
        }
      }
      const modelRaw = llmFieldEnvRawValue(llmEnvManaged.value, "model");
      if (modelRaw !== "") {
        payload.model = modelRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "model")) {
        const model = trimText(state.llm.model);
        if (model !== "") {
          payload.model = model;
        }
      }
      const reasoningEffortRaw = llmFieldEnvRawValue(llmEnvManaged.value, "reasoning_effort");
      if (reasoningEffortRaw !== "") {
        payload.reasoning_effort = reasoningEffortRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "reasoning_effort")) {
        const reasoningEffort = trimText(state.llm.reasoning_effort);
        if (reasoningEffort !== "") {
          payload.reasoning_effort = reasoningEffort;
        }
      }
      const toolsEmulationModeRaw = llmFieldEnvRawValue(llmEnvManaged.value, "tools_emulation_mode");
      if (toolsEmulationModeRaw !== "") {
        payload.tools_emulation_mode = toolsEmulationModeRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "tools_emulation_mode")) {
        const toolsEmulationMode = trimText(state.llm.tools_emulation_mode);
        if (toolsEmulationMode !== "") {
          payload.tools_emulation_mode = toolsEmulationMode;
        }
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        const tokenRaw = llmFieldEnvRawValue(llmEnvManaged.value, "cloudflare_api_token");
        if (tokenRaw !== "") {
          payload.cloudflare_api_token = tokenRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_api_token")) {
          const token = trimText(state.llm.cloudflare_api_token);
          if (token !== "") {
            payload.cloudflare_api_token = token;
          }
        }
        const accountIDRaw = llmFieldEnvRawValue(llmEnvManaged.value, "cloudflare_account_id");
        if (accountIDRaw !== "") {
          payload.cloudflare_account_id = accountIDRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_account_id")) {
          const accountID = trimText(state.llm.cloudflare_account_id);
          if (accountID !== "") {
            payload.cloudflare_account_id = accountID;
          }
        }
      } else {
        const apiKeyRaw = llmFieldEnvRawValue(llmEnvManaged.value, "api_key");
        if (apiKeyRaw !== "") {
          payload.api_key = apiKeyRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "api_key")) {
          const apiKey = trimText(state.llm.api_key);
          if (apiKey !== "") {
            payload.api_key = apiKey;
          }
        }
      }
      return payload;
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
      const telegram = data?.telegram && typeof data.telegram === "object" ? data.telegram : {};
      const slack = data?.slack && typeof data.slack === "object" ? data.slack : {};
      const guard = data?.guard && typeof data.guard === "object" ? data.guard : {};
      const guardNetwork = guard?.network && typeof guard.network === "object" ? guard.network : {};
      const guardURLFetch =
        guardNetwork?.url_fetch && typeof guardNetwork.url_fetch === "object" ? guardNetwork.url_fetch : {};
      const guardRedaction = guard?.redaction && typeof guard.redaction === "object" ? guard.redaction : {};
      const guardApprovals = guard?.approvals && typeof guard.approvals === "object" ? guard.approvals : {};
      consoleEnvManaged.value = data?.env_managed && typeof data.env_managed === "object" ? data.env_managed : {};
      for (const item of MANAGED_RUNTIME_ITEMS) {
        state.managedRuntimes[item.id] = values.includes(item.id);
      }
      state.telegram.bot_token = typeof telegram.bot_token === "string" ? telegram.bot_token : "";
      state.telegram.allowed_chat_ids_text = formatConfigList(telegram.allowed_chat_ids);
      state.telegram.group_trigger_mode = normalizeConsoleGroupTriggerMode(telegram.group_trigger_mode);
      state.slack.bot_token = typeof slack.bot_token === "string" ? slack.bot_token : "";
      state.slack.app_token = typeof slack.app_token === "string" ? slack.app_token : "";
      state.slack.allowed_team_ids_text = formatConfigList(slack.allowed_team_ids);
      state.slack.allowed_channel_ids_text = formatConfigList(slack.allowed_channel_ids);
      state.slack.group_trigger_mode = normalizeConsoleGroupTriggerMode(slack.group_trigger_mode);
      state.guard.enabled = typeof guard.enabled === "boolean" ? guard.enabled : true;
      state.guard.url_fetch_allowed_url_prefixes_text = formatConfigList(guardURLFetch.allowed_url_prefixes);
      state.guard.deny_private_ips =
        typeof guardURLFetch.deny_private_ips === "boolean" ? guardURLFetch.deny_private_ips : true;
      state.guard.follow_redirects =
        typeof guardURLFetch.follow_redirects === "boolean" ? guardURLFetch.follow_redirects : false;
      state.guard.allow_proxy = typeof guardURLFetch.allow_proxy === "boolean" ? guardURLFetch.allow_proxy : false;
      state.guard.redaction_enabled = typeof guardRedaction.enabled === "boolean" ? guardRedaction.enabled : true;
      state.guard.approvals_enabled =
        typeof guardApprovals.enabled === "boolean" ? guardApprovals.enabled : false;
      loadedConsoleManagedSnapshot.value = buildConsoleManagedRuntimeSnapshot(state);
      loadedConsoleTelegramSnapshot.value = buildConsoleTelegramSnapshot(state);
      loadedConsoleSlackSnapshot.value = buildConsoleSlackSnapshot(state);
      loadedConsoleGuardSnapshot.value = buildConsoleGuardSnapshot(state);
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

    function buildSavePayload(target = "all") {
      const multimodal = {
        image_sources: MULTIMODAL_SOURCES.filter((item) => state.multimodal[item.id]).map((item) => item.id),
      };
      const tools = {
        write_file: { enabled: state.tools.write_file },
        spawn: { enabled: state.tools.spawn },
        contacts_send: { enabled: state.tools.contacts_send },
        todo_update: { enabled: state.tools.todo_update },
        plan_create: { enabled: state.tools.plan_create },
        url_fetch: { enabled: state.tools.url_fetch },
        web_search: { enabled: state.tools.web_search },
        bash: { enabled: state.tools.bash },
        powershell: { enabled: state.tools.powershell },
      };
      if (target === "llm") {
        return { llm: buildLLMSettingsPayload() };
      }
      if (target === "multimodal") {
        return { multimodal };
      }
      if (target === "tools") {
        return { tools };
      }
      return {
        llm: buildLLMSettingsPayload(),
        multimodal,
        tools,
      };
    }

    function buildLLMSettingsPayload() {
      const payload = {};
      const provider = normalizeSetupProviderChoice(llmFieldValue(state.llm, llmEnvManaged.value, "provider"), { allowEmpty: true });
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "provider")) {
        payload.provider = normalizeSetupProviderForSave(state.llm.provider, state.llm.endpoint);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "endpoint")) {
        payload.endpoint = trimText(state.llm.endpoint);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "model")) {
        payload.model = trimText(state.llm.model);
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_api_token")) {
          payload.cloudflare_api_token = trimText(state.llm.cloudflare_api_token);
        }
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_account_id")) {
          payload.cloudflare_account_id = trimText(state.llm.cloudflare_account_id);
        }
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "api_key")) {
        payload.api_key = trimText(state.llm.api_key);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "reasoning_effort")) {
        payload.reasoning_effort = trimText(state.llm.reasoning_effort);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "tools_emulation_mode")) {
        payload.tools_emulation_mode = trimText(state.llm.tools_emulation_mode);
      }
      payload.profiles = state.llm.profiles.map((profile) => buildProfilePayload(profile));
      payload.fallback_profiles = normalizeNamedList(state.llm.fallback_profiles);
      return payload;
    }

    function buildProfileTestPayload(profile) {
      return {
        ...buildDefaultLLMTestPayload(),
        profiles: [buildProfilePayload(profile)],
      };
    }

    function effectiveProfileProviderChoice(profile) {
      const envManaged = llmProfileEnvManaged(profile);
      const explicitProvider = normalizeSetupProviderChoice(llmFieldValue(profile, envManaged, "provider"), {
        allowEmpty: true,
      });
      return explicitProvider || defaultProviderChoice.value;
    }

    function effectiveProfileFieldValue(profile, field) {
      const envManaged = llmProfileEnvManaged(profile);
      const localValue = llmFieldValue(profile, envManaged, field);
      if (localValue !== "") {
        return localValue;
      }
      return llmFieldValue(state.llm, llmEnvManaged.value, field);
    }

    function hasResolvableProfileTestTarget(profile) {
      const name = trimText(profile?.name);
      if (name === "" || name.toLowerCase() === "default") {
        return false;
      }
      const matches = state.llm.profiles.filter((item) => trimText(item?.name).toLowerCase() === name.toLowerCase()).length;
      return matches === 1;
    }

    function hasEffectiveProfileFieldValue(profile, field) {
      const envManaged = llmProfileEnvManaged(profile);
      return (
        hasLLMFieldValue(profile, envManaged, field) ||
        hasLLMFieldValue(state.llm, llmEnvManaged.value, field)
      );
    }

    function testConnectionDisabledForProfile(profile) {
      const provider = effectiveProfileProviderChoice(profile);
      if (testConnectionLoading.value || agentLoading.value || agentSaving.value) {
        return true;
      }
      if (!hasResolvableProfileTestTarget(profile) || provider === "") {
        return true;
      }
      if (!hasEffectiveProfileFieldValue(profile, "model")) {
        return true;
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        return (
          !hasEffectiveProfileFieldValue(profile, "cloudflare_api_token") ||
          !hasEffectiveProfileFieldValue(profile, "cloudflare_account_id")
        );
      }
      return setupProviderRequiresAPIKey(provider) && !hasEffectiveProfileFieldValue(profile, "api_key");
    }

    function primeConnectionTestState(targetProfile, nextPayload = null) {
      const payload = nextPayload || (targetProfile ? buildProfileTestPayload(targetProfile) : buildDefaultLLMTestPayload());
      const targetProviderChoice = targetProfile
        ? effectiveProfileProviderChoice(targetProfile)
        : normalizeSetupProviderChoice(llmFieldValue(state.llm, llmEnvManaged.value, "provider"), { allowEmpty: true });
      const targetEndpoint = targetProfile
        ? effectiveProfileFieldValue(targetProfile, "endpoint")
        : llmFieldValue(state.llm, llmEnvManaged.value, "endpoint");
      const targetModel = targetProfile
        ? effectiveProfileFieldValue(targetProfile, "model")
        : llmFieldValue(state.llm, llmEnvManaged.value, "model");
      testConnectionError.value = "";
      testConnectionBenchmarks.value = [];
      testConnectionMeta.provider = normalizeSetupProviderForSave(targetProviderChoice, targetEndpoint);
      testConnectionMeta.apiBase = trimText(targetEndpoint) || defaultEndpointForSetupProvider(targetProviderChoice);
      testConnectionMeta.model = trimText(targetModel) || String(payload.model || "").trim();
      return payload;
    }

    function buildConsoleSavePayload(target = "all") {
      const telegramEnv =
        consoleEnvManaged.value?.telegram && typeof consoleEnvManaged.value.telegram === "object"
          ? consoleEnvManaged.value.telegram
          : {};
      const slackEnv =
        consoleEnvManaged.value?.slack && typeof consoleEnvManaged.value.slack === "object"
          ? consoleEnvManaged.value.slack
          : {};
      const managed_runtimes = MANAGED_RUNTIME_ITEMS.filter((item) => state.managedRuntimes[item.id]).map((item) => item.id);
      const telegram = {
        bot_token: consoleFieldRawValue(telegramEnv, "bot_token") || trimText(state.telegram.bot_token),
        allowed_chat_ids: parseConfigListText(state.telegram.allowed_chat_ids_text),
        group_trigger_mode: normalizeConsoleGroupTriggerMode(state.telegram.group_trigger_mode),
      };
      const slack = {
        bot_token: consoleFieldRawValue(slackEnv, "bot_token") || trimText(state.slack.bot_token),
        app_token: consoleFieldRawValue(slackEnv, "app_token") || trimText(state.slack.app_token),
        allowed_team_ids: parseConfigListText(state.slack.allowed_team_ids_text),
        allowed_channel_ids: parseConfigListText(state.slack.allowed_channel_ids_text),
        group_trigger_mode: normalizeConsoleGroupTriggerMode(state.slack.group_trigger_mode),
      };
      const guard = {
        enabled: !!state.guard.enabled,
        network: {
          url_fetch: {
            allowed_url_prefixes: parseConfigListText(state.guard.url_fetch_allowed_url_prefixes_text),
            deny_private_ips: !!state.guard.deny_private_ips,
            follow_redirects: !!state.guard.follow_redirects,
            allow_proxy: !!state.guard.allow_proxy,
          },
        },
        redaction: {
          enabled: !!state.guard.redaction_enabled,
        },
        approvals: {
          enabled: !!state.guard.approvals_enabled,
        },
      };
      if (target === "runtimes") {
        return { managed_runtimes };
      }
      if (target === "telegram") {
        return { telegram };
      }
      if (target === "slack") {
        return { slack };
      }
      if (target === "guard") {
        return { guard };
      }
      return { managed_runtimes, telegram, slack, guard };
    }

    function consoleFieldEntry(kind, field) {
      const key = String(field || "").trim();
      const group = kind === "slack" ? consoleEnvManaged.value?.slack : consoleEnvManaged.value?.telegram;
      if (!key || !group || typeof group !== "object") {
        return null;
      }
      const entry = group[key];
      return entry && typeof entry === "object" ? entry : null;
    }

    function consoleFieldRawValue(group, field) {
      const key = String(field || "").trim();
      if (!key || !group || typeof group !== "object") {
        return "";
      }
      const entry = group[key];
      return typeof entry?.raw_value === "string" ? entry.raw_value.trim() : "";
    }

    function consoleFieldEnvManaged(kind, field) {
      const envName = consoleFieldEntry(kind, field)?.env_name;
      return typeof envName === "string" && envName.trim() !== "";
    }

    function consoleFieldManagedHeadline(kind, field) {
      const entry = consoleFieldEntry(kind, field);
      const envName = typeof entry?.env_name === "string" ? entry.env_name.trim() : "";
      if (!envName) {
        return "";
      }
      const value = typeof entry?.value === "string" ? entry.value.trim() : "";
      return value === "" ? envName : `${envName}=${value}`;
    }

    function updateTelegramField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.telegram, key)) {
        return;
      }
      state.telegram[key] = String(value || "");
    }

    function updateSlackField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.slack, key)) {
        return;
      }
      state.slack[key] = String(value || "");
    }

    function updateTelegramGroupTrigger(item) {
      updateTelegramField("group_trigger_mode", item?.value || "smart");
    }

    function updateSlackGroupTrigger(item) {
      updateSlackField("group_trigger_mode", item?.value || "smart");
    }

    function updateGuardField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.guard, key)) {
        return;
      }
      state.guard[key] = typeof state.guard[key] === "boolean" ? !!value : String(value || "");
    }

    async function saveAgentSettings(target = "all") {
      const normalizedTarget = ["all", "llm", "multimodal", "tools"].includes(String(target))
        ? String(target)
        : "all";
      if (normalizedTarget === "llm" && llmSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "multimodal" && multimodalSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "tools" && toolsSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "all" && agentLoading.value) {
        return;
      }
      if ((normalizedTarget === "llm" || normalizedTarget === "all") && agentValidationError.value !== "") {
        agentNoticeTarget.value = normalizedTarget;
        agentValidationVisible.value = true;
        agentErr.value = "";
        agentOk.value = "";
        return;
      }
      agentSaving.value = true;
      agentSavingTarget.value = normalizedTarget;
      agentNoticeTarget.value = normalizedTarget;
      agentValidationVisible.value = false;
      agentErr.value = "";
      agentOk.value = "";
      try {
        const payload = await apiFetch("/settings/agent", {
          method: "PUT",
          body: buildSavePayload(normalizedTarget),
        });
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        if (normalizedTarget === "llm" || normalizedTarget === "all") {
          const preservedMultimodal = JSON.parse(JSON.stringify(state.multimodal));
          const preservedTools = JSON.parse(JSON.stringify(state.tools));
          const previousMultimodalSnapshot = loadedMultimodalSnapshot.value;
          const previousToolsSnapshot = loadedToolsSnapshot.value;
          applyPayload(payload);
          if (normalizedTarget === "llm") {
            Object.assign(state.multimodal, preservedMultimodal);
            Object.assign(state.tools, preservedTools);
            loadedMultimodalSnapshot.value = previousMultimodalSnapshot;
            loadedToolsSnapshot.value = previousToolsSnapshot;
          }
          await loadEndpoints();
        } else if (normalizedTarget === "multimodal") {
          loadedMultimodalSnapshot.value = buildMultimodalSnapshot(state);
        } else if (normalizedTarget === "tools") {
          loadedToolsSnapshot.value = buildToolsSnapshot(state);
        }
        agentOk.value = t("msg_save_success");
      } catch (e) {
        agentErr.value = e.message || t("msg_save_failed");
      } finally {
        agentSaving.value = false;
        agentSavingTarget.value = "";
      }
    }

    async function saveConsoleSettings(target = "all") {
      const normalizedTarget = ["all", "runtimes", "telegram", "slack", "guard"].includes(String(target))
        ? String(target)
        : "all";
      if (!showConsoleManagedSettings.value) {
        return;
      }
      if (normalizedTarget === "runtimes" && consoleSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "telegram" && telegramSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "slack" && slackSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "guard" && guardSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "all" && (consoleLoading.value || consoleSaving.value || !consoleDirty.value)) {
        return;
      }
      consoleSaving.value = true;
      consoleSavingTarget.value = normalizedTarget;
      consoleNoticeTarget.value = normalizedTarget;
      consoleErr.value = "";
      consoleOk.value = "";
      try {
        const payload = await apiFetch("/settings/console", {
          method: "PUT",
          body: buildConsoleSavePayload(normalizedTarget),
        });
        consoleConfigPath.value =
          typeof payload.config_path === "string" ? payload.config_path : consoleConfigPath.value;
        applyConsolePayload(payload);
        consoleOk.value = t("msg_save_success");
      } catch (e) {
        consoleErr.value = e.message || t("msg_save_failed");
      } finally {
        consoleSaving.value = false;
        consoleSavingTarget.value = "";
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

    function openAPIBasePicker() {
      if (agentLoading.value || agentSaving.value) {
        return;
      }
      apiBasePickerOpen.value = true;
    }

    function applyAPIBaseOption(item) {
      state.llm.endpoint = String(item?.value || "").trim();
    }

    async function openModelPicker() {
      if (agentLoading.value || agentSaving.value) {
        return;
      }
      modelPickerOpen.value = true;
      modelPickerLoading.value = true;
      modelPickerError.value = "";
      modelPickerItems.value = [];
      try {
        const payload = await apiFetch("/settings/agent/models", {
          method: "POST",
          body: {
            endpoint: llmFieldValue(state.llm, llmEnvManaged.value, "endpoint"),
            api_key: llmFieldValue(state.llm, llmEnvManaged.value, "api_key"),
          },
        });
        const items = Array.isArray(payload?.items) ? payload.items : [];
        modelPickerItems.value = items.map((value) => ({
          id: value,
          title: value,
          value,
          note: "",
        }));
      } catch (e) {
        modelPickerError.value = e.message || t("msg_load_failed");
      } finally {
        modelPickerLoading.value = false;
      }
    }

    function applyModelOption(item) {
      state.llm.model = String(item?.value || "").trim();
    }

    async function openTestConnection(profileKey = "") {
      const targetProfile = state.llm.profiles.find((item) => item._key === profileKey) || null;
      if (!targetProfile && testConnectionDisabled.value) {
        return;
      }
      if (targetProfile && testConnectionDisabledForProfile(targetProfile)) {
        return;
      }
      testConnectionTargetProfileKey.value = targetProfile?._key || "";
      primeConnectionTestState(targetProfile);
      testConnectionOpen.value = true;
      await runConnectionTest();
    }

    async function runConnectionTest() {
      if (testConnectionLoading.value) {
        return;
      }
      const targetProfile = currentTestTargetProfile.value;
      const targetProfileName = trimText(targetProfile?.name);
      if (testConnectionTargetProfileKey.value !== "" && targetProfileName === "") {
        testConnectionError.value = t("settings_agent_profile_name_required");
        return;
      }
      const nextPayload = primeConnectionTestState(
        targetProfile,
        targetProfile ? buildProfileTestPayload(targetProfile) : buildDefaultLLMTestPayload(),
      );
      testConnectionLoading.value = true;
      try {
        const body = {
          llm: nextPayload,
        };
        if (targetProfileName !== "") {
          body.target_profile = targetProfileName;
        }
        const payload = await apiFetch("/settings/agent/test", {
          method: "POST",
          body,
        });
        testConnectionMeta.provider = String(payload?.provider || "").trim();
        const resolvedAPIBase = String(payload?.api_base || "").trim();
        if (resolvedAPIBase !== "") {
          testConnectionMeta.apiBase = resolvedAPIBase;
        }
        testConnectionMeta.model = String(payload?.model || "").trim();
        const items = Array.isArray(payload?.benchmarks) ? payload.benchmarks : [];
        testConnectionBenchmarks.value = items.map((item) => ({
          id: String(item?.id || "").trim(),
          ok: item?.ok === true,
          duration_ms: Number(item?.duration_ms || 0),
          detail: String(item?.detail || "").trim(),
          error: String(item?.error || "").trim(),
          raw_response: String(item?.raw_response || ""),
        }));
      } catch (e) {
        testConnectionError.value = e.message || t("msg_load_failed");
      } finally {
        testConnectionLoading.value = false;
      }
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

    function refreshMobileMode() {
      isMobile.value = typeof window !== "undefined" && window.innerWidth <= 920;
    }

    function showIndexView() {
      mobilePanelVisible.value = false;
    }

    function selectSection(id) {
      selectedSectionID.value = String(id || "").trim();
      if (isMobile.value) {
        mobilePanelVisible.value = true;
      }
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
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      void loadAgentSettings();
    });

    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
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
        if (["runtimes", "channels", "guard"].includes(selectedSectionID.value)) {
          selectedSectionID.value = "console";
        }
      },
      { immediate: true }
    );

    watch(deleteProfileDialogOpen, (open) => {
      if (!open) {
        deleteProfileTargetKey.value = "";
      }
    });

    return {
      t,
      lang,
      loggingOut,
      agentLoading,
      agentSaving,
      agentSavingTarget,
      agentNoticeTarget,
      agentErr,
      agentOk,
      agentValidationVisible,
      deleteProfileDialogOpen,
      consoleLoading,
      consoleSaving,
      consoleSavingTarget,
      consoleNoticeTarget,
      consoleErr,
      consoleOk,
      llmConfigPath,
      consoleConfigPath,
      state,
      llmEnvManaged,
      defaultProviderItems,
      profileProviderItems,
      profileBaseProvider,
      reasoningEffortItems,
      profileReasoningEffortItems,
      toolsEmulationItems,
      profileToolsEmulationItems,
      profileOptions,
      agentValidationError,
      deleteProfileDialogText,
      deleteProfileDialogActions,
      apiBasePickerItems,
      multimodalItems,
      toolItems,
      managedRuntimeItems,
      groupTriggerItems,
      settingsSections,
      selectedSection,
      panelHint,
      activeSaveKind,
      showIndexPane,
      showPanelPane,
      mobileShowBack,
      mobileBarTitle,
      pageClass,
      llmSaveDisabled,
      multimodalSaveDisabled,
      toolsSaveDisabled,
      consoleSaveDisabled,
      telegramSaveDisabled,
      slackSaveDisabled,
      guardSaveDisabled,
      testConnectionDisabled,
      testConnectionDisabledForProfile,
      logout,
      saveAgentSettings,
      saveConsoleSettings,
      updateDefaultLLMField,
      updateProfileField,
      llmProfileEnvManaged,
      addLLMProfile,
      confirmRemoveLLMProfile,
      removeLLMProfile,
      addFallbackProfile,
      updateFallbackProfile,
      removeFallbackProfile,
      moveFallbackProfile,
      openAPIBasePicker,
      applyAPIBaseOption,
      openModelPicker,
      applyModelOption,
      openTestConnection,
      runConnectionTest,
      setMultimodalSource,
      setToolEnabled,
      setManagedRuntimeEnabled,
      consoleFieldEnvManaged,
      consoleFieldManagedHeadline,
      updateTelegramField,
      updateSlackField,
      updateTelegramGroupTrigger,
      updateSlackGroupTrigger,
      updateGuardField,
      selectSection,
      isSelectedSection,
      sectionClass,
      showIndexView,
      apiBasePickerOpen,
      modelPickerOpen,
      modelPickerLoading,
      modelPickerError,
      modelPickerItems,
      testConnectionOpen,
      testConnectionLoading,
      testConnectionError,
      testConnectionBenchmarks,
      testConnectionMeta,
      onLanguageChange: applyLanguageChange,
    };
  },
  template: `
    <AppPage :title="t('settings_title')" :class="pageClass" :showMobileNavTrigger="!mobileShowBack">
      <template #leading>
        <div class="settings-page-bar">
          <QButton
            v-if="mobileShowBack"
            class="outlined xs icon settings-page-bar-back"
            :title="t('settings_title')"
            :aria-label="t('settings_title')"
            @click="showIndexView"
          >
            <QIconArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ mobileBarTitle }}</h2>
        </div>
      </template>
      <div class="settings-workbench">
        <aside v-if="showIndexPane" class="settings-index workspace-sidebar-section">
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

        <template v-if="showPanelPane && selectedSection">
          <div v-if="selectedSection.id === 'agent'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Agent" right="LLM Config" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_agent_block_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_agent_llm_hint", { path: llmConfigPath || "config.yaml" }) }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="agentSaving && agentSavingTarget === 'llm'"
                      :disabled="llmSaveDisabled"
                      @click="saveAgentSettings('llm')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="agentErr && agentNoticeTarget !== 'multimodal'"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="agentErr"
                  />
                  <QFence
                    v-if="agentValidationVisible && agentNoticeTarget !== 'multimodal' && !agentErr && agentValidationError"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="agentValidationError"
                  />
                  <QFence
                    v-if="agentOk && agentNoticeTarget !== 'multimodal'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="agentOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-agent-stack">
                    <section class="settings-agent-section">
                      <div class="settings-agent-section-copy">
                        <strong class="settings-toggle-title">{{ t("settings_agent_primary_title") }}</strong>
                        <p class="settings-toggle-note">{{ t("settings_agent_primary_note") }}</p>
                      </div>
                      <LLMConfigForm
                        :config="state.llm"
                        :busy="agentLoading || agentSaving"
                        :envManaged="llmEnvManaged"
                        :defaultProvider="profileBaseProvider"
                        :providerItems="defaultProviderItems"
                        :reasoningEffortItems="reasoningEffortItems"
                        :toolsEmulationItems="toolsEmulationItems"
                        :enableAPIBasePicker="true"
                        :enableModelPicker="true"
                        :showTestAction="true"
                        :testActionDisabled="testConnectionDisabled"
                        @update-field="updateDefaultLLMField"
                        @open-api-base-picker="openAPIBasePicker"
                        @open-model-picker="openModelPicker"
                        @open-test="openTestConnection"
                      />
                    </section>

                    <section class="settings-agent-section">
                      <header class="settings-agent-section-head">
                        <div class="settings-agent-section-copy">
                          <strong class="settings-toggle-title">{{ t("settings_agent_profiles_title") }}</strong>
                          <p class="settings-toggle-note">{{ t("settings_agent_profiles_note") }}</p>
                        </div>
                      </header>

                      <div class="settings-profile-list">
                        <article v-for="profile in state.llm.profiles" :key="profile._key" class="settings-profile-card">
                          <div class="settings-profile-head">
                            <div class="settings-field settings-profile-name">
                              <span class="settings-field-label">{{ t("settings_agent_profile_name_label") }}</span>
                              <div class="settings-field-control settings-profile-name-control">
                                <QInput
                                  :modelValue="profile.name"
                                  :placeholder="t('settings_agent_profile_name_placeholder')"
                                  :disabled="agentLoading || agentSaving"
                                  @update:modelValue="updateProfileField(profile._key, { field: 'name', value: $event })"
                                />
                                <QButton
                                  type="button"
                                  class="danger icon settings-profile-delete"
                                  :title="t('action_delete')"
                                  :aria-label="t('action_delete')"
                                  :disabled="agentLoading || agentSaving"
                                  @click="confirmRemoveLLMProfile(profile._key)"
                                >
                                  <QIconTrash class="icon" />
                                </QButton>
                              </div>
                            </div>
                          </div>

                          <LLMConfigForm
                            :config="profile"
                            :busy="agentLoading || agentSaving"
                            :envManaged="llmProfileEnvManaged(profile)"
                            :defaultProvider="profileBaseProvider"
                            :providerItems="profileProviderItems"
                            :reasoningEffortItems="profileReasoningEffortItems"
                            :toolsEmulationItems="profileToolsEmulationItems"
                            :providerPlaceholderKey="'settings_agent_provider_inherit'"
                            :allowProviderInherit="true"
                            :showTestAction="true"
                            :testActionDisabled="testConnectionDisabledForProfile(profile)"
                            @update-field="updateProfileField(profile._key, $event)"
                            @open-test="openTestConnection(profile._key)"
                          />
                        </article>

                        <QButton
                          type="button"
                          class="placeholder settings-profile-placeholder"
                          :disabled="agentLoading || agentSaving"
                          @click="addLLMProfile"
                        >
                          <QIconPlus class="icon" />
                          {{ t("settings_agent_profile_add") }}
                        </QButton>
                      </div>
                    </section>

                    <section class="settings-agent-section">
                      <header class="settings-agent-section-head">
                        <div class="settings-agent-section-copy">
                          <strong class="settings-toggle-title">{{ t("settings_agent_fallback_title") }}</strong>
                          <p class="settings-toggle-note">{{ t("settings_agent_fallback_note") }}</p>
                        </div>
                      </header>

                      <p v-if="!profileOptions.length" class="settings-agent-empty">{{ t("settings_agent_fallback_empty") }}</p>

                      <div v-else class="settings-fallback-list">
                        <div v-for="(fallbackName, index) in state.llm.fallback_profiles" :key="index" class="settings-fallback-row">
                          <span class="settings-fallback-index">{{ index + 1 }}</span>
                          <QDropdownMenu
                            :key="fallbackName + '-' + index"
                            class="settings-fallback-picker"
                            :items="profileOptions"
                            :initialItem="profileOptions.find((item) => item.value === fallbackName) || null"
                            :placeholder="t('settings_agent_fallback_placeholder')"
                            @change="updateFallbackProfile(index, $event)"
                          />
                          <div class="settings-fallback-actions">
                            <QButton
                              type="button"
                              class="outlined icon settings-fallback-action"
                              :title="t('settings_agent_order_up')"
                              :aria-label="t('settings_agent_order_up')"
                              :disabled="agentLoading || agentSaving || index === 0"
                              @click="moveFallbackProfile(index, -1)"
                            >
                              <QIconChevronUp class="icon" />
                            </QButton>
                            <QButton
                              type="button"
                              class="outlined icon settings-fallback-action"
                              :title="t('settings_agent_order_down')"
                              :aria-label="t('settings_agent_order_down')"
                              :disabled="agentLoading || agentSaving || index === state.llm.fallback_profiles.length - 1"
                              @click="moveFallbackProfile(index, 1)"
                            >
                              <QIconChevronDown class="icon" />
                            </QButton>
                            <QButton
                              type="button"
                              class="danger icon settings-fallback-action"
                              :title="t('action_delete')"
                              :aria-label="t('action_delete')"
                              :disabled="agentLoading || agentSaving"
                              @click="removeFallbackProfile(index)"
                            >
                              <QIconTrash class="icon" />
                            </QButton>
                          </div>
                        </div>

                        <QButton
                          type="button"
                          class="placeholder settings-profile-placeholder"
                          :disabled="agentLoading || agentSaving || !profileOptions.length"
                          @click="addFallbackProfile"
                        >
                          <QIconPlus class="icon" />
                          {{ t("settings_agent_fallback_add") }}
                        </QButton>
                      </div>
                    </section>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Agent" right="Multimodal" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_multimodal_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_multimodal_hint") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="agentSaving && agentSavingTarget === 'multimodal'"
                      :disabled="multimodalSaveDisabled"
                      @click="saveAgentSettings('multimodal')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="agentErr && agentNoticeTarget === 'multimodal'"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="agentErr"
                  />
                  <QFence
                    v-if="agentOk && agentNoticeTarget === 'multimodal'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="agentOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-toggle-list">
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
                </div>
              </div>
            </QCard>
          </div>

          <div v-else-if="selectedSection.id === 'channels'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Console" right="Telegram" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_telegram_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_console_telegram_token_note") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="consoleSaving && consoleSavingTarget === 'telegram'"
                      :disabled="telegramSaveDisabled"
                      @click="saveConsoleSettings('telegram')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="consoleErr && consoleNoticeTarget !== 'slack' && consoleNoticeTarget !== 'guard'"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="consoleErr"
                  />
                  <QFence
                    v-if="consoleOk && consoleNoticeTarget !== 'slack' && consoleNoticeTarget !== 'guard'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="consoleOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_telegram_bot_token_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('telegram', 'bot_token')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("telegram", "bot_token") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.telegram.bot_token"
                        inputType="password"
                        :placeholder="t('settings_console_telegram_bot_token_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateTelegramField('bot_token', $event)"
                      />
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_telegram_allowed_chat_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.telegram.allowed_chat_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_telegram_allowed_chat_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateTelegramField('allowed_chat_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_telegram_allowed_chat_ids_note") }}</p>
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.telegram.group_trigger_mode || 'telegram-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.telegram.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateTelegramGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_telegram_group_trigger_note") }}</p>
                    </label>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Console" right="Slack" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_slack_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_console_slack_token_note") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="consoleSaving && consoleSavingTarget === 'slack'"
                      :disabled="slackSaveDisabled"
                      @click="saveConsoleSettings('slack')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="consoleErr && consoleNoticeTarget === 'slack'"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="consoleErr"
                  />
                  <QFence
                    v-if="consoleOk && consoleNoticeTarget === 'slack'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="consoleOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_bot_token_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('slack', 'bot_token')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("slack", "bot_token") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.slack.bot_token"
                        inputType="password"
                        :placeholder="t('settings_console_slack_bot_token_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('bot_token', $event)"
                      />
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_app_token_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('slack', 'app_token')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("slack", "app_token") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.slack.app_token"
                        inputType="password"
                        :placeholder="t('settings_console_slack_app_token_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('app_token', $event)"
                      />
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_allowed_team_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.slack.allowed_team_ids_text"
                        :rows="3"
                        :placeholder="t('settings_console_slack_allowed_team_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('allowed_team_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_allowed_team_ids_note") }}</p>
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_allowed_channel_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.slack.allowed_channel_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_slack_allowed_channel_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('allowed_channel_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_allowed_channel_ids_note") }}</p>
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.slack.group_trigger_mode || 'slack-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.slack.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateSlackGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_group_trigger_note") }}</p>
                    </label>
                  </div>
                </div>
              </div>
            </QCard>
          </div>

          <div v-else-if="selectedSection.id === 'guard'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Console" right="Guard" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_guard_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_console_guard_note") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="consoleSaving && consoleSavingTarget === 'guard'"
                      :disabled="guardSaveDisabled"
                      @click="saveConsoleSettings('guard')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="consoleErr && (consoleNoticeTarget === '' || consoleNoticeTarget === 'guard')"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="consoleErr"
                  />
                  <QFence
                    v-if="consoleOk && consoleNoticeTarget === 'guard'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="consoleOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_guard_allowed_url_prefixes_label") }}</span>
                      <QTextarea
                        :modelValue="state.guard.url_fetch_allowed_url_prefixes_text"
                        :rows="4"
                        :placeholder="t('settings_console_guard_allowed_url_prefixes_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('url_fetch_allowed_url_prefixes_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_guard_allowed_url_prefixes_note") }}</p>
                    </label>
                  </div>

                  <div class="settings-toggle-list">
                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_enabled_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_enabled_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.enabled"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('enabled', $event)"
                      />
                    </div>

                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_deny_private_ips_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_deny_private_ips_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.deny_private_ips"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('deny_private_ips', $event)"
                      />
                    </div>

                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_follow_redirects_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_follow_redirects_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.follow_redirects"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('follow_redirects', $event)"
                      />
                    </div>

                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_allow_proxy_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_allow_proxy_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.allow_proxy"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('allow_proxy', $event)"
                      />
                    </div>

                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_redaction_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_redaction_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.redaction_enabled"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('redaction_enabled', $event)"
                      />
                    </div>

                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_console_guard_approvals_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_console_guard_approvals_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.guard.approvals_enabled"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('approvals_enabled', $event)"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </QCard>
          </div>

          <QCard v-else class="settings-panel-card" variant="default">
            <div class="settings-panel-shell">
              <header class="settings-panel-head">
                <div class="settings-panel-copy">
                  <AppKicker as="p" :left="selectedSection.kickerLeft" :right="selectedSection.kickerRight" />
                  <h3 class="settings-panel-title workspace-document-title">{{ selectedSection.title }}</h3>
                  <p class="settings-panel-meta">{{ panelHint }}</p>
                </div>
                <div class="settings-panel-actions">
                  <QButton
                    v-if="activeSaveKind === 'agent' && selectedSection.id === 'tools'"
                    class="primary"
                    :loading="agentSaving && agentSavingTarget === 'tools'"
                    :disabled="toolsSaveDisabled"
                    @click="saveAgentSettings('tools')"
                  >
                    {{ t("action_save") }}
                  </QButton>
                  <QButton
                    v-else-if="activeSaveKind === 'console' && selectedSection.id === 'runtimes'"
                    class="primary"
                    :loading="consoleSaving"
                    :disabled="consoleSaveDisabled"
                    @click="saveConsoleSettings('runtimes')"
                  >
                    {{ t("action_save") }}
                  </QButton>
                </div>
              </header>

              <div class="settings-panel-notices">
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
                <QFence
                  v-if="activeSaveKind === 'agent' && agentErr"
                  type="danger"
                  icon="QIconCloseCircle"
                  :text="agentErr"
                />
                <QFence
                  v-if="activeSaveKind === 'agent' && agentValidationVisible && !agentErr && agentValidationError"
                  type="danger"
                  icon="QIconCloseCircle"
                  :text="agentValidationError"
                />
                <QFence
                  v-if="activeSaveKind === 'agent' && agentOk"
                  type="success"
                  icon="QIconCheckCircle"
                  :text="agentOk"
                />
              </div>

              <div class="settings-panel-body">
              <div v-if="selectedSection.id === 'tools'" class="settings-toggle-list">
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
        </template>
      </div>

      <SetupPickerDialog
        v-model="apiBasePickerOpen"
        :items="apiBasePickerItems"
        :loading="false"
        :error="''"
        :filterPlaceholder="t('setup_llm_api_base_picker_filter_placeholder')"
        :emptyText="t('setup_llm_api_base_picker_empty')"
        @select="applyAPIBaseOption"
      />

      <SetupPickerDialog
        v-model="modelPickerOpen"
        :items="modelPickerItems"
        :loading="modelPickerLoading"
        :error="modelPickerError"
        :filterPlaceholder="t('setup_llm_model_picker_filter_placeholder')"
        :emptyText="t('setup_llm_model_picker_empty')"
        :showValue="false"
        @select="applyModelOption"
      />

      <SetupConnectionTestDialog
        v-model="testConnectionOpen"
        :loading="testConnectionLoading"
        :error="testConnectionError"
        :benchmarks="testConnectionBenchmarks"
        :provider="testConnectionMeta.provider"
        :apiBase="testConnectionMeta.apiBase"
        :model="testConnectionMeta.model"
        :showIntro="false"
        @retry="runConnectionTest"
      />
      <QMessageDialog
        v-model="deleteProfileDialogOpen"
        icon="QIconTrash"
        iconColor="red"
        :title="t('action_delete')"
        :text="deleteProfileDialogText"
        :actions="deleteProfileDialogActions"
      />
    </AppPage>
  `,
};

export default SettingsView;
