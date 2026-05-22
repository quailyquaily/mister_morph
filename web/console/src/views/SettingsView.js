import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useToast } from "quail-ui";
import "./SettingsView.css";

import AppKicker from "../components/AppKicker";
import AppPage from "../components/AppPage";
import CodexAuthDialog from "../components/CodexAuthDialog";
import ImageUploadField from "../components/ImageUploadField";
import LLMConfigForm from "../components/LLMConfigForm";
import MarkdownEditor from "../components/MarkdownEditor";
import SetupConnectionTestDialog from "../components/SetupConnectionTestDialog";
import SetupPickerDialog from "../components/SetupPickerDialog";
import defaultAvatarMarkup from "../assets/images/app_logo_current.svg?raw";
import {
  apiFetch,
  authState,
  endpointState,
  formatTime,
  loadEndpoints,
  localeState,
  runtimeApiDownloadForEndpoint,
  runtimeApiFetchForEndpoint,
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
  canOpenExternalURLInDesktop,
  openExternalPlaceholder,
  openExternalURL,
} from "../core/external-links";
import {
  canCheckDesktopUpdate,
  checkDesktopUpdate,
  desktopRuntimeVersion,
} from "../core/desktop-runtime";
import { recordSnapshotBuild } from "../core/performance";
import {
  defaultEndpointForSetupProvider,
  OPENAI_COMPATIBLE_API_BASE_OPTIONS,
  normalizeSetupProviderChoice,
  normalizeSetupProviderForSave,
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_OPTIONS,
  setupProviderRequiresAPIKey,
} from "../core/setup-contract";
import { invalidateConsoleSetupReadiness } from "../core/setup";
import { openReentrantDialog } from "../core/reentrant-dialog";
import {
  buildEmptyPersonaIdentityState,
  buildIdentityYAML,
  buildPersonaIdentitySnapshot,
  dispatchPersonaAvatarUpdated,
  dispatchPersonaIdentityUpdated,
  LEGACY_IDENTITY_ENDPOINT,
  LEGACY_SOUL_ENDPOINT,
  normalizeSoulDocument,
  parseIdentityProfile,
  PERSONA_AVATAR_ENDPOINT,
  PERSONA_AVATAR_MAX_SOURCE_BYTES,
  PERSONA_AVATAR_SIZE,
  PERSONA_AVATAR_SOURCE_TYPES,
  PERSONA_IDENTITY_ENDPOINT,
  PERSONA_SOUL_ENDPOINT,
} from "../core/persona-profile";

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
const SETTINGS_DEFAULT_SECTION_ID = "persona";
const SETTINGS_SECTION_IDS = new Set(["agent", "tools", "skills", "persona", "channels", "runtimes", "guard", "console"]);
const UPDATE_RELEASES_URL = "https://github.com/quailyquaily/mistermorph/releases";
let llmProfileKeySeed = 0;

function settingsRouteSection(route) {
  const value = route?.params?.section;
  const text = Array.isArray(value) ? value[0] : value;
  return String(text || "").trim();
}

function normalizeSettingsSectionID(value) {
  const id = String(value || "").trim();
  return SETTINGS_SECTION_IDS.has(id) ? id : SETTINGS_DEFAULT_SECTION_ID;
}

function settingsSectionPath(id) {
  const sectionID = normalizeSettingsSectionID(id);
  return sectionID === SETTINGS_DEFAULT_SECTION_ID ? "/settings" : `/settings/${sectionID}`;
}

function buildEmptyLLMForm() {
  return {
    provider: "",
    endpoint: "",
    model: "",
    context_window_tokens: "",
    api_key: "",
    bedrock_aws_key: "",
    bedrock_aws_secret: "",
    bedrock_region: "",
    bedrock_model_arn: "",
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

function normalizeText(value) {
  return String(value || "").replace(/\r\n/g, "\n");
}

function lineCount(value) {
  const text = String(value || "");
  if (!text) {
    return 0;
  }
  return text.split(/\r?\n/).length;
}

function normalizeAppVersion(version) {
  return trimText(version).replace(/^v/i, "");
}

function parseComparableVersion(version) {
  let normalized = normalizeAppVersion(version);
  if (!normalized || normalized.toLowerCase() === "dev") {
    return null;
  }
  const buildIndex = normalized.indexOf("+");
  if (buildIndex >= 0) {
    normalized = normalized.slice(0, buildIndex);
  }
  const [core, pre = ""] = normalized.split("-", 2);
  const parts = core.split(".");
  if (!parts.length) {
    return null;
  }
  const nums = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) {
      return null;
    }
    nums.push(Number(part));
  }
  return { parts: nums, pre };
}

function compareAppVersions(current, latest) {
  const left = parseComparableVersion(current);
  const right = parseComparableVersion(latest);
  if (!left || !right) {
    return null;
  }
  const maxLen = Math.max(left.parts.length, right.parts.length);
  for (let index = 0; index < maxLen; index += 1) {
    const a = index < left.parts.length ? left.parts[index] : 0;
    const b = index < right.parts.length ? right.parts[index] : 0;
    if (a < b) {
      return -1;
    }
    if (a > b) {
      return 1;
    }
  }
  if (left.pre === right.pre) {
    return 0;
  }
  if (left.pre === "") {
    return 1;
  }
  if (right.pre === "") {
    return -1;
  }
  return left.pre < right.pre ? -1 : 1;
}

