const PERSONA_IDENTITY_FILE = "identity.yaml";
const PERSONA_SOUL_FILE = "soul.md";
const PERSONA_IDENTITY_ENDPOINT = `/persona/files/${PERSONA_IDENTITY_FILE}`;
const PERSONA_SOUL_ENDPOINT = `/persona/files/${PERSONA_SOUL_FILE}`;
const PERSONA_AVATAR_ENDPOINT = "/persona/avatar";
const LEGACY_IDENTITY_ENDPOINT = "/state/files/IDENTITY.md";
const LEGACY_SOUL_ENDPOINT = "/state/files/SOUL.md";
const PERSONA_AVATAR_UPDATED_EVENT = "mistermorph:persona-avatar-updated";
const PERSONA_AVATAR_SIZE = 512;
const PERSONA_AVATAR_MAX_SOURCE_BYTES = 10 * 1024 * 1024;
const PERSONA_AVATAR_SOURCE_TYPES = new Set(["image/png", "image/jpeg", "image/webp"]);
const IDENTITY_FIELDS = ["name", "name_alts", "creature", "vibe", "emoji"];
const IDENTITY_YAML_FENCE_RE = /```ya?ml\s*\n([\s\S]*?)```/i;

function trimText(value) {
  return String(value || "").trim();
}

function normalizeText(value) {
  return String(value || "").replace(/\r\n/g, "\n");
}

function buildEmptyPersonaIdentityState() {
  return {
    name: "",
    creature: "",
    vibe: "",
    emoji: "",
  };
}

function yamlString(value) {
  return JSON.stringify(trimText(value));
}

function parseYAMLScalar(value) {
  const raw = trimText(value);
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

function extractIdentityYAML(raw) {
  const content = normalizeText(raw);
  const match = IDENTITY_YAML_FENCE_RE.exec(content);
  return match ? match[1] : content;
}

function parseIdentityProfile(raw) {
  const profile = buildEmptyPersonaIdentityState();
  const lines = extractIdentityYAML(raw).split("\n");
  for (const line of lines) {
    const lineMatch = /^\s*(name|creature|vibe|emoji)\s*:\s*(.*)\s*$/.exec(line);
    if (!lineMatch) {
      continue;
    }
    profile[lineMatch[1]] = parseYAMLScalar(lineMatch[2]);
  }
  return profile;
}

function buildPersonaIdentitySnapshot(values) {
  return JSON.stringify({
    name: trimText(values?.name),
    creature: trimText(values?.creature),
    vibe: trimText(values?.vibe),
    emoji: trimText(values?.emoji),
  });
}

function buildKnownIdentityYAML(values) {
  return [
    `name: ${yamlString(values?.name)}`,
    "name_alts: []",
    `creature: ${yamlString(values?.creature)}`,
    `vibe: ${yamlString(values?.vibe)}`,
    `emoji: ${yamlString(values?.emoji)}`,
  ].join("\n");
}

function stripKnownIdentityYAMLFields(yamlBlock) {
  const lines = normalizeText(yamlBlock).split("\n");
  const out = [];
  let skipping = false;
  for (const line of lines) {
    const topLevelMatch = /^([A-Za-z_][A-Za-z0-9_]*)\s*:/.exec(line);
    if (topLevelMatch) {
      skipping = IDENTITY_FIELDS.includes(topLevelMatch[1]);
      if (!skipping) {
        out.push(line);
      }
      continue;
    }
    if (!skipping) {
      out.push(line);
    }
  }
  return out.join("\n").trim();
}

function buildIdentityYAML(values, previousRaw = "") {
  const known = buildKnownIdentityYAML(values);
  const preserved = stripKnownIdentityYAMLFields(extractIdentityYAML(previousRaw));
  return `${preserved ? `${known}\n${preserved}` : known}\n`;
}

function normalizeSoulDocument(raw) {
  const value = normalizeText(raw).trim();
  return value ? `${value}\n` : "";
}

function dispatchPersonaAvatarUpdated() {
  window.dispatchEvent(new CustomEvent(PERSONA_AVATAR_UPDATED_EVENT, { detail: { at: Date.now() } }));
}

export {
  buildEmptyPersonaIdentityState,
  buildIdentityYAML,
  buildPersonaIdentitySnapshot,
  dispatchPersonaAvatarUpdated,
  LEGACY_IDENTITY_ENDPOINT,
  LEGACY_SOUL_ENDPOINT,
  normalizeSoulDocument,
  parseIdentityProfile,
  PERSONA_AVATAR_ENDPOINT,
  PERSONA_AVATAR_MAX_SOURCE_BYTES,
  PERSONA_AVATAR_SIZE,
  PERSONA_AVATAR_SOURCE_TYPES,
  PERSONA_AVATAR_UPDATED_EVENT,
  PERSONA_IDENTITY_ENDPOINT,
  PERSONA_IDENTITY_FILE,
  PERSONA_SOUL_ENDPOINT,
  PERSONA_SOUL_FILE,
};
