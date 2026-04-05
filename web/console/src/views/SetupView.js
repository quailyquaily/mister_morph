import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./SetupView.css";

import MarkdownEditor from "../components/MarkdownEditor";
import SetupConnectionTestDialog from "../components/SetupConnectionTestDialog";
import SetupPickerDialog from "../components/SetupPickerDialog";
import {
  apiFetch,
  loadEndpoints,
  runtimeApiFetchForEndpoint,
  setSelectedEndpointRef,
  translate,
} from "../core/context";
import {
  hasLLMFieldValue as hasManagedLLMFieldValue,
  isLLMFieldEnvManaged as isManagedLLMField,
  llmFieldEnvName as managedLLMFieldEnvName,
  llmFieldEnvValue as managedLLMFieldEnvValue,
  llmFieldManagedDisplayValue as managedLLMFieldDisplayValue,
  llmFieldManagedHeadline as managedLLMFieldHeadline,
  llmFieldValue as managedLLMFieldValue,
} from "../core/llm-env-managed";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import {
  consoleSetupTargetEndpointRef,
  resolveConsoleSetupStage,
  setupStagePath,
} from "../core/setup";
import {
  defaultEndpointForSetupProvider,
  OPENAI_COMPATIBLE_API_BASE_OPTIONS,
  normalizeSetupProviderChoice,
  normalizeSetupProviderForSave,
  resolveSetupAPIKeyHelp,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_OPENAI_COMPATIBLE,
  SETUP_PROVIDER_OPTIONS,
  setupProviderRequiresAPIKey,
  setupProviderSupportsModelLookup,
} from "../core/setup-contract";
import { pickRandomPersonaSeed } from "../core/persona-seeds";
import { findSoulPreset, SOUL_PRESETS } from "../core/soul-presets";
import { endpointState } from "../stores";

const TOTAL_STEPS = 3;
const IDENTITY_FIELDS = ["name", "creature", "vibe", "emoji"];
const IDENTITY_YAML_FENCE_RE = /```(?:yaml|yml)\s*\n([\s\S]*?)\n```/i;
const PREVIOUS_STAGE = {
  persona: "llm",
  soul: "persona",
};
const NEXT_STAGE = {
  llm: "persona",
  persona: "soul",
  soul: "done",
};
const SETUP_STAGE_ORDER = {
  llm: 1,
  persona: 2,
  soul: 3,
  done: 4,
};

const STAGE_META = {
  llm: {
    index: 1,
    titleKey: "setup_llm_title",
    introKey: "setup_llm_intro",
    submitKey: "setup_action_continue",
    tone: "llm",
  },
  persona: {
    index: 2,
    titleKey: "setup_persona_title",
    introKey: "setup_persona_intro",
    submitKey: "setup_action_continue",
    tone: "persona",
  },
  soul: {
    index: 3,
    titleKey: "setup_soul_title",
    introKey: "setup_soul_intro",
    submitKey: "setup_action_finish",
    tone: "soul",
  },
  done: {
    index: TOTAL_STEPS,
    titleKey: "setup_done_title",
    introKey: "setup_done_intro",
    tone: "done",
    kickerKey: "setup_stage_done",
  },
};

function normalizeText(value) {
  return String(value || "").replace(/\r\n/g, "\n");
}

function normalizeSoulDocument(raw) {
  const value = normalizeText(raw).trim();
  return value ? `${value}\n` : "";
}

function buildCustomSoulDocument() {
  return normalizeSoulDocument(`# SOUL.md

## Core Truths

- 

## Boundaries

- 

## Vibe


## Continuity

`);
}

function buildDefaultPayload() {
  return {
    llm: {
      provider: SETUP_PROVIDER_OPENAI_COMPATIBLE,
      endpoint: "",
      model: "",
      api_key: "",
      cloudflare_api_token: "",
      cloudflare_account_id: "",
      reasoning_effort: "",
      tools_emulation_mode: "",
    },
    multimodal: {
      image_sources: [],
    },
    tools: {
      write_file: { enabled: true },
      spawn: { enabled: true },
      contacts_send: { enabled: true },
      todo_update: { enabled: true },
      plan_create: { enabled: true },
      url_fetch: { enabled: true },
      web_search: { enabled: true },
      bash: { enabled: true },
    },
  };
}

function buildEmptyIdentityProfile() {
  return {
    name: "",
    creature: "",
    vibe: "",
    emoji: "",
  };
}

function normalizePayload(data) {
  const base = buildDefaultPayload();
  return {
    llm: {
      ...base.llm,
      ...(data?.llm && typeof data.llm === "object" ? data.llm : {}),
    },
    multimodal: {
      ...base.multimodal,
      ...(data?.multimodal && typeof data.multimodal === "object" ? data.multimodal : {}),
    },
    tools: {
      ...base.tools,
      ...(data?.tools && typeof data.tools === "object" ? data.tools : {}),
    },
  };
}

function yamlString(value) {
  return JSON.stringify(String(value || "").trim());
}

function buildIdentityYAML(values) {
  return [
    `name: ${yamlString(values.name)}`,
    "name_alts: []",
    `creature: ${yamlString(values.creature)}`,
    `vibe: ${yamlString(values.vibe)}`,
    `emoji: ${yamlString(values.emoji)}`,
  ].join("\n");
}

function buildIdentityMarkdown(values) {
  return [
    "# IDENTITY.md - Who Am I?",
    "",
    "```yaml",
    buildIdentityYAML(values),
    "```",
    "",
    "*This isn't just metadata. It's the start of figuring out who you are.*",
    "",
  ].join("\n");
}

function parseYAMLScalar(value) {
  const raw = String(value || "").trim();
  if (!raw) {
    return "";
  }
  if (raw.startsWith("\"") && raw.endsWith("\"")) {
    try {
      return JSON.parse(raw);
    } catch {
      return raw.slice(1, -1);
    }
  }
  if (raw.startsWith("'") && raw.endsWith("'")) {
    return raw.slice(1, -1).replace(/''/g, "'");
  }
  return raw;
}