async function copyTextToClipboard(text) {
  const value = String(text || "");
  if (!value) {
    return false;
  }
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return true;
  }
  if (typeof document === "undefined") {
    return false;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  try {
    return document.execCommand("copy");
  } finally {
    document.body.removeChild(textarea);
  }
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

function parseSkillLoadText(value) {
  return String(value || "")
    .split(/\r?\n/)
    .map((item) => trimText(item))
    .filter((item) => item !== "");
}

function formatSkillLoadList(values) {
  return normalizeNamedList(Array.isArray(values) ? values : []).join("\n");
}

function toolEnabledValue(entry) {
  return !!(entry && typeof entry === "object" && entry.enabled === true);
}

function normalizeSkillItems(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  return values
    .map((item) => ({
      id: trimText(item?.id),
      name: trimText(item?.name),
      description: trimText(item?.description),
    }))
    .filter((item) => item.id !== "" || item.name !== "");
}

function serializeLLMProfile(profile) {
  return {
    name: trimText(profile?.name),
    provider: trimText(profile?.provider),
    endpoint: trimText(profile?.endpoint),
    model: trimText(profile?.model),
    context_window_tokens: trimText(profile?.context_window_tokens),
    api_key: trimText(profile?.api_key),
    bedrock_aws_key: trimText(profile?.bedrock_aws_key),
    bedrock_aws_secret: trimText(profile?.bedrock_aws_secret),
    bedrock_region: trimText(profile?.bedrock_region),
    bedrock_model_arn: trimText(profile?.bedrock_model_arn),
    cloudflare_api_token: trimText(profile?.cloudflare_api_token),
    cloudflare_account_id: trimText(profile?.cloudflare_account_id),
    reasoning_effort: trimText(profile?.reasoning_effort),
    tools_emulation_mode: trimText(profile?.tools_emulation_mode),
  };
}

function buildLLMSnapshot(state) {
  recordSnapshotBuild("settings.llm");
  return JSON.stringify({
    llm: {
      provider: trimText(state.llm.provider),
      endpoint: trimText(state.llm.endpoint),
      model: trimText(state.llm.model),
      context_window_tokens: trimText(state.llm.context_window_tokens),
      api_key: trimText(state.llm.api_key),
      bedrock_aws_key: trimText(state.llm.bedrock_aws_key),
      bedrock_aws_secret: trimText(state.llm.bedrock_aws_secret),
      bedrock_region: trimText(state.llm.bedrock_region),
      bedrock_model_arn: trimText(state.llm.bedrock_model_arn),
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
  recordSnapshotBuild("settings.multimodal");
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
  recordSnapshotBuild("settings.tools");
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

function buildSkillsSnapshot(state) {
  recordSnapshotBuild("settings.skills");
  return JSON.stringify({
    skills: {
      enabled: !!state.skills.enabled,
      load: parseSkillLoadText(state.skills.load_text),
    },
  });
}

function formatSkillCount(count) {
  const value = Math.max(0, Number(count) || 0);
  return value === 1 ? "1 Skill" : `${value} Skills`;
}

function buildConsoleManagedRuntimeSnapshot(state) {
  recordSnapshotBuild("settings.console.managed_runtimes");
  return JSON.stringify({
    telegram: !!state.managedRuntimes.telegram,
    slack: !!state.managedRuntimes.slack,
  });
}

function buildConsoleTelegramSnapshot(state) {
  recordSnapshotBuild("settings.console.telegram");
  return JSON.stringify({
    bot_token: trimText(state.telegram.bot_token),
    allowed_chat_ids: parseConfigListText(state.telegram.allowed_chat_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.telegram.group_trigger_mode),
  });
}

function buildConsoleSlackSnapshot(state) {
  recordSnapshotBuild("settings.console.slack");
  return JSON.stringify({
    bot_token: trimText(state.slack.bot_token),
    app_token: trimText(state.slack.app_token),
    allowed_team_ids: parseConfigListText(state.slack.allowed_team_ids_text),
    allowed_channel_ids: parseConfigListText(state.slack.allowed_channel_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.slack.group_trigger_mode),
  });
}

function buildConsoleGuardSnapshot(state) {
  recordSnapshotBuild("settings.console.guard");
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
    CodexAuthDialog,
    ImageUploadField,
    LLMConfigForm,
    MarkdownEditor,
    SetupConnectionTestDialog,
    SetupPickerDialog,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const router = useRouter();
    const route = useRoute();
    const lang = computed(() => localeState.lang);
    const loggingOut = ref(false);
    const agentLoading = ref(false);
    const agentSaving = ref(false);
    const agentSavingTarget = ref("");
    const agentSettingsReadOnly = ref(false);
    const agentNoticeTarget = ref("");
    const agentErr = ref("");
    const agentOk = ref("");
    const agentValidationVisible = ref(false);
    const skillsValidationVisible = ref(false);
    const deleteProfileDialogOpen = ref(false);
    const deleteProfileTargetKey = ref("");
    const llmConfigPath = ref("");
    const loadedLLMSnapshot = ref("");
    const loadedMultimodalSnapshot = ref("");
    const loadedSkillsSnapshot = ref("");
    const loadedToolsSnapshot = ref("");
    const llmDirty = ref(false);
    const multimodalDirty = ref(false);
    const skillsDirty = ref(false);
    const toolsDirty = ref(false);
    const agentSettingsLoaded = ref(false);
    const llmEnvManaged = ref({});
    const consoleLoading = ref(false);
    const consoleSaving = ref(false);
    const consoleSavingTarget = ref("");
    const consoleNoticeTarget = ref("");
    const consoleErr = ref("");
    const consoleOk = ref("");
    const consoleConfigPath = ref("");
    const loadedConsoleManagedSnapshot = ref("");
    const loadedConsoleTelegramSnapshot = ref("");
    const loadedConsoleSlackSnapshot = ref("");
    const loadedConsoleGuardSnapshot = ref("");
    const consoleManagedDirty = ref(false);
    const consoleTelegramDirty = ref(false);
    const consoleSlackDirty = ref(false);
    const consoleGuardDirty = ref(false);
    const consoleSettingsLoaded = ref(false);
    const consoleEnvManaged = ref({});
    const personaLoading = ref(false);
    const personaSaving = ref(false);
    const personaSavingTarget = ref("");
    const personaErr = ref("");
    const personaOk = ref("");
    const loadedIdentityRaw = ref("");
    const loadedIdentitySnapshot = ref("");
    const loadedSoulSnapshot = ref("");
    const personaSettingsLoaded = ref(false);
    const soulContent = ref("");
    const personaAvatarURL = ref("");
    const personaAvatarBusy = ref(false);
    let personaAvatarObjectURL = "";
    const personaAvatarSourceTypes = Array.from(PERSONA_AVATAR_SOURCE_TYPES);
    const desktopUpdateBindingAvailable = ref(false);
    const desktopLoading = ref(false);
    const desktopChecking = ref(false);
    const desktopErr = ref("");
    const desktopOk = ref("");
    const desktopCurrentVersion = ref(desktopRuntimeVersion() || "dev");
    const desktopUpdateResult = ref(null);
    const desktopSettingsLoaded = ref(false);
    const desktopChecksumCopied = ref(false);
    const desktopChangelogField = ref(null);
    const selectedSectionID = ref(normalizeSettingsSectionID(settingsRouteSection(route)));
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
    const codexAuthLoading = ref(false);
    const codexAuthBusy = ref(false);
    const codexAuthError = ref("");
    const codexAuthDialogOpen = ref(false);
    const codexLoginSession = ref("");
    const codexLoginVerificationURL = ref("");
    const codexLoginUserCode = ref("");
    const codexLoginExpiresAt = ref("");
    let codexLoginPollTimer = 0;
    let desktopChecksumCopyTimer = 0;
    const codexAuthStatus = reactive({
      logged_in: false,
      access_token_present: false,
      refresh_token_present: false,
      access_token_expired: false,
      expires_at: "",
      account_id: "",
      file_mode_ok: true,
      file_mode_warning: "",
    });

    const state = reactive({
      persona: buildEmptyPersonaIdentityState(),
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
      skills: {
        enabled: true,
        load_text: "",
        loaded: [],
        available: [],
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

    function clearLoadedAgentSnapshots() {
      loadedLLMSnapshot.value = "";
      loadedMultimodalSnapshot.value = "";
      loadedSkillsSnapshot.value = "";
      loadedToolsSnapshot.value = "";
      llmDirty.value = false;
      multimodalDirty.value = false;
      skillsDirty.value = false;
      toolsDirty.value = false;
      agentSettingsLoaded.value = false;
    }

    function currentAgentSnapshotScope(sectionID = selectedSectionID.value) {
      switch (sectionID) {
        case "agent":
          return "agent";
        case "tools":
          return "tools";
        case "skills":
          return "skills";
        default:
          return "";
      }
    }

    function setLoadedAgentSnapshots(scope = "all") {
      const normalizedScope = String(scope || "all");
      if (normalizedScope === "all" || normalizedScope === "agent" || normalizedScope === "llm") {
        loadedLLMSnapshot.value = buildLLMSnapshot(state);
        llmDirty.value = false;
      }
      if (normalizedScope === "all" || normalizedScope === "agent" || normalizedScope === "multimodal") {
        loadedMultimodalSnapshot.value = buildMultimodalSnapshot(state);
        multimodalDirty.value = false;
      }
      if (normalizedScope === "all" || normalizedScope === "skills") {
        loadedSkillsSnapshot.value = buildSkillsSnapshot(state);
        skillsDirty.value = false;
      }
      if (normalizedScope === "all" || normalizedScope === "tools") {
        loadedToolsSnapshot.value = buildToolsSnapshot(state);
        toolsDirty.value = false;
      }
    }

    function ensureLoadedAgentSnapshotsForSection(sectionID = selectedSectionID.value) {
      if (!agentSettingsLoaded.value) {
        return;
      }
      const scope = currentAgentSnapshotScope(sectionID);
      if (scope === "agent") {
        if (!loadedLLMSnapshot.value) {
          setLoadedAgentSnapshots("llm");
        }
        if (!loadedMultimodalSnapshot.value) {
          setLoadedAgentSnapshots("multimodal");
        }
      } else if (scope === "tools" && !loadedToolsSnapshot.value) {
        setLoadedAgentSnapshots("tools");
      } else if (scope === "skills" && !loadedSkillsSnapshot.value) {
        setLoadedAgentSnapshots("skills");
      }
    }

    function updateLLMDirty() {
      llmDirty.value = buildLLMSnapshot(state) !== loadedLLMSnapshot.value;
    }

    function updateMultimodalDirty() {
      multimodalDirty.value = buildMultimodalSnapshot(state) !== loadedMultimodalSnapshot.value;
    }

    function updateSkillsDirty() {
      skillsDirty.value = buildSkillsSnapshot(state) !== loadedSkillsSnapshot.value;
    }

    function updateToolsDirty() {
      toolsDirty.value = buildToolsSnapshot(state) !== loadedToolsSnapshot.value;
    }

    function setLoadedConsoleSnapshots() {
      loadedConsoleManagedSnapshot.value = buildConsoleManagedRuntimeSnapshot(state);
      loadedConsoleTelegramSnapshot.value = buildConsoleTelegramSnapshot(state);
      loadedConsoleSlackSnapshot.value = buildConsoleSlackSnapshot(state);
      loadedConsoleGuardSnapshot.value = buildConsoleGuardSnapshot(state);
      consoleManagedDirty.value = false;
      consoleTelegramDirty.value = false;
      consoleSlackDirty.value = false;
      consoleGuardDirty.value = false;
    }

    function clearLoadedConsoleSnapshots() {
      loadedConsoleManagedSnapshot.value = "";
      loadedConsoleTelegramSnapshot.value = "";
      loadedConsoleSlackSnapshot.value = "";
      loadedConsoleGuardSnapshot.value = "";
      consoleManagedDirty.value = false;
      consoleTelegramDirty.value = false;
      consoleSlackDirty.value = false;
      consoleGuardDirty.value = false;
      consoleSettingsLoaded.value = false;
    }

    function updateConsoleManagedDirty() {
      consoleManagedDirty.value = buildConsoleManagedRuntimeSnapshot(state) !== loadedConsoleManagedSnapshot.value;
    }

    function updateConsoleTelegramDirty() {
      consoleTelegramDirty.value = buildConsoleTelegramSnapshot(state) !== loadedConsoleTelegramSnapshot.value;
    }

    function updateConsoleSlackDirty() {
      consoleSlackDirty.value = buildConsoleSlackSnapshot(state) !== loadedConsoleSlackSnapshot.value;
    }

    function updateConsoleGuardDirty() {
      consoleGuardDirty.value = buildConsoleGuardSnapshot(state) !== loadedConsoleGuardSnapshot.value;
    }

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
    const agentSettingsEndpointRef = computed(() => trimText(endpointState.selectedRef) || LOCAL_CONSOLE_ENDPOINT_REF);
    const personaSettingsEndpointRef = computed(() => trimText(endpointState.selectedRef) || LOCAL_CONSOLE_ENDPOINT_REF);
    const agentSettingsIsLocal = computed(() => agentSettingsEndpointRef.value === LOCAL_CONSOLE_ENDPOINT_REF);

    const settingsSections = computed(() => {
      const items = [
        {
          id: "persona",
          title: t("settings_persona_title"),
          meta: t("settings_section_persona_meta"),
          kickerLeft: "Agent",
          kickerRight: "Persona",
          saveKind: "persona",
        },
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
        {
          id: "skills",
          title: t("settings_skills_title"),
          meta: t("settings_section_skills_meta"),
          kickerLeft: "Agent",
          kickerRight: "Skills",
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
          if (agentSettingsReadOnly.value) {
            return t("settings_agent_llm_hint_read_only");
          }
          return t("settings_agent_llm_hint", { path: llmConfigPath.value || "config.yaml" });
        case "tools":
          return t("settings_tools_hint");
        case "skills":
          return t("settings_skills_hint");
        case "persona":
          return t("settings_persona_hint");
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
    const defaultIsCodexProvider = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_OPENAI_CODEX);
    const showCodexAuthCard = computed(() => {
      if (!agentSettingsIsLocal.value) {
        return false;
      }
      if (defaultIsCodexProvider.value) {
        return true;
      }
      return state.llm.profiles.some((profile) => effectiveProfileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX);
    });
    const codexAuthSummary = computed(() => {
      if (codexAuthLoading.value) {
        return t("settings_codex_auth_loading");
      }
      if (!codexAuthStatus.logged_in) {
        return t("settings_codex_auth_signed_out");
      }
      if (codexAuthStatus.access_token_expired && codexAuthStatus.refresh_token_present) {
        return t("settings_codex_auth_refreshable");
      }
      if (codexAuthStatus.access_token_expired) {
        return t("settings_codex_auth_expired");
      }
      return t("settings_codex_auth_signed_in");
    });
    const codexAuthButtonState = computed(() => {
      if (codexAuthLoading.value) {
        return "loading";
      }
      if (!codexAuthStatus.logged_in) {
        return "signed-out";
      }
      if (codexAuthStatus.access_token_expired && codexAuthStatus.refresh_token_present) {
        return "refreshable";
      }
      if (codexAuthStatus.access_token_expired) {
        return "expired";
      }
      return "signed-in";
    });
    const codexAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(codexAuthButtonState.value));
    const codexAuthButtonTitle = computed(() => `${t("settings_codex_auth_title")}: ${codexAuthSummary.value}`);
    const codexLoginExpiresLabel = computed(() =>
      codexLoginExpiresAt.value ? formatTime(codexLoginExpiresAt.value) : t("ttl_unknown")
    );
    const defaultShowCloudflareAccountField = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_CLOUDFLARE);
    const defaultShowBedrockFields = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_BEDROCK);
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
        (agentSettingsIsLocal.value && defaultIsCodexProvider.value && !codexAuthStatus.logged_in) ||
        (setupProviderRequiresAPIKey(defaultProviderChoice.value) &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, defaultCredentialFieldName.value)) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_aws_key")) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_aws_secret")) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_region")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_api_token")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_account_id"))
    );
    const currentTestTargetProfile = computed(() =>
      state.llm.profiles.find((item) => item._key === testConnectionTargetProfileKey.value) || null
    );
    const llmSaveDisabled = computed(
      () =>
        agentLoading.value ||
        agentSaving.value ||
        agentSettingsReadOnly.value ||
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "provider") ||
        !llmDirty.value ||
        (agentSettingsIsLocal.value && defaultIsCodexProvider.value && !codexAuthStatus.logged_in) ||
        (setupProviderRequiresAPIKey(defaultProviderChoice.value) &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, defaultCredentialFieldName.value)) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_aws_key")) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_aws_secret")) ||
        (defaultShowBedrockFields.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "bedrock_region")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_api_token")) ||
        (defaultShowCloudflareAccountField.value &&
          !hasLLMFieldValue(state.llm, llmEnvManaged.value, "cloudflare_account_id"))
    );
    const multimodalSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || agentSettingsReadOnly.value || !multimodalDirty.value
    );
    const skillsSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || agentSettingsReadOnly.value || !skillsDirty.value
    );
    const toolsSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || agentSettingsReadOnly.value || !toolsDirty.value
    );
    const consoleDirty = computed(
      () =>
        consoleManagedDirty.value ||
        consoleTelegramDirty.value ||
        consoleSlackDirty.value ||
        consoleGuardDirty.value
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
    const personaIdentityDirty = computed(() => buildPersonaIdentitySnapshot(state.persona) !== loadedIdentitySnapshot.value);
    const personaSoulDirty = computed(() => normalizeSoulDocument(soulContent.value) !== loadedSoulSnapshot.value);
    const personaDirty = computed(() => personaIdentityDirty.value || personaSoulDirty.value);
    const personaSaveDisabled = computed(() => personaLoading.value || personaSaving.value || !personaDirty.value);
    const personaAvatarDisabled = computed(() => personaLoading.value || personaSaving.value || personaAvatarBusy.value);
    const personaEditorMeta = computed(() =>
      t("settings_persona_soul_editor_meta", {
        lines: lineCount(soulContent.value),
        chars: soulContent.value.length,
      })
    );
    const desktopCheckDisabled = computed(() => desktopLoading.value || desktopChecking.value);
    const desktopDisplayedCurrentVersion = computed(
      () => trimText(desktopUpdateResult.value?.current_version) || trimText(desktopCurrentVersion.value) || "dev"
    );
    const desktopUpdateCheckHint = computed(() =>
      t("settings_desktop_update_check_hint", { version: desktopDisplayedCurrentVersion.value })
    );
    const desktopUpdateLatestVersionText = computed(() => trimText(desktopUpdateResult.value?.latest_version) || "-");
    const desktopUpdateReleaseNotes = computed(
      () => String(desktopUpdateResult.value?.release_notes || "").trim() || t("settings_desktop_update_changelog_empty")
    );
    const desktopUpdateChecksum = computed(() => trimText(desktopUpdateResult.value?.checksum));
    const desktopUpdateAssetURL = computed(() => trimText(desktopUpdateResult.value?.asset_url));
    const desktopUpdateDownloadDisabled = computed(() => {
      const result = desktopUpdateResult.value;
      const current = normalizeAppVersion(result?.current_version);
      const latest = normalizeAppVersion(result?.latest_version);
      if (!result || !desktopUpdateAssetURL.value || !current || current.toLowerCase() === "dev") {
        return true;
      }
      const comparison = compareAppVersions(current, latest);
      return comparison !== -1;
    });
    const skillsValidationError = computed(() => {
      const entries = parseSkillLoadText(state.skills.load_text);
      const seenRaw = new Set();
      const queryToID = new Map();
      for (const item of [...state.skills.loaded, ...state.skills.available]) {
        const id = trimText(item?.id);
        const name = trimText(item?.name);
        const canonical = id || name;
        if (!canonical) {
          continue;
        }
        if (id) {
          queryToID.set(id.toLowerCase(), canonical.toLowerCase());
        }
        if (name) {
          queryToID.set(name.toLowerCase(), canonical.toLowerCase());
        }
      }
      const hasWildcard = entries.includes("*");
      if (hasWildcard && entries.length > 1) {
        return t("settings_skills_load_error_wildcard");
      }
      const seenResolved = new Map();
      for (const entry of entries) {
        const key = entry.toLowerCase();
        if (seenRaw.has(key)) {
          return t("settings_skills_load_error_duplicate", { name: entry });
        }
        seenRaw.add(key);
        if (entry === "*") {
          continue;
        }
        const resolvedID = queryToID.get(key);
        if (!resolvedID) {
          return t("settings_skills_load_error_unknown", { name: entry });
        }
        if (seenResolved.has(resolvedID)) {
          return t("settings_skills_load_error_duplicate", { name: entry });
        }
        seenResolved.set(resolvedID, entry);
      }
      return "";
    });

    let agentSettingsRequestSeq = 0;
    let personaSettingsRequestSeq = 0;

    function resetAgentSettingsState() {
      Object.assign(state.llm, buildEmptyLLMForm());
      state.llm.profiles = [];
      state.llm.fallback_profiles = [];
      for (const item of MULTIMODAL_SOURCES) {
        state.multimodal[item.id] = false;
      }
      state.skills.enabled = true;
      state.skills.load_text = "";
      state.skills.loaded = [];
      state.skills.available = [];
      state.tools.write_file = true;
      state.tools.spawn = true;
      state.tools.contacts_send = true;
      state.tools.todo_update = true;
      state.tools.plan_create = true;
      state.tools.url_fetch = true;
      state.tools.web_search = true;
      state.tools.bash = true;
      state.tools.powershell = false;
      llmEnvManaged.value = {};
      agentSettingsReadOnly.value = !agentSettingsIsLocal.value;
      llmConfigPath.value = "";
      agentValidationVisible.value = false;
      skillsValidationVisible.value = false;
      clearLoadedAgentSnapshots();
    }

    function agentSettingsFetch(endpointRef, pathname, options = {}) {
      const ref = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      if (ref === LOCAL_CONSOLE_ENDPOINT_REF) {
        return apiFetch(pathname, options);
      }
      return runtimeApiFetchForEndpoint(ref, pathname, options);
    }

    function agentSettingsErrorMessage(err, endpointRef, fallbackKey) {
      if (trimText(endpointRef) !== LOCAL_CONSOLE_ENDPOINT_REF && err?.status === 404) {
        return t("settings_agent_endpoint_unsupported");
      }
      return err?.message || t(fallbackKey);
    }

    function isCurrentAgentSettingsRequest(seq, endpointRef) {
      return seq === agentSettingsRequestSeq && trimText(endpointRef) === agentSettingsEndpointRef.value;
    }

    function isCurrentPersonaSettingsRequest(seq, endpointRef) {
      return seq === personaSettingsRequestSeq && trimText(endpointRef) === personaSettingsEndpointRef.value;
    }

    function applyPayload(data, options = {}) {
      const snapshotScope = String(options?.snapshotScope || currentAgentSnapshotScope());
      const llm = data?.llm && typeof data.llm === "object" ? data.llm : {};
      const envManagedPayload = data?.env_managed && typeof data.env_managed === "object" ? data.env_managed : {};
      const llmEnvManagedPayload =
        envManagedPayload?.llm && typeof envManagedPayload.llm === "object" ? envManagedPayload.llm : {};
      const llmProfileEnvManagedPayload =
        envManagedPayload?.llm_profiles && typeof envManagedPayload.llm_profiles === "object"
          ? envManagedPayload.llm_profiles
          : {};
      const multimodal = data?.multimodal && typeof data.multimodal === "object" ? data.multimodal : {};
      const skills = data?.skills && typeof data.skills === "object" ? data.skills : {};
      const tools = data?.tools && typeof data.tools === "object" ? data.tools : {};
      const imageSources = Array.isArray(multimodal.image_sources) ? multimodal.image_sources : [];
      const profiles = Array.isArray(llm.profiles) ? llm.profiles : [];
      agentSettingsReadOnly.value = !agentSettingsIsLocal.value || data?.read_only === true;

      state.llm.provider = normalizeSetupProviderChoice(llm.provider, { allowEmpty: true });
      state.llm.endpoint = typeof llm.endpoint === "string" ? llm.endpoint : "";
      state.llm.model = typeof llm.model === "string" ? llm.model : "";
      state.llm.context_window_tokens = typeof llm.context_window_tokens === "string" ? llm.context_window_tokens : "";
      state.llm.api_key = typeof llm.api_key === "string" ? llm.api_key : "";
      state.llm.bedrock_aws_key = typeof llm.bedrock_aws_key === "string" ? llm.bedrock_aws_key : "";
      state.llm.bedrock_aws_secret = typeof llm.bedrock_aws_secret === "string" ? llm.bedrock_aws_secret : "";
      state.llm.bedrock_region = typeof llm.bedrock_region === "string" ? llm.bedrock_region : "";
      state.llm.bedrock_model_arn = typeof llm.bedrock_model_arn === "string" ? llm.bedrock_model_arn : "";
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
          context_window_tokens:
            typeof profile?.context_window_tokens === "string" ? profile.context_window_tokens : "",
          api_key: typeof profile?.api_key === "string" ? profile.api_key : "",
          bedrock_aws_key: typeof profile?.bedrock_aws_key === "string" ? profile.bedrock_aws_key : "",
          bedrock_aws_secret: typeof profile?.bedrock_aws_secret === "string" ? profile.bedrock_aws_secret : "",
          bedrock_region: typeof profile?.bedrock_region === "string" ? profile.bedrock_region : "",
          bedrock_model_arn: typeof profile?.bedrock_model_arn === "string" ? profile.bedrock_model_arn : "",
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
      applySkillsPayload(skills);
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
      agentSettingsLoaded.value = true;
      setLoadedAgentSnapshots(snapshotScope);
    }

    function applySkillsPayload(skills) {
      const payload = skills && typeof skills === "object" ? skills : {};
      state.skills.enabled = payload.enabled !== false;
      state.skills.load_text = formatSkillLoadList(payload.load);
      state.skills.loaded = normalizeSkillItems(payload.loaded);
      state.skills.available = normalizeSkillItems(payload.available);
      skillsValidationVisible.value = false;
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
      if (agentSettingsReadOnly.value) {
        return;
      }
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.llm, key)) {
        return;
      }
      const nextValue = String(value || "");
      if (state.llm[key] === nextValue) {
        return;
      }
      state.llm[key] = nextValue;
      updateLLMDirty();
    }

    function updateProfileField(profileKey, { field, value }) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const profile = state.llm.profiles.find((item) => item._key === profileKey);
      const key = String(field || "").trim();
      if (!profile || !key || !Object.prototype.hasOwnProperty.call(profile, key)) {
        return;
      }
      const nextValue = String(value || "");
      if (profile[key] === nextValue) {
        return;
      }
      const previousName = trimText(profile.name);
      profile[key] = nextValue;
      if (key !== "name") {
        updateLLMDirty();
        return;
      }
      const nextName = trimText(profile.name);
      if (previousName && previousName !== nextName) {
        state.llm.fallback_profiles = state.llm.fallback_profiles.map((item) =>
          trimText(item) === previousName ? nextName : item,
        );
      }
      updateLLMDirty();
    }

    function addLLMProfile() {
      if (agentSettingsReadOnly.value) {
        return;
      }
      state.llm.profiles.push(buildLLMProfileState());
      updateLLMDirty();
    }

    function confirmRemoveLLMProfile(profileKey) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      deleteProfileTargetKey.value = String(profileKey || "").trim();
      deleteProfileDialogOpen.value = deleteProfileTargetKey.value !== "";
    }

    function closeDeleteProfileDialog() {
      deleteProfileDialogOpen.value = false;
      deleteProfileTargetKey.value = "";
    }

    function removeLLMProfile(profileKey) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const index = state.llm.profiles.findIndex((item) => item._key === profileKey);
      if (index < 0) {
        return;
      }
      const [removed] = state.llm.profiles.splice(index, 1);
      const removedName = trimText(removed?.name);
      if (removedName) {
        state.llm.fallback_profiles = state.llm.fallback_profiles.filter((item) => trimText(item) !== removedName);
      }
      updateLLMDirty();
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
      if (agentSettingsReadOnly.value) {
        return;
      }
      const firstProfile = profileOptions.value[0]?.value || "";
      if (!firstProfile) {
        return;
      }
      state.llm.fallback_profiles.push(firstProfile);
      updateLLMDirty();
    }

    function updateFallbackProfile(index, item) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (index < 0 || index >= state.llm.fallback_profiles.length) {
        return;
      }
      state.llm.fallback_profiles[index] = trimText(item?.value);
      updateLLMDirty();
    }

    function removeFallbackProfile(index) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (index < 0 || index >= state.llm.fallback_profiles.length) {
        return;
      }
      state.llm.fallback_profiles.splice(index, 1);
      updateLLMDirty();
    }

    function moveFallbackProfile(index, delta) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const nextIndex = index + delta;
      if (index < 0 || index >= state.llm.fallback_profiles.length || nextIndex < 0 || nextIndex >= state.llm.fallback_profiles.length) {
        return;
      }
      const items = [...state.llm.fallback_profiles];
      const [current] = items.splice(index, 1);
      items.splice(nextIndex, 0, current);
      state.llm.fallback_profiles = items;
      updateLLMDirty();
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
        endpoint:
          effectiveProvider === SETUP_PROVIDER_OPENAI_CODEX || effectiveProvider === SETUP_PROVIDER_BEDROCK
            ? ""
            : llmFieldEnvRawValue(envManaged, "endpoint") || trimText(profile.endpoint),
        model: llmFieldEnvRawValue(envManaged, "model") || trimText(profile.model),
        context_window_tokens:
          llmFieldEnvRawValue(envManaged, "context_window_tokens") || trimText(profile.context_window_tokens),
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
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else if (effectiveProvider === SETUP_PROVIDER_BEDROCK) {
        payload.bedrock_aws_key =
          llmFieldEnvRawValue(envManaged, "bedrock_aws_key") || trimText(profile.bedrock_aws_key);
        payload.bedrock_aws_secret =
          llmFieldEnvRawValue(envManaged, "bedrock_aws_secret") || trimText(profile.bedrock_aws_secret);
        payload.bedrock_region =
          llmFieldEnvRawValue(envManaged, "bedrock_region") || trimText(profile.bedrock_region);
        payload.bedrock_model_arn =
          llmFieldEnvRawValue(envManaged, "bedrock_model_arn") || trimText(profile.bedrock_model_arn);
        payload.api_key = "";
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
      } else if (effectiveProvider === SETUP_PROVIDER_OPENAI_CODEX) {
        payload.api_key = "";
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else {
        payload.api_key = llmFieldEnvRawValue(envManaged, "api_key") || trimText(profile.api_key);
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
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
      if (provider === SETUP_PROVIDER_OPENAI_CODEX || provider === SETUP_PROVIDER_BEDROCK) {
        payload.endpoint = "";
      } else if (endpointRaw !== "") {
        payload.endpoint = endpointRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "endpoint")) {
        const endpoint = trimText(state.llm.endpoint);
        if (endpoint !== "" && provider !== SETUP_PROVIDER_BEDROCK) {
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
      const contextWindowRaw = llmFieldEnvRawValue(llmEnvManaged.value, "context_window_tokens");
      if (contextWindowRaw !== "") {
        payload.context_window_tokens = contextWindowRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "context_window_tokens")) {
        const contextWindowTokens = trimText(state.llm.context_window_tokens);
        if (contextWindowTokens !== "") {
          payload.context_window_tokens = contextWindowTokens;
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
      if (provider === SETUP_PROVIDER_BEDROCK) {
        const awsKeyRaw = llmFieldEnvRawValue(llmEnvManaged.value, "bedrock_aws_key");
        if (awsKeyRaw !== "") {
          payload.bedrock_aws_key = awsKeyRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_aws_key")) {
          const value = trimText(state.llm.bedrock_aws_key);
          if (value !== "") {
            payload.bedrock_aws_key = value;
          }
        }
        const awsSecretRaw = llmFieldEnvRawValue(llmEnvManaged.value, "bedrock_aws_secret");
        if (awsSecretRaw !== "") {
          payload.bedrock_aws_secret = awsSecretRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_aws_secret")) {
          const value = trimText(state.llm.bedrock_aws_secret);
          if (value !== "") {
            payload.bedrock_aws_secret = value;
          }
        }
        const regionRaw = llmFieldEnvRawValue(llmEnvManaged.value, "bedrock_region");
        if (regionRaw !== "") {
          payload.bedrock_region = regionRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_region")) {
          const value = trimText(state.llm.bedrock_region);
          if (value !== "") {
            payload.bedrock_region = value;
          }
        }
        const modelARNRaw = llmFieldEnvRawValue(llmEnvManaged.value, "bedrock_model_arn");
        if (modelARNRaw !== "") {
          payload.bedrock_model_arn = modelARNRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_model_arn")) {
          const value = trimText(state.llm.bedrock_model_arn);
          if (value !== "") {
            payload.bedrock_model_arn = value;
          }
        }
      } else if (provider === SETUP_PROVIDER_CLOUDFLARE) {
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
      } else if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        payload.api_key = "";
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
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

    async function loadAgentSettings(endpointRef = agentSettingsEndpointRef.value) {
      const requestSeq = ++agentSettingsRequestSeq;
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      agentLoading.value = true;
      agentSettingsReadOnly.value = targetEndpointRef !== LOCAL_CONSOLE_ENDPOINT_REF;
      agentErr.value = "";
      agentOk.value = "";
      try {
        const data = await agentSettingsFetch(targetEndpointRef, "/settings/agent");
        if (!isCurrentAgentSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        llmConfigPath.value = typeof data.config_path === "string" ? data.config_path : "";
        applyPayload(data);
      } catch (e) {
        if (!isCurrentAgentSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        agentErr.value = agentSettingsErrorMessage(e, targetEndpointRef, "msg_load_failed");
      } finally {
        if (isCurrentAgentSettingsRequest(requestSeq, targetEndpointRef)) {
          agentLoading.value = false;
        }
      }
    }

    function applyCodexAuthStatus(payload) {
      const status = payload && typeof payload.status === "object" ? payload.status : payload;
      codexAuthStatus.logged_in = status?.logged_in === true;
      codexAuthStatus.access_token_present = status?.access_token_present === true;
      codexAuthStatus.refresh_token_present = status?.refresh_token_present === true;
      codexAuthStatus.access_token_expired = status?.access_token_expired === true;
      codexAuthStatus.expires_at = typeof status?.expires_at === "string" ? status.expires_at : "";
      codexAuthStatus.account_id = typeof status?.account_id === "string" ? status.account_id : "";
      codexAuthStatus.file_mode_ok = status?.file_mode_ok !== false;
      codexAuthStatus.file_mode_warning = typeof status?.file_mode_warning === "string" ? status.file_mode_warning : "";
    }

    async function loadCodexAuthStatus() {
      if (codexAuthLoading.value) {
        return;
      }
      codexAuthLoading.value = true;
      codexAuthError.value = "";
      try {
        const payload = await apiFetch("/auth/codex/status");
        applyCodexAuthStatus(payload);
      } catch (e) {
        codexAuthError.value = e.message || t("msg_load_failed");
      } finally {
        codexAuthLoading.value = false;
      }
    }

    async function openCodexAuthDialog() {
      const shouldStartLogin = codexAuthNeedsLogin.value && !codexLoginSession.value && !codexAuthBusy.value;
      let authWindow = null;
      if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
        // Open synchronously from the click event so popup blockers allow the auth tab.
        authWindow = openExternalPlaceholder();
      }
      await openReentrantDialog(codexAuthDialogOpen);
      void loadCodexAuthStatus();
      if (shouldStartLogin) {
        void startCodexLogin(authWindow);
      }
    }

    function clearCodexLoginTimer() {
      if (codexLoginPollTimer) {
        clearTimeout(codexLoginPollTimer);
        codexLoginPollTimer = 0;
      }
    }

    function resetCodexLoginSession() {
      clearCodexLoginTimer();
      codexLoginSession.value = "";
      codexLoginVerificationURL.value = "";
      codexLoginUserCode.value = "";
      codexLoginExpiresAt.value = "";
    }

    function scheduleCodexLoginPoll(intervalSeconds = 5) {
      clearCodexLoginTimer();
      const delay = Math.max(2, Number(intervalSeconds) || 5) * 1000;
      codexLoginPollTimer = window.setTimeout(() => {
        void pollCodexLogin();
      }, delay);
    }

    async function startCodexLogin(authWindow = null) {
      if (codexAuthBusy.value) {
        if (authWindow && !authWindow.closed) {
          authWindow.close();
        }
        return;
      }
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      resetCodexLoginSession();
      let authWindowUsed = false;
      try {
        const payload = await apiFetch("/auth/codex/login/start", { method: "POST" });
        codexLoginSession.value = String(payload?.session_id || "").trim();
        codexLoginVerificationURL.value = String(payload?.verification_url || "").trim();
        codexLoginUserCode.value = String(payload?.user_code || "").trim();
        codexLoginExpiresAt.value = String(payload?.expires_at || "").trim();
        if (codexLoginVerificationURL.value) {
          if (authWindow && !authWindow.closed) {
            authWindow.location.href = codexLoginVerificationURL.value;
            authWindowUsed = true;
          } else {
            openExternalURL(codexLoginVerificationURL.value);
          }
        }
        scheduleCodexLoginPoll(payload?.interval_seconds);
      } catch (e) {
        codexAuthError.value = e.message || t("msg_load_failed");
      } finally {
        if (!authWindowUsed && authWindow && !authWindow.closed) {
          authWindow.close();
        }
        codexAuthBusy.value = false;
      }
    }

    async function pollCodexLogin() {
      const sessionID = codexLoginSession.value;
      if (!sessionID || codexAuthBusy.value) {
        return;
      }
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      try {
        const payload = await apiFetch("/auth/codex/login/poll", {
          method: "POST",
          body: { session_id: sessionID, set_default: true },
        });
        if (payload?.pending === true) {
          scheduleCodexLoginPoll(5);
          return;
        }
        applyCodexAuthStatus(payload);
        resetCodexLoginSession();
        if (payload?.settings_updated === true) {
          invalidateConsoleSetupReadiness();
          await loadAgentSettings(agentSettingsEndpointRef.value);
        }
      } catch (e) {
        codexAuthError.value = e.message || t("msg_load_failed");
      } finally {
        codexAuthBusy.value = false;
      }
    }

    async function logoutCodexAuth() {
      if (codexAuthBusy.value) {
        return;
      }
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      try {
        const payload = await apiFetch("/auth/codex/logout", { method: "POST" });
        applyCodexAuthStatus(payload);
        resetCodexLoginSession();
      } catch (e) {
        codexAuthError.value = e.message || t("msg_delete_failed");
      } finally {
        codexAuthBusy.value = false;
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
      consoleSettingsLoaded.value = true;
      setLoadedConsoleSnapshots();
    }

    function resetConsoleSettingsState() {
      state.managedRuntimes.telegram = false;
      state.managedRuntimes.slack = false;
      Object.assign(state.telegram, buildEmptyTelegramConsoleState());
      Object.assign(state.slack, buildEmptySlackConsoleState());
      Object.assign(state.guard, buildEmptyGuardConsoleState());
      consoleEnvManaged.value = {};
      consoleConfigPath.value = "";
      clearLoadedConsoleSnapshots();
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

    async function loadDesktopSettings() {
      desktopLoading.value = true;
      desktopErr.value = "";
      desktopOk.value = "";
      try {
        const data = await apiFetch("/settings/auto-update");
        desktopCurrentVersion.value = desktopRuntimeVersion() || trimText(data?.current_version) || "dev";
        desktopSettingsLoaded.value = true;
      } catch (e) {
        desktopErr.value = e.message || t("msg_load_failed");
      } finally {
        desktopLoading.value = false;
      }
    }

    function applyPersonaIdentityContent(raw) {
      loadedIdentityRaw.value = normalizeText(raw);
      Object.assign(state.persona, parseIdentityProfile(loadedIdentityRaw.value));
      loadedIdentitySnapshot.value = buildPersonaIdentitySnapshot(state.persona);
    }

    function applyPersonaSoulContent(raw) {
      const next = normalizeSoulDocument(raw);
      soulContent.value = next;
      loadedSoulSnapshot.value = next;
    }

    function updatePersonaSoulContent(value) {
      soulContent.value = String(value || "");
      personaOk.value = "";
    }

    function setPersonaAvatarObjectURL(nextURL) {
      if (personaAvatarObjectURL) {
        URL.revokeObjectURL(personaAvatarObjectURL);
      }
      personaAvatarObjectURL = nextURL || "";
      personaAvatarURL.value = personaAvatarObjectURL;
    }

    function resetPersonaSettingsState() {
      Object.assign(state.persona, buildEmptyPersonaIdentityState());
      soulContent.value = "";
      loadedIdentityRaw.value = "";
      loadedIdentitySnapshot.value = buildPersonaIdentitySnapshot(state.persona);
      loadedSoulSnapshot.value = "";
      personaSettingsLoaded.value = false;
      setPersonaAvatarObjectURL("");
    }

    async function loadPersonaFile(endpointRef, primaryEndpoint, fallbackEndpoint) {
      try {
        const payload = await runtimeApiFetchForEndpoint(endpointRef, primaryEndpoint);
        return String(payload?.content || "");
      } catch (e) {
        if (e?.status !== 404 || !fallbackEndpoint) {
          throw e;
        }
      }
      try {
        const payload = await runtimeApiFetchForEndpoint(endpointRef, fallbackEndpoint);
        return String(payload?.content || "");
      } catch (e) {
        if (e?.status === 404) {
          return "";
        }
        throw e;
      }
    }

    async function loadPersonaAvatar(endpointRef, requestSeq = personaSettingsRequestSeq) {
      try {
        const blob = await runtimeApiDownloadForEndpoint(endpointRef, PERSONA_AVATAR_ENDPOINT);
        const nextURL = URL.createObjectURL(blob);
        if (!isCurrentPersonaSettingsRequest(requestSeq, endpointRef)) {
          URL.revokeObjectURL(nextURL);
          return;
        }
        setPersonaAvatarObjectURL(nextURL);
      } catch (e) {
        if (!isCurrentPersonaSettingsRequest(requestSeq, endpointRef)) {
          return;
        }
        if (e?.status === 404) {
          setPersonaAvatarObjectURL("");
          return;
        }
        throw e;
      }
    }

    async function loadPersonaSettings(endpointRef = personaSettingsEndpointRef.value) {
      const requestSeq = ++personaSettingsRequestSeq;
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      personaLoading.value = true;
      personaErr.value = "";
      personaOk.value = "";
      try {
        const identityContent = await loadPersonaFile(
          targetEndpointRef,
          PERSONA_IDENTITY_ENDPOINT,
          LEGACY_IDENTITY_ENDPOINT
        );
        if (!isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        applyPersonaIdentityContent(identityContent);

        const soul = await loadPersonaFile(targetEndpointRef, PERSONA_SOUL_ENDPOINT, LEGACY_SOUL_ENDPOINT);
        if (!isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        applyPersonaSoulContent(soul);
        await loadPersonaAvatar(targetEndpointRef, requestSeq);
        if (!isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        personaSettingsLoaded.value = true;
      } catch (e) {
        if (!isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        personaErr.value = e.message || t("msg_load_failed");
        toast.error(personaErr.value);
      } finally {
        if (isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          personaLoading.value = false;
        }
      }
    }

    async function savePersona() {
      if (personaSaveDisabled.value) {
        return;
      }
      personaSaving.value = true;
      personaSavingTarget.value = "persona";
      personaErr.value = "";
      personaOk.value = "";
      const targetEndpointRef = personaSettingsEndpointRef.value;
      let setupReadinessDirty = false;
      try {
        if (personaIdentityDirty.value) {
          const content = buildIdentityYAML(state.persona, loadedIdentityRaw.value);
          await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_IDENTITY_ENDPOINT, {
            method: "PUT",
            body: { content },
          });
          if (targetEndpointRef !== personaSettingsEndpointRef.value) {
            return;
          }
          loadedIdentityRaw.value = content;
          loadedIdentitySnapshot.value = buildPersonaIdentitySnapshot(state.persona);
          dispatchPersonaIdentityUpdated();
          setupReadinessDirty = true;
        }
        if (personaSoulDirty.value) {
          const content = normalizeSoulDocument(soulContent.value);
          await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_SOUL_ENDPOINT, {
            method: "PUT",
            body: { content },
          });
          if (targetEndpointRef !== personaSettingsEndpointRef.value) {
            return;
          }
          soulContent.value = content;
          loadedSoulSnapshot.value = content;
          setupReadinessDirty = true;
        }
        if (setupReadinessDirty) {
          invalidateConsoleSetupReadiness();
        }
        personaOk.value = t("msg_save_success");
        toast.success(personaOk.value);
      } catch (e) {
        personaErr.value = e.message || t("msg_save_failed");
        toast.error(personaErr.value);
      } finally {
        personaSaving.value = false;
        personaSavingTarget.value = "";
      }
    }

    async function savePersonaAvatar(blob) {
      personaAvatarBusy.value = true;
      personaErr.value = "";
      personaOk.value = "";
      const targetEndpointRef = personaSettingsEndpointRef.value;
      try {
        await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_AVATAR_ENDPOINT, {
          method: "PUT",
          headers: { "Content-Type": "image/webp" },
          body: blob,
        });
        if (targetEndpointRef !== personaSettingsEndpointRef.value) {
          return;
        }
        await loadPersonaAvatar(targetEndpointRef);
        dispatchPersonaAvatarUpdated();
        personaOk.value = t("msg_save_success");
        toast.success(personaOk.value);
      } catch (e) {
        personaErr.value = e.message || t("msg_save_failed");
        toast.error(personaErr.value);
      } finally {
        personaAvatarBusy.value = false;
      }
    }

    async function deletePersonaAvatar() {
      personaAvatarBusy.value = true;
      personaErr.value = "";
      personaOk.value = "";
      const targetEndpointRef = personaSettingsEndpointRef.value;
      try {
        await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_AVATAR_ENDPOINT, {
          method: "DELETE",
        });
        if (targetEndpointRef !== personaSettingsEndpointRef.value) {
          return;
        }
        setPersonaAvatarObjectURL("");
        dispatchPersonaAvatarUpdated();
        personaOk.value = t("msg_delete_success");
        toast.success(personaOk.value);
      } catch (e) {
        personaErr.value = e.message || t("msg_delete_failed");
        toast.error(personaErr.value);
      } finally {
        personaAvatarBusy.value = false;
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
      if (target === "skills") {
        return { skills: { enabled: !!state.skills.enabled, load: parseSkillLoadText(state.skills.load_text) } };
      }
      if (target === "tools") {
        return { tools };
      }
      return {
        llm: buildLLMSettingsPayload(),
        multimodal,
        skills: { enabled: !!state.skills.enabled, load: parseSkillLoadText(state.skills.load_text) },
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
        payload.endpoint =
          provider === SETUP_PROVIDER_OPENAI_CODEX || provider === SETUP_PROVIDER_BEDROCK
            ? ""
            : trimText(state.llm.endpoint);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "model")) {
        payload.model = trimText(state.llm.model);
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "context_window_tokens")) {
        payload.context_window_tokens = trimText(state.llm.context_window_tokens);
      }
      if (provider === SETUP_PROVIDER_BEDROCK) {
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_aws_key")) {
          payload.bedrock_aws_key = trimText(state.llm.bedrock_aws_key);
        }
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_aws_secret")) {
          payload.bedrock_aws_secret = trimText(state.llm.bedrock_aws_secret);
        }
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_region")) {
          payload.bedrock_region = trimText(state.llm.bedrock_region);
        }
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "bedrock_model_arn")) {
          payload.bedrock_model_arn = trimText(state.llm.bedrock_model_arn);
        }
      } else if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_api_token")) {
          payload.cloudflare_api_token = trimText(state.llm.cloudflare_api_token);
        }
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "cloudflare_account_id")) {
          payload.cloudflare_account_id = trimText(state.llm.cloudflare_account_id);
        }
      } else if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        payload.api_key = "";
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
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

    function profileUsesCodexProvider(profile) {
      return agentSettingsIsLocal.value && effectiveProfileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX;
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
      if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        return agentSettingsIsLocal.value && !codexAuthStatus.logged_in;
      }
      if (provider === SETUP_PROVIDER_BEDROCK) {
        return (
          !hasEffectiveProfileFieldValue(profile, "bedrock_aws_key") ||
          !hasEffectiveProfileFieldValue(profile, "bedrock_aws_secret") ||
          !hasEffectiveProfileFieldValue(profile, "bedrock_region")
        );
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
      updateConsoleTelegramDirty();
    }

    function updateSlackField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.slack, key)) {
        return;
      }
      state.slack[key] = String(value || "");
      updateConsoleSlackDirty();
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
      updateConsoleGuardDirty();
    }

    async function saveAgentSettings(target = "all") {
      const normalizedTarget = ["all", "llm", "multimodal", "skills", "tools"].includes(String(target))
        ? String(target)
        : "all";
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (normalizedTarget === "llm" && llmSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "multimodal" && multimodalSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "skills" && skillsSaveDisabled.value) {
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
      if ((normalizedTarget === "skills" || normalizedTarget === "all") && skillsValidationError.value !== "") {
        agentNoticeTarget.value = normalizedTarget;
        skillsValidationVisible.value = true;
        agentErr.value = "";
        agentOk.value = "";
        return;
      }
      agentSaving.value = true;
      agentSavingTarget.value = normalizedTarget;
      agentNoticeTarget.value = normalizedTarget;
      agentValidationVisible.value = false;
      skillsValidationVisible.value = false;
      agentErr.value = "";
      agentOk.value = "";
      const targetEndpointRef = agentSettingsEndpointRef.value;
      try {
        const payload = await agentSettingsFetch(targetEndpointRef, "/settings/agent", {
          method: "PUT",
          body: buildSavePayload(normalizedTarget),
        });
        if (targetEndpointRef !== agentSettingsEndpointRef.value) {
          return;
        }
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        if (normalizedTarget === "llm" || normalizedTarget === "all") {
          if (targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF) {
            invalidateConsoleSetupReadiness();
          }
          const preservedMultimodal = JSON.parse(JSON.stringify(state.multimodal));
          const preservedSkills = JSON.parse(JSON.stringify(state.skills));
          const preservedTools = JSON.parse(JSON.stringify(state.tools));
          const previousMultimodalSnapshot = loadedMultimodalSnapshot.value;
          const previousSkillsSnapshot = loadedSkillsSnapshot.value;
          const previousToolsSnapshot = loadedToolsSnapshot.value;
          const previousMultimodalDirty = multimodalDirty.value;
          const previousSkillsDirty = skillsDirty.value;
          const previousToolsDirty = toolsDirty.value;
          applyPayload(payload, { snapshotScope: normalizedTarget === "llm" ? "llm" : "all" });
          if (normalizedTarget === "llm") {
            Object.assign(state.multimodal, preservedMultimodal);
            Object.assign(state.skills, preservedSkills);
            Object.assign(state.tools, preservedTools);
            loadedMultimodalSnapshot.value = previousMultimodalSnapshot;
            loadedSkillsSnapshot.value = previousSkillsSnapshot;
            loadedToolsSnapshot.value = previousToolsSnapshot;
            multimodalDirty.value = previousMultimodalDirty;
            skillsDirty.value = previousSkillsDirty;
            toolsDirty.value = previousToolsDirty;
          }
          await loadEndpoints();
        } else if (normalizedTarget === "multimodal") {
          loadedMultimodalSnapshot.value = buildMultimodalSnapshot(state);
          multimodalDirty.value = false;
        } else if (normalizedTarget === "skills") {
          applySkillsPayload(payload?.skills);
          loadedSkillsSnapshot.value = buildSkillsSnapshot(state);
          skillsDirty.value = false;
        } else if (normalizedTarget === "tools") {
          loadedToolsSnapshot.value = buildToolsSnapshot(state);
          toolsDirty.value = false;
        }
        agentOk.value = t("msg_save_success");
      } catch (e) {
        agentErr.value = agentSettingsErrorMessage(e, targetEndpointRef, "msg_save_failed");
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

    async function runDesktopUpdateCheck() {
      if (desktopCheckDisabled.value) {
        return;
      }
      desktopChecking.value = true;
      desktopErr.value = "";
      desktopOk.value = "";
      desktopChecksumCopied.value = false;
      try {
        desktopUpdateResult.value = desktopUpdateBindingAvailable.value
          ? await checkDesktopUpdate()
          : await apiFetch("/settings/auto-update/check", { method: "POST" });
        desktopCurrentVersion.value = trimText(desktopUpdateResult.value?.current_version) || desktopCurrentVersion.value;
        await nextTick();
        syncDesktopChangelogReadonly();
      } catch (e) {
        desktopErr.value = e.message || t("msg_load_failed");
      } finally {
        desktopChecking.value = false;
      }
    }

    function syncDesktopChangelogReadonly() {
      const root = desktopChangelogField.value?.$el || desktopChangelogField.value;
      const textarea = root?.querySelector?.("textarea");
      if (textarea) {
        textarea.readOnly = true;
      }
    }

    async function copyDesktopUpdateChecksum() {
      const checksum = desktopUpdateChecksum.value;
      if (!checksum) {
        return;
      }
      desktopErr.value = "";
      try {
        const copied = await copyTextToClipboard(checksum);
        if (copied) {
          desktopChecksumCopied.value = true;
          desktopOk.value = t("settings_desktop_update_checksum_copied");
          if (desktopChecksumCopyTimer) {
            window.clearTimeout(desktopChecksumCopyTimer);
          }
          desktopChecksumCopyTimer = window.setTimeout(() => {
            desktopChecksumCopied.value = false;
            desktopChecksumCopyTimer = 0;
          }, 1200);
        }
      } catch (e) {
        desktopErr.value = e.message || t("msg_save_failed");
      }
    }

    function openDesktopUpdateDownload() {
      if (desktopUpdateDownloadDisabled.value) {
        return;
      }
      openExternalURL(desktopUpdateAssetURL.value);
    }

    function openDesktopUpdateReleases() {
      openExternalURL(UPDATE_RELEASES_URL);
    }

    async function logout() {
      loggingOut.value = true;
      try {
        await apiFetch("/auth/logout", { method: "POST" });
      } catch {
        // ignore logout failure
      } finally {
        authState.clear();
        router.replace("/login");
        loggingOut.value = false;
      }
    }

    function openAPIBasePicker() {
      if (agentLoading.value || agentSaving.value || agentSettingsReadOnly.value) {
        return;
      }
      apiBasePickerOpen.value = true;
    }

    function applyAPIBaseOption(item) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const nextEndpoint = String(item?.value || "").trim();
      if (state.llm.endpoint === nextEndpoint) {
        return;
      }
      state.llm.endpoint = nextEndpoint;
      updateLLMDirty();
    }

    async function openModelPicker() {
      if (agentLoading.value || agentSaving.value) {
        return;
      }
      modelPickerOpen.value = true;
      modelPickerLoading.value = true;
      modelPickerError.value = "";
      modelPickerItems.value = [];
      const targetEndpointRef = agentSettingsEndpointRef.value;
      try {
        const payload = await agentSettingsFetch(targetEndpointRef, "/settings/agent/models", {
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
        modelPickerError.value = agentSettingsErrorMessage(e, targetEndpointRef, "msg_load_failed");
      } finally {
        modelPickerLoading.value = false;
      }
    }

    function applyModelOption(item) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const nextModel = String(item?.value || "").trim();
      if (state.llm.model === nextModel) {
        return;
      }
      state.llm.model = nextModel;
      updateLLMDirty();
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
      await openReentrantDialog(testConnectionOpen);
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
      const targetEndpointRef = agentSettingsEndpointRef.value;
      testConnectionLoading.value = true;
      try {
        const body = {
          llm: nextPayload,
        };
        if (targetProfileName !== "") {
          body.target_profile = targetProfileName;
        }
        const payload = await agentSettingsFetch(targetEndpointRef, "/settings/agent/test", {
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
        testConnectionError.value = agentSettingsErrorMessage(e, targetEndpointRef, "msg_load_failed");
      } finally {
        testConnectionLoading.value = false;
      }
    }

    function setMultimodalSource(id, value) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (!Object.prototype.hasOwnProperty.call(state.multimodal, id)) {
        return;
      }
      state.multimodal[id] = !!value;
      updateMultimodalDirty();
    }

    function setToolEnabled(id, value) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (!Object.prototype.hasOwnProperty.call(state.tools, id)) {
        return;
      }
      state.tools[id] = !!value;
      updateToolsDirty();
    }

    function setSkillsEnabled(value) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      state.skills.enabled = !!value;
      updateSkillsDirty();
    }

    function updateSkillsLoadText(value) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      state.skills.load_text = String(value || "");
      skillsValidationVisible.value = false;
      updateSkillsDirty();
    }

    function setManagedRuntimeEnabled(id, value) {
      if (!Object.prototype.hasOwnProperty.call(state.managedRuntimes, id)) {
        return;
      }
      state.managedRuntimes[id] = !!value;
      updateConsoleManagedDirty();
    }

    function refreshMobileMode() {
      isMobile.value = typeof window !== "undefined" && window.innerWidth <= 920;
    }

    function showIndexView() {
      mobilePanelVisible.value = false;
    }

    function openCreditsPage() {
      router.push("/settings/credits");
    }

    function openLogsPage() {
      router.push("/logs");
    }

    function selectSection(id) {
      const sectionID = normalizeSettingsSectionID(id);
      selectedSectionID.value = sectionID;
      if (isMobile.value) {
        mobilePanelVisible.value = true;
      }
      const nextPath = settingsSectionPath(sectionID);
      if (route.path !== nextPath) {
        router.push(nextPath);
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

    function ensureSettingsSectionData(sectionID = selectedSectionID.value) {
      const normalizedSectionID = normalizeSettingsSectionID(sectionID);
      if (["agent", "tools", "skills"].includes(normalizedSectionID)) {
        if (!agentSettingsLoaded.value && !agentLoading.value) {
          void loadAgentSettings(agentSettingsEndpointRef.value);
          return;
        }
        ensureLoadedAgentSnapshotsForSection(normalizedSectionID);
        return;
      }
      if (normalizedSectionID === "persona") {
        if (!personaSettingsLoaded.value && !personaLoading.value) {
          void loadPersonaSettings(personaSettingsEndpointRef.value);
        }
        return;
      }
      if (["channels", "runtimes", "guard"].includes(normalizedSectionID)) {
        if (showConsoleManagedSettings.value && !consoleSettingsLoaded.value && !consoleLoading.value) {
          void loadConsoleSettings();
        }
        return;
      }
      if (normalizedSectionID === "console") {
        desktopUpdateBindingAvailable.value = canCheckDesktopUpdate();
        if (!desktopSettingsLoaded.value && !desktopLoading.value) {
          void loadDesktopSettings();
        }
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      if (isMobile.value && settingsRouteSection(route)) {
        mobilePanelVisible.value = true;
      }
      ensureSettingsSectionData(selectedSectionID.value);
    });

    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
      clearCodexLoginTimer();
      if (desktopChecksumCopyTimer) {
        window.clearTimeout(desktopChecksumCopyTimer);
        desktopChecksumCopyTimer = 0;
      }
      setPersonaAvatarObjectURL("");
    });

    watch(
      () => route.params.section,
      () => {
        const routeSection = settingsRouteSection(route);
        const sectionID = normalizeSettingsSectionID(routeSection);
        selectedSectionID.value = sectionID;
        ensureSettingsSectionData(sectionID);
        if (routeSection && routeSection !== sectionID) {
          router.replace(settingsSectionPath(sectionID));
        }
        if (isMobile.value && routeSection) {
          mobilePanelVisible.value = true;
        }
      },
      { immediate: true }
    );

    watch(
      settingsSections,
      (items) => {
        if (!items.some((item) => item.id === selectedSectionID.value)) {
          const sectionID = items[0]?.id || SETTINGS_DEFAULT_SECTION_ID;
          selectedSectionID.value = sectionID;
          const nextPath = settingsSectionPath(sectionID);
          if (route.path !== nextPath) {
            router.replace(nextPath);
          }
          ensureSettingsSectionData(sectionID);
        }
      },
      { immediate: true }
    );

    watch(
      () => endpointState.selectedRef,
      (next, previous) => {
        if (trimText(next) === trimText(previous)) {
          return;
        }
        agentSettingsRequestSeq += 1;
        resetAgentSettingsState();
        agentErr.value = "";
        agentOk.value = "";
        personaSettingsRequestSeq += 1;
        resetPersonaSettingsState();
        personaErr.value = "";
        personaOk.value = "";
        resetConsoleSettingsState();
        consoleErr.value = "";
        consoleOk.value = "";
        modelPickerOpen.value = false;
        modelPickerError.value = "";
        testConnectionOpen.value = false;
        testConnectionError.value = "";
        ensureSettingsSectionData(selectedSectionID.value);
      }
    );

    watch(
      showConsoleManagedSettings,
      (enabled) => {
        consoleErr.value = "";
        consoleOk.value = "";
        if (enabled) {
          ensureSettingsSectionData(selectedSectionID.value);
          return;
        }
        if (["runtimes", "channels", "guard"].includes(selectedSectionID.value)) {
          selectedSectionID.value = "console";
          ensureSettingsSectionData("console");
        }
      },
      { immediate: true }
    );

    watch(deleteProfileDialogOpen, (open) => {
      if (!open) {
        deleteProfileTargetKey.value = "";
      }
    });

    watch(codexAuthDialogOpen, (open) => {
      if (!open) {
        resetCodexLoginSession();
        codexAuthError.value = "";
      }
    });

    watch(desktopUpdateReleaseNotes, () => {
      void nextTick(syncDesktopChangelogReadonly);
    });

    watch(
      showCodexAuthCard,
      (visible) => {
        if (visible) {
          void loadCodexAuthStatus();
        } else {
          resetCodexLoginSession();
          codexAuthError.value = "";
        }
      },
      { immediate: false }
    );

    return {
      t,
      lang,
      loggingOut,
      agentLoading,
      agentSaving,
      agentSavingTarget,
      agentSettingsReadOnly,
      agentNoticeTarget,
      agentErr,
      agentOk,
      agentValidationVisible,
      skillsValidationVisible,
      deleteProfileDialogOpen,
      consoleLoading,
      consoleSaving,
      consoleSavingTarget,
      consoleNoticeTarget,
      consoleErr,
      consoleOk,
      personaLoading,
      personaSaving,
      personaSavingTarget,
      personaErr,
      personaOk,
      soulContent,
      personaAvatarURL,
      personaAvatarBusy,
      personaAvatarDisabled,
      personaAvatarSourceTypes,
      defaultAvatarMarkup,
      PERSONA_AVATAR_MAX_SOURCE_BYTES,
      PERSONA_AVATAR_SIZE,
      desktopUpdateBindingAvailable,
      desktopLoading,
      desktopChecking,
      desktopErr,
      desktopOk,
      desktopChecksumCopied,
      desktopChangelogField,
      llmConfigPath,
      agentSettingsIsLocal,
      consoleConfigPath,
      desktopUpdateResult,
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
      skillsValidationError,
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
      skillsSaveDisabled,
      toolsSaveDisabled,
      consoleSaveDisabled,
      telegramSaveDisabled,
      slackSaveDisabled,
      guardSaveDisabled,
      personaDirty,
      personaSaveDisabled,
      personaEditorMeta,
      desktopCheckDisabled,
      desktopUpdateCheckHint,
      desktopUpdateLatestVersionText,
      desktopUpdateReleaseNotes,
      desktopUpdateChecksum,
      desktopUpdateDownloadDisabled,
      testConnectionDisabled,
      testConnectionDisabledForProfile,
      showCodexAuthCard,
      codexAuthLoading,
      codexAuthBusy,
      codexAuthError,
      codexAuthDialogOpen,
      codexAuthStatus,
      codexAuthSummary,
      codexAuthButtonState,
      codexAuthButtonTitle,
      codexLoginSession,
      codexLoginVerificationURL,
      codexLoginUserCode,
      codexLoginExpiresLabel,
      pollCodexLogin,
      logoutCodexAuth,
      loadCodexAuthStatus,
      openCodexAuthDialog,
      logout,
      saveAgentSettings,
      saveConsoleSettings,
      savePersona,
      savePersonaAvatar,
      deletePersonaAvatar,
      updatePersonaSoulContent,
      runDesktopUpdateCheck,
      copyDesktopUpdateChecksum,
      openDesktopUpdateDownload,
      openDesktopUpdateReleases,
      updateDefaultLLMField,
      updateProfileField,
      llmProfileEnvManaged,
      profileUsesCodexProvider,
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
      setSkillsEnabled,
      updateSkillsLoadText,
      formatSkillCount,
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
      openCreditsPage,
      openLogsPage,
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
      onLanguageChange: localeState.applyLanguageChange,
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
            <button type="button" class="settings-index-link workspace-sidebar-item" @click="openCreditsPage">
              <span class="workspace-sidebar-item-copy">
                <span class="workspace-sidebar-item-title">{{ t("settings_credits_title") }}</span>
                <span class="workspace-sidebar-item-meta">{{ t("settings_credits_meta") }}</span>
              </span>
              <span class="workspace-sidebar-item-marker">
                <QIconLinkExternal class="icon" />
              </span>
            </button>
          </div>
        </aside>

        <div v-if="showPanelPane && selectedSection" class="settings-panel-scroll">
          <div v-if="selectedSection.id === 'agent'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Agent" right="LLM Config" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_agent_block_title") }}</h3>
                    <p class="settings-panel-meta">{{ panelHint }}</p>
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
                        :readOnly="agentSettingsReadOnly"
                        :envManaged="llmEnvManaged"
                        :defaultProvider="profileBaseProvider"
                        :providerItems="defaultProviderItems"
                        :reasoningEffortItems="reasoningEffortItems"
                        :toolsEmulationItems="toolsEmulationItems"
                        :enableAPIBasePicker="true"
                        :enableModelPicker="true"
                        :showTestAction="true"
                        :testActionDisabled="testConnectionDisabled"
                        :showCodexAuthAction="agentSettingsIsLocal"
                        :codexAuthState="codexAuthButtonState"
                        :codexAuthTitle="codexAuthButtonTitle"
                        @update-field="updateDefaultLLMField"
                        @open-api-base-picker="openAPIBasePicker"
                        @open-model-picker="openModelPicker"
                        @open-test="openTestConnection"
                        @open-codex-auth="openCodexAuthDialog"
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
                                  :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                                  @update:modelValue="updateProfileField(profile._key, { field: 'name', value: $event })"
                                />
                                <QButton
                                  type="button"
                                  class="danger icon settings-profile-delete"
                                  :title="t('action_delete')"
                                  :aria-label="t('action_delete')"
                                  :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
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
                            :readOnly="agentSettingsReadOnly"
                            :envManaged="llmProfileEnvManaged(profile)"
                            :defaultProvider="profileBaseProvider"
                            :providerItems="profileProviderItems"
                            :reasoningEffortItems="profileReasoningEffortItems"
                            :toolsEmulationItems="profileToolsEmulationItems"
                            :providerPlaceholderKey="'settings_agent_provider_inherit'"
                            :allowProviderInherit="true"
                            :showTestAction="true"
                            :testActionDisabled="testConnectionDisabledForProfile(profile)"
                            :showCodexAuthAction="profileUsesCodexProvider(profile)"
                            :codexAuthState="codexAuthButtonState"
                            :codexAuthTitle="codexAuthButtonTitle"
                            @update-field="updateProfileField(profile._key, $event)"
                            @open-test="openTestConnection(profile._key)"
                            @open-codex-auth="openCodexAuthDialog"
                          />
                        </article>

                        <QButton
                          type="button"
                          class="placeholder settings-profile-placeholder"
                          :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
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
                            :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                            @change="updateFallbackProfile(index, $event)"
                          />
                          <div class="settings-fallback-actions">
                            <QButton
                              type="button"
                              class="outlined icon settings-fallback-action"
                              :title="t('settings_agent_order_up')"
                              :aria-label="t('settings_agent_order_up')"
                              :disabled="agentLoading || agentSaving || agentSettingsReadOnly || index === 0"
                              @click="moveFallbackProfile(index, -1)"
                            >
                              <QIconChevronUp class="icon" />
                            </QButton>
                            <QButton
                              type="button"
                              class="outlined icon settings-fallback-action"
                              :title="t('settings_agent_order_down')"
                              :aria-label="t('settings_agent_order_down')"
                              :disabled="agentLoading || agentSaving || agentSettingsReadOnly || index === state.llm.fallback_profiles.length - 1"
                              @click="moveFallbackProfile(index, 1)"
                            >
                              <QIconChevronDown class="icon" />
                            </QButton>
                            <QButton
                              type="button"
                              class="danger icon settings-fallback-action"
                              :title="t('action_delete')"
                              :aria-label="t('action_delete')"
                              :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                              @click="removeFallbackProfile(index)"
                            >
                              <QIconTrash class="icon" />
                            </QButton>
                          </div>
                        </div>

                        <QButton
                          type="button"
                          class="placeholder settings-profile-placeholder"
                          :disabled="agentLoading || agentSaving || agentSettingsReadOnly || !profileOptions.length"
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
                        :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
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

          <div v-else-if="selectedSection.id === 'skills'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Agent" right="Skills" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_skills_title") }}</h3>
                    <p class="settings-panel-meta">{{ panelHint }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="agentSaving && agentSavingTarget === 'skills'"
                      :disabled="skillsSaveDisabled"
                      @click="saveAgentSettings('skills')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-notices">
                  <QFence
                    v-if="agentErr && (agentNoticeTarget === '' || agentNoticeTarget === 'skills')"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="agentErr"
                  />
                  <QFence
                    v-if="skillsValidationVisible && !agentErr && skillsValidationError"
                    type="danger"
                    icon="QIconCloseCircle"
                    :text="skillsValidationError"
                  />
                  <QFence
                    v-if="agentOk && agentNoticeTarget === 'skills'"
                    type="success"
                    icon="QIconCheckCircle"
                    :text="agentOk"
                  />
                </div>

                <div class="settings-panel-body">
                  <div class="settings-toggle-list">
                    <div class="settings-toggle-row">
                      <div class="settings-toggle-copy">
                        <strong class="settings-toggle-title">{{ t("settings_skills_enabled_title") }}</strong>
                        <span class="settings-toggle-note">{{ t("settings_skills_enabled_note") }}</span>
                      </div>
                      <QSwitch
                        :modelValue="state.skills.enabled"
                        :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                        @update:modelValue="setSkillsEnabled"
                      />
                    </div>
                  </div>

                  <div class="settings-form-grid">
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_skills_load_label") }}</span>
                      <QTextarea
                        :modelValue="state.skills.load_text"
                        :rows="4"
                        :placeholder="t('settings_skills_load_placeholder')"
                        :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                        @update:modelValue="updateSkillsLoadText"
                      />
                      <p class="settings-field-note">{{ t("settings_skills_load_note") }}</p>
                    </label>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-skill-list-shell">
                <header class="settings-skill-list-head">
                  <AppKicker
                    as="h3"
                    class="settings-skill-list-kicker"
                    :left="t('settings_skills_loaded_title')"
                    :right="formatSkillCount(state.skills.loaded.length)"
                  />
                </header>
                <p v-if="!state.skills.loaded.length" class="settings-skill-empty">{{ t("settings_skills_loaded_empty") }}</p>
                <div v-else class="settings-skill-grid">
                  <article v-for="skill in state.skills.loaded" :key="'loaded-' + (skill.id || skill.name)" class="settings-skill-card">
                    <div class="settings-skill-card-head">
                      <strong class="settings-skill-card-title">{{ skill.name || skill.id }}</strong>
                      <code v-if="skill.id && skill.id !== skill.name" class="settings-skill-card-id">{{ skill.id }}</code>
                    </div>
                    <p class="settings-skill-card-desc">{{ skill.description || t("settings_skills_description_empty") }}</p>
                  </article>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-skill-list-shell">
                <header class="settings-skill-list-head">
                  <AppKicker
                    as="h3"
                    class="settings-skill-list-kicker"
                    :left="t('settings_skills_available_title')"
                    :right="formatSkillCount(state.skills.available.length)"
                  />
                </header>
                <p v-if="!state.skills.available.length" class="settings-skill-empty">{{ t("settings_skills_available_empty") }}</p>
                <div v-else class="settings-skill-grid">
                  <article v-for="skill in state.skills.available" :key="'available-' + (skill.id || skill.name)" class="settings-skill-card">
                    <div class="settings-skill-card-head">
                      <strong class="settings-skill-card-title">{{ skill.name || skill.id }}</strong>
                      <code v-if="skill.id && skill.id !== skill.name" class="settings-skill-card-id">{{ skill.id }}</code>
                    </div>
                    <p class="settings-skill-card-desc">{{ skill.description || t("settings_skills_description_empty") }}</p>
                  </article>
                </div>
              </div>
            </QCard>
          </div>

          <div v-else-if="selectedSection.id === 'persona'" class="settings-panel-body settings-panel-body-plain">
            <div v-if="personaLoading" class="settings-panel-notices">
              <QProgress v-if="personaLoading" :infinite="true" />
            </div>

            <QCard variant="default">
              <div class="settings-panel-shell settings-persona-card">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Agent" right="Persona" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_persona_title") }}</h3>
                    <p class="settings-panel-meta">{{ panelHint }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="personaSaving && personaSavingTarget === 'persona'"
                      :disabled="personaSaveDisabled"
                      @click="savePersona"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-body">
                  <div class="settings-form-grid settings-persona-form">
                    <div class="settings-field is-wide settings-persona-avatar-field">
                      <span class="settings-field-label">{{ t("settings_persona_avatar_title") }}</span>
                      <ImageUploadField
                        :previewUrl="personaAvatarURL"
                        :defaultMarkup="defaultAvatarMarkup"
                        :disabled="personaAvatarDisabled"
                        :busy="personaAvatarBusy"
                        :crop="true"
                        :outputSize="PERSONA_AVATAR_SIZE"
                        outputType="image/webp"
                        :outputQuality="0.9"
                        :accept="'image/png,image/jpeg,image/webp'"
                        :allowedTypes="personaAvatarSourceTypes"
                        :maxBytes="PERSONA_AVATAR_MAX_SOURCE_BYTES"
                        :dialogTitle="t('settings_persona_avatar_title')"
                        @save="savePersonaAvatar"
                        @delete="deletePersonaAvatar"
                      />
                    </div>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_persona_identity_name_label") }}</span>
                      <QInput
                        v-model="state.persona.name"
                        :placeholder="t('settings_persona_identity_name_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </label>

                    <label class="settings-field">
                      <span class="settings-field-label">{{ t("settings_persona_identity_emoji_label") }}</span>
                      <QInput
                        v-model="state.persona.emoji"
                        :placeholder="t('settings_persona_identity_emoji_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </label>

                    <label class="settings-field">
                      <span class="settings-field-label">{{ t("settings_persona_identity_creature_label") }}</span>
                      <QInput
                        v-model="state.persona.creature"
                        :placeholder="t('settings_persona_identity_creature_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_persona_identity_vibe_label") }}</span>
                      <QTextarea
                        v-model="state.persona.vibe"
                        :rows="4"
                        :placeholder="t('settings_persona_identity_vibe_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </label>

                    <div class="settings-field is-wide settings-persona-soul-field">
                      <div class="settings-persona-soul-label">
                        <span class="settings-field-label">{{ t("settings_persona_soul_title") }}</span>
                        <span class="settings-panel-meta">{{ personaEditorMeta }}</span>
                      </div>
                      <div class="settings-persona-soul-editor">
                        <MarkdownEditor
                          :modelValue="soulContent"
                          height="460px"
                          :disabled="personaLoading || personaSaving"
                          :placeholder="t('settings_persona_soul_placeholder')"
                          :aria-label="t('settings_persona_soul_title')"
                          @update:modelValue="updatePersonaSoulContent"
                        />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </QCard>
          </div>

          <div v-else-if="selectedSection.id === 'console'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" :left="selectedSection.kickerLeft" :right="selectedSection.kickerRight" />
                    <h3 class="settings-panel-title workspace-document-title">{{ selectedSection.title }}</h3>
                    <p class="settings-panel-meta">{{ panelHint }}</p>
                  </div>
                </header>

                <div class="settings-panel-body">
                  <div class="settings-console-list">
                    <div class="settings-console-row">
                      <div class="settings-card-copy">
                        <h4 class="settings-card-title">{{ t("settings_language_title") }}</h4>
                        <p class="settings-card-note">{{ t("settings_language_hint") }}</p>
                      </div>
                      <QLanguageSelector class="settings-console-control" :lang="lang" :presist="true" @change="onLanguageChange" />
                    </div>
                    <div class="settings-console-row">
                      <div class="settings-card-copy">
                        <h4 class="settings-card-title">{{ t("settings_logs_title") }}</h4>
                        <p class="settings-card-note">{{ t("settings_logs_hint") }}</p>
                      </div>
                      <QButton class="outlined settings-console-control settings-console-action" @click="openLogsPage">
                        <QIconCode class="icon settings-console-action-icon" />
                        {{ t("settings_logs_open") }}
                      </QButton>
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

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <AppKicker as="p" left="Console" :right="t('settings_desktop_update_check_title')" />
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_auto_update_card_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_auto_update_card_hint") }}</p>
                  </div>
                </header>

                <div class="settings-panel-body">
                  <div v-if="desktopErr || desktopOk" class="settings-panel-notices">
                    <QFence
                      v-if="desktopErr"
                      type="danger"
                      icon="QIconCloseCircle"
                      :text="desktopErr"
                    />
                    <QFence
                      v-if="desktopOk"
                      type="success"
                      icon="QIconCheckCircle"
                      :text="desktopOk"
                    />
                  </div>

                  <div class="settings-console-list">
                    <div class="settings-console-row settings-console-row--desktop-update">
                      <div class="settings-card-copy">
                        <h4 class="settings-card-title">{{ t("settings_desktop_update_check_title") }}</h4>
                        <p class="settings-card-note">{{ desktopUpdateCheckHint }}</p>
                      </div>
                      <QButton
                        class="outlined sm icon settings-desktop-update-check-button"
                        :loading="desktopChecking"
                        :disabled="desktopCheckDisabled"
                        :title="t('settings_desktop_update_check_action')"
                        :aria-label="t('settings_desktop_update_check_action')"
                        @click="runDesktopUpdateCheck"
                      >
                        <QIconRefresh class="icon settings-console-action-icon" />
                      </QButton>
                    </div>
                  </div>

                  <div v-if="desktopUpdateResult" class="settings-desktop-update-result">
                    <div class="settings-desktop-update-result-head">
                      <span class="settings-desktop-update-label">{{ t("settings_desktop_update_latest_version") }}</span>
                      <strong class="settings-desktop-update-version">{{ desktopUpdateLatestVersionText }}</strong>
                    </div>

                    <div class="settings-desktop-update-checksum-row">
                      <span class="settings-desktop-update-label">{{ t("settings_desktop_update_checksum_label") }}</span>
                      <button
                        type="button"
                        class="settings-desktop-update-checksum-button"
                        :disabled="!desktopUpdateChecksum"
                        :title="t('settings_desktop_update_checksum_copy_title')"
                        :aria-label="t('settings_desktop_update_checksum_copy_title')"
                        @click="copyDesktopUpdateChecksum"
                      >
                        <code>{{ desktopUpdateChecksum || "-" }}</code>
                        <QIconCheckCircle v-if="desktopChecksumCopied" class="icon settings-desktop-update-checksum-icon" />
                        <QIconCopy v-else class="icon settings-desktop-update-checksum-icon" />
                      </button>
                    </div>

                    <label class="settings-desktop-update-changelog-field">
                      <span class="settings-desktop-update-label">{{ t("settings_desktop_update_changelog_label") }}</span>
                      <QTextarea
                        ref="desktopChangelogField"
                        class="settings-desktop-update-changelog"
                        :modelValue="desktopUpdateReleaseNotes"
                        :rows="8"
                      />
                    </label>

                    <div class="settings-desktop-update-result-actions">
                      <QButton
                        class="plain sm settings-desktop-update-result-action"
                        @click="openDesktopUpdateReleases"
                      >
                        <QIconArrowUpRight class="icon settings-console-action-icon" />
                        {{ t("settings_desktop_update_view_releases_action") }}
                      </QButton>
                      <QButton
                        class="outlined sm settings-desktop-update-result-action"
                        :disabled="desktopUpdateDownloadDisabled"
                        @click="openDesktopUpdateDownload"
                      >
                        <QIconDownloadCloud class="icon settings-console-action-icon" />
                        {{ t("settings_desktop_update_download_action") }}
                      </QButton>
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
                      :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
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
              </div>
            </div>
          </QCard>
        </div>
      </div>

      <SetupPickerDialog
        v-model="apiBasePickerOpen"
        :items="apiBasePickerItems"
        :loading="false"
        :error="''"
        :title="t('setup_llm_api_base_picker_title')"
        :filterPlaceholder="t('setup_llm_api_base_picker_filter_placeholder')"
        :emptyText="t('setup_llm_api_base_picker_empty')"
        @select="applyAPIBaseOption"
      />

      <SetupPickerDialog
        v-model="modelPickerOpen"
        :items="modelPickerItems"
        :loading="modelPickerLoading"
        :error="modelPickerError"
        :title="t('setup_llm_model_picker_title')"
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
      <CodexAuthDialog
        v-model="codexAuthDialogOpen"
        :loading="codexAuthLoading"
        :busy="codexAuthBusy"
        :error="codexAuthError"
        :status="codexAuthStatus"
        :summary="codexAuthSummary"
        :loginSession="codexLoginSession"
        :verificationURL="codexLoginVerificationURL"
        :userCode="codexLoginUserCode"
        :loginExpiresLabel="codexLoginExpiresLabel"
        @logout="logoutCodexAuth"
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
