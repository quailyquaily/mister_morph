import { isMap, parseDocument } from "yaml";

const PERSONA_IDENTITY_FILE = "identity.yaml";
const PERSONA_SOUL_FILE = "soul.md";
const PERSONA_IDENTITY_ENDPOINT = `/persona/files/${PERSONA_IDENTITY_FILE}`;
const PERSONA_SOUL_ENDPOINT = `/persona/files/${PERSONA_SOUL_FILE}`;
const PERSONA_AVATAR_ENDPOINT = "/persona/avatar";
const LEGACY_IDENTITY_ENDPOINT = "/state/files/IDENTITY.md";
const LEGACY_SOUL_ENDPOINT = "/state/files/SOUL.md";
const PERSONA_IDENTITY_UPDATED_EVENT = "mistermorph:persona-identity-updated";
const PERSONA_AVATAR_UPDATED_EVENT = "mistermorph:persona-avatar-updated";
const PERSONA_AVATAR_SIZE = 512;
const PERSONA_AVATAR_MAX_SOURCE_BYTES = 10 * 1024 * 1024;
const PERSONA_AVATAR_SOURCE_TYPES = new Set(["image/png", "image/jpeg", "image/webp"]);
const IDENTITY_FIELDS = ["name", "creature", "vibe", "emoji"];
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

function extractIdentityYAML(raw) {
  const content = normalizeText(raw);
  const match = IDENTITY_YAML_FENCE_RE.exec(content);
  return match ? match[1] : content;
}

function createIdentityDocument(raw) {
  const source = extractIdentityYAML(raw).trim();
  const doc = parseDocument(source || "{}", { prettyErrors: false });
  if (doc.errors.length > 0) {
    throw new Error(`identity.yaml is invalid: ${doc.errors[0].message}`);
  }
  if (!isMap(doc.contents)) {
    return null;
  }
  return doc;
}

function parseIdentityProfile(raw) {
  const profile = buildEmptyPersonaIdentityState();
  let doc;
  try {
    doc = createIdentityDocument(raw);
  } catch {
    return profile;
  }
  if (!doc) {
    return profile;
  }
  const parsed = doc.toJS();
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return profile;
  }
  for (const field of IDENTITY_FIELDS) {
    profile[field] = trimText(parsed[field]);
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

function setIdentityField(map, field, value) {
  map.set(field, trimText(value));
}

function identityYAMLToString(doc) {
  const output = doc.toString({ lineWidth: 0 }).trim();
  return output ? `${output}\n` : "";
}

function buildDefaultIdentityYAML(values) {
  const doc = parseDocument("{}");
  doc.contents = doc.createNode({
    name: trimText(values?.name),
    name_alts: [],
    creature: trimText(values?.creature),
    vibe: trimText(values?.vibe),
    emoji: trimText(values?.emoji),
  });
  return identityYAMLToString(doc);
}

function buildIdentityYAML(values, previousRaw = "") {
  if (!trimText(previousRaw)) {
    return buildDefaultIdentityYAML(values);
  }
  const doc = createIdentityDocument(previousRaw);
  if (!doc) {
    return buildDefaultIdentityYAML(values);
  }
  const map = doc.contents;
  for (const field of IDENTITY_FIELDS) {
    setIdentityField(map, field, values?.[field]);
  }
  return identityYAMLToString(doc);
}

function normalizeSoulDocument(raw) {
  const value = normalizeText(raw).trim();
  return value ? `${value}\n` : "";
}

function dispatchPersonaAvatarUpdated() {
  window.dispatchEvent(new CustomEvent(PERSONA_AVATAR_UPDATED_EVENT, { detail: { at: Date.now() } }));
}

function dispatchPersonaIdentityUpdated() {
  window.dispatchEvent(new CustomEvent(PERSONA_IDENTITY_UPDATED_EVENT, { detail: { at: Date.now() } }));
}

export {
  buildEmptyPersonaIdentityState,
  buildIdentityYAML,
  buildPersonaIdentitySnapshot,
  dispatchPersonaIdentityUpdated,
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
  PERSONA_IDENTITY_UPDATED_EVENT,
  PERSONA_SOUL_ENDPOINT,
  PERSONA_SOUL_FILE,
};