function parseIdentityProfile(raw) {
  const content = normalizeText(raw);
  const match = IDENTITY_YAML_FENCE_RE.exec(content);
  const profile = buildEmptyIdentityProfile();
  if (!match) {
    return profile;
  }
  const lines = match[1].split("\n");
  for (const line of lines) {
    const lineMatch = /^\s*(name|creature|vibe|emoji)\s*:\s*(.*)\s*$/.exec(line);
    if (!lineMatch) {
      continue;
    }
    profile[lineMatch[1]] = parseYAMLScalar(lineMatch[2]);
  }
  return profile;
}

function updateIdentityMarkdown(raw, values) {
  const content = normalizeText(raw);
  const nextYAML = buildIdentityYAML(values);
  const match = IDENTITY_YAML_FENCE_RE.exec(content);
  if (!match) {
    const trimmed = content.trim();
    if (trimmed) {
      return `${trimmed}\n\n\`\`\`yaml\n${nextYAML}\n\`\`\`\n`;
    }
    return buildIdentityMarkdown(values);
  }
  const lines = match[1].split("\n");
  const seen = new Set();
  const nextLines = lines.map((line) => {
    const lineMatch = /^(\s*)(name|creature|vibe|emoji)\s*:\s*(.*)\s*$/.exec(line);
    if (!lineMatch) {
      return line;
    }
    const key = lineMatch[2];
    seen.add(key);
    return `${lineMatch[1]}${key}: ${yamlString(values[key])}`;
  });
  for (const key of IDENTITY_FIELDS) {
    if (!seen.has(key)) {
      nextLines.push(`${key}: ${yamlString(values[key])}`);
    }
  }
  const nextFence = `\`\`\`yaml\n${nextLines.join("\n")}\n\`\`\``;
  return `${content.slice(0, match.index)}${nextFence}${content.slice(match.index + match[0].length)}`;
}

function hasSoulDocument(raw) {
  return normalizeSoulDocument(raw).trim() !== "";
}

function normalizeStage(value) {
  if (value === "persona" || value === "soul" || value === "done") {
    return value;
  }
  return "llm";
}

function setupStageIndex(stage) {
  return SETUP_STAGE_ORDER[normalizeStage(stage)] || SETUP_STAGE_ORDER.llm;
}

function resolveDoneGreetingKey(date = new Date()) {
  const hour = date.getHours();
  if (hour >= 5 && hour < 10) {
    return "setup_done_greeting_morning";
  }
  if (hour >= 10 && hour < 12) {
    return "setup_done_greeting_day";
  }
  if (hour >= 12 && hour < 19) {
    return "setup_done_greeting_afternoon";
  }
  return "setup_done_greeting_evening";
}

