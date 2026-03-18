import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./SetupView.css";

import catSoulTemplate from "../../../../assets/config/souls/cat.md?raw";
import dogSoulTemplate from "../../../../assets/config/souls/dog.md?raw";
import researchScholarSoulTemplate from "../../../../assets/config/souls/research_scholar.md?raw";
import softwareEngineerSoulTemplate from "../../../../assets/config/souls/software_engineer.md?raw";
import catFaceImage from "../assets/souls/cat/Faceset.png";
import catSpriteImage from "../assets/souls/cat/SpriteSheet.png";
import dogFaceImage from "../assets/souls/dog/Faceset.png";
import dogSpriteImage from "../assets/souls/dog/SpriteSheet.png";
import engineerFaceImage from "../assets/souls/software_engineer/Faceset.png";
import engineerSpriteImage from "../assets/souls/software_engineer/SpriteSheet.png";
import scholarFaceImage from "../assets/souls/research_scholar/Faceset.png";
import scholarSpriteImage from "../assets/souls/research_scholar/SpriteSheet.png";
import MarkdownEditor from "../components/MarkdownEditor";
import {
  apiFetch,
  loadEndpoints,
  runtimeApiFetchForEndpoint,
  setSelectedEndpointRef,
  translate,
} from "../core/context";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import {
  consoleSetupTargetEndpointRef,
  resolveConsoleSetupStage,
  setupStagePath,
} from "../core/setup";
import { endpointState } from "../stores";

const PROVIDER_OPTIONS = [
  { title: "OpenAI", value: "openai" },
  { title: "Anthropic", value: "anthropic" },
  { title: "Gemini", value: "gemini" },
  { title: "xAI", value: "xai" },
  { title: "DeepSeek", value: "deepseek" },
];

const TOTAL_STEPS = 3;
const IDENTITY_FIELDS = ["name", "creature", "vibe", "emoji"];
const IDENTITY_YAML_FENCE_RE = /```(?:yaml|yml)\s*\n([\s\S]*?)\n```/i;
const FRONTMATTER_RE = /^---\n([\s\S]*?)\n---\n*/;
const PREVIOUS_STAGE = {
  persona: "llm",
  soul: "persona",
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
  return normalizeSoulDocument(`---
status: done
---

# SOUL.md

## Core Truths

- 

## Boundaries

- 

## Vibe


## Continuity

`);
}

const SOUL_PRESETS = [
  {
    id: "research_scholar",
    icon: "QIconEcosystem",
    titleKey: "setup_soul_preset_scholar_title",
    noteKey: "setup_soul_preset_scholar_note",
    content: normalizeSoulDocument(researchScholarSoulTemplate),
    faceSrc: scholarFaceImage,
    spriteSrc: scholarSpriteImage,
    spriteFrames: 4,
    spriteFrameWidth: 32,
    spriteFrameHeight: 32,
    spriteScale: 2.5,
  },
  {
    id: "software_engineer",
    icon: "QIconSpeedoMeter",
    titleKey: "setup_soul_preset_engineer_title",
    noteKey: "setup_soul_preset_engineer_note",
    content: normalizeSoulDocument(softwareEngineerSoulTemplate),
    faceSrc: engineerFaceImage,
    spriteSrc: engineerSpriteImage,
    spriteFrames: 4,
    spriteFrameWidth: 32,
    spriteFrameHeight: 32,
    spriteScale: 2.5,
  },
  {
    id: "cat",
    icon: "QIconFingerprint",
    titleKey: "setup_soul_preset_cat_title",
    noteKey: "setup_soul_preset_cat_note",
    content: normalizeSoulDocument(catSoulTemplate),
    faceSrc: catFaceImage,
    spriteSrc: catSpriteImage,
    spriteFrames: 2,
    spriteFrameWidth: 16,
    spriteFrameHeight: 16,
    spriteScale: 2.5,
  },
  {
    id: "dog",
    icon: "QIconUsers",
    titleKey: "setup_soul_preset_dog_title",
    noteKey: "setup_soul_preset_dog_note",
    content: normalizeSoulDocument(dogSoulTemplate),
    faceSrc: dogFaceImage,
    spriteSrc: dogSpriteImage,
    spriteFrames: 2,
    spriteFrameWidth: 16,
    spriteFrameHeight: 16,
    spriteScale: 2.5,
  },
];

function findSoulPreset(id) {
  return SOUL_PRESETS.find((item) => item.id === id) || SOUL_PRESETS[0];
}

