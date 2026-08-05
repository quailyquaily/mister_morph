import heartbeatTemplate from "../../../../assets/config/HEARTBEAT.md?raw";
import identityTemplate from "../../../../assets/config/persona/identity.yaml?raw";
import cronTemplate from "../../../../assets/config/cron.yaml?raw";
import soulTemplate from "../../../../assets/config/persona/soul.md?raw";

const SETUP_PROVIDER_NONE = "";
const SETUP_PROVIDER_OPENAI = "openai";
const SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE = "openai_chat_compatible";
const SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE = "openai_response_compatible";
const SETUP_PROVIDER_ANTHROPIC_COMPATIBLE = "anthropic_compatible";
const SETUP_PROVIDER_OPENAI_COMPATIBLE = SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE;
const SETUP_PROVIDER_GEMINI = "gemini";
const SETUP_PROVIDER_ANTHROPIC = "anthropic";
const SETUP_PROVIDER_BEDROCK = "bedrock";
const SETUP_PROVIDER_CLOUDFLARE = "cloudflare";
const SETUP_PROVIDER_OPENAI_CODEX = "openai_codex";
const SETUP_PROVIDER_XAI_OAUTH = "xai_oauth";
const SETUP_PROVIDER_MISTERMORPH_PRO = "mistermorph_pro";
const SETUP_PROVIDER_XAI = "xai";
const SETUP_PROVIDER_META = "meta";
const SETUP_PROVIDER_DEEPSEEK = "deepseek";
const SETUP_PROVIDER_KIMI = "kimi";
const SETUP_PROVIDER_OPENROUTER = "openrouter";
const SETUP_PROVIDER_GROQ = "groq";
const SETUP_PROVIDER_SAKANA = "sakana";

const SETUP_PROVIDER_OPTIONS = [
  { title: "OpenAI", value: SETUP_PROVIDER_OPENAI },
  { title: "OpenAI Codex", value: SETUP_PROVIDER_OPENAI_CODEX },
  { title: "xAI Grok OAuth", value: SETUP_PROVIDER_XAI_OAUTH },
  { title: "Google Gemini", value: SETUP_PROVIDER_GEMINI },
  { title: "Claude AI", value: SETUP_PROVIDER_ANTHROPIC },
  { title: "AWS Bedrock", value: SETUP_PROVIDER_BEDROCK },
  { title: "Cloudflare", value: SETUP_PROVIDER_CLOUDFLARE },
  { title: "MisterMorph Pro", value: SETUP_PROVIDER_MISTERMORPH_PRO },
  { title: "xAI", value: SETUP_PROVIDER_XAI },
  { title: "Meta", value: SETUP_PROVIDER_META },
  { title: "Deepseek", value: SETUP_PROVIDER_DEEPSEEK },
  { title: "Kimi", value: SETUP_PROVIDER_KIMI },
  { title: "OpenRouter", value: SETUP_PROVIDER_OPENROUTER },
  { title: "Groq", value: SETUP_PROVIDER_GROQ },
  { title: "Sakana AI", value: SETUP_PROVIDER_SAKANA },
  { title: "OpenAI Chat Compatible", value: SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE },
  { title: "OpenAI Response Compatible", value: SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE },
  { title: "Claude AI Compatible", value: SETUP_PROVIDER_ANTHROPIC_COMPATIBLE },
];

const SETUP_PROVIDER_UI_META = {
  [SETUP_PROVIDER_OPENAI]: { supportsModelLookup: true },
  [SETUP_PROVIDER_OPENAI_CODEX]: { supportsCustomAPIBase: true, supportsAPIKey: true },
  [SETUP_PROVIDER_XAI_OAUTH]: {},
  [SETUP_PROVIDER_GEMINI]: {},
  [SETUP_PROVIDER_ANTHROPIC]: {},
  [SETUP_PROVIDER_BEDROCK]: {},
  [SETUP_PROVIDER_CLOUDFLARE]: {},
  [SETUP_PROVIDER_MISTERMORPH_PRO]: { supportsModelLookup: true },
  [SETUP_PROVIDER_XAI]: { supportsModelLookup: true },
  [SETUP_PROVIDER_META]: {},
  [SETUP_PROVIDER_DEEPSEEK]: { supportsModelLookup: true },
  [SETUP_PROVIDER_KIMI]: { supportsModelLookup: true },
  [SETUP_PROVIDER_OPENROUTER]: { supportsModelLookup: true },
  [SETUP_PROVIDER_GROQ]: { supportsModelLookup: true },
  [SETUP_PROVIDER_SAKANA]: { supportsModelLookup: true },
  [SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE]: { requiresAPIBase: true, supportsModelLookup: true },
  [SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE]: { requiresAPIBase: true, supportsModelLookup: true },
  [SETUP_PROVIDER_ANTHROPIC_COMPATIBLE]: { requiresAPIBase: true },
};