const SetupView = {
  components: {
    MarkdownEditor,
    SetupConnectionTestDialog,
    SetupPickerDialog,
  },
  setup() {
    const t = translate;
    const route = useRoute();
    const router = useRouter();

    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const spriteTick = ref(0);
    let spriteTimer = 0;

    const loadedPayload = ref(buildDefaultPayload());
    const loadedConfigSource = ref("defaults");
    const llmEnvManaged = ref({});
    const loadedIdentityRaw = ref("");
    const loadedSoulRaw = ref("");
    const llmForm = reactive({
      provider: SETUP_PROVIDER_OPENAI_COMPATIBLE,
      endpoint: "",
      model: "",
      api_key: "",
      cloudflare_api_token: "",
      cloudflare_account_id: "",
    });
    const personaForm = reactive(buildEmptyIdentityProfile());
    const soulSelectionContent = ref("");
    const soulEditorDraft = ref(buildCustomSoulDocument());
    const soulPresetId = ref("");
    const soulSelectionKind = ref("");
    const soulEditMode = ref(false);
    const modelPickerOpen = ref(false);
    const modelPickerLoading = ref(false);
    const modelPickerError = ref("");
    const modelPickerItems = ref([]);
    const personaNameInput = ref(null);
    const apiBasePickerOpen = ref(false);
    const testConnectionOpen = ref(false);
    const testConnectionLoading = ref(false);
    const testConnectionError = ref("");
    const testConnectionBenchmarks = ref([]);
    const testConnectionMeta = reactive({
      provider: "",
      apiBase: "",
      model: "",
    });

    const routeStage = computed(() => normalizeStage(route.meta?.setupStage));
    const repairKey = computed(() => String(route.query?.repair || "").trim());
    const inRepairMode = computed(() => repairKey.value !== "");
    const stageMeta = computed(() => STAGE_META[routeStage.value] || STAGE_META.llm);
    const setupName = computed(() => String(personaForm.name || "").trim() || t("setup_done_name_fallback"));
    const stageTitle = computed(() =>
      t(stageMeta.value.titleKey, {
        name: setupName.value,
      })
    );
    const stageIntro = computed(() =>
      String(
        t(stageMeta.value.introKey, {
          name: setupName.value,
          greeting: t(resolveDoneGreetingKey()),
        })
      ).trim()
    );
    const providerItems = computed(() => SETUP_PROVIDER_OPTIONS);
    const providerItem = computed(
      () => providerItems.value.find((item) => item.value === llmForm.provider) || null
    );
    const showCloudflareAccountField = computed(
      () => normalizeSetupProviderChoice(llmFieldValue("provider")) === SETUP_PROVIDER_CLOUDFLARE
    );
    const credentialFieldName = computed(() => (showCloudflareAccountField.value ? "cloudflare_api_token" : "api_key"));
    const credentialLabelKey = computed(() =>
      showCloudflareAccountField.value ? "settings_agent_cloudflare_api_token_label" : "settings_agent_api_key_label"
    );
    const credentialPlaceholderKey = computed(() =>
      showCloudflareAccountField.value ? "settings_agent_cloudflare_api_token_placeholder" : "settings_agent_api_key_placeholder"
    );
    const credentialHintKey = computed(() =>
      showCloudflareAccountField.value ? "setup_llm_api_token_hint" : "setup_llm_api_key_hint"
    );
    const credentialHintPlainKey = computed(() =>
      showCloudflareAccountField.value ? "setup_llm_api_token_hint_plain" : "setup_llm_api_key_hint_plain"
    );
    const showOpenAICompatibleHelpers = computed(() => setupProviderSupportsModelLookup(llmFieldValue("provider")));
    const modelLookupDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        !showOpenAICompatibleHelpers.value ||
        !hasLLMFieldValue("api_key")
    );
    const apiBasePickerItems = computed(() =>
      OPENAI_COMPATIBLE_API_BASE_OPTIONS.map((item) => ({
        id: item.id,
        title: item.title,
        value: item.baseURL,
        note: "",
      }))
    );
    const credentialHelp = computed(() => {
      const provider = llmFieldValue("provider");
      if (provider === "" || isLLMFieldEnvManaged(credentialFieldName.value)) {
        return null;
      }
      return resolveSetupAPIKeyHelp(provider, llmFieldValue("endpoint"));
    });
    const credentialHelpParts = computed(() => {
      if (!credentialHelp.value) {
        return null;
      }
      const marker = "__PROVIDER__";
      const template = String(t(credentialHintKey.value, { provider: marker }) || "");
      const index = template.indexOf(marker);
      if (index === -1) {
        return {
          before: template.trim(),
          after: "",
        };
      }
      return {
        before: template.slice(0, index),
        after: template.slice(index + marker.length),
      };
    });
    const previousStage = computed(() => PREVIOUS_STAGE[routeStage.value] || "");
    const showPrevious = computed(() => !inRepairMode.value && previousStage.value !== "");
    const stageKicker = computed(
      () =>
        `[[ ${t("setup_title")} // ${
          stageMeta.value.kickerKey
            ? t(stageMeta.value.kickerKey)
            : t("setup_stage_short", { current: stageMeta.value.index })
        } ]]`
    );
    const screenClass = computed(() => ["setup-screen", `is-${stageMeta.value.tone}`]);
    const llmSaveDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        !hasLLMFieldValue("provider") ||
        !hasLLMFieldValue("model") ||
        !hasLLMFieldValue(credentialFieldName.value) ||
        (showCloudflareAccountField.value && !hasLLMFieldValue("cloudflare_account_id"))
    );
    const testConnectionDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        testConnectionLoading.value ||
        !hasLLMFieldValue("provider") ||
        !hasLLMFieldValue("model") ||
        (setupProviderRequiresAPIKey(llmFieldValue("provider")) && !hasLLMFieldValue(credentialFieldName.value)) ||
        (showCloudflareAccountField.value && !hasLLMFieldValue("cloudflare_api_token")) ||
        (showCloudflareAccountField.value && !hasLLMFieldValue("cloudflare_account_id"))
    );
    const personaSaveDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        String(personaForm.name || "").trim() === ""
    );
    const soulSaveDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        (soulEditMode.value
          ? normalizeSoulDocument(soulEditorDraft.value).trim() === ""
          : String(soulPresetId.value || "").trim() === "")
    );
    const progressSteps = computed(() =>
      Array.from({ length: TOTAL_STEPS }, (_, index) => ({
        index: index + 1,
        active: index + 1 <= stageMeta.value.index,
      }))
    );
    const soulPresetCards = computed(() =>
      SOUL_PRESETS.map((item, index) => ({
        ...item,
        indexLabel: String(index + 1).padStart(2, "0"),
        stackIndex: index,
        title: t(item.titleKey),
        note: t(item.noteKey),
      }))
    );
    const hasSoulSelection = computed(() => soulSelectionKind.value === "preset" || soulSelectionKind.value === "custom");
    const isCustomSoulSelected = computed(() => soulSelectionKind.value === "custom");
    const soulDocumentExists = computed(() => hasSoulDocument(loadedSoulRaw.value));
    const customSoulCardIcon = computed(() => (soulDocumentExists.value ? "QIconCpuChip" : "QIconPlus"));
    const selectedSoulCard = computed(() => {
      if (soulSelectionKind.value === "preset") {
        return soulPresetCards.value.find((item) => item.id === soulPresetId.value) || null;
      }
      if (isCustomSoulSelected.value) {
        return {
          id: "custom",
          icon: soulDocumentExists.value ? "QIconCpuChip" : "QIconPlus",
          title: soulDocumentExists.value ? t("setup_soul_existing_title") : t("setup_soul_custom_title"),
          note: "",
        };
      }
      return null;
    });
    const selectedSoulSpriteStageStyle = computed(() => {
      if (!selectedSoulCard.value?.spriteSrc) {
        return null;
      }
      const frameWidth = Number(selectedSoulCard.value.spriteFrameWidth) || 16;
      const frameHeight = Number(selectedSoulCard.value.spriteFrameHeight) || 16;
      const scale = Number(selectedSoulCard.value.spriteScale) || 5;
      return {
        width: `${frameWidth * scale}px`,
        height: `${frameHeight * scale}px`,
      };
    });
    const selectedSoulSpriteStyle = computed(() => {
      if (!selectedSoulCard.value?.spriteSrc) {
        return null;
      }
      const frameWidth = Number(selectedSoulCard.value.spriteFrameWidth) || 16;
      const frameHeight = Number(selectedSoulCard.value.spriteFrameHeight) || 16;
      const scale = Number(selectedSoulCard.value.spriteScale) || 5;
      const frame = spriteTick.value % Math.max(Number(selectedSoulCard.value.spriteFrames) || 1, 1);
      return {
        width: `${frameWidth}px`,
        height: `${frameHeight}px`,
        backgroundImage: `url(${selectedSoulCard.value.spriteSrc})`,
        backgroundPosition: `${-frame * frameWidth}px 0px`,
        backgroundRepeat: "no-repeat",
        "--sprite-scale": String(scale),
      };
    });
    const soulSaveVisible = computed(() => soulEditMode.value || soulSelectionKind.value === "preset");
    const doneStatusItems = computed(() => [
      {
        stage: "llm",
        icon: "QIconSettings",
        key: t("settings_agent_provider_label"),
        value: t("setup_done_status_ready"),
        action: t("setup_action_edit_llm"),
      },
      {
        stage: "persona",
        icon: "QIconUsers",
        key: t("setup_identity_title"),
        value: t("setup_done_status_ready"),
        action: t("setup_action_edit_persona"),
      },
      {
        stage: "soul",
        icon: "QIconEcosystem",
        key: t("setup_soul_editor_label"),
        value: t("setup_done_status_ready"),
        action: t("setup_action_edit_soul"),
      },
    ]);
    const soulUsesCustomContent = computed(() => soulDocumentExists.value);

    async function enterChat() {
      const setupState = await resolveConsoleSetupStage(endpointState.items);
      const targetRef = consoleSetupTargetEndpointRef(setupState.setup) || CONSOLE_LOCAL_ENDPOINT_REF;
      if (targetRef) {
        setSelectedEndpointRef(targetRef);
      }
      await router.replace("/chat");
    }

    function applyPersonaContent(raw) {
      loadedIdentityRaw.value = normalizeText(raw);
      const parsed = parseIdentityProfile(loadedIdentityRaw.value);
      personaForm.name = parsed.name;
      personaForm.creature = parsed.creature;
      personaForm.vibe = parsed.vibe;
      personaForm.emoji = parsed.emoji;
    }

    function applySoulContent(raw) {
      const next = normalizeSoulDocument(raw);
      loadedSoulRaw.value = next;
      soulSelectionContent.value = next;
      soulEditorDraft.value = next || buildCustomSoulDocument();
      soulPresetId.value = "";
      soulSelectionKind.value = hasSoulDocument(next) ? "custom" : "";
      soulEditMode.value = false;
    }

    function applyLLMPayload(data) {
      const normalized = normalizePayload(data);
      const envManagedPayload = data?.env_managed && typeof data.env_managed === "object" ? data.env_managed : {};
      const llmEnvManagedPayload =
        envManagedPayload?.llm && typeof envManagedPayload.llm === "object" ? envManagedPayload.llm : {};
      loadedPayload.value = normalized;
      loadedConfigSource.value = String(data?.config_source || "defaults").trim() || "defaults";
      llmEnvManaged.value = llmEnvManagedPayload;
      if (loadedConfigSource.value !== "config") {
        llmForm.provider = SETUP_PROVIDER_OPENAI_COMPATIBLE;
        llmForm.endpoint = "";
        llmForm.model = "";
        llmForm.api_key = "";
        llmForm.cloudflare_api_token = "";
        llmForm.cloudflare_account_id = "";
        return;
      }
      llmForm.provider = normalizeSetupProviderChoice(normalized.llm.provider, { allowEmpty: true });
      llmForm.endpoint = String(normalized.llm.endpoint || "").trim();
      llmForm.model = String(normalized.llm.model || "").trim();
      llmForm.api_key = String(normalized.llm.api_key || "").trim();
      llmForm.cloudflare_api_token = String(normalized.llm.cloudflare_api_token || "").trim();
      llmForm.cloudflare_account_id = String(normalized.llm.cloudflare_account_id || "").trim();
    }

    function llmFieldEnvName(field) {
      return managedLLMFieldEnvName(llmEnvManaged.value, field);
    }

    function llmFieldEnvValue(field) {
      return managedLLMFieldEnvValue(llmEnvManaged.value, field);
    }

    function isLLMFieldEnvManaged(field) {
      return isManagedLLMField(llmEnvManaged.value, field);
    }

    function llmFieldValue(field) {
      return managedLLMFieldValue(llmForm, llmEnvManaged.value, field);
    }

    function hasLLMFieldValue(field) {
      return hasManagedLLMFieldValue(llmForm, llmEnvManaged.value, field);
    }

    function llmFieldManagedDisplayValue(field) {
      return managedLLMFieldDisplayValue(llmForm, llmEnvManaged.value, field);
    }

    function llmFieldManagedHeadline(field) {
      return managedLLMFieldHeadline(llmForm, llmEnvManaged.value, field);
    }

    async function loadLLMForm() {
      loading.value = true;
      err.value = "";
      try {
        const payload = await apiFetch("/settings/agent");
        applyLLMPayload(payload);
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function loadPersonaForm() {
      loading.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/IDENTITY.md");
        applyPersonaContent(data?.content || "");
      } catch (e) {
        if (e?.status === 404) {
          applyPersonaContent("");
          return;
        }
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function loadSoulForm() {
      loading.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/SOUL.md");
        applySoulContent(data?.content || "");
      } catch (e) {
        if (e?.status === 404) {
          applySoulContent("");
          return;
        }
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function loadStageForm(stage) {
      if (stage === "llm") {
        await loadLLMForm();
        return;
      }
      if (stage === "persona") {
        await loadPersonaForm();
        return;
      }
      if (stage === "soul") {
        await loadSoulForm();
        return;
      }
      if (stage === "done") {
        await loadPersonaForm();
        await loadSoulForm();
      }
    }

    async function syncRoute(options = {}) {
      if (inRepairMode.value) {
        if (options.loadStage !== false) {
          await loadStageForm(routeStage.value);
        }
        return false;
      }
      if (options.refreshEndpoints !== false) {
        await loadEndpoints();
      }
      const setupState = await resolveConsoleSetupStage(endpointState.items);
      if (route.path === "/setup") {
        const target =
          setupState.stage === "ready"
            ? options.onReady === "done"
              ? "/setup/done"
              : "/setup/done"
            : setupStagePath(setupState.stage);
        await router.replace({ path: target, query: route.query });
        return false;
      }
      if (setupState.stage !== "ready") {
        const targetPath = setupStagePath(setupState.stage);
        const canStayOnCurrentSetupPath =
          options.allowPrevious === true &&
          route.path !== "/setup" &&
          route.path.startsWith("/setup/") &&
          setupStageIndex(routeStage.value) <= setupStageIndex(setupState.stage);
        if (!canStayOnCurrentSetupPath && route.path !== targetPath) {
          await router.replace({ path: targetPath, query: route.query });
          return false;
        }
      }
      if (options.onReady === "done" && route.path !== "/setup/done") {
        if (setupState.stage === "ready") {
          await router.replace({ path: "/setup/done", query: route.query });
          return false;
        }
      }
      if (options.loadStage !== false) {
        await loadStageForm(routeStage.value);
      }
      return false;
    }

    async function finishStep() {
      if (inRepairMode.value) {
        await router.replace({ path: "/setup", query: {} });
        return;
      }
      const nextStage = NEXT_STAGE[routeStage.value];
      if (nextStage) {
        await router.replace({ path: setupStagePath(nextStage), query: route.query });
        return;
      }
      await syncRoute({ refreshEndpoints: true, loadStage: false, onReady: "done" });
    }

    async function saveLLM() {
      if (llmSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const llm = buildLLMSettingsPayload();
        if (Object.keys(llm).length === 0) {
          await finishStep();
          return;
        }
        const payload = await apiFetch("/settings/agent", {
          method: "PUT",
          body: {
            llm,
            multimodal: loadedPayload.value.multimodal,
            tools: loadedPayload.value.tools,
          },
        });
        applyLLMPayload(payload);
        await finishStep();
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    function buildLLMSettingsPayload() {
      const payload = {};
      const provider = normalizeSetupProviderChoice(llmFieldValue("provider"), { allowEmpty: true });
      const useCloudflareCredentials = normalizeSetupProviderChoice(llmForm.provider) === SETUP_PROVIDER_CLOUDFLARE;
      if (!isLLMFieldEnvManaged("provider")) {
        payload.provider = normalizeSetupProviderForSave(llmForm.provider, llmForm.endpoint);
      }
      if (!isLLMFieldEnvManaged("endpoint")) {
        payload.endpoint = String(llmForm.endpoint || "").trim();
      }
      if (!isLLMFieldEnvManaged("model")) {
        payload.model = String(llmForm.model || "").trim();
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        if (!isLLMFieldEnvManaged("cloudflare_api_token")) {
          payload.cloudflare_api_token = useCloudflareCredentials ? String(llmForm.cloudflare_api_token || "").trim() : "";
        }
        if (!isLLMFieldEnvManaged("cloudflare_account_id")) {
          payload.cloudflare_account_id = String(llmForm.cloudflare_account_id || "").trim();
        }
      } else if (!isLLMFieldEnvManaged("api_key")) {
        payload.api_key = String(llmForm.api_key || "").trim();
      }
      return payload;
    }

    function buildLLMTestPayload() {
      const payload = {};
      const provider = normalizeSetupProviderChoice(llmFieldValue("provider"), { allowEmpty: true });
      if (!isLLMFieldEnvManaged("provider") && provider !== "") {
        payload.provider = normalizeSetupProviderForSave(llmForm.provider, llmForm.endpoint);
      }
      if (!isLLMFieldEnvManaged("endpoint")) {
        const endpoint = String(llmForm.endpoint || "").trim();
        if (endpoint !== "") {
          payload.endpoint = endpoint;
        }
      }
      if (!isLLMFieldEnvManaged("model")) {
        const model = String(llmForm.model || "").trim();
        if (model !== "") {
          payload.model = model;
        }
      }
      if (provider === SETUP_PROVIDER_CLOUDFLARE) {
        if (!isLLMFieldEnvManaged("cloudflare_api_token")) {
          const token = String(llmForm.cloudflare_api_token || "").trim();
          if (token !== "") {
            payload.cloudflare_api_token = token;
          }
        }
        if (!isLLMFieldEnvManaged("cloudflare_account_id")) {
          const accountID = String(llmForm.cloudflare_account_id || "").trim();
          if (accountID !== "") {
            payload.cloudflare_account_id = accountID;
          }
        }
      } else if (!isLLMFieldEnvManaged("api_key")) {
        const apiKey = String(llmForm.api_key || "").trim();
        if (apiKey !== "") {
          payload.api_key = apiKey;
        }
      }
      return payload;
    }

    async function savePersona() {
      if (personaSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const content = inRepairMode.value ? buildIdentityMarkdown(personaForm) : updateIdentityMarkdown(loadedIdentityRaw.value, personaForm);
        loadedIdentityRaw.value = content;
        await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/IDENTITY.md", {
          method: "PUT",
          body: {
            content,
          },
        });
        await finishStep();
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    async function fillRandomPersona() {
      if (loading.value || saving.value) {
        return;
      }
      const seed = pickRandomPersonaSeed();
      personaForm.name = seed.name;
      personaForm.emoji = seed.emoji;
      personaForm.creature = seed.creature;
      personaForm.vibe = seed.vibe;
      await focusPersonaNameField();
    }

    async function saveSoul() {
      if (soulSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const source = soulEditMode.value ? soulEditorDraft.value : soulSelectionContent.value;
        const content = normalizeSoulDocument(source);
        loadedSoulRaw.value = content;
        soulSelectionContent.value = content;
        soulEditorDraft.value = content;
        soulPresetId.value = "";
        soulSelectionKind.value = hasSoulDocument(content) ? "custom" : "";
        soulEditMode.value = false;
        await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/SOUL.md", {
          method: "PUT",
          body: {
            content,
          },
        });
        await finishStep();
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    function onProviderChange(item) {
      const nextProvider = String(item?.value || "").trim() || providerItems.value[0].value;
      llmForm.provider = nextProvider;
    }

    function openExternal(url) {
      const target = String(url || "").trim();
      if (!target) {
        return;
      }
      window.open(target, "_blank", "noopener,noreferrer");
    }

    function openAPIBasePicker() {
      if (!showOpenAICompatibleHelpers.value || loading.value || saving.value) {
        return;
      }
      apiBasePickerOpen.value = true;
    }

    function applyAPIBaseOption(item) {
      llmForm.endpoint = String(item?.value || "").trim();
    }

    async function openModelPicker() {
      if (modelLookupDisabled.value) {
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
            endpoint: llmFieldValue("endpoint"),
            api_key: llmFieldValue("api_key"),
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
      llmForm.model = String(item?.value || "").trim();
    }

    function primeConnectionTestState(nextPayload = null) {
      const payload = nextPayload || buildLLMTestPayload();
      const targetProviderChoice = normalizeSetupProviderChoice(llmFieldValue("provider"), { allowEmpty: true });
      const targetEndpoint = llmFieldValue("endpoint");
      const targetModel = llmFieldValue("model");
      testConnectionError.value = "";
      testConnectionBenchmarks.value = [];
      testConnectionMeta.provider = normalizeSetupProviderForSave(targetProviderChoice, targetEndpoint);
      testConnectionMeta.apiBase = String(targetEndpoint || "").trim() || defaultEndpointForSetupProvider(targetProviderChoice);
      testConnectionMeta.model = String(targetModel || "").trim() || String(payload.model || "").trim();
      return payload;
    }

    async function openTestConnection() {
      if (testConnectionDisabled.value) {
        return;
      }
      primeConnectionTestState();
      testConnectionOpen.value = true;
      await runConnectionTest();
    }

    async function runConnectionTest() {
      if (testConnectionLoading.value) {
        return;
      }
      const nextPayload = primeConnectionTestState(buildLLMTestPayload());
      testConnectionLoading.value = true;
      try {
        const payload = await apiFetch("/settings/agent/test", {
          method: "POST",
          body: {
            llm: nextPayload,
          },
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

    function applySoulPreset(id) {
      const preset = findSoulPreset(id);
      soulSelectionKind.value = "preset";
      soulPresetId.value = preset.id;
      soulSelectionContent.value = preset.content;
    }

    function selectCustomSoul() {
      const customSource =
        soulUsesCustomContent.value
          ? loadedSoulRaw.value
          : isCustomSoulSelected.value && normalizeSoulDocument(soulSelectionContent.value).trim() !== ""
            ? soulSelectionContent.value
            : buildCustomSoulDocument();
      soulSelectionKind.value = "custom";
      soulPresetId.value = "";
      soulSelectionContent.value = normalizeSoulDocument(customSource) || buildCustomSoulDocument();
    }

    function openSoulEditor() {
      const source =
        normalizeSoulDocument(soulSelectionContent.value).trim() !== ""
          ? soulSelectionContent.value
          : buildCustomSoulDocument();
      soulSelectionContent.value = source;
      soulEditorDraft.value = source;
      soulEditMode.value = true;
    }

    function cancelSoulEditor() {
      soulEditorDraft.value = soulSelectionContent.value;
      soulEditMode.value = false;
    }

    function goToStage(stage) {
      void router.push({ path: setupStagePath(stage), query: route.query });
    }

    function goPrevious() {
      if (previousStage.value) {
        goToStage(previousStage.value);
      }
    }

    async function focusPersonaNameField() {
      if (routeStage.value !== "persona") {
        return;
      }
      await nextTick();
      const target = personaNameInput.value;
      if (!target) {
        return;
      }
      if (typeof target.focus === "function") {
        target.focus();
        return;
      }
      const el = target?.$el || target;
      const input = el?.querySelector?.("input, textarea");
      if (typeof input?.focus === "function") {
        input.focus();
      }
    }

    watch(
      () => route.fullPath,
      () => {
        void syncRoute({ refreshEndpoints: true, loadStage: true, allowPrevious: true });
      },
      { immediate: true }
    );

    watch(
      routeStage,
      (stage) => {
        if (stage === "persona") {
          void focusPersonaNameField();
        }
      },
      { immediate: true }
    );

    onMounted(() => {
      spriteTimer = window.setInterval(() => {
        spriteTick.value = (spriteTick.value + 1) % 240;
      }, 220);
    });

    onBeforeUnmount(() => {
      if (spriteTimer) {
        window.clearInterval(spriteTimer);
      }
    });

    return {
      t,
      routeStage,
      stageMeta,
      stageTitle,
      stageIntro,
      stageKicker,
      screenClass,
      progressSteps,
      err,
      loading,
      saving,
      llmForm,
      personaForm,
      soulSelectionContent,
      soulEditorDraft,
      soulPresetId,
      soulSelectionKind,
      soulEditMode,
      soulUsesCustomContent,
      soulPresetCards,
      hasSoulSelection,
      selectedSoulCard,
      selectedSoulSpriteStageStyle,
      selectedSoulSpriteStyle,
      isCustomSoulSelected,
      customSoulCardIcon,
      soulSaveVisible,
      doneStatusItems,
      providerItems,
      providerItem,
      llmEnvManaged,
      showCloudflareAccountField,
      showOpenAICompatibleHelpers,
      modelLookupDisabled,
      apiBasePickerItems,
      credentialLabelKey,
      credentialPlaceholderKey,
      credentialHelp,
      credentialHelpParts,
      credentialHintPlainKey,
      llmFieldEnvName,
      llmFieldEnvValue,
      isLLMFieldEnvManaged,
      llmFieldValue,
      hasLLMFieldValue,
      llmFieldManagedDisplayValue,
      llmFieldManagedHeadline,
      previousStage,
      showPrevious,
      llmSaveDisabled,
      testConnectionDisabled,
      personaSaveDisabled,
      soulSaveDisabled,
      onProviderChange,
      applySoulPreset,
      selectCustomSoul,
      openSoulEditor,
      cancelSoulEditor,
      goPrevious,
      goToStage,
      enterChat,
      saveLLM,
      savePersona,
      saveSoul,
      fillRandomPersona,
      openExternal,
      openAPIBasePicker,
      applyAPIBaseOption,
      openModelPicker,
      applyModelOption,
      openTestConnection,
      runConnectionTest,
      testConnectionOpen,
      testConnectionLoading,
      testConnectionError,
      testConnectionBenchmarks,
      testConnectionMeta,
      modelPickerOpen,
      modelPickerLoading,
      modelPickerError,
      modelPickerItems,
      apiBasePickerOpen,
      personaNameInput,
    };
  },
  template: `
    <section :class="screenClass">
      <QCard class="setup-shell stat-item" variant="default">
        <header class="setup-head">
          <p class="ui-kicker setup-step">{{ stageKicker }}</p>
          <div class="setup-progress" aria-hidden="true">
            <span
              v-for="item in progressSteps"
              :key="item.index"
              :class="['setup-progress-segment', { 'is-active': item.active }]"
            ></span>
          </div>
          <h1 class="setup-title">{{ stageTitle }}</h1>
          <p v-if="stageIntro" class="setup-copy">{{ stageIntro }}</p>
        </header>

        <form
          v-if="routeStage === 'llm'"
          class="setup-form setup-form-llm"
          @submit.prevent="saveLLM"
        >
          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_provider_label") }}</span>
            <div v-if="isLLMFieldEnvManaged('provider')" class="setup-env-managed">
              <code class="setup-env-managed-env">{{ llmFieldManagedHeadline("provider") }}</code>
              <p class="setup-env-managed-body">{{ t("settings_env_managed_body") }}</p>
            </div>
            <QDropdownMenu
              v-else
              :key="llmForm.provider || 'provider'"
              :items="providerItems"
              :initialItem="providerItem"
              :placeholder="t('settings_agent_provider_placeholder')"
              :disabled="loading || saving"
              @change="onProviderChange"
            />
          </label>

          <label v-if="!showCloudflareAccountField" class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_endpoint_label") }}</span>
            <div v-if="isLLMFieldEnvManaged('endpoint')" class="setup-env-managed">
              <code class="setup-env-managed-env">{{ llmFieldManagedHeadline("endpoint") }}</code>
              <p class="setup-env-managed-body">{{ t("settings_env_managed_body") }}</p>
            </div>
            <div v-else class="setup-field-control">
              <QInput
                v-model="llmForm.endpoint"
                :placeholder="t('settings_agent_endpoint_placeholder')"
                :disabled="loading || saving"
              />
              <QButton
                type="button"
                class="outlined icon setup-field-action"
                :title="t('setup_llm_api_base_picker_title')"
                :aria-label="t('setup_llm_api_base_picker_title')"
                :disabled="!showOpenAICompatibleHelpers || loading || saving"
                @click.prevent="openAPIBasePicker"
              >
                <QIconLink class="icon" />
              </QButton>
            </div>
          </label>

          <label v-if="showCloudflareAccountField" class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_cloudflare_account_label") }}</span>
            <div v-if="isLLMFieldEnvManaged('cloudflare_account_id')" class="setup-env-managed">
              <code class="setup-env-managed-env">{{ llmFieldManagedHeadline("cloudflare_account_id") }}</code>
              <p class="setup-env-managed-body">{{ t("settings_env_managed_body") }}</p>
            </div>
            <QInput
              v-else
              v-model="llmForm.cloudflare_account_id"
              :placeholder="t('settings_agent_cloudflare_account_placeholder')"
              :disabled="loading || saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t(credentialLabelKey) }}</span>
            <div v-if="showCloudflareAccountField ? isLLMFieldEnvManaged('cloudflare_api_token') : isLLMFieldEnvManaged('api_key')" class="setup-env-managed">
              <code class="setup-env-managed-env">{{ llmFieldManagedHeadline(showCloudflareAccountField ? "cloudflare_api_token" : "api_key") }}</code>
              <p class="setup-env-managed-body">{{ t("settings_env_managed_body") }}</p>
            </div>
            <QInput
              v-else-if="showCloudflareAccountField"
              v-model="llmForm.cloudflare_api_token"
              inputType="password"
              :placeholder="t(credentialPlaceholderKey)"
              :disabled="loading || saving"
            />
            <QInput
              v-else
              v-model="llmForm.api_key"
              inputType="password"
              :placeholder="t(credentialPlaceholderKey)"
              :disabled="loading || saving"
            />
            <p v-if="credentialHelp" class="setup-field-hint">
              <button v-if="credentialHelp.url" type="button" class="setup-field-link" @click="openExternal(credentialHelp.url)">
                <span>{{ credentialHelpParts?.before }}</span>
                <span class="setup-field-link-provider">{{ credentialHelp.title }}</span>
                <span>{{ credentialHelpParts?.after }}</span>
                <QIconArrowUpRight class="icon setup-field-link-icon" />
              </button>
              <span v-else class="setup-field-link is-static">
                {{ t(credentialHintPlainKey, { provider: credentialHelp.title }) }}
              </span>
            </p>
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_model_label") }}</span>
            <div v-if="isLLMFieldEnvManaged('model')" class="setup-env-managed">
              <code class="setup-env-managed-env">{{ llmFieldManagedHeadline("model") }}</code>
              <p class="setup-env-managed-body">{{ t("settings_env_managed_body") }}</p>
            </div>
            <div v-else class="setup-field-control">
              <QInput
                v-model="llmForm.model"
                :placeholder="t('settings_agent_model_placeholder')"
                :disabled="loading || saving"
              />
              <QButton
                type="button"
                class="outlined icon setup-field-action"
                :title="t('setup_llm_model_picker_title')"
                :aria-label="t('setup_llm_model_picker_title')"
                :disabled="modelLookupDisabled"
                @click.prevent="openModelPicker"
              >
                <QIconSearch class="icon" />
              </QButton>
            </div>
          </label>

          <QFence v-if="err" class="setup-error is-wide" type="danger" icon="QIconCloseCircle" :text="err" />

          <div class="setup-footer is-wide">
            <div class="setup-footer-side">
              <QButton type="button" class="outlined setup-aux-action" :disabled="testConnectionDisabled" @click="openTestConnection">
                {{ t("setup_llm_test_button") }}
              </QButton>
            </div>
            <div class="setup-footer-side is-end">
              <QButton class="primary setup-submit" :loading="saving" :disabled="llmSaveDisabled" @click="saveLLM">
                {{ t(stageMeta.submitKey) }}
              </QButton>
            </div>
          </div>
        </form>

        <form
          v-else-if="routeStage === 'persona'"
          class="setup-form setup-form-persona"
          @submit.prevent="savePersona"
        >
          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("setup_identity_name_label") }}</span>
            <QInput
              ref="personaNameInput"
              v-model="personaForm.name"
              :placeholder="t('setup_identity_name_placeholder')"
              :disabled="saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("setup_identity_emoji_label") }}</span>
            <QInput
              v-model="personaForm.emoji"
              :placeholder="t('setup_identity_emoji_placeholder')"
              :disabled="saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("setup_identity_creature_label") }}</span>
            <QInput
              v-model="personaForm.creature"
              :placeholder="t('setup_identity_creature_placeholder')"
              :disabled="saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("setup_identity_vibe_label") }}</span>
            <QTextarea
              v-model="personaForm.vibe"
              :rows="3"
              :placeholder="t('setup_identity_vibe_placeholder')"
              :disabled="saving"
            />
          </label>

          <QFence v-if="err" class="setup-error is-wide" type="danger" icon="QIconCloseCircle" :text="err" />

          <div class="setup-footer setup-footer-persona is-wide">
            <div class="setup-footer-side">
              <QButton v-if="showPrevious" type="button" class="outlined" @click="goPrevious">{{ t("setup_action_previous") }}</QButton>
            </div>
            <div class="setup-footer-side setup-footer-center">
              <QButton
                type="button"
                class="outlined icon setup-persona-random-button"
                :title="t('setup_persona_randomize')"
                :aria-label="t('setup_persona_randomize')"
                :disabled="loading || saving"
                @click="fillRandomPersona"
              >
                <QIconDice class="icon" />
              </QButton>
            </div>
            <div class="setup-footer-side is-end">
              <QButton class="primary setup-submit" :loading="saving" :disabled="personaSaveDisabled" @click="savePersona">
                {{ t(stageMeta.submitKey) }}
              </QButton>
            </div>
          </div>
        </form>

        <form
          v-else-if="routeStage === 'soul'"
          class="setup-form setup-form-soul"
          @submit.prevent="saveSoul"
        >
          <section v-if="!soulEditMode" class="setup-soul-presets is-wide">
            <section :class="['setup-soul-spotlight-wrap', { 'is-active': selectedSoulCard }]">
              <section :class="['setup-soul-spotlight', { 'is-active': selectedSoulCard }]">
                <div v-if="selectedSoulCard && selectedSoulCard.spriteSrc" class="setup-soul-sprite-stage" :style="selectedSoulSpriteStageStyle" aria-hidden="true">
                  <div class="setup-soul-sprite" :style="selectedSoulSpriteStyle"></div>
                </div>
                <span v-else-if="selectedSoulCard" class="setup-soul-spotlight-mark" aria-hidden="true">
                  <component :is="selectedSoulCard.icon" class="setup-soul-spotlight-icon icon" />
                </span>
                <h3 v-if="selectedSoulCard" class="setup-soul-spotlight-title">{{ selectedSoulCard.title }}</h3>
                <p v-if="selectedSoulCard && selectedSoulCard.note" class="setup-soul-spotlight-note">{{ selectedSoulCard.note }}</p>
                <QButton v-if="isCustomSoulSelected" type="button" class="outlined xs" @click="openSoulEditor">
                  {{ t("setup_action_edit_soul") }}
                </QButton>
              </section>
            </section>
            <div class="setup-soul-card-stack" :class="{ 'is-custom-active': isCustomSoulSelected }">
              <button
                v-for="preset in soulPresetCards"
                :key="preset.id"
                type="button"
                :class="['setup-soul-card', 'setup-soul-stack-card', { 'is-active': soulPresetId === preset.id }]"
                :style="{ '--stack-order': preset.stackIndex }"
                @click="applySoulPreset(preset.id)"
                >
                <span class="setup-soul-card-mark" aria-hidden="true">
                  <img v-if="preset.faceSrc" :src="preset.faceSrc" class="setup-soul-card-face" alt="" />
                  <component v-else :is="preset.icon" class="setup-soul-card-icon icon" />
                </span>
                <strong class="setup-soul-card-title">{{ preset.title }}</strong>
              </button>
              <button
                type="button"
                class="setup-soul-card setup-soul-stack-card setup-soul-card-custom"
                :class="{ 'is-active': isCustomSoulSelected }"
                :style="{ '--stack-order': soulPresetCards.length }"
                @click="selectCustomSoul"
              >
                <span class="setup-soul-card-mark is-blank" aria-hidden="true">
                  <component :is="customSoulCardIcon" class="setup-soul-card-icon icon" />
                </span>
                <span class="setup-soul-card-placeholder" aria-hidden="true">
                  <span></span>
                  <span></span>
                </span>
              </button>
            </div>
          </section>

          <section v-else class="setup-soul-editor is-wide">
            <div class="setup-soul-editor-head">
              <p class="setup-field-label">{{ t("setup_soul_editor_label") }}</p>
              <QButton type="button" class="plain" @click="cancelSoulEditor">{{ t("action_cancel") }}</QButton>
            </div>
            <MarkdownEditor
              v-model="soulEditorDraft"
              :disabled="saving"
              :height="'360px'"
              :hint="t('setup_soul_editor_hint')"
              :aria-label="t('setup_soul_editor_label')"
            />
          </section>

          <QFence v-if="err" class="setup-error is-wide" type="danger" icon="QIconCloseCircle" :text="err" />

          <div class="setup-footer is-wide">
            <div class="setup-footer-side">
              <QButton v-if="showPrevious" type="button" class="outlined" @click="goPrevious">{{ t("setup_action_previous") }}</QButton>
            </div>
            <div class="setup-footer-side is-end">
              <QButton v-if="soulSaveVisible" class="primary setup-submit" :loading="saving" :disabled="soulSaveDisabled" @click="saveSoul">
                {{ t(stageMeta.submitKey) }}
              </QButton>
            </div>
          </div>
        </form>

        <section v-else class="setup-form setup-form-done">
          <section class="setup-done-summary is-wide">
            <p class="setup-field-label">{{ t("setup_done_status_label") }}</p>
            <div class="setup-done-status-list">
              <div v-for="item in doneStatusItems" :key="item.key" class="setup-done-status-row">
                <div class="setup-done-status-main">
                  <component :is="item.icon" class="setup-done-status-icon icon" />
                  <span class="setup-done-status-key">{{ item.key }}</span>
                </div>
                <div class="setup-done-status-actions">
                  <strong class="setup-done-status-value">{{ item.value }}</strong>
                  <QButton class="plain xs icon setup-done-edit-button" :title="item.action" :aria-label="item.action" @click="goToStage(item.stage)">
                    <QIconEdit class="icon" />
                  </QButton>
                </div>
              </div>
            </div>
          </section>

          <div class="setup-footer setup-footer-center is-wide">
            <QButton class="primary setup-submit" @click="enterChat">
              {{ t("setup_action_enter_chat") }}
            </QButton>
          </div>
        </section>

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
      </QCard>
    </section>
  `,
};

export default SetupView;
