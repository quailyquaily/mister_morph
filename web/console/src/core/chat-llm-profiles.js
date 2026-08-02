function text(value) {
  return String(value || "").trim();
}

export function normalizeChatLLMProfileMetadata(raw) {
  return {
    inferenceProvider: text(raw?.inference_provider || raw?.inferenceProvider),
    modelName: text(raw?.model || raw?.model_name || raw?.modelName),
  };
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
    const metadata = normalizeChatLLMProfileMetadata(raw);
    profiles.push({
      name,
      ...metadata,
    });
  }
  return profiles.sort((a, b) => a.name.localeCompare(b.name));
}

export function lastUsedChatLLMProfile(rawTasks) {
  if (!Array.isArray(rawTasks)) {
    return "";
  }
  let latestTask = null;
  let latestCreatedAt = 0;
  for (const task of rawTasks) {
    if (text(task?.steer_target_task_id)) {
      continue;
    }
    const parsedCreatedAt = Date.parse(text(task?.created_at));
    const createdAt = Number.isFinite(parsedCreatedAt) ? parsedCreatedAt : 0;
    if (latestTask === null || createdAt >= latestCreatedAt) {
      latestTask = task;
      latestCreatedAt = createdAt;
    }
  }
  return text(latestTask?.llm_profile);
}

export function resolveAvailableChatLLMProfile(rawProfile, profiles) {
  const profile = text(rawProfile);
  if (!profile) {
    return "";
  }
  return Array.isArray(profiles) && profiles.some((item) => text(item?.name) === profile) ? profile : "";
}
