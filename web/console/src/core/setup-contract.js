import heartbeatTemplate from "../../../../assets/config/HEARTBEAT.md?raw";
import identityTemplate from "../../../../assets/config/persona/identity.yaml?raw";
import cronTemplate from "../../../../assets/config/cron.yaml?raw";
import scriptsTemplate from "../../../../assets/config/SCRIPTS.md?raw";
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
const SETUP_PROVIDER_MISTERMORPH_PRO = "mistermorph_pro";
const SETUP_PROVIDER_XAI = "xai";
const SETUP_PROVIDER_DEEPSEEK = "deepseek";
const SETUP_PROVIDER_KIMI = "kimi";
const SETUP_PROVIDER_OPENROUTER = "openrouter";
const SETUP_PROVIDER_GROQ = "groq";

const SETUP_PROVIDER_OPTIONS = [
  { title: "OpenAI", value: SETUP_PROVIDER_OPENAI },
  { title: "OpenAI Codex", value: SETUP_PROVIDER_OPENAI_CODEX },
  { title: "Google Gemini", value: SETUP_PROVIDER_GEMINI },
  { title: "Claude AI", value: SETUP_PROVIDER_ANTHROPIC },
  { title: "AWS Bedrock", value: SETUP_PROVIDER_BEDROCK },
  { title: "Cloudflare", value: SETUP_PROVIDER_CLOUDFLARE },
  { title: "MisterMorph Pro", value: SETUP_PROVIDER_MISTERMORPH_PRO },
  { title: "xAI", value: SETUP_PROVIDER_XAI },
  { title: "Deepseek", value: SETUP_PROVIDER_DEEPSEEK },
  { title: "Kimi", value: SETUP_PROVIDER_KIMI },
  { title: "OpenRouter", value: SETUP_PROVIDER_OPENROUTER },
  { title: "Groq", value: SETUP_PROVIDER_GROQ },
  { title: "OpenAI Chat Compatible", value: SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE },
  { title: "OpenAI Response Compatible", value: SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE },
  { title: "Claude AI Compatible", value: SETUP_PROVIDER_ANTHROPIC_COMPATIBLE },
];

const SETUP_PROVIDER_REGISTRY = {
  [SETUP_PROVIDER_OPENAI]: { provider: "openai_resp", endpoint: "https://api.openai.com" },
  [SETUP_PROVIDER_OPENAI_CODEX]: { provider: "openai_codex", endpoint: "" },
  [SETUP_PROVIDER_GEMINI]: { provider: "gemini", endpoint: "https://generativelanguage.googleapis.com" },
  [SETUP_PROVIDER_ANTHROPIC]: { provider: "anthropic", endpoint: "https://api.anthropic.com" },
  [SETUP_PROVIDER_BEDROCK]: { provider: "bedrock", endpoint: "" },
  [SETUP_PROVIDER_CLOUDFLARE]: { provider: "cloudflare", endpoint: "https://api.cloudflare.com/client/v4" },
  [SETUP_PROVIDER_MISTERMORPH_PRO]: { provider: "openai", endpoint: "https://router.mistermorph.com/api/v1" },
  [SETUP_PROVIDER_XAI]: { provider: "xai", endpoint: "https://api.x.ai" },
  [SETUP_PROVIDER_DEEPSEEK]: { provider: "deepseek", endpoint: "https://api.deepseek.com" },
  [SETUP_PROVIDER_KIMI]: { provider: "openai_custom", endpoint: "https://api.moonshot.cn" },
  [SETUP_PROVIDER_OPENROUTER]: { provider: "openai_custom", endpoint: "https://openrouter.ai/api/v1" },
  [SETUP_PROVIDER_GROQ]: { provider: "openai_custom", endpoint: "https://api.groq.com/openai/v1" },
  [SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE]: { provider: "openai_custom", endpoint: "", requiresAPIBase: true },
  [SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE]: { provider: "openai_resp", endpoint: "", requiresAPIBase: true },
  [SETUP_PROVIDER_ANTHROPIC_COMPATIBLE]: { provider: "anthropic", endpoint: "", requiresAPIBase: true },
};

