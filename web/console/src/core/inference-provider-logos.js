import bedrockLogo from "../assets/model-vendors/amazon-bedrock.svg";
import claudeLogo from "../assets/model-vendors/claude.svg";
import cloudflareLogo from "../assets/model-vendors/cloudflare.svg";
import deepseekLogo from "../assets/model-vendors/deepseek.png";
import geminiLogo from "../assets/model-vendors/gemini.png";
import groqLogo from "../assets/model-vendors/groq.svg";
import kimiLogo from "../assets/model-vendors/kimi.svg";
import metaLogo from "../assets/model-vendors/meta.svg";
import openAILogo from "../assets/model-vendors/openai.svg";
import openRouterLogo from "../assets/model-vendors/openrouter.svg";
import sakanaLogo from "../assets/model-vendors/sakana.svg";
import xAILogo from "../assets/model-vendors/xai.svg";
import misterMorphLogo from "../assets/images/app_logo_current.svg";

const INFERENCE_PROVIDER_LOGOS = {
  openai: { src: openAILogo, className: "is-openai" },
  openai_codex: { src: openAILogo, className: "is-openai is-codex", badge: "Codex" },
  gemini: { src: geminiLogo, className: "is-gemini" },
  anthropic: { src: claudeLogo, className: "is-claude" },
  bedrock: { src: bedrockLogo, className: "is-bedrock" },
  cloudflare: { src: cloudflareLogo, className: "is-cloudflare" },
  mistermorph_pro: { src: misterMorphLogo, className: "is-mistermorph" },
  xai: { src: xAILogo, className: "is-xai" },
  deepseek: { src: deepseekLogo, className: "is-deepseek" },
  kimi: { src: kimiLogo, className: "is-kimi" },
  meta: { src: metaLogo, className: "is-meta" },
  openrouter: { src: openRouterLogo, className: "is-openrouter" },
  groq: { src: groqLogo, className: "is-groq" },
  sakana: { src: sakanaLogo, className: "is-sakana" },
  openai_chat_compatible: { src: openAILogo, className: "is-openai is-compatible", badge: "Chat" },
  openai_response_compatible: { src: openAILogo, className: "is-openai is-compatible", badge: "Resp" },
  anthropic_compatible: { src: claudeLogo, className: "is-claude is-compatible", badge: "API" },
};

export function inferenceProviderLogo(value) {
  const provider = String(value || "")
    .trim()
    .toLowerCase();
  return INFERENCE_PROVIDER_LOGOS[provider] || { src: "", className: "is-fallback", badge: "" };
}
