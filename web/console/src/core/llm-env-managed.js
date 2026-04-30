const LLM_ENV_MANAGED_SECRET_FIELDS = new Set([
  "api_key",
  "bedrock_aws_key",
  "bedrock_aws_secret",
  "cloudflare_api_token",
]);

function llmEnvEntry(envManaged, field) {
  const key = String(field || "").trim();
  if (!key || !envManaged || typeof envManaged !== "object") {
    return null;
  }
  const entry = envManaged[key];
  return entry && typeof entry === "object" ? entry : null;
}

function llmFieldEnvName(envManaged, field) {
  const entry = llmEnvEntry(envManaged, field);
  return typeof entry?.env_name === "string" ? entry.env_name : "";
}

function llmFieldEnvValue(envManaged, field) {
  const entry = llmEnvEntry(envManaged, field);
  return typeof entry?.value === "string" ? entry.value.trim() : "";
}

function llmFieldEnvRawValue(envManaged, field) {
  const entry = llmEnvEntry(envManaged, field);
  return typeof entry?.raw_value === "string" ? entry.raw_value.trim() : "";
}

function isLLMFieldEnvManaged(envManaged, field) {
  return llmFieldEnvName(envManaged, field) !== "";
}

function llmFieldValue(form, envManaged, field) {
  const key = String(field || "").trim();
  if (!key) {
    return "";
  }
  if (isLLMFieldEnvManaged(envManaged, key)) {
    return llmFieldEnvValue(envManaged, key);
  }
  const value = form && typeof form === "object" ? form[key] : "";
  return typeof value === "string" ? value.trim() : "";
}

function hasLLMFieldValue(form, envManaged, field) {
  return llmFieldValue(form, envManaged, field) !== "" || isLLMFieldEnvManaged(envManaged, field);
}

function llmFieldManagedDisplayValue(form, envManaged, field) {
  const envValue = llmFieldEnvValue(envManaged, field);
  if (envValue !== "") {
    return envValue;
  }
  const key = String(field || "").trim();
  if (LLM_ENV_MANAGED_SECRET_FIELDS.has(key)) {
    return "";
  }
  return isLLMFieldEnvManaged(envManaged, key) ? llmFieldValue(form, envManaged, key) : "";
}

function llmFieldManagedHeadline(form, envManaged, field) {
  const envName = llmFieldEnvName(envManaged, field);
  if (!envName) {
    return "";
  }
  const value = llmFieldManagedDisplayValue(form, envManaged, field);
  return value === "" ? envName : `${envName}=${value}`;
}

export {
  hasLLMFieldValue,
  isLLMFieldEnvManaged,
  llmFieldEnvName,
  llmFieldEnvRawValue,
  llmFieldEnvValue,
  llmFieldManagedDisplayValue,
  llmFieldManagedHeadline,
  llmFieldValue,
};
