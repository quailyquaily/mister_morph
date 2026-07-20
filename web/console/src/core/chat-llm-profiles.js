function text(value) {
  return String(value || "").trim();
}

export function normalizeChatLLMProfiles(rawItems) {
  if (!Array.isArray(rawItems)) {
    return [];
  }
  const seen = new Set();
  const profiles = [];
  for (const raw of rawItems) {
    const name = text(raw?.name);
    if (!name || seen.has(name)) {
      continue;
    }
    seen.add(name);
    profiles.push({
      name,
      inferenceProvider: text(raw?.inference_provider || raw?.inferenceProvider),
      modelName: text(raw?.model || raw?.model_name || raw?.modelName),
    });
  }
  return profiles.sort((a, b) => a.name.localeCompare(b.name));
}
