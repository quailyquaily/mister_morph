import { computed } from "vue";

import { translate } from "../core/context";
import { openExternalURL as openExternal } from "../core/external-links";
import {
  hasLLMFieldValue,
  isLLMFieldEnvManaged,
  llmFieldEnvName,
  llmFieldManagedHeadline,
  llmFieldValue,
} from "../core/llm-env-managed";
import {
  normalizeSetupProviderChoice,
  resolveSetupAPIKeyHelp,
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_MISTERMORPH_PRO,
  SETUP_PROVIDER_OPENAI_CODEX,
  SETUP_PROVIDER_XAI_OAUTH,
  setupProviderRequiresAPIBase,
  setupProviderRequiresAPIKey,
  setupProviderSupportsCustomAPIBase,
  setupProviderSupportsAPIKey,
  setupProviderSupportsModelLookup,
} from "../core/setup-contract";
import InferenceProviderPicker from "./InferenceProviderPicker";

const LLMConfigForm = {
  components: {
    InferenceProviderPicker,
  },
  props: {
    config: {
      type: Object,
      required: true,
    },
    busy: Boolean,
    disabledReason: {
      type: String,
      default: "",
    },
    envManaged: {
      type: Object,
      default: () => ({}),
    },
    secretFields: {
      type: Object,
      default: () => ({}),
    },
    providerItems: {
      type: Array,
      default: () => [],
    },
    reasoningEffortItems: {
      type: Array,
      default: () => [],
    },
    toolsEmulationItems: {
      type: Array,
      default: () => [],
    },
    enableAPIBasePicker: Boolean,
    enableModelPicker: Boolean,
    showAdvanced: Boolean,
    advancedOnly: Boolean,
    modelLookupCredentialsReady: {
      default: null,
      validator: (value) => value === null || typeof value === "boolean",
    },
    readOnly: Boolean,
    showCodexAuthAction: Boolean,
    codexAuthState: {
      type: String,
      default: "signed-out",
    },
    codexAuthTitle: {
      type: String,
      default: "",
    },
    codexAuthDisabled: Boolean,
    showXAIAuthAction: Boolean,
    xaiAuthState: {
      type: String,
      default: "signed-out",
    },
    xaiAuthTitle: {
      type: String,
      default: "",
    },
    showProAuthAction: Boolean,
    proAuthState: {
      type: String,
      default: "signed-out",
    },
    proAuthTitle: {
      type: String,
      default: "",
    },
  },
  emits: [
    "update-field",
    "open-api-base-picker",
    "open-model-picker",
    "open-codex-auth",
    "open-xai-auth",
    "open-pro-auth",
  ],
  setup(props, { emit }) {
    const t = translate;

    function configValue(field) {
      const value = props.config && typeof props.config === "object" ? props.config[field] : "";
      return typeof value === "string" ? value : "";
    }

    function fieldEnvName(field) {
      return llmFieldEnvName(props.envManaged, field);
    }

    function isFieldEnvManaged(field) {
      return isLLMFieldEnvManaged(props.envManaged, field);
    }

    function fieldValue(field) {
      return llmFieldValue(props.config, props.envManaged, field);
    }

    function fieldManagedHeadline(field) {
      return llmFieldManagedHeadline(props.config, props.envManaged, field);
    }

    function isSecretConfigured(field) {
      return props.secretFields?.[field]?.configured === true;
    }

    function isSecretEditable(field) {
      return props.secretFields?.[field]?.editable !== false;
    }

    function secretPlaceholder(field, fallbackKey) {
      return isSecretConfigured(field) ? t("settings_secret_configured_placeholder") : t(fallbackKey);
    }

    const providerItem = computed(() => {
      const provider = normalizeSetupProviderChoice(configValue("inference_provider") || configValue("provider"), { allowEmpty: true });
      return props.providerItems.find((item) => item.value === provider) || null;
    });
    const providerManagedField = computed(() => {
      if (isFieldEnvManaged("inference_provider")) {
        return "inference_provider";
      }
      if (isFieldEnvManaged("provider")) {
        return "provider";
      }
      return "";
    });
    const effectiveProviderChoice = computed(() => {
      return normalizeSetupProviderChoice(fieldValue("inference_provider") || fieldValue("provider"), { allowEmpty: true });
    });
    const showCloudflareAccountField = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_CLOUDFLARE);
    const showCodexOAuthFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_OPENAI_CODEX);
    const showXAIOAuthFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_XAI_OAUTH);
    const showProOAuthFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_MISTERMORPH_PRO);
    const showBedrockFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_BEDROCK);
    const showAzureFields = computed(() => effectiveProviderChoice.value === "azure");
    const showEndpointField = computed(() => setupProviderSupportsCustomAPIBase(effectiveProviderChoice.value));
    const showCredentialFields = computed(
      () =>
        !showBedrockFields.value &&
        !showXAIOAuthFields.value &&
        !showProOAuthFields.value &&
        (showCloudflareAccountField.value || setupProviderSupportsAPIKey(effectiveProviderChoice.value)),
    );
    const credentialLabelKey = computed(() =>
      showCloudflareAccountField.value ? "settings_agent_cloudflare_api_token_label" : "settings_agent_api_key_label",
    );
    const credentialPlaceholderKey = computed(() =>
      showCloudflareAccountField.value
        ? "settings_agent_cloudflare_api_token_placeholder"
        : "settings_agent_api_key_placeholder",
    );
    const credentialHintKey = computed(() =>
      showCloudflareAccountField.value ? "setup_llm_api_token_hint" : "setup_llm_api_key_hint",
    );
    const credentialHintPlainKey = computed(() =>
      showCloudflareAccountField.value ? "setup_llm_api_token_hint_plain" : "setup_llm_api_key_hint_plain",
    );
    const reasoningEffortItem = computed(
      () =>
        props.reasoningEffortItems.find((item) => item.value === String(configValue("reasoning_effort") || "").trim()) ||
        props.reasoningEffortItems[0] ||
        null,
    );
    const toolsEmulationItem = computed(
      () =>
        props.toolsEmulationItems.find((item) => item.value === String(configValue("tools_emulation_mode") || "").trim()) ||
        props.toolsEmulationItems[0] ||
        null,
    );
    const showOpenAICompatibleHelpers = computed(
      () =>
        setupProviderSupportsModelLookup(effectiveProviderChoice.value) &&
        (props.enableAPIBasePicker || props.enableModelPicker),
    );
    const providerHasAuthAction = computed(
      () =>
        (props.showCodexAuthAction && showCodexOAuthFields.value) ||
        (props.showXAIAuthAction && showXAIOAuthFields.value) ||
        (props.showProAuthAction && showProOAuthFields.value),
    );
    const endpointHasPickerAction = computed(() => props.enableAPIBasePicker);
    const codexAuthNeedsLogin = computed(() => String(props.codexAuthState || "").trim() === "signed-out");
    const xaiAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(String(props.xaiAuthState || "").trim()));
    const proAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(String(props.proAuthState || "").trim()));
    const codexAuthActionClass = computed(() =>
      [
        "outlined",
        codexAuthNeedsLogin.value ? "" : "icon",
        "settings-field-action",
        "settings-codex-auth-button",
        codexAuthNeedsLogin.value ? "is-login" : "",
        `is-${String(props.codexAuthState || "signed-out").trim() || "signed-out"}`,
      ]
        .filter(Boolean)
        .join(" "),
    );
    const proAuthActionClass = computed(() =>
      [
        "outlined",
        proAuthNeedsLogin.value ? "" : "icon",
        "settings-field-action",
        "settings-codex-auth-button",
        proAuthNeedsLogin.value ? "is-login" : "",
        `is-${String(props.proAuthState || "signed-out").trim() || "signed-out"}`,
      ]
        .filter(Boolean)
        .join(" "),
    );
    const xaiAuthActionClass = computed(() =>
      [
        "outlined",
        xaiAuthNeedsLogin.value ? "" : "icon",
        "settings-field-action",
        "settings-codex-auth-button",
        xaiAuthNeedsLogin.value ? "is-login" : "",
        `is-${String(props.xaiAuthState || "signed-out").trim() || "signed-out"}`,
      ]
        .filter(Boolean)
        .join(" "),
    );
    const modelLookupCredentialsReadyValue = computed(() => {
      if (props.modelLookupCredentialsReady !== null) {
        return props.modelLookupCredentialsReady === true;
      }
      if (showProOAuthFields.value) {
        return !proAuthNeedsLogin.value;
      }
      return hasLLMFieldValue(props.config, props.envManaged, "api_key") || isSecretConfigured("api_key");
    });
    const modelLookupDisabled = computed(
      () =>
        props.busy ||
        props.readOnly ||
        !props.enableModelPicker ||
        !showOpenAICompatibleHelpers.value ||
        !modelLookupCredentialsReadyValue.value,
    );
    const credentialHelp = computed(() => {
      const provider = effectiveProviderChoice.value;
      if (
        provider === "" ||
        showBedrockFields.value ||
        setupProviderRequiresAPIBase(provider) ||
        isFieldEnvManaged(showCloudflareAccountField.value ? "cloudflare_api_token" : "api_key")
      ) {
        return null;
      }
      return resolveSetupAPIKeyHelp(provider, fieldValue("endpoint"));
    });
    const credentialHelpParts = computed(() => {
      if (!credentialHelp.value) {
        return null;
      }
      const marker = "__PROVIDER__";
      const template = String(t(credentialHintKey.value, { provider: marker }) || "");
      const index = template.indexOf(marker);
      if (index === -1) {
        return {
          before: template.trim(),
          after: "",
        };
      }
      return {
        before: template.slice(0, index),
        after: template.slice(index + marker.length),
      };
    });

    function updateField(field, value) {
      if (props.readOnly) {
        return;
      }
      emit("update-field", { field, value: String(value || "") });
    }

    function onProviderChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      const nextProvider = String(item.value || "").trim();
      const currentProvider = String(configValue("inference_provider") || configValue("provider") || "").trim();
      const currentEndpoint = String(configValue("endpoint") || "").trim();

      updateField("inference_provider", nextProvider);
      if (
        setupProviderSupportsCustomAPIBase(nextProvider) &&
        setupProviderSupportsCustomAPIBase(currentProvider) &&
        currentEndpoint !== ""
      ) {
        return;
      }
      updateField("endpoint", "");
      const normalizedProvider = normalizeSetupProviderChoice(nextProvider, { allowEmpty: true });
      if (
        normalizedProvider === SETUP_PROVIDER_XAI_OAUTH ||
        normalizedProvider === SETUP_PROVIDER_MISTERMORPH_PRO
      ) {
        updateField("api_key", "");
        updateField("cloudflare_api_token", "");
        updateField("cloudflare_account_id", "");
        updateField("bedrock_aws_key", "");
        updateField("bedrock_aws_secret", "");
        updateField("bedrock_aws_session_token", "");
        updateField("bedrock_aws_profile", "");
        updateField("bedrock_region", "");
        updateField("bedrock_model_arn", "");
      }
      if (normalizedProvider === SETUP_PROVIDER_XAI_OAUTH) {
        updateField("model", "grok-4.5");
      }
    }

    function onReasoningEffortChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      updateField("reasoning_effort", item.value);
    }

    function onToolsEmulationChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      updateField("tools_emulation_mode", item.value);
    }

    return {
      t,
      providerItem,
      providerManagedField,
      effectiveProviderChoice,
      showCloudflareAccountField,
      showCodexOAuthFields,
      showXAIOAuthFields,
      showProOAuthFields,
      showBedrockFields,
      showAzureFields,
      showEndpointField,
      showCredentialFields,
      providerHasAuthAction,
      endpointHasPickerAction,
      credentialLabelKey,
      credentialPlaceholderKey,
      credentialHintPlainKey,
      reasoningEffortItem,
      toolsEmulationItem,
      showOpenAICompatibleHelpers,
      codexAuthNeedsLogin,
      xaiAuthNeedsLogin,
      proAuthNeedsLogin,
      codexAuthActionClass,
      xaiAuthActionClass,
      proAuthActionClass,
      modelLookupDisabled,
      credentialHelp,
      credentialHelpParts,
      fieldEnvName,
      isFieldEnvManaged,
      fieldManagedHeadline,
      isSecretConfigured,
      isSecretEditable,
      secretPlaceholder,
      fieldValue,
      updateField,
      onProviderChange,
      onReasoningEffortChange,
      onToolsEmulationChange,
      openExternal,
    };
  },
  template: `
    <div class="settings-form-grid">
      <template v-if="!advancedOnly">
      <div class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_provider_label") }}</span>
        <div v-if="providerHasAuthAction" class="settings-field-control">
          <div v-if="providerManagedField" class="settings-env-managed">
            <code class="settings-env-managed-env">{{ fieldManagedHeadline(providerManagedField) }}</code>
            <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
          </div>
          <InferenceProviderPicker
            v-else
            :modelValue="providerItem?.value || ''"
            :items="providerItems"
            :placeholder="t('settings_agent_provider_placeholder')"
            :disabled="busy || readOnly"
            :disabledReason="disabledReason"
            :readOnly="readOnly"
            @change="onProviderChange"
          />
          <QButton
            v-if="showCodexAuthAction && showCodexOAuthFields"
            type="button"
            :class="codexAuthActionClass"
            :title="codexAuthTitle"
            :aria-label="codexAuthTitle"
            :disabled="codexAuthDisabled"
            @click.prevent="$emit('open-codex-auth')"
          >
            <PhArrowClockwise v-if="codexAuthState === 'loading'" class="icon" />
            <PhCheckCircle v-else-if="codexAuthState === 'signed-in'" class="icon" />
            <template v-else-if="codexAuthNeedsLogin">{{ t("settings_codex_auth_login_codex") }}</template>
            <PhXCircle v-else class="icon" />
          </QButton>
          <QButton
            v-if="showXAIAuthAction && showXAIOAuthFields"
            type="button"
            :class="xaiAuthActionClass"
            :title="xaiAuthTitle"
            :aria-label="xaiAuthTitle"
            :disabled="busy"
            @click.prevent="$emit('open-xai-auth')"
          >
            <PhArrowClockwise v-if="xaiAuthState === 'loading'" class="icon" />
            <PhCheckCircle v-else-if="xaiAuthState === 'signed-in'" class="icon" />
            <PhArrowClockwise v-else-if="xaiAuthState === 'refreshable'" class="icon" />
            <template v-else-if="xaiAuthNeedsLogin">{{ t("settings_xai_auth_login") }}</template>
            <PhXCircle v-else class="icon" />
          </QButton>
          <QButton
            v-if="showProAuthAction && showProOAuthFields"
            type="button"
            :class="proAuthActionClass"
            :title="proAuthTitle"
            :aria-label="proAuthTitle"
            :disabled="busy"
            @click.prevent="$emit('open-pro-auth')"
          >
            <PhArrowClockwise v-if="proAuthState === 'loading'" class="icon" />
            <PhCheckCircle v-else-if="proAuthState === 'signed-in'" class="icon" />
            <PhArrowClockwise v-else-if="proAuthState === 'refreshable'" class="icon" />
            <template v-else-if="proAuthNeedsLogin">{{ t("settings_pro_auth_login_pro") }}</template>
            <PhXCircle v-else class="icon" />
          </QButton>
        </div>
        <div v-else-if="providerManagedField" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline(providerManagedField) }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <InferenceProviderPicker
          v-else
          :modelValue="providerItem?.value || ''"
          :items="providerItems"
          :placeholder="t('settings_agent_provider_placeholder')"
          :disabled="busy || readOnly"
          :disabledReason="disabledReason"
          :readOnly="readOnly"
          @change="onProviderChange"
        />
      </div>

      <div v-if="showEndpointField" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_endpoint_label") }}</span>
        <div v-if="isFieldEnvManaged('endpoint')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("endpoint") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <div v-else-if="endpointHasPickerAction" class="settings-field-control">
          <QInput
            :modelValue="config.endpoint"
            :placeholder="t('settings_agent_endpoint_placeholder')"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('endpoint', $event)"
          />
          <QButton
            type="button"
            class="outlined icon settings-field-action"
            :title="t('setup_llm_api_base_picker_title')"
            :aria-label="t('setup_llm_api_base_picker_title')"
            :disabled="busy || readOnly || !showOpenAICompatibleHelpers"
            @click.prevent="$emit('open-api-base-picker')"
          >
            <PhLink class="icon" />
          </QButton>
        </div>
        <QInput
          v-else
          :modelValue="config.endpoint"
          :placeholder="t('settings_agent_endpoint_placeholder')"
          :disabled="busy || readOnly"
          @update:modelValue="updateField('endpoint', $event)"
        />
      </div>

      <div v-if="showCloudflareAccountField" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_cloudflare_account_label") }}</span>
        <div v-if="isFieldEnvManaged('cloudflare_account_id')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("cloudflare_account_id") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.cloudflare_account_id"
          :placeholder="t('settings_agent_cloudflare_account_placeholder')"
          :disabled="busy || readOnly"
          @update:modelValue="updateField('cloudflare_account_id', $event)"
        />
      </div>

      <div v-if="showBedrockFields" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_aws_key_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_aws_key')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_aws_key") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_aws_key"
          inputType="password"
          :placeholder="secretPlaceholder('bedrock_aws_key', 'settings_agent_bedrock_aws_key_placeholder')"
          :disabled="busy || readOnly || !isSecretEditable('bedrock_aws_key')"
          @update:modelValue="updateField('bedrock_aws_key', $event)"
        />
      </div>

      <div v-if="showBedrockFields" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_aws_secret_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_aws_secret')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_aws_secret") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_aws_secret"
          inputType="password"
          :placeholder="secretPlaceholder('bedrock_aws_secret', 'settings_agent_bedrock_aws_secret_placeholder')"
          :disabled="busy || readOnly || !isSecretEditable('bedrock_aws_secret')"
          @update:modelValue="updateField('bedrock_aws_secret', $event)"
        />
      </div>

      <div v-if="showBedrockFields" class="settings-field">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_region_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_region')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_region") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_region"
          :placeholder="t('settings_agent_bedrock_region_placeholder')"
          :disabled="busy || readOnly"
          @update:modelValue="updateField('bedrock_region', $event)"
        />
      </div>

      <div v-if="showBedrockFields" class="settings-field">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_model_arn_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_model_arn')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_model_arn") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_model_arn"
          :placeholder="t('settings_agent_bedrock_model_arn_placeholder')"
          :disabled="busy || readOnly"
          @update:modelValue="updateField('bedrock_model_arn', $event)"
        />
      </div>

      <div v-if="showCredentialFields" class="settings-field">
        <span class="settings-field-label">{{ t(credentialLabelKey) }}</span>
        <div
          v-if="showCloudflareAccountField ? isFieldEnvManaged('cloudflare_api_token') : isFieldEnvManaged('api_key')"
          class="settings-env-managed"
        >
          <code class="settings-env-managed-env">
            {{ fieldManagedHeadline(showCloudflareAccountField ? "cloudflare_api_token" : "api_key") }}
          </code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else-if="showCloudflareAccountField"
          :modelValue="config.cloudflare_api_token"
          inputType="password"
          :placeholder="secretPlaceholder('cloudflare_api_token', credentialPlaceholderKey)"
          :disabled="busy || readOnly || !isSecretEditable('cloudflare_api_token')"
          @update:modelValue="updateField('cloudflare_api_token', $event)"
        />
        <QInput
          v-else
          :modelValue="config.api_key"
          inputType="password"
          :placeholder="secretPlaceholder('api_key', credentialPlaceholderKey)"
          :disabled="busy || readOnly || !isSecretEditable('api_key')"
          @update:modelValue="updateField('api_key', $event)"
        />
        <p v-if="credentialHelp" class="settings-field-hint">
          <button v-if="credentialHelp.url" type="button" class="settings-field-link" @click="openExternal(credentialHelp.url)">
            <span>{{ credentialHelpParts?.before }}</span>
            <span class="settings-field-link-provider">{{ credentialHelp.title }}</span>
            <span>{{ credentialHelpParts?.after }}</span>
            <PhArrowUpRight class="icon settings-field-link-icon" />
          </button>
          <span v-else class="settings-field-link is-static">
            {{ t(credentialHintPlainKey, { provider: credentialHelp.title }) }}
          </span>
        </p>
      </div>

      <div :class="['settings-field', showCredentialFields ? '' : 'is-wide']">
        <span class="settings-field-label">{{ t("settings_agent_model_label") }}</span>
        <div v-if="isFieldEnvManaged('model')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("model") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <div v-else class="settings-field-control">
          <QInput
            :modelValue="config.model"
            :placeholder="t('settings_agent_model_placeholder')"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('model', $event)"
          />
          <QButton
            v-if="enableModelPicker"
            type="button"
            class="outlined icon settings-field-action"
            :title="t('setup_llm_model_picker_title')"
            :aria-label="t('setup_llm_model_picker_title')"
            :disabled="modelLookupDisabled"
            @click.prevent="$emit('open-model-picker')"
          >
            <PhMagnifyingGlass class="icon" />
          </QButton>
        </div>
      </div>
      </template>

      <div v-if="showAdvanced" class="settings-field-row is-wide is-three">
        <div class="settings-field">
          <span class="settings-field-label">{{ t("settings_llm_reasoning_label") }}</span>
          <div v-if="isFieldEnvManaged('reasoning_effort')" class="settings-env-managed">
            <code class="settings-env-managed-env">{{ fieldManagedHeadline("reasoning_effort") }}</code>
            <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
          </div>
          <QDropdownMenu
            v-else
            :key="String(config.reasoning_effort || '') || 'reasoning'"
            :items="reasoningEffortItems"
            :initialItem="reasoningEffortItem"
            :placeholder="t('settings_llm_reasoning_placeholder')"
            :disabled="busy || readOnly"
            @change="onReasoningEffortChange"
          />
        </div>

        <div class="settings-field">
          <span class="settings-field-label">{{ t("settings_llm_tools_emulation_label") }}</span>
          <div v-if="isFieldEnvManaged('tools_emulation_mode')" class="settings-env-managed">
            <code class="settings-env-managed-env">{{ fieldManagedHeadline("tools_emulation_mode") }}</code>
            <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
          </div>
          <QDropdownMenu
            v-else
            :key="String(config.tools_emulation_mode || '') || 'tools-emulation'"
            :items="toolsEmulationItems"
            :initialItem="toolsEmulationItem"
            :placeholder="t('settings_llm_tools_emulation_placeholder')"
            :disabled="busy || readOnly"
            @change="onToolsEmulationChange"
          />
        </div>

        <div class="settings-field">
          <span class="settings-field-label">{{ t("settings_agent_context_window_tokens_label") }}</span>
          <div v-if="isFieldEnvManaged('context_window_tokens')" class="settings-env-managed">
            <code class="settings-env-managed-env">{{ fieldManagedHeadline("context_window_tokens") }}</code>
            <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
          </div>
          <QInput
            v-else
            :modelValue="config.context_window_tokens"
            inputType="number"
            min="0"
            step="1"
            :placeholder="t('settings_agent_context_window_tokens_placeholder')"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('context_window_tokens', $event)"
          />
        </div>
      </div>

      <template v-if="showAdvanced">
        <div v-if="showBedrockFields" class="settings-field is-wide">
          <span class="settings-field-label">AWS session token</span>
          <QInput
            :modelValue="config.bedrock_aws_session_token"
            inputType="password"
            :placeholder="isSecretConfigured('bedrock_aws_session_token') ? t('settings_secret_configured_placeholder') : ''"
            :disabled="busy || readOnly || !isSecretEditable('bedrock_aws_session_token')"
            @update:modelValue="updateField('bedrock_aws_session_token', $event)"
          />
        </div>
        <div v-if="showBedrockFields" class="settings-field">
          <span class="settings-field-label">AWS profile</span>
          <QInput
            :modelValue="config.bedrock_aws_profile"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('bedrock_aws_profile', $event)"
          />
        </div>
        <div v-if="showAzureFields" class="settings-field is-wide">
          <span class="settings-field-label">Azure deployment</span>
          <QInput
            :modelValue="config.azure_deployment"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('azure_deployment', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Supports image parts</span>
          <QInput
            :modelValue="config.supports_image_parts"
            placeholder="auto, true, or false"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('supports_image_parts', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Cache TTL</span>
          <QInput
            :modelValue="config.cache_ttl"
            placeholder="off, short, long, or 5m"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('cache_ttl', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Cache key prefix</span>
          <QInput
            :modelValue="config.cache_key_prefix"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('cache_key_prefix', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Request timeout</span>
          <QInput
            :modelValue="config.request_timeout"
            placeholder="90s"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('request_timeout', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Temperature</span>
          <QInput
            :modelValue="config.temperature"
            inputType="number"
            step="0.1"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('temperature', $event)"
          />
        </div>
        <div class="settings-field">
          <span class="settings-field-label">Reasoning budget tokens</span>
          <QInput
            :modelValue="config.reasoning_budget_tokens"
            inputType="number"
            min="0"
            step="1"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('reasoning_budget_tokens', $event)"
          />
        </div>
        <div class="settings-field is-wide">
          <span class="settings-field-label">HTTP headers</span>
          <QTextarea
            :modelValue="config.headers_text"
            placeholder="{}"
            :disabled="busy || readOnly"
            @update:modelValue="updateField('headers_text', $event)"
          />
        </div>
      </template>
    </div>
  `,
};

export default LLMConfigForm;
