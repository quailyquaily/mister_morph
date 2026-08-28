import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useToast } from "quail-ui";
import "./SettingsView.css";

import AppPage from "../components/AppPage";
import CodexAuthDialog from "../components/CodexAuthDialog";
import XAIAuthDialog from "../components/XAIAuthDialog";
import ProAuthDialog from "../components/ProAuthDialog";
import ImageUploadField from "../components/ImageUploadField";
import LLMConfigForm from "../components/LLMConfigForm";
import AppMarkdownEditor from "../components/AppMarkdownEditor";
import SettingsCreditsPanel from "../components/SettingsCreditsPanel";
import SetupConnectionTestDialog from "../components/SetupConnectionTestDialog";
import SetupPickerDialog from "../components/SetupPickerDialog";
import RuntimePanel from "./RuntimeView";
import defaultAvatarMarkup from "../assets/images/app_logo_current.svg?raw";
import {
  apiFetch,
  authState,
  endpointApiFetch,
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
import useProAuthFlow from "../composables/useProAuthFlow";
import useXAIAuthFlow from "../composables/useXAIAuthFlow";
import {
  canCheckDesktopUpdate,
  checkDesktopUpdate,
  desktopRuntimeVersion,
} from "../core/desktop-runtime";
import { recordSnapshotBuild } from "../core/performance";
import {
  OPENAI_COMPATIBLE_API_BASE_OPTIONS,
  normalizeSetupProviderChoice,
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_MISTERMORPH_PRO,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_XAI_OAUTH,
  SETUP_PROVIDER_OPTIONS,
  setupProviderRequiresAPIKey,
  setupProviderSupportsCustomAPIBase,
  setupOpenAICodexUsesAPIKey,
} from "../core/setup-contract";
import { invalidateConsoleSetupReadiness } from "../core/setup";
import { endpointRoutePath } from "../core/endpoint-routes";
import { openReentrantDialog } from "../core/reentrant-dialog";
import {
  buildEmptyPersonaIdentityState,
  buildIdentityYAML,
  buildPersonaIdentitySnapshot,
  dispatchPersonaAvatarUpdated,
  dispatchPersonaIdentityUpdated,
  normalizeSoulDocument,
  parseIdentityProfile,
  PERSONA_AVATAR_ENDPOINT,
  PERSONA_AVATAR_MAX_SOURCE_BYTES,
  PERSONA_AVATAR_SIZE,
  PERSONA_AVATAR_SOURCE_TYPES,
  PERSONA_IDENTITY_ENDPOINT,
  PERSONA_SOUL_ENDPOINT,
} from "../core/persona-profile";

const TOOL_ITEMS = [
  { id: "write_file", titleKey: "settings_tool_write_file", noteKey: "settings_tool_note_write_file" },
  { id: "spawn", titleKey: "settings_tool_spawn", noteKey: "settings_tool_note_spawn" },
  { id: "coder", titleKey: "settings_tool_coder", noteKey: "settings_tool_note_coder" },
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
  { id: "lark", titleKey: "settings_console_runtime_lark", noteKey: "settings_console_runtime_note_lark" },
];

const CHANNEL_GROUP_TRIGGER_VALUES = ["smart", "strict", "talkative"];
const LOCAL_CONSOLE_ENDPOINT_REF = "ep_console_local";
const SETTINGS_DEFAULT_SECTION_ID = "persona";
const SETTINGS_SECTION_IDS = new Set([
  "agent",
  "tools",
  "skills",
  "persona",
  "channels",
  "runtimes",
  "guard",
  "console",
  "runtime",
  "credits",
]);
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

function settingsSectionPath(endpointRef, id) {
  const sectionID = normalizeSettingsSectionID(id);
  const pagePath =
    sectionID === SETTINGS_DEFAULT_SECTION_ID ? "/settings" : `/settings/${sectionID}`;
  return endpointRoutePath(endpointRef, pagePath);
}

function buildEmptyLLMForm() {
  return {
    inference_provider: "",
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

function buildEmptyLineConsoleState() {
  return {
    channel_access_token: "",
    channel_secret: "",
    allowed_group_ids_text: "",
    group_trigger_mode: "smart",
  };
}

function buildEmptyLarkConsoleState() {
  return {
    app_id: "",
    app_secret: "",
    allowed_chat_ids_text: "",
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
  const profile = {
    _key: nextLLMProfileKey(),
    _envManaged: {},
    _savedName: "",
    _savedSnapshot: "",
    name: "",
    ...buildEmptyLLMForm(),
    ...(data && typeof data === "object" ? data : {}),
  };
  profile._savedName = trimText(profile.name);
  profile._savedSnapshot = JSON.stringify(serializeLLMProfile(profile));
  return profile;
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

function skillLoadEntry(skill) {
  return trimText(skill?.id) || trimText(skill?.name);
}

function skillLoadEntryMatches(skill, entry) {
  const key = trimText(entry).toLowerCase();
  if (!key) {
    return false;
  }
  return trimText(skill?.id).toLowerCase() === key || trimText(skill?.name).toLowerCase() === key;
}

function serializeLLMProfile(profile) {
  return {
    name: trimText(profile?.name),
    inference_provider: trimText(profile?.inference_provider),
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
      inference_provider: trimText(state.llm.inference_provider),
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
      fallback_profiles: normalizeNamedList(state.llm.fallback_profiles),
    },
  });
}

function buildToolsSnapshot(state) {
  recordSnapshotBuild("settings.tools");
  return JSON.stringify({
    tools: {
      write_file: !!state.tools.write_file,
      spawn: !!state.tools.spawn,
      coder: !!state.tools.coder,
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
    lark: !!state.managedRuntimes.lark,
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

function buildConsoleLineSnapshot(state) {
  recordSnapshotBuild("settings.console.line");
  return JSON.stringify({
    channel_access_token: trimText(state.line.channel_access_token),
    channel_secret: trimText(state.line.channel_secret),
    allowed_group_ids: parseConfigListText(state.line.allowed_group_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.line.group_trigger_mode),
  });
}

function buildConsoleLarkSnapshot(state) {
  recordSnapshotBuild("settings.console.lark");
  return JSON.stringify({
    app_id: trimText(state.lark.app_id),
    app_secret: trimText(state.lark.app_secret),
    allowed_chat_ids: parseConfigListText(state.lark.allowed_chat_ids_text),
    group_trigger_mode: normalizeConsoleGroupTriggerMode(state.lark.group_trigger_mode),
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
    AppPage,
    CodexAuthDialog,
    XAIAuthDialog,
    ProAuthDialog,
    ImageUploadField,
    LLMConfigForm,
    AppMarkdownEditor,
    SettingsCreditsPanel,
    SetupConnectionTestDialog,
    SetupPickerDialog,
    RuntimePanel,
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
    const agentSettingsReadOnlyReason = ref("");
    const agentSettingsReadOnlyMessage = computed(
      () => trimText(agentSettingsReadOnlyReason.value) || t("settings_agent_llm_hint_read_only")
    );
    const agentBusyReason = computed(() => {
      if (agentLoading.value) {
        return "agentLoading";
      }
      if (!agentSaving.value) {
        return "";
      }
      const target = trimText(agentSavingTarget.value);
      return target ? `agentSaving:${target}` : "agentSaving";
    });
    const agentFormDisabledReason = computed(() =>
      agentSettingsReadOnly.value ? agentSettingsReadOnlyMessage.value : agentBusyReason.value
    );
    const agentValidationVisible = ref(false);
    const skillsValidationVisible = ref(false);
    const deleteProfileDialogOpen = ref(false);
    const deleteProfileTargetKey = ref("");
    const llmConfigPath = ref("");
    const loadedLLMSnapshot = ref("");
    const loadedSkillsSnapshot = ref("");
    const loadedToolsSnapshot = ref("");
    const llmDirty = ref(false);
    const skillsDirty = ref(false);
    const toolsDirty = ref(false);
    const agentSettingsLoaded = ref(false);
    const llmEnvManaged = ref({});
    const consoleLoading = ref(false);
    const consoleSaving = ref(false);
    const consoleSavingTarget = ref("");
    const consoleConfigPath = ref("");
    const loadedConsoleManagedSnapshot = ref("");
    const loadedConsoleTelegramSnapshot = ref("");
    const loadedConsoleSlackSnapshot = ref("");
    const loadedConsoleLineSnapshot = ref("");
    const loadedConsoleLarkSnapshot = ref("");
    const loadedConsoleGuardSnapshot = ref("");
    const consoleManagedDirty = ref(false);
    const consoleTelegramDirty = ref(false);
    const consoleSlackDirty = ref(false);
    const consoleLineDirty = ref(false);
    const consoleLarkDirty = ref(false);
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
    const desktopLoading = ref(false);
    const desktopChecking = ref(false);
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
    const modelPickerTargetProfileKey = ref("");
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
    const codexLoginEndpointRef = ref("");
    const codexLoginVerificationURL = ref("");
    const codexLoginUserCode = ref("");
    const codexLoginExpiresAt = ref("");
    let codexLoginPollTimer = 0;
    let codexAuthStatusRequestSeq = 0;
    let codexAuthOperationSeq = 0;
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
        current_profile: "",
        profiles: [],
        fallback_profiles: [],
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
        coder: false,
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
        lark: false,
      },
      telegram: buildEmptyTelegramConsoleState(),
      slack: buildEmptySlackConsoleState(),
      line: buildEmptyLineConsoleState(),
      lark: buildEmptyLarkConsoleState(),
      guard: buildEmptyGuardConsoleState(),
    });

    function clearLoadedAgentSnapshots() {
      loadedLLMSnapshot.value = "";
      loadedSkillsSnapshot.value = "";
      loadedToolsSnapshot.value = "";
      llmDirty.value = false;
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
      } else if (scope === "tools" && !loadedToolsSnapshot.value) {
        setLoadedAgentSnapshots("tools");
      } else if (scope === "skills" && !loadedSkillsSnapshot.value) {
        setLoadedAgentSnapshots("skills");
      }
    }

    function updateLLMDirty() {
      llmDirty.value = buildLLMSnapshot(state) !== loadedLLMSnapshot.value;
    }

    function updateLoadedFallbackProfile(originalName, nextName) {
      const original = trimText(originalName);
      if (!original || !loadedLLMSnapshot.value) {
        return;
      }
      try {
        const snapshot = JSON.parse(loadedLLMSnapshot.value);
        const values = Array.isArray(snapshot?.llm?.fallback_profiles)
          ? snapshot.llm.fallback_profiles
          : [];
        snapshot.llm.fallback_profiles = nextName
          ? values.map((value) => trimText(value).toLowerCase() === original.toLowerCase() ? trimText(nextName) : value)
          : values.filter((value) => trimText(value).toLowerCase() !== original.toLowerCase());
        loadedLLMSnapshot.value = JSON.stringify(snapshot);
        updateLLMDirty();
      } catch {
        // A missing snapshot only affects the disabled state of the page-level save button.
      }
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
      loadedConsoleLineSnapshot.value = buildConsoleLineSnapshot(state);
      loadedConsoleLarkSnapshot.value = buildConsoleLarkSnapshot(state);
      loadedConsoleGuardSnapshot.value = buildConsoleGuardSnapshot(state);
      consoleManagedDirty.value = false;
      consoleTelegramDirty.value = false;
      consoleSlackDirty.value = false;
      consoleLineDirty.value = false;
      consoleLarkDirty.value = false;
      consoleGuardDirty.value = false;
    }

    function clearLoadedConsoleSnapshots() {
      loadedConsoleManagedSnapshot.value = "";
      loadedConsoleTelegramSnapshot.value = "";
      loadedConsoleSlackSnapshot.value = "";
      loadedConsoleLineSnapshot.value = "";
      loadedConsoleLarkSnapshot.value = "";
      loadedConsoleGuardSnapshot.value = "";
      consoleManagedDirty.value = false;
      consoleTelegramDirty.value = false;
      consoleSlackDirty.value = false;
      consoleLineDirty.value = false;
      consoleLarkDirty.value = false;
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

    function updateConsoleLineDirty() {
      consoleLineDirty.value = buildConsoleLineSnapshot(state) !== loadedConsoleLineSnapshot.value;
    }

    function updateConsoleLarkDirty() {
      consoleLarkDirty.value = buildConsoleLarkSnapshot(state) !== loadedConsoleLarkSnapshot.value;
    }

    function updateConsoleGuardDirty() {
      consoleGuardDirty.value = buildConsoleGuardSnapshot(state) !== loadedConsoleGuardSnapshot.value;
    }

    const providerItems = SETUP_PROVIDER_OPTIONS;
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
    const toolsEmulationItems = computed(() => [
      { title: t("settings_llm_tools_emulation_off"), value: "off" },
      { title: t("settings_llm_tools_emulation_fallback"), value: "fallback" },
      { title: t("settings_llm_tools_emulation_force"), value: "force" },
    ]);
    const toolItems = computed(() => TOOL_ITEMS);
    const managedRuntimeItems = computed(() => MANAGED_RUNTIME_ITEMS);
    const groupTriggerItems = computed(() => [
      { title: t("settings_console_group_trigger_smart"), value: "smart" },
      { title: t("settings_console_group_trigger_strict"), value: "strict" },
      { title: t("settings_console_group_trigger_talkative"), value: "talkative" },
    ]);
    const settingsEndpointRef = computed(() => trimText(endpointState.selectedRef) || LOCAL_CONSOLE_ENDPOINT_REF);
    const selectedEndpointIsConsole = computed(
      () =>
        settingsEndpointRef.value === LOCAL_CONSOLE_ENDPOINT_REF ||
        trimText(runtimeEndpointByRef(settingsEndpointRef.value)?.mode).toLowerCase() === "console"
    );
    const consoleEndpointRef = computed(() =>
      selectedEndpointIsConsole.value ? settingsEndpointRef.value : LOCAL_CONSOLE_ENDPOINT_REF
    );
    const {
      proAuthLoading,
      proAuthBusy,
      proAuthError,
      proAuthDialogOpen,
      proAuthStatus,
      proAuthSummary,
      proAuthButtonState,
      proAuthButtonTitle,
      proLoginSession,
      proLoginVerificationURL,
      proLoginUserCode,
      proLoginExpiresLabel,
      loadProAuthStatus,
      openProAuthDialog,
      pollProLogin,
      logoutProAuth,
      resetProAuthFlow,
      resetProAuthEndpointState,
    } = useProAuthFlow({
      getEndpointRef: () => settingsEndpointRef.value,
      request: endpointApiFetch,
      async onSettingsUpdated(_payload, endpointRef) {
        await loadAgentSettings(endpointRef);
      },
    });
    const {
      xaiAuthLoading,
      xaiAuthBusy,
      xaiAuthError,
      xaiAuthDialogOpen,
      xaiSetDefault,
      xaiAuthStatus,
      xaiAuthSummary,
      xaiAuthButtonState,
      xaiAuthReady,
      xaiAuthButtonTitle,
      xaiLoginSession,
      xaiLoginVerificationURL,
      xaiLoginUserCode,
      xaiLoginExpiresLabel,
      loadXAIAuthStatus,
      openXAIAuthDialog,
      reloginXAIAuth,
      pollXAILogin,
      logoutXAIAuth,
      resetXAIAuthFlow,
      resetXAIAuthEndpointState,
    } = useXAIAuthFlow({
      getEndpointRef: () => settingsEndpointRef.value,
      request: endpointApiFetch,
      async onSettingsUpdated(_payload, endpointRef) {
        await loadAgentSettings(endpointRef);
      },
    });

    const settingsSections = computed(() => {
      const items = [
        {
          id: "persona",
          title: t("settings_persona_title"),
          meta: t("settings_section_persona_meta"),
          saveKind: "persona",
        },
        {
          id: "agent",
          title: t("settings_agent_block_title"),
          meta: t("settings_section_agent_meta"),
          saveKind: "agent",
        },
        {
          id: "tools",
          title: t("settings_tools_title"),
          meta: t("settings_section_tools_meta"),
          saveKind: "agent",
        },
        {
          id: "skills",
          title: t("settings_skills_title"),
          meta: t("settings_section_skills_meta"),
          saveKind: "agent",
        },
      ];
      if (selectedEndpointIsConsole.value) {
        items.push({
          id: "channels",
          title: t("settings_console_channels_title"),
          saveKind: "console",
        });
        items.push({
          id: "runtimes",
          title: t("settings_console_runtime_title"),
          meta: t("settings_section_runtimes_meta"),
          saveKind: "console",
        });
        items.push({
          id: "guard",
          title: t("settings_console_guard_title"),
          meta: t("settings_section_guard_meta"),
          saveKind: "console",
        });
      }
      items.push({
        id: "console",
        title: t("settings_console_title"),
        meta: t("settings_section_console_meta"),
        saveKind: "",
      });
      items.push({
        id: "runtime",
        title: t("runtime_title"),
        saveKind: "",
      });
      items.push({
        id: "credits",
        title: t("settings_credits_title"),
        saveKind: "",
      });
      return items;
    });

    const selectedSection = computed(
      () => settingsSections.value.find((item) => item.id === selectedSectionID.value) || settingsSections.value[0] || null
    );
    const activeSaveKind = computed(() => String(selectedSection.value?.saveKind || ""));
    const showIndexPane = computed(() => !isMobile.value || !mobilePanelVisible.value);
    const showPanelPane = computed(() => !isMobile.value || mobilePanelVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobilePanelVisible.value);
    const mobileBarTitle = computed(() =>
      mobileShowBack.value ? selectedSection.value?.title || t("settings_title") : t("settings_title")
    );
    const pageClass = computed(() => (isMobile.value ? "settings-page settings-page-mobile-split" : "settings-page"));
    const defaultProviderChoice = computed(() =>
      normalizeSetupProviderChoice(
        llmFieldValue(state.llm, llmEnvManaged.value, "inference_provider") ||
          llmFieldValue(state.llm, llmEnvManaged.value, "provider"),
        { allowEmpty: true },
      )
    );
    const defaultIsCodexProvider = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_OPENAI_CODEX);
    const defaultCodexUsesAPIKey = computed(() =>
      setupOpenAICodexUsesAPIKey(
        llmFieldValue(state.llm, llmEnvManaged.value, "endpoint"),
        hasLLMFieldValue(state.llm, llmEnvManaged.value, "api_key"),
      )
    );
    const defaultCodexAuthDisabled = computed(
      () =>
        trimText(llmFieldValue(state.llm, llmEnvManaged.value, "endpoint")) !== "" &&
        hasLLMFieldValue(state.llm, llmEnvManaged.value, "api_key"),
    );
    const defaultIsXAIProvider = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_XAI_OAUTH);
    const defaultIsProProvider = computed(() => defaultProviderChoice.value === SETUP_PROVIDER_MISTERMORPH_PRO);
    const codexOAuthInUse = computed(
      () =>
        (defaultIsCodexProvider.value && !defaultCodexUsesAPIKey.value) ||
        state.llm.profiles.some(
          (profile) =>
            profileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX &&
            !profileUsesCodexAPIKey(profile),
        ),
    );
    const showCodexAuthCard = computed(() => {
      if (defaultIsCodexProvider.value) {
        return true;
      }
      return state.llm.profiles.some((profile) => profileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX);
    });
    const showProAuthCard = computed(() => {
      if (!selectedEndpointIsConsole.value) {
        return false;
      }
      if (defaultIsProProvider.value) {
        return true;
      }
      return state.llm.profiles.some((profile) => profileProviderChoice(profile) === SETUP_PROVIDER_MISTERMORPH_PRO);
    });
    const showXAIAuthCard = computed(() => {
      if (!selectedEndpointIsConsole.value) {
        return false;
      }
      if (defaultIsXAIProvider.value) {
        return true;
      }
      return state.llm.profiles.some((profile) => profileProviderChoice(profile) === SETUP_PROVIDER_XAI_OAUTH);
    });
    const codexAuthSummary = computed(() => {
      if (codexAuthLoading.value) {
        return t("settings_codex_auth_loading");
      }
      return codexAuthStatus.logged_in
        ? t("settings_codex_auth_signed_in")
        : t("settings_codex_auth_signed_out");
    });
    const codexAuthButtonState = computed(() => {
      if (codexAuthLoading.value) {
        return "loading";
      }
      return codexAuthStatus.logged_in ? "signed-in" : "signed-out";
    });
    const codexAuthNeedsLogin = computed(() => codexAuthButtonState.value === "signed-out");
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
    function profileValidationError(profile) {
      const name = trimText(profile?.name);
      if (!name) {
        return t("settings_agent_profile_name_required");
      }
      if (name.toLowerCase() === "default") {
        return t("settings_agent_profile_name_reserved");
      }
      const matches = state.llm.profiles.filter(
        (item) => item._key !== profile?._key && trimText(item?.name).toLowerCase() === name.toLowerCase(),
      );
      if (matches.length > 0) {
        return t("settings_agent_profile_name_duplicate", { name });
      }
      if (profileProviderChoice(profile) === "") {
        return t("settings_agent_profile_provider_required");
      }
      return "";
    }
    function profileDirty(profile) {
      return JSON.stringify(serializeLLMProfile(profile)) !== String(profile?._savedSnapshot || "");
    }
    function profileIsInUse(profile) {
      const currentProfile = trimText(state.llm.current_profile);
      const savedProfileName = trimText(profile?._savedName);
      return currentProfile !== "" && savedProfileName !== "" && savedProfileName === currentProfile;
    }
    const agentValidationError = computed(() => {
      if (
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "inference_provider") &&
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "provider")
      ) {
        return "";
      }
      const seen = new Set();
      for (const profile of state.llm.profiles) {
        const savedName = trimText(profile?._savedName);
        if (savedName) {
          seen.add(savedName.toLowerCase());
        }
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
        defaultProviderChoice.value === "" ||
        !hasLLMFieldValue(state.llm, llmEnvManaged.value, "model") ||
        (defaultIsCodexProvider.value && !defaultCodexUsesAPIKey.value && !codexAuthStatus.logged_in) ||
        (selectedEndpointIsConsole.value && defaultIsXAIProvider.value && !xaiAuthReady.value) ||
        (selectedEndpointIsConsole.value && defaultIsProProvider.value && !proAuthStatus.logged_in) ||
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
        defaultProviderChoice.value === "" ||
        !llmDirty.value ||
        (defaultIsCodexProvider.value && !defaultCodexUsesAPIKey.value && !codexAuthStatus.logged_in) ||
        (selectedEndpointIsConsole.value && defaultIsXAIProvider.value && !xaiAuthReady.value) ||
        (selectedEndpointIsConsole.value && defaultIsProProvider.value && !proAuthStatus.logged_in) ||
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
    function profileSaveDisabled(profile) {
      const provider = profileProviderChoice(profile);
      return (
        agentLoading.value ||
        agentSaving.value ||
        agentSettingsReadOnly.value ||
        !profileDirty(profile) ||
        profileValidationError(profile) !== "" ||
        (provider === SETUP_PROVIDER_OPENAI_CODEX &&
          !profileUsesCodexAPIKey(profile) &&
          !codexAuthStatus.logged_in) ||
        (selectedEndpointIsConsole.value && provider === SETUP_PROVIDER_XAI_OAUTH && !xaiAuthReady.value) ||
        (selectedEndpointIsConsole.value && provider === SETUP_PROVIDER_MISTERMORPH_PRO && !proAuthStatus.logged_in)
      );
    }
    const skillsSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || agentSettingsReadOnly.value || !skillsDirty.value
    );
    const allSkillItems = computed(() => {
      const items = [];
      const seen = new Set();
      for (const item of [...state.skills.loaded, ...state.skills.available]) {
        const entry = skillLoadEntry(item);
        if (!entry) {
          continue;
        }
        const key = entry.toLowerCase();
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        items.push(item);
      }
      return items;
    });
    const currentSkillLoadEntries = computed(() => parseSkillLoadText(state.skills.load_text));
    const displayedLoadedSkills = computed(() => {
      const entries = currentSkillLoadEntries.value;
      if (!entries.length || (entries.length === 1 && entries[0] === "*")) {
        return allSkillItems.value;
      }
      return allSkillItems.value.filter((skill) => entries.some((entry) => skillLoadEntryMatches(skill, entry)));
    });
    const displayedAvailableSkills = computed(() => {
      const loaded = new Set(displayedLoadedSkills.value.map((skill) => skillLoadEntry(skill).toLowerCase()));
      return allSkillItems.value.filter((skill) => !loaded.has(skillLoadEntry(skill).toLowerCase()));
    });
    const toolsSaveDisabled = computed(
      () => agentLoading.value || agentSaving.value || agentSettingsReadOnly.value || !toolsDirty.value
    );
    const consoleDirty = computed(
      () =>
        consoleManagedDirty.value ||
        consoleTelegramDirty.value ||
        consoleSlackDirty.value ||
        consoleLineDirty.value ||
        consoleLarkDirty.value ||
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
    const lineSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleLineDirty.value
    );
    const larkSaveDisabled = computed(
      () => consoleLoading.value || consoleSaving.value || !consoleLarkDirty.value
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
    let consoleSettingsRequestSeq = 0;
    let desktopSettingsRequestSeq = 0;

    function resetAgentSettingsState() {
      Object.assign(state.llm, buildEmptyLLMForm());
      state.llm.current_profile = "";
      state.llm.profiles = [];
      state.llm.fallback_profiles = [];
      state.skills.enabled = true;
      state.skills.load_text = "";
      state.skills.loaded = [];
      state.skills.available = [];
      state.tools.write_file = true;
      state.tools.spawn = true;
      state.tools.coder = false;
      state.tools.contacts_send = true;
      state.tools.todo_update = true;
      state.tools.plan_create = true;
      state.tools.url_fetch = true;
      state.tools.web_search = true;
      state.tools.bash = true;
      state.tools.powershell = false;
      llmEnvManaged.value = {};
      agentSettingsReadOnly.value = false;
      agentSettingsReadOnlyReason.value = "";
      llmConfigPath.value = "";
      agentValidationVisible.value = false;
      skillsValidationVisible.value = false;
      clearLoadedAgentSnapshots();
    }

    function agentSettingsErrorMessage(err, endpointRef, fallbackKey) {
      if (trimText(endpointRef) !== LOCAL_CONSOLE_ENDPOINT_REF && err?.status === 404) {
        return t("settings_agent_endpoint_unsupported");
      }
      return err?.message || t(fallbackKey);
    }

    function isCurrentAgentSettingsRequest(seq, endpointRef) {
      return seq === agentSettingsRequestSeq && trimText(endpointRef) === settingsEndpointRef.value;
    }

    function isCurrentPersonaSettingsRequest(seq, endpointRef) {
      return seq === personaSettingsRequestSeq && trimText(endpointRef) === settingsEndpointRef.value;
    }

    function isCurrentConsoleSettingsRequest(seq, endpointRef) {
      return seq === consoleSettingsRequestSeq && trimText(endpointRef) === settingsEndpointRef.value;
    }

    function isCurrentDesktopSettingsRequest(seq, endpointRef) {
      return seq === desktopSettingsRequestSeq && trimText(endpointRef) === consoleEndpointRef.value;
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
      const skills = data?.skills && typeof data.skills === "object" ? data.skills : {};
      const tools = data?.tools && typeof data.tools === "object" ? data.tools : {};
      const profiles = Array.isArray(llm.profiles) ? llm.profiles : [];
      agentSettingsReadOnly.value = data?.read_only === true;
      agentSettingsReadOnlyReason.value = agentSettingsReadOnly.value ? trimText(data?.read_only_reason) : "";

      state.llm.inference_provider = normalizeSetupProviderChoice(llm.inference_provider || llm.provider, { allowEmpty: true });
      state.llm.provider = typeof llm.provider === "string" ? llm.provider : "";
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
      state.llm.current_profile = typeof llm.current_profile === "string" ? llm.current_profile : "";
      state.llm.profiles = profiles.map((profile) =>
        buildLLMProfileState({
          name: trimText(profile?.name),
          _envManaged:
            llmProfileEnvManagedPayload?.[trimText(profile?.name)] &&
            typeof llmProfileEnvManagedPayload[trimText(profile?.name)] === "object"
              ? llmProfileEnvManagedPayload[trimText(profile?.name)]
              : {},
          inference_provider: normalizeSetupProviderChoice(profile?.inference_provider || profile?.provider, { allowEmpty: true }),
          provider: typeof profile?.provider === "string" ? profile.provider : "",
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
      applySkillsPayload(skills);
      state.tools.write_file = toolEnabledValue(tools.write_file);
      state.tools.spawn = toolEnabledValue(tools.spawn);
      state.tools.coder = toolEnabledValue(tools.coder);
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
      profile[key] = nextValue;
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
      const savedName = trimText(removed?._savedName);
      const removedNames = new Set([removedName, savedName].filter(Boolean).map((name) => name.toLowerCase()));
      state.llm.fallback_profiles = state.llm.fallback_profiles.filter((item) => {
        const name = trimText(item);
        return name !== "" && !removedNames.has(name.toLowerCase());
      });
      updateLLMDirty();
    }

    async function deleteLLMProfile() {
      const profileKey = deleteProfileTargetKey.value;
      const profile = state.llm.profiles.find((item) => item._key === profileKey) || null;
      closeDeleteProfileDialog();
      if (!profile) {
        return;
      }
      const savedName = trimText(profile._savedName);
      if (!savedName) {
        removeLLMProfile(profileKey);
        return;
      }
      if (agentLoading.value || agentSaving.value || agentSettingsReadOnly.value) {
        return;
      }
      agentSaving.value = true;
      agentSavingTarget.value = `profile:${profileKey}`;
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/agent", {
          method: "PUT",
          body: { llm: { delete_profile: savedName } },
        });
        if (targetEndpointRef !== settingsEndpointRef.value) {
          return;
        }
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        removeLLMProfile(profileKey);
        updateLoadedFallbackProfile(savedName, "");
        if (targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF) {
          invalidateConsoleSetupReadiness();
        }
        await loadEndpoints();
        toast.success(t("msg_delete_success"));
      } catch (e) {
        toast.error(agentSettingsErrorMessage(e, targetEndpointRef, "msg_delete_failed"));
      } finally {
        agentSaving.value = false;
        agentSavingTarget.value = "";
      }
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
      const provider = normalizeSetupProviderChoice(
        llmFieldValue(profile, envManaged, "inference_provider") || llmFieldValue(profile, envManaged, "provider"),
        { allowEmpty: true },
      );
      const inferenceProviderRaw = llmFieldEnvRawValue(envManaged, "inference_provider");
      const providerRaw = llmFieldEnvRawValue(envManaged, "provider");
      const payload = {
        name: trimText(profile.name),
        inference_provider: providerRaw === "" ? inferenceProviderRaw || trimText(profile.inference_provider) : inferenceProviderRaw,
        provider: providerRaw,
        endpoint:
          setupProviderSupportsCustomAPIBase(provider)
            ? llmFieldEnvRawValue(envManaged, "endpoint") || trimText(profile.endpoint)
            : "",
        model: llmFieldEnvRawValue(envManaged, "model") || trimText(profile.model),
        context_window_tokens:
          llmFieldEnvRawValue(envManaged, "context_window_tokens") || trimText(profile.context_window_tokens),
        reasoning_effort:
          llmFieldEnvRawValue(envManaged, "reasoning_effort") || trimText(profile.reasoning_effort),
        tools_emulation_mode:
          llmFieldEnvRawValue(envManaged, "tools_emulation_mode") || trimText(profile.tools_emulation_mode),
      };
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        payload.cloudflare_api_token =
          llmFieldEnvRawValue(envManaged, "cloudflare_api_token") || trimText(profile.cloudflare_api_token);
        payload.cloudflare_account_id =
          llmFieldEnvRawValue(envManaged, "cloudflare_account_id") || trimText(profile.cloudflare_account_id);
        payload.api_key = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else if (provider === SETUP_PROVIDER_BEDROCK) {
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
      } else if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        payload.api_key = llmFieldEnvRawValue(envManaged, "api_key") || trimText(profile.api_key);
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else if (
        provider === SETUP_PROVIDER_XAI_OAUTH ||
        provider === SETUP_PROVIDER_MISTERMORPH_PRO
      ) {
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
      const provider = normalizeSetupProviderChoice(
        llmFieldValue(state.llm, llmEnvManaged.value, "inference_provider") || llmFieldValue(state.llm, llmEnvManaged.value, "provider"),
        { allowEmpty: true },
      );
      const inferenceProviderRaw = llmFieldEnvRawValue(llmEnvManaged.value, "inference_provider");
      const providerRaw = llmFieldEnvRawValue(llmEnvManaged.value, "provider");
      if (inferenceProviderRaw !== "") {
        payload.inference_provider = inferenceProviderRaw;
      } else if (providerRaw !== "") {
        payload.provider = providerRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "inference_provider") && provider !== "") {
        payload.inference_provider = state.llm.inference_provider;
      }
      const endpointRaw = llmFieldEnvRawValue(llmEnvManaged.value, "endpoint");
      if (endpointRaw !== "") {
        payload.endpoint = endpointRaw;
      } else if (!setupProviderSupportsCustomAPIBase(provider)) {
        payload.endpoint = "";
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
        const apiKeyRaw = llmFieldEnvRawValue(llmEnvManaged.value, "api_key");
        if (apiKeyRaw !== "") {
          payload.api_key = apiKeyRaw;
        } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "api_key")) {
          const apiKey = trimText(state.llm.api_key);
          if (apiKey !== "") {
            payload.api_key = apiKey;
          }
        }
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else if (
        provider === SETUP_PROVIDER_XAI_OAUTH ||
        provider === SETUP_PROVIDER_MISTERMORPH_PRO
      ) {
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

    async function loadAgentSettings(endpointRef = settingsEndpointRef.value) {
      const requestSeq = ++agentSettingsRequestSeq;
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      agentLoading.value = true;
      agentSettingsReadOnly.value = false;
      agentSettingsReadOnlyReason.value = "";
      try {
        const data = await endpointApiFetch(targetEndpointRef, "/settings/agent");
        if (!isCurrentAgentSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        llmConfigPath.value = typeof data.config_path === "string" ? data.config_path : "";
        applyPayload(data);
      } catch (e) {
        if (!isCurrentAgentSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        toast.error(agentSettingsErrorMessage(e, targetEndpointRef, "msg_load_failed"));
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

    function resetCodexAuthStatus() {
      Object.assign(codexAuthStatus, {
        logged_in: false,
        access_token_present: false,
        refresh_token_present: false,
        access_token_expired: false,
        expires_at: "",
        account_id: "",
        file_mode_ok: true,
        file_mode_warning: "",
      });
    }

    function isCurrentCodexAuthStatusRequest(requestSeq, endpointRef) {
      return requestSeq === codexAuthStatusRequestSeq && endpointRef === settingsEndpointRef.value;
    }

    async function loadCodexAuthStatus(endpointRef = settingsEndpointRef.value) {
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      const requestSeq = ++codexAuthStatusRequestSeq;
      codexAuthLoading.value = true;
      codexAuthError.value = "";
      try {
        let payload = await endpointApiFetch(targetEndpointRef, "/auth/codex/status");
        if (!isCurrentCodexAuthStatusRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        applyCodexAuthStatus(payload);
        const status = payload && typeof payload.status === "object" ? payload.status : payload;
        if (
          codexOAuthInUse.value &&
          status?.refresh_token_present === true &&
          (status?.access_token_present !== true || status?.access_token_expired === true)
        ) {
          payload = await endpointApiFetch(targetEndpointRef, "/auth/codex/refresh", { method: "POST" });
          if (!isCurrentCodexAuthStatusRequest(requestSeq, targetEndpointRef)) {
            return;
          }
          applyCodexAuthStatus(payload);
        }
      } catch (e) {
        if (isCurrentCodexAuthStatusRequest(requestSeq, targetEndpointRef)) {
          codexAuthError.value = e?.message || t("msg_load_failed");
        }
      } finally {
        if (isCurrentCodexAuthStatusRequest(requestSeq, targetEndpointRef)) {
          codexAuthLoading.value = false;
        }
      }
    }

    async function openCodexAuthDialog() {
      const targetEndpointRef = settingsEndpointRef.value;
      const shouldStartLogin = codexAuthNeedsLogin.value && !codexLoginSession.value && !codexAuthBusy.value;
      let authWindow = null;
      if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
        // Open synchronously from the click event so popup blockers allow the auth tab.
        authWindow = openExternalPlaceholder();
      }
      await openReentrantDialog(codexAuthDialogOpen);
      void loadCodexAuthStatus(targetEndpointRef);
      if (shouldStartLogin) {
        void startCodexLogin(authWindow, targetEndpointRef);
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
      codexLoginEndpointRef.value = "";
      codexLoginVerificationURL.value = "";
      codexLoginUserCode.value = "";
      codexLoginExpiresAt.value = "";
    }

    function cancelCodexAuthFlow() {
      codexAuthOperationSeq += 1;
      codexAuthBusy.value = false;
      resetCodexLoginSession();
    }

    function resetCodexAuthEndpointState() {
      codexAuthStatusRequestSeq += 1;
      codexAuthLoading.value = false;
      codexAuthError.value = "";
      codexAuthDialogOpen.value = false;
      cancelCodexAuthFlow();
      resetCodexAuthStatus();
    }

    function scheduleCodexLoginPoll(intervalSeconds = 5) {
      clearCodexLoginTimer();
      const delay = Math.max(2, Number(intervalSeconds) || 5) * 1000;
      codexLoginPollTimer = window.setTimeout(() => {
        void pollCodexLogin();
      }, delay);
    }

    async function startCodexLogin(authWindow = null, endpointRef = settingsEndpointRef.value) {
      if (codexAuthBusy.value) {
        if (authWindow && !authWindow.closed) {
          authWindow.close();
        }
        return;
      }
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      const operationSeq = ++codexAuthOperationSeq;
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      resetCodexLoginSession();
      codexLoginEndpointRef.value = targetEndpointRef;
      let authWindowUsed = false;
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/auth/codex/login/start", { method: "POST" });
        if (
          operationSeq !== codexAuthOperationSeq ||
          targetEndpointRef !== settingsEndpointRef.value ||
          targetEndpointRef !== codexLoginEndpointRef.value
        ) {
          return;
        }
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
        if (operationSeq === codexAuthOperationSeq && targetEndpointRef === settingsEndpointRef.value) {
          codexAuthError.value = e?.message || t("msg_load_failed");
        }
      } finally {
        if (!authWindowUsed && authWindow && !authWindow.closed) {
          authWindow.close();
        }
        if (operationSeq === codexAuthOperationSeq) {
          codexAuthBusy.value = false;
        }
      }
    }

    async function pollCodexLogin() {
      const sessionID = codexLoginSession.value;
      const targetEndpointRef = codexLoginEndpointRef.value;
      if (!sessionID || !targetEndpointRef || codexAuthBusy.value) {
        return;
      }
      if (targetEndpointRef !== settingsEndpointRef.value) {
        cancelCodexAuthFlow();
        return;
      }
      const operationSeq = ++codexAuthOperationSeq;
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/auth/codex/login/poll", {
          method: "POST",
          body: { session_id: sessionID, set_default: false },
        });
        if (
          operationSeq !== codexAuthOperationSeq ||
          targetEndpointRef !== settingsEndpointRef.value ||
          targetEndpointRef !== codexLoginEndpointRef.value
        ) {
          return;
        }
        if (payload?.pending === true) {
          scheduleCodexLoginPoll(5);
          return;
        }
        applyCodexAuthStatus(payload);
        resetCodexLoginSession();
        if (payload?.settings_updated === true) {
          invalidateConsoleSetupReadiness();
          await loadAgentSettings(targetEndpointRef);
        }
      } catch (e) {
        if (operationSeq === codexAuthOperationSeq && targetEndpointRef === settingsEndpointRef.value) {
          codexAuthError.value = e?.message || t("msg_load_failed");
        }
      } finally {
        if (operationSeq === codexAuthOperationSeq) {
          codexAuthBusy.value = false;
        }
      }
    }

    async function logoutCodexAuth() {
      if (codexAuthBusy.value) {
        return;
      }
      const targetEndpointRef = settingsEndpointRef.value;
      const operationSeq = ++codexAuthOperationSeq;
      codexAuthBusy.value = true;
      codexAuthError.value = "";
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/auth/codex/logout", { method: "POST" });
        if (operationSeq !== codexAuthOperationSeq || targetEndpointRef !== settingsEndpointRef.value) {
          return;
        }
        applyCodexAuthStatus(payload);
        resetCodexLoginSession();
      } catch (e) {
        if (operationSeq === codexAuthOperationSeq && targetEndpointRef === settingsEndpointRef.value) {
          codexAuthError.value = e?.message || t("msg_delete_failed");
        }
      } finally {
        if (operationSeq === codexAuthOperationSeq) {
          codexAuthBusy.value = false;
        }
      }
    }

    function applyConsolePayload(data) {
      const values = Array.isArray(data?.managed_runtimes) ? data.managed_runtimes : [];
      const telegram = data?.telegram && typeof data.telegram === "object" ? data.telegram : {};
      const slack = data?.slack && typeof data.slack === "object" ? data.slack : {};
      const line = data?.line && typeof data.line === "object" ? data.line : {};
      const lark = data?.lark && typeof data.lark === "object" ? data.lark : {};
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
      state.line.channel_access_token = typeof line.channel_access_token === "string" ? line.channel_access_token : "";
      state.line.channel_secret = typeof line.channel_secret === "string" ? line.channel_secret : "";
      state.line.allowed_group_ids_text = formatConfigList(line.allowed_group_ids);
      state.line.group_trigger_mode = normalizeConsoleGroupTriggerMode(line.group_trigger_mode);
      state.lark.app_id = typeof lark.app_id === "string" ? lark.app_id : "";
      state.lark.app_secret = typeof lark.app_secret === "string" ? lark.app_secret : "";
      state.lark.allowed_chat_ids_text = formatConfigList(lark.allowed_chat_ids);
      state.lark.group_trigger_mode = normalizeConsoleGroupTriggerMode(lark.group_trigger_mode);
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
      consoleSettingsRequestSeq += 1;
      consoleLoading.value = false;
      consoleSaving.value = false;
      consoleSavingTarget.value = "";
      state.managedRuntimes.telegram = false;
      state.managedRuntimes.slack = false;
      state.managedRuntimes.lark = false;
      Object.assign(state.telegram, buildEmptyTelegramConsoleState());
      Object.assign(state.slack, buildEmptySlackConsoleState());
      Object.assign(state.line, buildEmptyLineConsoleState());
      Object.assign(state.lark, buildEmptyLarkConsoleState());
      Object.assign(state.guard, buildEmptyGuardConsoleState());
      consoleEnvManaged.value = {};
      consoleConfigPath.value = "";
      clearLoadedConsoleSnapshots();
    }

    function resetDesktopSettingsState() {
      desktopSettingsRequestSeq += 1;
      desktopLoading.value = false;
      desktopChecking.value = false;
      desktopSettingsLoaded.value = false;
      desktopCurrentVersion.value = "";
      desktopUpdateResult.value = null;
    }

    async function loadConsoleSettings() {
      if (!selectedEndpointIsConsole.value) {
        return;
      }
      const requestSeq = ++consoleSettingsRequestSeq;
      const targetEndpointRef = settingsEndpointRef.value;
      consoleLoading.value = true;
      try {
        const data = await endpointApiFetch(targetEndpointRef, "/settings/console");
        if (!isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        consoleConfigPath.value = typeof data.config_path === "string" ? data.config_path : "";
        applyConsolePayload(data);
      } catch (e) {
        if (isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          toast.error(e.message || t("msg_load_failed"));
        }
      } finally {
        if (isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          consoleLoading.value = false;
        }
      }
    }

    async function loadDesktopSettings() {
      const requestSeq = ++desktopSettingsRequestSeq;
      const targetEndpointRef = consoleEndpointRef.value;
      desktopLoading.value = true;
      try {
        const data = await endpointApiFetch(targetEndpointRef, "/settings/auto-update");
        if (!isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        const nativeVersion = targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF ? desktopRuntimeVersion() : "";
        desktopCurrentVersion.value = nativeVersion || trimText(data?.current_version) || "dev";
        desktopSettingsLoaded.value = true;
      } catch (e) {
        if (isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          toast.error(e.message || t("msg_load_failed"));
        }
      } finally {
        if (isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          desktopLoading.value = false;
        }
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

    async function loadPersonaFile(endpointRef, endpoint) {
      try {
        const payload = await runtimeApiFetchForEndpoint(endpointRef, endpoint);
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

    async function loadPersonaSettings(endpointRef = settingsEndpointRef.value) {
      const requestSeq = ++personaSettingsRequestSeq;
      const targetEndpointRef = trimText(endpointRef) || LOCAL_CONSOLE_ENDPOINT_REF;
      personaLoading.value = true;
      personaErr.value = "";
      personaOk.value = "";
      try {
        const identityContent = await loadPersonaFile(targetEndpointRef, PERSONA_IDENTITY_ENDPOINT);
        if (!isCurrentPersonaSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        applyPersonaIdentityContent(identityContent);

        const soul = await loadPersonaFile(targetEndpointRef, PERSONA_SOUL_ENDPOINT);
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
      const targetEndpointRef = settingsEndpointRef.value;
      let setupReadinessDirty = false;
      try {
        if (personaIdentityDirty.value) {
          const content = buildIdentityYAML(state.persona, loadedIdentityRaw.value);
          await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_IDENTITY_ENDPOINT, {
            method: "PUT",
            body: { content },
          });
          if (targetEndpointRef !== settingsEndpointRef.value) {
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
          if (targetEndpointRef !== settingsEndpointRef.value) {
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
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_AVATAR_ENDPOINT, {
          method: "PUT",
          headers: { "Content-Type": "image/webp" },
          body: blob,
        });
        if (targetEndpointRef !== settingsEndpointRef.value) {
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
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        await runtimeApiFetchForEndpoint(targetEndpointRef, PERSONA_AVATAR_ENDPOINT, {
          method: "DELETE",
        });
        if (targetEndpointRef !== settingsEndpointRef.value) {
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
      const tools = {
        write_file: { enabled: state.tools.write_file },
        spawn: { enabled: state.tools.spawn },
        coder: { enabled: state.tools.coder },
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
      if (target === "skills") {
        return { skills: { enabled: !!state.skills.enabled, load: parseSkillLoadText(state.skills.load_text) } };
      }
      if (target === "tools") {
        return { tools };
      }
      return {
        llm: buildLLMSettingsPayload(),
        skills: { enabled: !!state.skills.enabled, load: parseSkillLoadText(state.skills.load_text) },
        tools,
      };
    }

    function buildLLMSettingsPayload() {
      const payload = {};
      const provider = normalizeSetupProviderChoice(
        llmFieldValue(state.llm, llmEnvManaged.value, "inference_provider") || llmFieldValue(state.llm, llmEnvManaged.value, "provider"),
        { allowEmpty: true },
      );
      const inferenceProviderRaw = llmFieldEnvRawValue(llmEnvManaged.value, "inference_provider");
      const providerRaw = llmFieldEnvRawValue(llmEnvManaged.value, "provider");
      if (inferenceProviderRaw !== "") {
        payload.inference_provider = inferenceProviderRaw;
      } else if (providerRaw !== "") {
        payload.provider = providerRaw;
      } else if (!isLLMFieldEnvManaged(llmEnvManaged.value, "inference_provider")) {
        payload.inference_provider = state.llm.inference_provider;
      }
      if (!isLLMFieldEnvManaged(llmEnvManaged.value, "endpoint")) {
        payload.endpoint = setupProviderSupportsCustomAPIBase(provider) ? trimText(state.llm.endpoint) : "";
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
        if (!isLLMFieldEnvManaged(llmEnvManaged.value, "api_key")) {
          payload.api_key = trimText(state.llm.api_key);
        }
        payload.cloudflare_api_token = "";
        payload.cloudflare_account_id = "";
        payload.bedrock_aws_key = "";
        payload.bedrock_aws_secret = "";
        payload.bedrock_region = "";
        payload.bedrock_model_arn = "";
      } else if (
        provider === SETUP_PROVIDER_XAI_OAUTH ||
        provider === SETUP_PROVIDER_MISTERMORPH_PRO
      ) {
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
      payload.fallback_profiles = normalizeNamedList(state.llm.fallback_profiles);
      return payload;
    }

    function buildProfileTestPayload(profile) {
      return {
        profiles: [buildProfilePayload(profile)],
      };
    }

    function profileProviderChoice(profile) {
      const envManaged = llmProfileEnvManaged(profile);
      return normalizeSetupProviderChoice(
        llmFieldValue(profile, envManaged, "inference_provider") || llmFieldValue(profile, envManaged, "provider"),
        { allowEmpty: true },
      );
    }

    function profileUsesCodexProvider(profile) {
      return profileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX;
    }

    function profileCodexAuthDisabled(profile) {
      const envManaged = llmProfileEnvManaged(profile);
      return (
        trimText(llmFieldValue(profile, envManaged, "endpoint")) !== "" &&
        hasLLMFieldValue(profile, envManaged, "api_key")
      );
    }

    function profileUsesCodexAPIKey(profile) {
      const envManaged = llmProfileEnvManaged(profile);
      return (
        profileProviderChoice(profile) === SETUP_PROVIDER_OPENAI_CODEX &&
        setupOpenAICodexUsesAPIKey(
          llmFieldValue(profile, envManaged, "endpoint"),
          hasLLMFieldValue(profile, envManaged, "api_key"),
        )
      );
    }

    function profileUsesXAIProvider(profile) {
      return selectedEndpointIsConsole.value && profileProviderChoice(profile) === SETUP_PROVIDER_XAI_OAUTH;
    }

    function profileUsesProProvider(profile) {
      return selectedEndpointIsConsole.value && profileProviderChoice(profile) === SETUP_PROVIDER_MISTERMORPH_PRO;
    }

    function hasResolvableProfileTestTarget(profile) {
      const name = trimText(profile?.name);
      if (name === "" || name.toLowerCase() === "default") {
        return false;
      }
      const matches = state.llm.profiles.filter((item) => trimText(item?.name).toLowerCase() === name.toLowerCase()).length;
      return matches === 1;
    }

    function profileModelLookupCredentialsReady(profile) {
      const provider = profileProviderChoice(profile);
      const envManaged = llmProfileEnvManaged(profile);
      if (provider === SETUP_PROVIDER_MISTERMORPH_PRO) {
        return !selectedEndpointIsConsole.value || proAuthStatus.logged_in;
      }
      if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        return profileUsesCodexAPIKey(profile) || codexAuthStatus.logged_in;
      }
      if (provider === SETUP_PROVIDER_XAI_OAUTH) {
        return !selectedEndpointIsConsole.value || xaiAuthReady.value;
      }
      if (!setupProviderRequiresAPIKey(provider)) {
        return true;
      }
      return hasLLMFieldValue(profile, envManaged, "api_key");
    }

    function testConnectionDisabledForProfile(profile) {
      const provider = profileProviderChoice(profile);
      const envManaged = llmProfileEnvManaged(profile);
      if (testConnectionLoading.value || agentLoading.value || agentSaving.value) {
        return true;
      }
      if (!hasResolvableProfileTestTarget(profile) || provider === "") {
        return true;
      }
      if (!hasLLMFieldValue(profile, envManaged, "model")) {
        return true;
      }
      if (provider === SETUP_PROVIDER_OPENAI_CODEX) {
        return !profileUsesCodexAPIKey(profile) && !codexAuthStatus.logged_in;
      }
      if (provider === SETUP_PROVIDER_XAI_OAUTH) {
        return selectedEndpointIsConsole.value && !xaiAuthReady.value;
      }
      if (provider === SETUP_PROVIDER_MISTERMORPH_PRO) {
        return selectedEndpointIsConsole.value && !proAuthStatus.logged_in;
      }
      if (provider === SETUP_PROVIDER_BEDROCK) {
        return (
          !hasLLMFieldValue(profile, envManaged, "bedrock_aws_key") ||
          !hasLLMFieldValue(profile, envManaged, "bedrock_aws_secret") ||
          !hasLLMFieldValue(profile, envManaged, "bedrock_region")
        );
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        return (
          !hasLLMFieldValue(profile, envManaged, "cloudflare_api_token") ||
          !hasLLMFieldValue(profile, envManaged, "cloudflare_account_id")
        );
      }
      return setupProviderRequiresAPIKey(provider) && !hasLLMFieldValue(profile, envManaged, "api_key");
    }

    function llmActionMenuItems(profile = null) {
      const items = [
        {
          id: "benchmark",
          title: t("setup_llm_test_button"),
          disabled: profile ? testConnectionDisabledForProfile(profile) : testConnectionDisabled.value,
          action: () => openTestConnection(profile?._key),
        },
      ];
      if (!profile) {
        return items;
      }
      return [
        ...items,
        { id: "delete-divider", divider: true },
        {
          id: "delete",
          title: t("action_delete"),
          danger: true,
          disabled: agentLoading.value || agentSaving.value || agentSettingsReadOnly.value,
          action: () => confirmRemoveLLMProfile(profile._key),
        },
      ];
    }

    function primeConnectionTestState(targetProfile, nextPayload = null) {
      const payload = nextPayload || (targetProfile ? buildProfileTestPayload(targetProfile) : buildDefaultLLMTestPayload());
      const profileEnvManaged = targetProfile ? llmProfileEnvManaged(targetProfile) : null;
      const targetProviderChoice = targetProfile
        ? profileProviderChoice(targetProfile)
        : normalizeSetupProviderChoice(
            llmFieldValue(state.llm, llmEnvManaged.value, "inference_provider") ||
              llmFieldValue(state.llm, llmEnvManaged.value, "provider"),
            { allowEmpty: true },
          );
      const targetEndpoint = targetProfile
        ? llmFieldValue(targetProfile, profileEnvManaged, "endpoint")
        : llmFieldValue(state.llm, llmEnvManaged.value, "endpoint");
      const targetModel = targetProfile
        ? llmFieldValue(targetProfile, profileEnvManaged, "model")
        : llmFieldValue(state.llm, llmEnvManaged.value, "model");
      testConnectionError.value = "";
      testConnectionBenchmarks.value = [];
      testConnectionMeta.provider = targetProviderChoice;
      testConnectionMeta.apiBase = trimText(targetEndpoint);
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
      const lineEnv =
        consoleEnvManaged.value?.line && typeof consoleEnvManaged.value.line === "object"
          ? consoleEnvManaged.value.line
          : {};
      const larkEnv =
        consoleEnvManaged.value?.lark && typeof consoleEnvManaged.value.lark === "object"
          ? consoleEnvManaged.value.lark
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
      const line = {
        channel_access_token:
          consoleFieldRawValue(lineEnv, "channel_access_token") || trimText(state.line.channel_access_token),
        channel_secret: consoleFieldRawValue(lineEnv, "channel_secret") || trimText(state.line.channel_secret),
        allowed_group_ids: parseConfigListText(state.line.allowed_group_ids_text),
        group_trigger_mode: normalizeConsoleGroupTriggerMode(state.line.group_trigger_mode),
      };
      const lark = {
        app_id: consoleFieldRawValue(larkEnv, "app_id") || trimText(state.lark.app_id),
        app_secret: consoleFieldRawValue(larkEnv, "app_secret") || trimText(state.lark.app_secret),
        allowed_chat_ids: parseConfigListText(state.lark.allowed_chat_ids_text),
        group_trigger_mode: normalizeConsoleGroupTriggerMode(state.lark.group_trigger_mode),
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
      if (target === "line") {
        return { line };
      }
      if (target === "lark") {
        return { lark };
      }
      if (target === "guard") {
        return { guard };
      }
      return { managed_runtimes, telegram, slack, line, lark, guard };
    }

    function consoleFieldEntry(kind, field) {
      const key = String(field || "").trim();
      const channel = String(kind || "").trim();
      const group = consoleEnvManaged.value?.[channel];
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

    function updateLineField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.line, key)) {
        return;
      }
      state.line[key] = String(value || "");
      updateConsoleLineDirty();
    }

    function updateLarkField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.lark, key)) {
        return;
      }
      state.lark[key] = String(value || "");
      updateConsoleLarkDirty();
    }

    function updateLineGroupTrigger(item) {
      updateLineField("group_trigger_mode", item?.value || "smart");
    }

    function updateLarkGroupTrigger(item) {
      updateLarkField("group_trigger_mode", item?.value || "smart");
    }

    function updateGuardField(field, value) {
      const key = String(field || "").trim();
      if (!key || !Object.prototype.hasOwnProperty.call(state.guard, key)) {
        return;
      }
      state.guard[key] = typeof state.guard[key] === "boolean" ? !!value : String(value || "");
      updateConsoleGuardDirty();
    }

    async function saveLLMProfile(profileKey) {
      const profile = state.llm.profiles.find((item) => item._key === profileKey) || null;
      if (!profile || agentLoading.value || agentSaving.value || agentSettingsReadOnly.value) {
        return;
      }
      const validationError = profileValidationError(profile);
      if (validationError) {
        toast.error(validationError);
        return;
      }
      if (profileSaveDisabled(profile)) {
        return;
      }

      const originalName = trimText(profile._savedName);
      const nextName = trimText(profile.name);
      agentSaving.value = true;
      agentSavingTarget.value = `profile:${profileKey}`;
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/agent", {
          method: "PUT",
          body: {
            llm: {
              profile: {
                original_name: originalName,
                ...buildProfilePayload(profile),
              },
            },
          },
        });
        if (targetEndpointRef !== settingsEndpointRef.value) {
          return;
        }
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        const profileEnvManaged = payload?.env_managed?.llm_profiles?.[nextName];
        profile._envManaged =
          profileEnvManaged && typeof profileEnvManaged === "object" ? profileEnvManaged : {};
        profile._savedName = nextName;
        profile._savedSnapshot = JSON.stringify(serializeLLMProfile(profile));
        if (originalName && originalName !== nextName) {
          state.llm.fallback_profiles = state.llm.fallback_profiles.map((item) =>
            trimText(item).toLowerCase() === originalName.toLowerCase() ? nextName : item,
          );
          updateLoadedFallbackProfile(originalName, nextName);
        }
        if (targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF) {
          invalidateConsoleSetupReadiness();
        }
        await loadEndpoints();
        toast.success(t("msg_save_success"));
      } catch (e) {
        toast.error(agentSettingsErrorMessage(e, targetEndpointRef, "msg_save_failed"));
      } finally {
        agentSaving.value = false;
        agentSavingTarget.value = "";
      }
    }

    async function saveAgentSettings(target = "all") {
      const normalizedTarget = ["all", "llm", "skills", "tools"].includes(String(target))
        ? String(target)
        : "all";
      if (agentSettingsReadOnly.value) {
        return;
      }
      if (normalizedTarget === "llm" && llmSaveDisabled.value) {
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
        agentValidationVisible.value = true;
        return;
      }
      if ((normalizedTarget === "skills" || normalizedTarget === "all") && skillsValidationError.value !== "") {
        skillsValidationVisible.value = true;
        return;
      }
      agentSaving.value = true;
      agentSavingTarget.value = normalizedTarget;
      agentValidationVisible.value = false;
      skillsValidationVisible.value = false;
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/agent", {
          method: "PUT",
          body: buildSavePayload(normalizedTarget),
        });
        if (targetEndpointRef !== settingsEndpointRef.value) {
          return;
        }
        llmConfigPath.value = typeof payload.config_path === "string" ? payload.config_path : llmConfigPath.value;
        if (normalizedTarget === "llm" || normalizedTarget === "all") {
          if (targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF) {
            invalidateConsoleSetupReadiness();
          }
          const preservedProfiles = state.llm.profiles;
          const preservedSkills = JSON.parse(JSON.stringify(state.skills));
          const preservedTools = JSON.parse(JSON.stringify(state.tools));
          const previousSkillsSnapshot = loadedSkillsSnapshot.value;
          const previousToolsSnapshot = loadedToolsSnapshot.value;
          const previousSkillsDirty = skillsDirty.value;
          const previousToolsDirty = toolsDirty.value;
          applyPayload(payload, { snapshotScope: normalizedTarget === "llm" ? "llm" : "all" });
          state.llm.profiles = preservedProfiles;
          if (normalizedTarget === "llm") {
            Object.assign(state.skills, preservedSkills);
            Object.assign(state.tools, preservedTools);
            loadedSkillsSnapshot.value = previousSkillsSnapshot;
            loadedToolsSnapshot.value = previousToolsSnapshot;
            skillsDirty.value = previousSkillsDirty;
            toolsDirty.value = previousToolsDirty;
          }
          await loadEndpoints();
        } else if (normalizedTarget === "skills") {
          applySkillsPayload(payload?.skills);
          loadedSkillsSnapshot.value = buildSkillsSnapshot(state);
          skillsDirty.value = false;
        } else if (normalizedTarget === "tools") {
          loadedToolsSnapshot.value = buildToolsSnapshot(state);
          toolsDirty.value = false;
        }
        const saveMessage = t("msg_save_success");
        toast.success(saveMessage);
      } catch (e) {
        toast.error(agentSettingsErrorMessage(e, targetEndpointRef, "msg_save_failed"));
      } finally {
        agentSaving.value = false;
        agentSavingTarget.value = "";
      }
    }

    async function saveConsoleSettings(target = "all") {
      const normalizedTarget = ["all", "runtimes", "telegram", "slack", "line", "lark", "guard"].includes(String(target))
        ? String(target)
        : "all";
      if (!selectedEndpointIsConsole.value) {
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
      if (normalizedTarget === "line" && lineSaveDisabled.value) {
        return;
      }
      if (normalizedTarget === "lark" && larkSaveDisabled.value) {
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
      const requestSeq = ++consoleSettingsRequestSeq;
      const targetEndpointRef = settingsEndpointRef.value;
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/console", {
          method: "PUT",
          body: buildConsoleSavePayload(normalizedTarget),
        });
        if (!isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        consoleConfigPath.value =
          typeof payload.config_path === "string" ? payload.config_path : consoleConfigPath.value;
        applyConsolePayload(payload);
        toast.success(t("msg_save_success"));
      } catch (e) {
        if (isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          toast.error(e.message || t("msg_save_failed"));
        }
      } finally {
        if (isCurrentConsoleSettingsRequest(requestSeq, targetEndpointRef)) {
          consoleSaving.value = false;
          consoleSavingTarget.value = "";
        }
      }
    }

    async function runDesktopUpdateCheck() {
      if (desktopCheckDisabled.value) {
        return;
      }
      desktopChecking.value = true;
      desktopChecksumCopied.value = false;
      const requestSeq = desktopSettingsRequestSeq;
      const targetEndpointRef = consoleEndpointRef.value;
      try {
        const result = canCheckDesktopUpdate() && targetEndpointRef === LOCAL_CONSOLE_ENDPOINT_REF
          ? await checkDesktopUpdate()
          : await endpointApiFetch(targetEndpointRef, "/settings/auto-update/check", { method: "POST" });
        if (!isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          return;
        }
        desktopUpdateResult.value = result;
        desktopCurrentVersion.value = trimText(desktopUpdateResult.value?.current_version) || desktopCurrentVersion.value;
        await nextTick();
        syncDesktopChangelogReadonly();
      } catch (e) {
        if (isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          toast.error(e.message || t("msg_load_failed"));
        }
      } finally {
        if (isCurrentDesktopSettingsRequest(requestSeq, targetEndpointRef)) {
          desktopChecking.value = false;
        }
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
      try {
        const copied = await copyTextToClipboard(checksum);
        if (copied) {
          desktopChecksumCopied.value = true;
          toast.success(t("settings_desktop_update_checksum_copied"));
          if (desktopChecksumCopyTimer) {
            window.clearTimeout(desktopChecksumCopyTimer);
          }
          desktopChecksumCopyTimer = window.setTimeout(() => {
            desktopChecksumCopied.value = false;
            desktopChecksumCopyTimer = 0;
          }, 1200);
        }
      } catch (e) {
        toast.error(e.message || t("msg_save_failed"));
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

    async function openModelPicker(profileKey = "") {
      if (agentLoading.value || agentSaving.value || agentSettingsReadOnly.value) {
        return;
      }
      const normalizedProfileKey = String(profileKey || "").trim();
      const targetProfile = normalizedProfileKey
        ? state.llm.profiles.find((profile) => profile._key === normalizedProfileKey) || null
        : null;
      if (normalizedProfileKey && !targetProfile) {
        return;
      }
      if (targetProfile && !profileModelLookupCredentialsReady(targetProfile)) {
        return;
      }
      modelPickerTargetProfileKey.value = targetProfile?._key || "";
      modelPickerOpen.value = true;
      modelPickerLoading.value = true;
      modelPickerError.value = "";
      modelPickerItems.value = [];
      const targetEndpointRef = settingsEndpointRef.value;
      const targetProfileEnvManaged = targetProfile ? llmProfileEnvManaged(targetProfile) : null;
      const provider = targetProfile
        ? profileProviderChoice(targetProfile)
        : llmFieldValue(state.llm, llmEnvManaged.value, "inference_provider") ||
          llmFieldValue(state.llm, llmEnvManaged.value, "provider");
      const providerChoice = normalizeSetupProviderChoice(provider, { allowEmpty: true });
      const endpoint = targetProfile
        ? llmFieldValue(targetProfile, targetProfileEnvManaged, "endpoint")
        : llmFieldValue(state.llm, llmEnvManaged.value, "endpoint");
      const apiKey = targetProfile
        ? llmFieldValue(targetProfile, targetProfileEnvManaged, "api_key")
        : llmFieldValue(state.llm, llmEnvManaged.value, "api_key");
      const apiKeyRaw = targetProfile
        ? llmFieldEnvRawValue(targetProfileEnvManaged, "api_key")
        : llmFieldEnvRawValue(llmEnvManaged.value, "api_key");
      try {
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/agent/models", {
          method: "POST",
          body: {
            inference_provider: providerChoice,
            endpoint: setupProviderSupportsCustomAPIBase(providerChoice) ? endpoint : "",
            api_key:
              providerChoice === SETUP_PROVIDER_MISTERMORPH_PRO
                ? ""
                : apiKeyRaw || apiKey,
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
      const targetProfile = state.llm.profiles.find((profile) => profile._key === modelPickerTargetProfileKey.value) || null;
      if (targetProfile) {
        updateProfileField(targetProfile._key, { field: "model", value: nextModel });
        return;
      }
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
      const shouldReloadCodexAuthStatus = targetProfile
        ? profileUsesCodexProvider(targetProfile) && !profileUsesCodexAPIKey(targetProfile)
        : defaultIsCodexProvider.value && !defaultCodexUsesAPIKey.value;
      const targetEndpointRef = settingsEndpointRef.value;
      testConnectionLoading.value = true;
      try {
        const body = {
          llm: nextPayload,
        };
        if (targetProfileName !== "") {
          body.target_profile = targetProfileName;
        }
        const payload = await endpointApiFetch(targetEndpointRef, "/settings/agent/test", {
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
        if (shouldReloadCodexAuthStatus) {
          void loadCodexAuthStatus(targetEndpointRef);
        }
      }
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

    function explicitSkillLoadEntries() {
      const entries = parseSkillLoadText(state.skills.load_text);
      if (!entries.length || (entries.length === 1 && entries[0] === "*")) {
        return normalizeNamedList(allSkillItems.value.map((skill) => skillLoadEntry(skill)));
      }
      return normalizeNamedList(entries.filter((entry) => entry !== "*"));
    }

    function setSkillLoaded(skill, loaded) {
      if (agentSettingsReadOnly.value) {
        return;
      }
      const target = skillLoadEntry(skill);
      if (!target) {
        return;
      }
      let entries = explicitSkillLoadEntries();
      if (loaded) {
        if (!entries.some((entry) => skillLoadEntryMatches(skill, entry))) {
          entries.push(target);
        }
      } else {
        entries = entries.filter((entry) => !skillLoadEntryMatches(skill, entry));
      }
      const allItems = allSkillItems.value;
      const loadsAllSkills =
        allItems.length > 0 && allItems.every((item) => entries.some((entry) => skillLoadEntryMatches(item, entry)));
      state.skills.load_text = loadsAllSkills ? "" : formatSkillLoadList(entries);
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

    function openLogsPage() {
      router.push(endpointRoutePath(endpointState.selectedRef, "/logs"));
    }

    function selectSection(id) {
      const sectionID = normalizeSettingsSectionID(id);
      selectedSectionID.value = sectionID;
      if (isMobile.value) {
        mobilePanelVisible.value = true;
      }
      const nextPath = settingsSectionPath(endpointState.selectedRef, sectionID);
      if (route.path !== nextPath) {
        router.push(nextPath);
      }
    }

    function isSelectedSection(item) {
      return !isMobile.value && String(item?.id || "") === selectedSectionID.value;
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
          void loadAgentSettings(settingsEndpointRef.value);
          return;
        }
        ensureLoadedAgentSnapshotsForSection(normalizedSectionID);
        return;
      }
      if (normalizedSectionID === "persona") {
        if (!personaSettingsLoaded.value && !personaLoading.value) {
          void loadPersonaSettings(settingsEndpointRef.value);
        }
        return;
      }
      if (["channels", "runtimes", "guard"].includes(normalizedSectionID)) {
        if (selectedEndpointIsConsole.value && !consoleSettingsLoaded.value && !consoleLoading.value) {
          void loadConsoleSettings();
        }
        return;
      }
      if (normalizedSectionID === "console") {
        if (!desktopSettingsLoaded.value && !desktopLoading.value) {
          void loadDesktopSettings();
        }
      }
    }

    function discardSettingsDrafts() {
      agentSettingsRequestSeq += 1;
      agentLoading.value = false;
      resetAgentSettingsState();

      personaSettingsRequestSeq += 1;
      personaLoading.value = false;
      resetPersonaSettingsState();
      personaErr.value = "";
      personaOk.value = "";

      resetConsoleSettingsState();

      apiBasePickerOpen.value = false;
      modelPickerOpen.value = false;
      modelPickerTargetProfileKey.value = "";
      modelPickerError.value = "";
      modelPickerItems.value = [];
      testConnectionOpen.value = false;
      testConnectionError.value = "";
      testConnectionBenchmarks.value = [];
      testConnectionTargetProfileKey.value = "";
      testConnectionMeta.provider = "";
      testConnectionMeta.apiBase = "";
      testConnectionMeta.model = "";
      closeDeleteProfileDialog();
      codexAuthDialogOpen.value = false;
      xaiAuthDialogOpen.value = false;
      proAuthDialogOpen.value = false;
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
      discardSettingsDrafts();
    });

    watch(
      () => settingsRouteSection(route),
      (routeSection, previousRouteSection) => {
        const sectionID = normalizeSettingsSectionID(routeSection);
        if (previousRouteSection !== undefined && normalizeSettingsSectionID(previousRouteSection) !== sectionID) {
          discardSettingsDrafts();
        }
        selectedSectionID.value = sectionID;
        ensureSettingsSectionData(sectionID);
        if (routeSection && routeSection !== sectionID) {
          router.replace(settingsSectionPath(endpointState.selectedRef, sectionID));
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
          const nextPath = settingsSectionPath(endpointState.selectedRef, sectionID);
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
        resetCodexAuthEndpointState();
        resetXAIAuthEndpointState();
        resetProAuthEndpointState();
        discardSettingsDrafts();
        ensureSettingsSectionData(selectedSectionID.value);
      }
    );

    watch(consoleEndpointRef, () => {
      resetDesktopSettingsState();
      if (selectedSectionID.value === "console") {
        ensureSettingsSectionData("console");
      }
    });

    watch(
      selectedEndpointIsConsole,
      (enabled) => {
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
        cancelCodexAuthFlow();
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
          resetCodexAuthEndpointState();
        }
      },
      { immediate: false }
    );

    watch(
      showXAIAuthCard,
      (visible) => {
        if (visible) {
          void loadXAIAuthStatus();
        } else {
          resetXAIAuthFlow();
        }
      },
      { immediate: false }
    );

    watch(
      showProAuthCard,
      (visible) => {
        if (visible) {
          void loadProAuthStatus();
        } else {
          resetProAuthFlow();
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
      agentFormDisabledReason,
      agentSettingsReadOnly,
      agentSettingsReadOnlyMessage,
      agentValidationVisible,
      skillsValidationVisible,
      deleteProfileDialogOpen,
      consoleLoading,
      consoleSaving,
      consoleSavingTarget,
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
      desktopLoading,
      desktopChecking,
      desktopChecksumCopied,
      desktopChangelogField,
      llmConfigPath,
      consoleConfigPath,
      desktopUpdateResult,
      state,
      llmEnvManaged,
      providerItems,
      reasoningEffortItems,
      toolsEmulationItems,
      profileOptions,
      agentValidationError,
      profileSaveDisabled,
      skillsValidationError,
      deleteProfileDialogText,
      deleteProfileDialogActions,
      apiBasePickerItems,
      toolItems,
      managedRuntimeItems,
      groupTriggerItems,
      settingsSections,
      selectedSection,
      selectedEndpointIsConsole,
      activeSaveKind,
      isMobile,
      showIndexPane,
      showPanelPane,
      mobileShowBack,
      mobileBarTitle,
      pageClass,
      llmSaveDisabled,
      skillsSaveDisabled,
      toolsSaveDisabled,
      consoleSaveDisabled,
      telegramSaveDisabled,
      slackSaveDisabled,
      lineSaveDisabled,
      larkSaveDisabled,
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
      profileIsInUse,
      llmActionMenuItems,
      showCodexAuthCard,
      defaultCodexAuthDisabled,
      showXAIAuthCard,
      showProAuthCard,
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
      xaiAuthLoading,
      xaiAuthBusy,
      xaiAuthError,
      xaiAuthDialogOpen,
      xaiSetDefault,
      xaiAuthStatus,
      xaiAuthSummary,
      xaiAuthButtonState,
      xaiAuthReady,
      xaiAuthButtonTitle,
      xaiLoginSession,
      xaiLoginVerificationURL,
      xaiLoginUserCode,
      xaiLoginExpiresLabel,
      pollXAILogin,
      logoutXAIAuth,
      loadXAIAuthStatus,
      openXAIAuthDialog,
      reloginXAIAuth,
      proAuthLoading,
      proAuthBusy,
      proAuthError,
      proAuthDialogOpen,
      proAuthStatus,
      proAuthSummary,
      proAuthButtonState,
      proAuthButtonTitle,
      proLoginSession,
      proLoginVerificationURL,
      proLoginUserCode,
      proLoginExpiresLabel,
      pollProLogin,
      logoutProAuth,
      loadProAuthStatus,
      openProAuthDialog,
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
      profileModelLookupCredentialsReady,
      profileUsesCodexProvider,
      profileCodexAuthDisabled,
      profileUsesXAIProvider,
      profileUsesProProvider,
      addLLMProfile,
      saveLLMProfile,
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
      setSkillsEnabled,
      setSkillLoaded,
      displayedLoadedSkills,
      displayedAvailableSkills,
      formatSkillCount,
      setToolEnabled,
      setManagedRuntimeEnabled,
      consoleFieldEnvManaged,
      consoleFieldManagedHeadline,
      updateTelegramField,
      updateSlackField,
      updateLineField,
      updateLarkField,
      updateTelegramGroupTrigger,
      updateSlackGroupTrigger,
      updateLineGroupTrigger,
      updateLarkGroupTrigger,
      updateGuardField,
      selectSection,
      isSelectedSection,
      sectionClass,
      showIndexView,
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
    <AppPage :title="t('settings_title')" :class="pageClass">
      <template #leading>
        <div class="settings-page-bar">
          <QButton
            v-if="mobileShowBack"
            class="plain xs icon settings-page-bar-back"
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
              </span>
              <span class="workspace-sidebar-item-marker">
                <QBadge v-if="isSelectedSection(item)" dot type="primary" size="sm" />
              </span>
            </button>
          </div>
        </aside>

        <div v-if="showPanelPane && selectedSection" class="settings-panel-scroll">
          <div v-if="selectedSection.id === 'agent'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head settings-llm-panel-head">
                  <div class="settings-panel-copy">
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_agent_block_title") }}</h3>
                    <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
                  </div>
                  <div class="settings-profile-actions settings-default-llm-actions">
                    <QButton
                      class="primary settings-profile-save"
                      :loading="agentSaving && agentSavingTarget === 'llm'"
                      :disabled="llmSaveDisabled"
                      @click="saveAgentSettings('llm')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                    <QDropdownMenu
                      class="settings-llm-actions-menu"
                      :items="llmActionMenuItems()"
                      hideSelected
                      hideActionLabel
                      :disabled="agentLoading || agentSaving"
                    >
                      <svg
                        class="settings-llm-actions-menu-icon"
                        viewBox="0 0 16 16"
                        aria-hidden="true"
                        focusable="false"
                      >
                        <circle cx="3" cy="8" r="1.25" fill="currentColor" />
                        <circle cx="8" cy="8" r="1.25" fill="currentColor" />
                        <circle cx="13" cy="8" r="1.25" fill="currentColor" />
                      </svg>
                      <span class="settings-llm-actions-menu-accessible">{{ t("todo_action_more") }}</span>
                    </QDropdownMenu>
                  </div>
                </header>

                <QFence
                  v-if="agentValidationVisible && agentValidationError"
                  type="danger"
                  icon="QIconCloseCircle"
                  :text="agentValidationError"
                />

                <QFence
                  v-if="agentSettingsReadOnly"
                  type="warning"
                  :text="agentSettingsReadOnlyMessage"
                />

                <div class="settings-panel-body">
                  <div class="settings-agent-stack">
                    <section class="settings-agent-section">
                      <LLMConfigForm
                        :config="state.llm"
                        :busy="agentLoading || agentSaving"
                        :disabledReason="agentFormDisabledReason"
                        :readOnly="agentSettingsReadOnly"
                        :envManaged="llmEnvManaged"
                        :providerItems="providerItems"
                        :reasoningEffortItems="reasoningEffortItems"
                        :toolsEmulationItems="toolsEmulationItems"
                        :enableAPIBasePicker="true"
                        :enableModelPicker="true"
                        :showCodexAuthAction="true"
                        :codexAuthDisabled="defaultCodexAuthDisabled"
                        :codexAuthState="codexAuthButtonState"
                        :codexAuthTitle="codexAuthButtonTitle"
                        :showXAIAuthAction="selectedEndpointIsConsole"
                        :xaiAuthState="xaiAuthButtonState"
                        :xaiAuthTitle="xaiAuthButtonTitle"
                        :showProAuthAction="selectedEndpointIsConsole"
                        :proAuthState="proAuthButtonState"
                        :proAuthTitle="proAuthButtonTitle"
                        @update-field="updateDefaultLLMField"
                        @open-api-base-picker="openAPIBasePicker"
                        @open-model-picker="openModelPicker"
                        @open-codex-auth="openCodexAuthDialog"
                        @open-xai-auth="openXAIAuthDialog"
                        @open-pro-auth="openProAuthDialog"
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
                          <div class="settings-profile-toolbar">
                            <span
                              class="settings-profile-status"
                              :class="{ 'is-in-use': profileIsInUse(profile) }"
                            >
                              <span class="settings-profile-status-dot" aria-hidden="true"></span>
                              {{
                                t(
                                  profileIsInUse(profile)
                                    ? "settings_agent_profile_status_in_use"
                                    : "settings_agent_profile_status_available"
                                )
                              }}
                            </span>
                            <div class="settings-profile-actions">
                              <QButton
                                type="button"
                                class="primary settings-profile-save"
                                :loading="agentSaving && agentSavingTarget === 'profile:' + profile._key"
                                :disabled="profileSaveDisabled(profile)"
                                @click="saveLLMProfile(profile._key)"
                              >
                                {{ t("action_save") }}
                              </QButton>
                              <QDropdownMenu
                                class="settings-llm-actions-menu"
                                :items="llmActionMenuItems(profile)"
                                hideSelected
                                hideActionLabel
                                :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                              >
                                <svg
                                  class="settings-llm-actions-menu-icon"
                                  viewBox="0 0 16 16"
                                  aria-hidden="true"
                                  focusable="false"
                                >
                                  <circle cx="3" cy="8" r="1.25" fill="currentColor" />
                                  <circle cx="8" cy="8" r="1.25" fill="currentColor" />
                                  <circle cx="13" cy="8" r="1.25" fill="currentColor" />
                                </svg>
                                <span class="settings-llm-actions-menu-accessible">{{ t("todo_action_more") }}</span>
                              </QDropdownMenu>
                            </div>
                          </div>

                          <div class="settings-profile-head">
                            <div class="settings-field settings-profile-name">
                              <span class="settings-field-label">{{ t("settings_agent_profile_name_label") }}</span>
                              <QInput
                                :modelValue="profile.name"
                                :placeholder="t('settings_agent_profile_name_placeholder')"
                                :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                                @update:modelValue="updateProfileField(profile._key, { field: 'name', value: $event })"
                              />
                            </div>
                          </div>

                          <LLMConfigForm
                            :config="profile"
                            :busy="agentLoading || agentSaving"
                            :disabledReason="agentFormDisabledReason"
                            :readOnly="agentSettingsReadOnly"
                            :envManaged="llmProfileEnvManaged(profile)"
                            :providerItems="providerItems"
                            :reasoningEffortItems="reasoningEffortItems"
                            :toolsEmulationItems="toolsEmulationItems"
                            :enableModelPicker="true"
                            :modelLookupCredentialsReady="profileModelLookupCredentialsReady(profile)"
                            :showCodexAuthAction="profileUsesCodexProvider(profile)"
                            :codexAuthDisabled="profileCodexAuthDisabled(profile)"
                            :codexAuthState="codexAuthButtonState"
                            :codexAuthTitle="codexAuthButtonTitle"
                            :showXAIAuthAction="profileUsesXAIProvider(profile)"
                            :xaiAuthState="xaiAuthButtonState"
                            :xaiAuthTitle="xaiAuthButtonTitle"
                            :showProAuthAction="profileUsesProProvider(profile)"
                            :proAuthState="proAuthButtonState"
                            :proAuthTitle="proAuthButtonTitle"
                            @update-field="updateProfileField(profile._key, $event)"
                            @open-model-picker="openModelPicker(profile._key)"
                            @open-codex-auth="openCodexAuthDialog"
                            @open-xai-auth="openXAIAuthDialog"
                            @open-pro-auth="openProAuthDialog"
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
                              class="danger plain icon settings-fallback-action"
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

          </div>

          <div v-else-if="selectedSection.id === 'channels'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
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

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <div class="settings-field is-wide">
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
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_telegram_allowed_chat_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.telegram.allowed_chat_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_telegram_allowed_chat_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateTelegramField('allowed_chat_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_telegram_allowed_chat_ids_note") }}</p>
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.telegram.group_trigger_mode || 'telegram-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.telegram.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateTelegramGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_telegram_group_trigger_note") }}</p>
                    </div>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
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

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <div class="settings-field is-wide">
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
                    </div>

                    <div class="settings-field is-wide">
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
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_allowed_team_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.slack.allowed_team_ids_text"
                        :rows="3"
                        :placeholder="t('settings_console_slack_allowed_team_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('allowed_team_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_allowed_team_ids_note") }}</p>
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_slack_allowed_channel_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.slack.allowed_channel_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_slack_allowed_channel_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateSlackField('allowed_channel_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_allowed_channel_ids_note") }}</p>
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.slack.group_trigger_mode || 'slack-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.slack.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateSlackGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_slack_group_trigger_note") }}</p>
                    </div>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_line_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_console_line_token_note") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="consoleSaving && consoleSavingTarget === 'line'"
                      :disabled="lineSaveDisabled"
                      @click="saveConsoleSettings('line')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_line_channel_access_token_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('line', 'channel_access_token')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("line", "channel_access_token") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.line.channel_access_token"
                        inputType="password"
                        :placeholder="t('settings_console_line_channel_access_token_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLineField('channel_access_token', $event)"
                      />
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_line_channel_secret_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('line', 'channel_secret')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("line", "channel_secret") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.line.channel_secret"
                        inputType="password"
                        :placeholder="t('settings_console_line_channel_secret_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLineField('channel_secret', $event)"
                      />
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_line_allowed_group_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.line.allowed_group_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_line_allowed_group_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLineField('allowed_group_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_line_allowed_group_ids_note") }}</p>
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.line.group_trigger_mode || 'line-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.line.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateLineGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_line_group_trigger_note") }}</p>
                    </div>
                  </div>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_lark_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_console_lark_token_note") }}</p>
                  </div>
                  <div class="settings-panel-actions">
                    <QButton
                      class="primary"
                      :loading="consoleSaving && consoleSavingTarget === 'lark'"
                      :disabled="larkSaveDisabled"
                      @click="saveConsoleSettings('lark')"
                    >
                      {{ t("action_save") }}
                    </QButton>
                  </div>
                </header>

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_lark_app_id_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('lark', 'app_id')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("lark", "app_id") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.lark.app_id"
                        :placeholder="t('settings_console_lark_app_id_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLarkField('app_id', $event)"
                      />
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_lark_app_secret_label") }}</span>
                      <div v-if="consoleFieldEnvManaged('lark', 'app_secret')" class="settings-env-managed">
                        <code class="settings-env-managed-env">{{ consoleFieldManagedHeadline("lark", "app_secret") }}</code>
                        <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
                      </div>
                      <QInput
                        v-else
                        :modelValue="state.lark.app_secret"
                        inputType="password"
                        :placeholder="t('settings_console_lark_app_secret_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLarkField('app_secret', $event)"
                      />
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_lark_allowed_chat_ids_label") }}</span>
                      <QTextarea
                        :modelValue="state.lark.allowed_chat_ids_text"
                        :rows="4"
                        :placeholder="t('settings_console_lark_allowed_chat_ids_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateLarkField('allowed_chat_ids_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_lark_allowed_chat_ids_note") }}</p>
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_group_trigger_label") }}</span>
                      <QDropdownMenu
                        :key="state.lark.group_trigger_mode || 'lark-group-trigger'"
                        :items="groupTriggerItems"
                        :initialItem="groupTriggerItems.find((item) => item.value === state.lark.group_trigger_mode) || groupTriggerItems[0]"
                        @change="updateLarkGroupTrigger"
                      />
                      <p class="settings-field-note">{{ t("settings_console_lark_group_trigger_note") }}</p>
                    </div>
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
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_console_guard_title") }}</h3>
                    <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
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

                <div class="settings-panel-body">
                  <div class="settings-form-grid">
                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_console_guard_allowed_url_prefixes_label") }}</span>
                      <QTextarea
                        :modelValue="state.guard.url_fetch_allowed_url_prefixes_text"
                        :rows="4"
                        :placeholder="t('settings_console_guard_allowed_url_prefixes_placeholder')"
                        :disabled="consoleLoading || consoleSaving"
                        @update:modelValue="updateGuardField('url_fetch_allowed_url_prefixes_text', $event)"
                      />
                      <p class="settings-field-note">{{ t("settings_console_guard_allowed_url_prefixes_note") }}</p>
                    </div>
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
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_skills_title") }}</h3>
                    <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
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

                <QFence
                  v-if="skillsValidationVisible && skillsValidationError"
                  type="danger"
                  icon="QIconCloseCircle"
                  :text="skillsValidationError"
                />

                <QFence
                  v-if="agentSettingsReadOnly"
                  type="warning"
                  :text="agentSettingsReadOnlyMessage"
                />

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

                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-skill-list-shell">
                <header class="settings-skill-list-head">
                  <h3 class="settings-skill-list-title">{{ t("settings_skills_loaded_title") }}</h3>
                  <span class="settings-skill-list-count">{{ formatSkillCount(displayedLoadedSkills.length) }}</span>
                </header>
                <p v-if="!displayedLoadedSkills.length" class="settings-skill-empty">{{ t("settings_skills_loaded_empty") }}</p>
                <div v-else class="settings-skill-grid">
                  <article v-for="skill in displayedLoadedSkills" :key="'loaded-' + (skill.id || skill.name)" class="settings-skill-card">
                    <div class="settings-skill-card-head">
                      <div class="settings-skill-card-copy">
                        <strong class="settings-skill-card-title">{{ skill.name || skill.id }}</strong>
                        <code v-if="skill.id && skill.id !== skill.name" class="settings-skill-card-id">{{ skill.id }}</code>
                      </div>
                      <QSwitch
                        :modelValue="true"
                        :aria-label="t('settings_skills_disable_action')"
                        :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                        @update:modelValue="setSkillLoaded(skill, $event)"
                      />
                    </div>
                    <p class="settings-skill-card-desc">{{ skill.description || t("settings_skills_description_empty") }}</p>
                  </article>
                </div>
              </div>
            </QCard>

            <QCard variant="default">
              <div class="settings-skill-list-shell">
                <header class="settings-skill-list-head">
                  <h3 class="settings-skill-list-title">{{ t("settings_skills_available_title") }}</h3>
                  <span class="settings-skill-list-count">{{ formatSkillCount(displayedAvailableSkills.length) }}</span>
                </header>
                <p v-if="!displayedAvailableSkills.length" class="settings-skill-empty">{{ t("settings_skills_available_empty") }}</p>
                <div v-else class="settings-skill-grid">
                  <article v-for="skill in displayedAvailableSkills" :key="'available-' + (skill.id || skill.name)" class="settings-skill-card">
                    <div class="settings-skill-card-head">
                      <div class="settings-skill-card-copy">
                        <strong class="settings-skill-card-title">{{ skill.name || skill.id }}</strong>
                        <code v-if="skill.id && skill.id !== skill.name" class="settings-skill-card-id">{{ skill.id }}</code>
                      </div>
                      <QSwitch
                        :modelValue="false"
                        :aria-label="t('settings_skills_enable_action')"
                        :disabled="agentLoading || agentSaving || agentSettingsReadOnly"
                        @update:modelValue="setSkillLoaded(skill, $event)"
                      />
                    </div>
                    <p class="settings-skill-card-desc">{{ skill.description || t("settings_skills_description_empty") }}</p>
                  </article>
                </div>
              </div>
            </QCard>
          </div>

          <div v-else-if="selectedSection.id === 'persona'" class="settings-panel-body settings-panel-body-plain">
            <QProgress v-if="personaLoading" :infinite="true" />

            <QCard variant="default">
              <div class="settings-panel-shell settings-persona-card">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_persona_title") }}</h3>
                    <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
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

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_persona_identity_name_label") }}</span>
                      <QInput
                        v-model="state.persona.name"
                        :placeholder="t('settings_persona_identity_name_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </div>

                    <div class="settings-field">
                      <span class="settings-field-label">{{ t("settings_persona_identity_emoji_label") }}</span>
                      <QInput
                        v-model="state.persona.emoji"
                        :placeholder="t('settings_persona_identity_emoji_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </div>

                    <div class="settings-field">
                      <span class="settings-field-label">{{ t("settings_persona_identity_creature_label") }}</span>
                      <QInput
                        v-model="state.persona.creature"
                        :placeholder="t('settings_persona_identity_creature_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </div>

                    <div class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_persona_identity_vibe_label") }}</span>
                      <QTextarea
                        v-model="state.persona.vibe"
                        :rows="4"
                        :placeholder="t('settings_persona_identity_vibe_placeholder')"
                        :disabled="personaLoading || personaSaving"
                      />
                    </div>

                    <div class="settings-field is-wide settings-persona-soul-field">
                      <div class="settings-persona-soul-label">
                        <span class="settings-field-label">{{ t("settings_persona_soul_title") }}</span>
                        <span class="settings-panel-meta">{{ personaEditorMeta }}</span>
                      </div>
                      <div class="settings-persona-soul-editor">
                        <AppMarkdownEditor
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

          <RuntimePanel v-else-if="selectedSection.id === 'runtime'" class="settings-runtime-panel" />

          <SettingsCreditsPanel v-else-if="selectedSection.id === 'credits'" />

          <div v-else-if="selectedSection.id === 'console'" class="settings-panel-body settings-panel-body-plain">
            <QCard variant="default">
              <div class="settings-panel-shell">
                <header class="settings-panel-head">
                  <div class="settings-panel-copy">
                    <h3 class="settings-panel-title workspace-document-title">{{ selectedSection.title }}</h3>
                    <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
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
                    <h3 class="settings-panel-title workspace-document-title">{{ t("settings_auto_update_card_title") }}</h3>
                    <p class="settings-panel-meta">{{ t("settings_auto_update_card_hint") }}</p>
                  </div>
                </header>

                <div class="settings-panel-body">
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
                  <h3 class="settings-panel-title workspace-document-title">{{ selectedSection.title }}</h3>
                  <p class="settings-panel-meta">{{ selectedSection.meta }}</p>
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

              <QFence
                v-if="activeSaveKind === 'agent' && agentValidationVisible && agentValidationError"
                type="danger"
                icon="QIconCloseCircle"
                :text="agentValidationError"
              />

              <QFence
                v-if="activeSaveKind === 'agent' && agentSettingsReadOnly"
                type="warning"
                :text="agentSettingsReadOnlyMessage"
              />

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
      <XAIAuthDialog
        v-model="xaiAuthDialogOpen"
        v-model:setDefault="xaiSetDefault"
        :loading="xaiAuthLoading"
        :busy="xaiAuthBusy"
        :error="xaiAuthError"
        :status="xaiAuthStatus"
        :summary="xaiAuthSummary"
        :loginSession="xaiLoginSession"
        :verificationURL="xaiLoginVerificationURL"
        :userCode="xaiLoginUserCode"
        :loginExpiresLabel="xaiLoginExpiresLabel"
        @login="reloginXAIAuth"
        @logout="logoutXAIAuth"
      />
      <ProAuthDialog
        v-model="proAuthDialogOpen"
        :loading="proAuthLoading"
        :busy="proAuthBusy"
        :error="proAuthError"
        :status="proAuthStatus"
        :summary="proAuthSummary"
        :loginSession="proLoginSession"
        :verificationURL="proLoginVerificationURL"
        :userCode="proLoginUserCode"
        :loginExpiresLabel="proLoginExpiresLabel"
        @logout="logoutProAuth"
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
