import heartbeatTemplate from "../../../../assets/config/HEARTBEAT.md?raw";
import identityTemplate from "../../../../assets/config/IDENTITY.md?raw";
import scriptsTemplate from "../../../../assets/config/SCRIPTS.md?raw";
import soulTemplate from "../../../../assets/config/SOUL.md?raw";
import todoDoneTemplate from "../../../../assets/config/TODO.DONE.md?raw";
import todoTemplate from "../../../../assets/config/TODO.md?raw";

const SETUP_PROVIDER_NONE = "";
const SETUP_PROVIDER_OPENAI_COMPATIBLE = "openai_compatible";
const SETUP_PROVIDER_GEMINI = "gemini";
const SETUP_PROVIDER_ANTHROPIC = "anthropic";
const SETUP_PROVIDER_BEDROCK = "bedrock";
const SETUP_PROVIDER_CLOUDFLARE = "cloudflare";
const SETUP_PROVIDER_OPENAI_CODEX = "openai_codex";

const SETUP_PROVIDER_OPTIONS = [
  { title: "OpenAI Compatible", value: SETUP_PROVIDER_OPENAI_COMPATIBLE },
  { title: "OpenAI Codex OAuth", value: SETUP_PROVIDER_OPENAI_CODEX },
  { title: "Gemini", value: SETUP_PROVIDER_GEMINI },
  { title: "Anthropic", value: SETUP_PROVIDER_ANTHROPIC },
  { title: "Bedrock", value: SETUP_PROVIDER_BEDROCK },
  { title: "Cloudflare", value: SETUP_PROVIDER_CLOUDFLARE },
];

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
  { name: "TODO.md", content: todoTemplate },
  { name: "TODO.DONE.md", content: todoDoneTemplate },
  { name: "IDENTITY.md", content: identityTemplate },
  { name: "SOUL.md", content: soulTemplate },
];

function normalizeSetupProviderChoice(provider, options = {}) {
  const allowEmpty = options && options.allowEmpty === true;
  switch (String(provider || "").trim().toLowerCase()) {
    case "":
      return allowEmpty ? SETUP_PROVIDER_NONE : SETUP_PROVIDER_OPENAI_COMPATIBLE;
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
    default:
      return SETUP_PROVIDER_OPENAI_COMPATIBLE;
  }
}

function defaultEndpointForSetupProvider(choice) {
  switch (normalizeSetupProviderChoice(choice)) {
    case SETUP_PROVIDER_GEMINI:
      return "https://generativelanguage.googleapis.com";
    case SETUP_PROVIDER_ANTHROPIC:
      return "https://api.anthropic.com";
    case SETUP_PROVIDER_BEDROCK:
      return "";
    case SETUP_PROVIDER_CLOUDFLARE:
      return "https://api.cloudflare.com/client/v4";
    case SETUP_PROVIDER_OPENAI_CODEX:
      return "";
    default:
      return "https://api.openai.com";
  }
}

function normalizeSetupProviderForSave(choice, endpoint) {
  void endpoint;
  switch (normalizeSetupProviderChoice(choice)) {
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
    default:
      return "openai";
  }
}

function setupProviderSupportsModelLookup(choice) {
  return normalizeSetupProviderChoice(choice, { allowEmpty: true }) === SETUP_PROVIDER_OPENAI_COMPATIBLE;
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
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_GEMINI,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_OPENAI_COMPATIBLE,
  SETUP_PROVIDER_OPTIONS,
  SETUP_REQUIRED_MARKDOWN_FILES,
  defaultEndpointForSetupProvider,
  findOpenAICompatibleAPIBaseOption,
  normalizeSetupProviderChoice,
  normalizeSetupProviderForSave,
  resolveSetupAPIKeyHelp,
  setupProviderRequiresAPIKey,
  setupProviderSupportsModelLookup,
};