const OPENAI_COMPATIBLE_API_BASE_OPTIONS = [
  {
    id: "openai",
    title: "OpenAI",
    baseURL: "https://api.openai.com",
    dashboardURL: "https://platform.openai.com/settings",
  },
  {
    id: "xai",
    title: "xAI",
    baseURL: "https://api.x.ai",
    dashboardURL: "https://console.x.ai/",
  },
  {
    id: "meta",
    title: "Meta",
    baseURL: "https://api.ai.meta.com/v1",
    dashboardURL: "https://developer.meta.com/ai/",
  },
  {
    id: "moonshot",
    title: "Kimi / Moonshot",
    baseURL: "https://api.moonshot.cn",
    dashboardURL: "https://platform.moonshot.cn/",
  },
  {
    id: "minimax",
    title: "MiniMax",
    baseURL: "https://api.minimaxi.com/v1",
    dashboardURL: "https://platform.minimaxi.com/",
  },
  {
    id: "zai",
    title: "GLM / Z.AI",
    baseURL: "https://api.z.ai/api/paas/v4",
    dashboardURL: "https://docs.z.ai/guides",
  },
  {
    id: "deepseek",
    title: "DeepSeek",
    baseURL: "https://api.deepseek.com",
    dashboardURL: "https://platform.deepseek.com/",
  },
  {
    id: "openrouter",
    title: "OpenRouter",
    baseURL: "https://openrouter.ai/api/v1",
    dashboardURL: "https://openrouter.ai/settings",
  },
  {
    id: "groq",
    title: "Groq",
    baseURL: "https://api.groq.com/openai/v1",
    dashboardURL: "https://console.groq.com/keys/",
  },
];

const DIRECT_PROVIDER_API_KEY_HELP = {
  [SETUP_PROVIDER_OPENAI]: {
    title: "OpenAI",
    url: "https://platform.openai.com/settings",
  },
  [SETUP_PROVIDER_GEMINI]: {
    title: "Google AI Studio",
    url: "https://ai.google.dev/gemini-api/docs/api-key",
  },
  [SETUP_PROVIDER_ANTHROPIC]: {
    title: "Anthropic Console",
    url: "https://console.anthropic.com/settings/keys",
  },
  [SETUP_PROVIDER_CLOUDFLARE]: {
    title: "Cloudflare Dashboard",
    url: "https://dash.cloudflare.com/profile/api-tokens",
  },
  [SETUP_PROVIDER_XAI]: {
    title: "xAI",
    url: "https://console.x.ai/",
  },
  [SETUP_PROVIDER_META]: {
    title: "Meta Model API",
    url: "https://developer.meta.com/ai/",
  },
  [SETUP_PROVIDER_DEEPSEEK]: {
    title: "DeepSeek",
    url: "https://platform.deepseek.com/",
  },
  [SETUP_PROVIDER_KIMI]: {
    title: "Kimi / Moonshot",
    url: "https://platform.moonshot.cn/",
  },
  [SETUP_PROVIDER_OPENROUTER]: {
    title: "OpenRouter",
    url: "https://openrouter.ai/settings",
  },
  [SETUP_PROVIDER_GROQ]: {
    title: "Groq",
    url: "https://console.groq.com/keys/",
  },
  [SETUP_PROVIDER_SAKANA]: {
    title: "Sakana AI",
    url: "https://console.sakana.ai/",
  },
};

const SETUP_REQUIRED_MARKDOWN_FILES = [
  { name: "HEARTBEAT.md", content: heartbeatTemplate },
  { name: "cron.yaml", content: cronTemplate },
  { name: "identity.yaml", content: identityTemplate },
  { name: "soul.md", content: soulTemplate },
];