const OPENAI_COMPATIBLE_API_BASE_OPTIONS = [
  {
    id: "openai",
    title: "OpenAI",
    baseURL: "https://api.openai.com",
    dashboardURL: "https://platform.openai.com/settings",
    hosts: ["api.openai.com", "platform.openai.com"],
  },
  {
    id: "xai",
    title: "xAI",
    baseURL: "https://api.x.ai",
    dashboardURL: "https://console.x.ai/",
    hosts: ["api.x.ai", "console.x.ai", "docs.x.ai"],
  },
  {
    id: "moonshot",
    title: "Kimi / Moonshot",
    baseURL: "https://api.moonshot.cn",
    dashboardURL: "https://platform.moonshot.cn/",
    hosts: ["api.moonshot.cn", "platform.moonshot.cn"],
  },
  {
    id: "minimax",
    title: "MiniMax",
    baseURL: "https://api.minimaxi.com/v1",
    dashboardURL: "https://platform.minimaxi.com/",
    hosts: ["api.minimaxi.com", "platform.minimaxi.com"],
  },
  {
    id: "zai",
    title: "GLM / Z.AI",
    baseURL: "https://api.z.ai/api/paas/v4",
    dashboardURL: "https://docs.z.ai/guides",
    hosts: ["api.z.ai", "docs.z.ai"],
  },
  {
    id: "deepseek",
    title: "DeepSeek",
    baseURL: "https://api.deepseek.com",
    dashboardURL: "https://platform.deepseek.com/",
    hosts: ["api.deepseek.com", "platform.deepseek.com", "api-docs.deepseek.com"],
  },
  {
    id: "openrouter",
    title: "OpenRouter",
    baseURL: "https://openrouter.ai/api/v1",
    dashboardURL: "https://openrouter.ai/settings",
    hosts: ["openrouter.ai"],
  },
  {
    id: "groq",
    title: "Groq",
    baseURL: "https://api.groq.com/openai/v1",
    dashboardURL: "https://console.groq.com/keys/",
    hosts: ["api.groq.com", "console.groq.com"],
  },
];

const DIRECT_PROVIDER_API_KEY_HELP = {
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
};

function normalizeAPIBase(value) {
  return String(value || "").trim().replace(/\/+$/, "");
}

function parseAPIBase(value) {
  const normalized = normalizeAPIBase(value);
  if (!normalized) {
    return null;
  }
  try {
    return new URL(normalized);
  } catch {
    return null;
  }
}

const SETUP_REQUIRED_MARKDOWN_FILES = [
  { name: "HEARTBEAT.md", content: heartbeatTemplate },
  { name: "SCRIPTS.md", content: scriptsTemplate },
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
    case SETUP_PROVIDER_MISTERMORPH_PRO:
    case "mistermorph":
    case "mister_morph_pro":
      return SETUP_PROVIDER_MISTERMORPH_PRO;
    case SETUP_PROVIDER_XAI:
      return SETUP_PROVIDER_XAI;
    case SETUP_PROVIDER_DEEPSEEK:
      return SETUP_PROVIDER_DEEPSEEK;
    case SETUP_PROVIDER_KIMI:
      return SETUP_PROVIDER_KIMI;
    case SETUP_PROVIDER_OPENROUTER:
    case "open_router":
      return SETUP_PROVIDER_OPENROUTER;
    case SETUP_PROVIDER_GROQ:
      return SETUP_PROVIDER_GROQ;
    case "claude_compatible":
    case "claude_ai_compatible":
    case SETUP_PROVIDER_ANTHROPIC_COMPATIBLE:
      return SETUP_PROVIDER_ANTHROPIC_COMPATIBLE;
    default:
      return SETUP_PROVIDER_OPENAI_COMPATIBLE;
  }
}

function defaultEndpointForSetupProvider(choice) {
  const provider = normalizeSetupProviderChoice(choice);
  return SETUP_PROVIDER_REGISTRY[provider]?.endpoint || "";
}

