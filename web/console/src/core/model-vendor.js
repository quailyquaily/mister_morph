import modelVendorPrefixes from "./model-vendor-prefixes.json";
import anthropicIcon from "../assets/model-vendors/anthropic.png";
import deepseekIcon from "../assets/model-vendors/deepseek.png";
import geminiIcon from "../assets/model-vendors/gemini.png";
import groqIcon from "../assets/model-vendors/groq.svg";
import kimiIcon from "../assets/model-vendors/kimi.svg";
import metaIcon from "../assets/model-vendors/meta.svg";
import mistralIcon from "../assets/model-vendors/mistral.svg";
import openaiIcon from "../assets/model-vendors/openai.svg";
import qwenIcon from "../assets/model-vendors/qwen.svg";
import sakanaIcon from "../assets/model-vendors/sakana.svg";
import xaiIcon from "../assets/model-vendors/xai.svg";
import zaiIcon from "../assets/model-vendors/zai.svg";

const MODEL_VENDOR_ICONS = {
  anthropic: anthropicIcon,
  deepseek: deepseekIcon,
  gemini: geminiIcon,
  google: geminiIcon,
  groq: groqIcon,
  kimi: kimiIcon,
  meta: metaIcon,
  mistral: mistralIcon,
  openai: openaiIcon,
  qwen: qwenIcon,
  sakana: sakanaIcon,
  xai: xaiIcon,
  zai: zaiIcon,
};

const MODEL_VENDOR_LABELS = {
  anthropic: "Anthropic",
  deepseek: "DeepSeek",
  gemini: "Gemini",
  google: "Google",
  groq: "Groq",
  kimi: "Kimi",
  meta: "Meta",
  mistral: "Mistral",
  openai: "OpenAI",
  qwen: "Qwen",
  sakana: "Sakana AI",
  xai: "xAI",
  zai: "Z.AI",
};

const MODEL_VENDOR_RULES = Array.isArray(modelVendorPrefixes)
  ? [...modelVendorPrefixes]
      .filter((item) => item && typeof item.prefix === "string" && typeof item.vendor === "string")
      .sort((a, b) => b.prefix.length - a.prefix.length)
  : [];

function modelMatchCandidates(modelName) {
  const normalized = String(modelName || "")
    .trim()
    .toLowerCase();
  if (!normalized) {
    return [];
  }
  const candidates = [normalized];
  const slashIndex = normalized.lastIndexOf("/");
  if (slashIndex >= 0 && slashIndex < normalized.length - 1) {
    candidates.push(normalized.slice(slashIndex + 1));
  }
  return [...new Set(candidates)];
}

export function modelVendorMeta(modelName) {
  const candidates = modelMatchCandidates(modelName);
  if (candidates.length === 0) {
    return { vendor: "", icon: "", label: "" };
  }
  const matchedRule = candidates
    .flatMap((candidate) => MODEL_VENDOR_RULES.filter((item) => candidate.startsWith(item.prefix)))
    .sort((a, b) => b.prefix.length - a.prefix.length)[0];
  const vendor = matchedRule?.vendor || "";
  return {
    vendor,
    icon: vendor ? MODEL_VENDOR_ICONS[vendor] || "" : "",
    label: vendor ? MODEL_VENDOR_LABELS[vendor] || vendor : "",
  };
}