function normalizeSetupProviderChoice(provider, options = {}) {
  const allowEmpty = options && options.allowEmpty === true;
  switch (String(provider || "").trim().toLowerCase()) {
    case "":
      return allowEmpty ? SETUP_PROVIDER_NONE : SETUP_PROVIDER_OPENAI_COMPATIBLE;
    case "openai_compatible":
    case "openai_custom":
    case SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE:
      return SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE;
    case "openai_resp":
    case SETUP_PROVIDER_OPENAI:
      return SETUP_PROVIDER_OPENAI;
    case SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE:
      return SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE;
    case SETUP_PROVIDER_GEMINI:
      return SETUP_PROVIDER_GEMINI;
    case SETUP_PROVIDER_ANTHROPIC:
      return SETUP_PROVIDER_ANTHROPIC;
    case SETUP_PROVIDER_BEDROCK:
      return SETUP_PROVIDER_BEDROCK;
    case SETUP_PROVIDER_CLOUDFLARE:
      return SETUP_PROVIDER_CLOUDFLARE;
    case SETUP_PROVIDER_OPENAI_CODEX:
      return SETUP_PROVIDER_OPENAI_CODEX;
    case SETUP_PROVIDER_XAI_OAUTH:
      return SETUP_PROVIDER_XAI_OAUTH;
    case SETUP_PROVIDER_MISTERMORPH_PRO:
    case "mistermorph":
    case "mister_morph_pro":
      return SETUP_PROVIDER_MISTERMORPH_PRO;
    case SETUP_PROVIDER_XAI:
      return SETUP_PROVIDER_XAI;
    case SETUP_PROVIDER_META:
      return SETUP_PROVIDER_META;
    case SETUP_PROVIDER_DEEPSEEK:
      return SETUP_PROVIDER_DEEPSEEK;
    case SETUP_PROVIDER_KIMI:
      return SETUP_PROVIDER_KIMI;
    case SETUP_PROVIDER_OPENROUTER:
    case "open_router":
      return SETUP_PROVIDER_OPENROUTER;
    case SETUP_PROVIDER_GROQ:
      return SETUP_PROVIDER_GROQ;
    case SETUP_PROVIDER_SAKANA:
      return SETUP_PROVIDER_SAKANA;
    case "claude_compatible":
    case "claude_ai_compatible":
    case SETUP_PROVIDER_ANTHROPIC_COMPATIBLE:
      return SETUP_PROVIDER_ANTHROPIC_COMPATIBLE;
    default:
      return SETUP_PROVIDER_OPENAI_COMPATIBLE;
  }
}

function setupProviderRequiresAPIBase(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return SETUP_PROVIDER_UI_META[provider]?.requiresAPIBase === true;
}

function setupProviderSupportsCustomAPIBase(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  const meta = SETUP_PROVIDER_UI_META[provider];
  return meta?.supportsCustomAPIBase === true || meta?.requiresAPIBase === true;
}

function setupProviderSupportsAPIKey(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return SETUP_PROVIDER_UI_META[provider]?.supportsAPIKey === true || setupProviderRequiresAPIKey(provider);
}

function setupOpenAICodexUsesAPIKey(endpoint, hasAPIKey) {
  return String(endpoint || "").trim() !== "" && hasAPIKey === true;
}

function setupProviderSupportsModelLookup(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return SETUP_PROVIDER_UI_META[provider]?.supportsModelLookup === true;
}

function setupProviderRequiresAPIKey(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return ![
    SETUP_PROVIDER_CLOUDFLARE,
    SETUP_PROVIDER_BEDROCK,
    SETUP_PROVIDER_OPENAI_CODEX,
    SETUP_PROVIDER_XAI_OAUTH,
    SETUP_PROVIDER_MISTERMORPH_PRO,
  ].includes(provider);
}

function resolveSetupAPIKeyHelp(choice, endpoint) {
  void endpoint;
  const normalizedChoice = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  if (
    normalizedChoice === SETUP_PROVIDER_OPENAI_CODEX ||
    normalizedChoice === SETUP_PROVIDER_XAI_OAUTH ||
    normalizedChoice === SETUP_PROVIDER_MISTERMORPH_PRO ||
    setupProviderRequiresAPIBase(normalizedChoice)
  ) {
    return null;
  }
  return DIRECT_PROVIDER_API_KEY_HELP[normalizedChoice] || null;
}

export {
  OPENAI_COMPATIBLE_API_BASE_OPTIONS,
  SETUP_PROVIDER_NONE,
  SETUP_PROVIDER_ANTHROPIC,
  SETUP_PROVIDER_ANTHROPIC_COMPATIBLE,
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_DEEPSEEK,
  SETUP_PROVIDER_GEMINI,
  SETUP_PROVIDER_GROQ,
  SETUP_PROVIDER_KIMI,
  SETUP_PROVIDER_META,
  SETUP_PROVIDER_MISTERMORPH_PRO,
  SETUP_PROVIDER_OPENAI,
  SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_XAI_OAUTH,
  SETUP_PROVIDER_OPENAI_COMPATIBLE,
  SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE,
  SETUP_PROVIDER_OPENROUTER,
  SETUP_PROVIDER_OPTIONS,
  SETUP_PROVIDER_SAKANA,
  SETUP_PROVIDER_XAI,
  SETUP_REQUIRED_MARKDOWN_FILES,
  normalizeSetupProviderChoice,
  resolveSetupAPIKeyHelp,
  setupProviderRequiresAPIBase,
  setupProviderRequiresAPIKey,
  setupProviderSupportsCustomAPIBase,
  setupProviderSupportsAPIKey,
  setupProviderSupportsModelLookup,
  setupOpenAICodexUsesAPIKey,
};