function normalizeSetupProviderForSave(choice, endpoint) {
  void endpoint;
  const provider = normalizeSetupProviderChoice(choice);
  return SETUP_PROVIDER_REGISTRY[provider]?.provider || "";
}

function setupProviderRequiresAPIBase(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return SETUP_PROVIDER_REGISTRY[provider]?.requiresAPIBase === true;
}

function setupProviderSupportsModelLookup(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return (
    provider === SETUP_PROVIDER_OPENAI ||
    provider === SETUP_PROVIDER_MISTERMORPH_PRO ||
    provider === SETUP_PROVIDER_XAI ||
    provider === SETUP_PROVIDER_DEEPSEEK ||
    provider === SETUP_PROVIDER_KIMI ||
    provider === SETUP_PROVIDER_OPENROUTER ||
    provider === SETUP_PROVIDER_GROQ ||
    provider === SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE ||
    provider === SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE
  );
}

function setupProviderRequiresAPIKey(choice) {
  const provider = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  return ![SETUP_PROVIDER_CLOUDFLARE, SETUP_PROVIDER_BEDROCK, SETUP_PROVIDER_OPENAI_CODEX].includes(provider);
}

function findOpenAICompatibleAPIBaseOption(endpoint) {
  const normalized = normalizeAPIBase(endpoint);
  if (!normalized) {
    return OPENAI_COMPATIBLE_API_BASE_OPTIONS[0];
  }
  const lower = normalized.toLowerCase();
  for (const item of OPENAI_COMPATIBLE_API_BASE_OPTIONS) {
    const itemBase = item.baseURL.toLowerCase();
    if (lower === itemBase || lower.startsWith(`${itemBase}/`)) {
      return item;
    }
  }
  const parsed = parseAPIBase(normalized);
  if (!parsed) {
    return null;
  }
  const host = String(parsed.host || "").toLowerCase();
  return OPENAI_COMPATIBLE_API_BASE_OPTIONS.find((item) => item.hosts.includes(host)) || null;
}

function resolveSetupAPIKeyHelp(choice, endpoint) {
  const normalizedChoice = normalizeSetupProviderChoice(choice, { allowEmpty: true });
  if (normalizedChoice === SETUP_PROVIDER_GEMINI || normalizedChoice === SETUP_PROVIDER_ANTHROPIC || normalizedChoice === SETUP_PROVIDER_CLOUDFLARE) {
    return DIRECT_PROVIDER_API_KEY_HELP[normalizedChoice] || null;
  }
  if (normalizedChoice === SETUP_PROVIDER_OPENAI_CODEX) {
    return null;
  }
  const item = findOpenAICompatibleAPIBaseOption(endpoint);
  if (item) {
    return { title: item.title, url: item.dashboardURL };
  }
  const normalizedEndpoint = normalizeAPIBase(endpoint);
  if (!normalizedEndpoint) {
    return {
      title: OPENAI_COMPATIBLE_API_BASE_OPTIONS[0].title,
      url: OPENAI_COMPATIBLE_API_BASE_OPTIONS[0].dashboardURL,
    };
  }
  const parsed = parseAPIBase(normalizedEndpoint);
  return {
    title: parsed?.host || normalizedEndpoint,
    url: "",
  };
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
  SETUP_PROVIDER_MISTERMORPH_PRO,
  SETUP_PROVIDER_OPENAI,
  SETUP_PROVIDER_OPENAI_CHAT_COMPATIBLE,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_OPENAI_COMPATIBLE,
  SETUP_PROVIDER_OPENAI_RESPONSE_COMPATIBLE,
  SETUP_PROVIDER_OPENROUTER,
  SETUP_PROVIDER_OPTIONS,
  SETUP_PROVIDER_REGISTRY,
  SETUP_PROVIDER_XAI,
  SETUP_REQUIRED_MARKDOWN_FILES,
  defaultEndpointForSetupProvider,
  findOpenAICompatibleAPIBaseOption,
  normalizeSetupProviderChoice,
  normalizeSetupProviderForSave,
  resolveSetupAPIKeyHelp,
  setupProviderRequiresAPIBase,
  setupProviderRequiresAPIKey,
  setupProviderSupportsModelLookup,
};