function buildDefaultPayload() {
  return {
    llm: {
      provider: "openai",
      endpoint: "",
      model: "",
      api_key: "",
      reasoning_effort: "",
      tools_emulation_mode: "",
    },
    multimodal: {
      image_sources: [],
    },
    tools: {
      write_file_enabled: true,
      contacts_send_enabled: true,
      todo_update_enabled: true,
      plan_create_enabled: true,
      url_fetch_enabled: true,
      web_search_enabled: true,
      bash_enabled: true,
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

function stripFrontmatter(raw) {
  const content = normalizeText(raw);
  const match = FRONTMATTER_RE.exec(content);
  if (!match) {
    return content;
  }
  return content.slice(match[0].length);
}

function setFrontmatterStatus(raw, status) {
  const content = normalizeText(raw).trim();
  const match = FRONTMATTER_RE.exec(content);
  if (!match) {
    return normalizeSoulDocument(`---
status: ${status}
---

${content}`);
  }
  const lines = match[1].split("\n");
  let found = false;
  const nextLines = lines.map((line) => {
    if (!/^\s*status\s*:/.test(line)) {
      return line;
    }
    found = true;
    return `status: ${status}`;
  });
  if (!found) {
    nextLines.push(`status: ${status}`);
  }
  const body = content.slice(match[0].length).replace(/^\n+/, "");
  return normalizeSoulDocument(`---
${nextLines.join("\n")}
---

${body}`);
}

function soulComparable(raw) {
  return stripFrontmatter(raw).trim();
}

function detectSoulPresetId(raw) {
  const target = soulComparable(raw);
  if (!target) {
    return SOUL_PRESETS[0].id;
  }
  const match = SOUL_PRESETS.find((item) => soulComparable(item.content) === target);
  return match?.id || "";
}

function normalizeStage(value) {
  if (value === "persona" || value === "soul" || value === "done") {
    return value;
  }
  return "llm";
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
    const loadedIdentityRaw = ref("");
    const loadedSoulRaw = ref("");
    const llmForm = reactive({
      provider: "openai",
      endpoint: "",
      model: "",
      api_key: "",
    });
    const personaForm = reactive(buildEmptyIdentityProfile());
    const soulSelectionContent = ref("");
    const soulEditorDraft = ref(buildCustomSoulDocument());
    const soulPresetId = ref("");
    const soulSelectionKind = ref("");
    const soulEditMode = ref(false);

    const routeStage = computed(() => normalizeStage(route.meta?.setupStage));
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
    const providerItems = computed(() => PROVIDER_OPTIONS);
    const providerItem = computed(
      () => providerItems.value.find((item) => item.value === llmForm.provider) || providerItems.value[0]
    );
    const previousStage = computed(() => PREVIOUS_STAGE[routeStage.value] || "");
    const showPrevious = computed(() => previousStage.value !== "");
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
        String(llmForm.provider || "").trim() === "" ||
        String(llmForm.model || "").trim() === "" ||
        String(llmForm.api_key || "").trim() === ""
    );
    const personaSaveDisabled = computed(
      () =>
        loading.value ||
        saving.value ||
        String(personaForm.name || "").trim() === "" ||
        String(personaForm.creature || "").trim() === "" ||
        String(personaForm.vibe || "").trim() === "" ||
        String(personaForm.emoji || "").trim() === ""
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
    const selectedSoulCard = computed(() => {
      if (soulSelectionKind.value === "preset") {
        return soulPresetCards.value.find((item) => item.id === soulPresetId.value) || null;
      }
      if (isCustomSoulSelected.value) {
        return {
          id: "custom",
          icon: "QIconPlus",
          title: t("setup_soul_custom_title"),
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
    const soulUsesCustomContent = computed(
      () => detectSoulPresetId(loadedSoulRaw.value) === "" && normalizeSoulDocument(loadedSoulRaw.value).trim() !== ""
    );

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
      soulSelectionKind.value = "";
      soulEditMode.value = false;
    }

    function applyLLMPayload(data) {
      const normalized = normalizePayload(data);
      loadedPayload.value = normalized;
      llmForm.provider = String(normalized.llm.provider || "").trim() || "openai";
      if (!providerItems.value.some((item) => item.value === llmForm.provider)) {
        llmForm.provider = "openai";
      }
      llmForm.endpoint = String(normalized.llm.endpoint || "").trim();
      llmForm.model = String(normalized.llm.model || "").trim();
      llmForm.api_key = String(normalized.llm.api_key || "").trim();
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
      }
    }

    async function syncRoute(options = {}) {
      if (options.refreshEndpoints !== false) {
        await loadEndpoints();
      }
      const setupState = await resolveConsoleSetupStage(endpointState.items);
      if (setupState.stage !== "ready") {
        const expectedPath = setupStagePath(setupState.stage);
        if (route.path !== expectedPath) {
          await router.replace({ path: expectedPath, query: route.query });
          return false;
        }
        if (options.loadStage !== false) {
          await loadStageForm(setupState.stage);
        }
        return false;
      }
      if (route.path === "/setup") {
        await router.replace({ path: "/setup/done", query: route.query });
        return false;
      }
      if (options.onReady === "done" && route.path !== "/setup/done") {
        await router.replace({ path: "/setup/done", query: route.query });
        return false;
      }
      if (options.loadStage !== false) {
        await loadStageForm(routeStage.value);
      }
      return false;
    }

    async function saveLLM() {
      if (llmSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const payload = await apiFetch("/settings/agent", {
          method: "PUT",
          body: {
            llm: {
              ...loadedPayload.value.llm,
              provider: String(llmForm.provider || "").trim(),
              endpoint: String(llmForm.endpoint || "").trim(),
              model: String(llmForm.model || "").trim(),
              api_key: String(llmForm.api_key || "").trim(),
            },
            multimodal: loadedPayload.value.multimodal,
            tools: loadedPayload.value.tools,
          },
        });
        applyLLMPayload(payload);
        await syncRoute({ refreshEndpoints: true, loadStage: false, onReady: "done" });
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    async function savePersona() {
      if (personaSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const content = updateIdentityMarkdown(loadedIdentityRaw.value, personaForm);
        loadedIdentityRaw.value = content;
        await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/IDENTITY.md", {
          method: "PUT",
          body: {
            content,
          },
        });
        await syncRoute({ refreshEndpoints: true, loadStage: false, onReady: "done" });
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    async function saveSoul() {
      if (soulSaveDisabled.value) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        const source = soulEditMode.value ? soulEditorDraft.value : soulSelectionContent.value;
        const content = normalizeSoulDocument(setFrontmatterStatus(source, "done"));
        const presetId = detectSoulPresetId(content);
        loadedSoulRaw.value = content;
        soulSelectionContent.value = content;
        soulEditorDraft.value = content;
        soulPresetId.value = presetId;
        soulSelectionKind.value = presetId ? "preset" : "custom";
        soulEditMode.value = false;
        await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, "/state/files/SOUL.md", {
          method: "PUT",
          body: {
            content,
          },
        });
        await syncRoute({ refreshEndpoints: true, loadStage: false, onReady: "done" });
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    function onProviderChange(item) {
      llmForm.provider = String(item?.value || "").trim() || providerItems.value[0].value;
    }

    function applySoulPreset(id) {
      const preset = findSoulPreset(id);
      soulSelectionKind.value = "preset";
      soulPresetId.value = preset.id;
      soulSelectionContent.value = preset.content;
    }

    function selectCustomSoul() {
      const customSource =
        soulUsesCustomContent.value && detectSoulPresetId(loadedSoulRaw.value) === ""
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

    watch(
      () => route.fullPath,
      () => {
        void syncRoute({ refreshEndpoints: true, loadStage: true });
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
      soulSaveVisible,
      doneStatusItems,
      providerItems,
      providerItem,
      previousStage,
      showPrevious,
      llmSaveDisabled,
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
    };
  },
  template: `
    <section :class="screenClass">
      <section class="setup-shell stat-item">
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
            <QDropdownMenu
              :key="llmForm.provider || 'provider'"
              :items="providerItems"
              :initialItem="providerItem"
              :placeholder="t('settings_agent_provider_placeholder')"
              :disabled="loading || saving"
              @change="onProviderChange"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_endpoint_label") }}</span>
            <QInput
              v-model="llmForm.endpoint"
              :placeholder="t('settings_agent_endpoint_placeholder')"
              :disabled="loading || saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_model_label") }}</span>
            <QInput
              v-model="llmForm.model"
              :placeholder="t('settings_agent_model_placeholder')"
              :disabled="loading || saving"
            />
          </label>

          <label class="setup-field is-wide">
            <span class="setup-field-label">{{ t("settings_agent_api_key_label") }}</span>
            <QInput
              v-model="llmForm.api_key"
              inputType="password"
              :placeholder="t('settings_agent_api_key_placeholder')"
              :disabled="loading || saving"
            />
          </label>

          <QFence v-if="err" class="setup-error is-wide" type="danger" icon="QIconCloseCircle" :text="err" />

          <div class="setup-footer is-wide">
            <div class="setup-footer-side"></div>
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

          <div class="setup-footer is-wide">
            <div class="setup-footer-side">
              <QButton v-if="showPrevious" class="outlined" @click="goPrevious">{{ t("setup_action_previous") }}</QButton>
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
                <QButton v-if="isCustomSoulSelected" class="outlined xs" @click="openSoulEditor">
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
                  <QIconPlus class="setup-soul-card-icon icon" />
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
              <QButton class="plain" @click="cancelSoulEditor">{{ t("action_cancel") }}</QButton>
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
              <QButton v-if="showPrevious" class="outlined" @click="goPrevious">{{ t("setup_action_previous") }}</QButton>
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
      </section>
    </section>
  `,
};

export default SetupView;
