function displayValue(value, type) {
  if (type === "string_list") {
    return Array.isArray(value) ? value.map((item) => String(item)).join("\n") : "";
  }
  if (type === "json") {
    return JSON.stringify(value ?? null, null, 2);
  }
  if (type === "bool") {
    return value === true;
  }
  return value === undefined || value === null ? "" : String(value);
}

export function createConfigDraft(values, fields) {
  const source = values && typeof values === "object" ? values : {};
  const draft = {};
  for (const field of Array.isArray(fields) ? fields : []) {
    draft[field.path] = displayValue(source[field.path], field.type);
  }
  return draft;
}

function parseValue(value, field) {
  if (field.type === "bool") {
    return value === true;
  }
  if (field.type === "int") {
    const text = String(value ?? "").trim();
    if (!/^-?\d+$/.test(text)) {
      throw new Error(`${field.label || field.path} must be an integer`);
    }
    const parsed = Number(text);
    if (!Number.isSafeInteger(parsed)) {
      throw new Error(`${field.label || field.path} must be a safe integer`);
    }
    return parsed;
  }
  if (field.type === "float") {
    const text = String(value ?? "").trim();
    const parsed = Number(text);
    if (text === "" || !Number.isFinite(parsed)) {
      throw new Error(`${field.label || field.path} must be a number`);
    }
    return parsed;
  }
  if (field.type === "string_list") {
    const seen = new Set();
    const items = [];
    for (const line of String(value ?? "").split(/\r?\n/)) {
      const item = line.trim();
      if (item === "" || seen.has(item)) {
        continue;
      }
      seen.add(item);
      items.push(item);
    }
    return items;
  }
  if (field.type === "json") {
    try {
      return JSON.parse(String(value ?? ""));
    } catch {
      throw new Error(`${field.label || field.path} must be valid JSON`);
    }
  }
  return String(value ?? "").trim();
}

export function buildConfigUpdate(draft, original, resetPaths, fields) {
  const resets = new Set(Array.isArray(resetPaths) ? resetPaths : []);
  const changes = {};
  for (const field of Array.isArray(fields) ? fields : []) {
    if (resets.has(field.path) || Object.is(draft[field.path], original[field.path])) {
      continue;
    }
    changes[field.path] = parseValue(draft[field.path], field);
  }
  return {
    config_changes: changes,
    reset: [...resets],
  };
}
