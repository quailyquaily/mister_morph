import { computed } from "vue";

import { translate } from "../core/context";
import {
  hasLLMFieldValue,
  isLLMFieldEnvManaged,
  llmFieldEnvName,
  llmFieldManagedHeadline,
  llmFieldValue,
} from "../core/llm-env-managed";
import {
  defaultEndpointForSetupProvider,
  normalizeSetupProviderChoice,
  resolveSetupAPIKeyHelp,
  SETUP_PROVIDER_BEDROCK,
  SETUP_PROVIDER_CLOUDFLARE,
  SETUP_PROVIDER_OPENAI_CODEX,
  setupProviderSupportsModelLookup,
} from "../core/setup-contract";

const LLMConfigForm = {
  props: {
    config: {
      type: Object,
      required: true,
    },
    busy: Boolean,
    envManaged: {
      type: Object,
      default: () => ({}),
    },
    defaultProvider: {
      type: String,
      default: "",
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
    providerPlaceholderKey: {
      type: String,
      default: "settings_agent_provider_placeholder",
    },
    allowProviderInherit: Boolean,
    enableAPIBasePicker: Boolean,
    enableModelPicker: Boolean,
    showTestAction: Boolean,
    testActionDisabled: Boolean,
    showCodexAuthAction: Boolean,
    codexAuthState: {
      type: String,
      default: "signed-out",
    },
    codexAuthTitle: {
      type: String,
      default: "",
    },
  },
  emits: ["update-field", "open-api-base-picker", "open-model-picker", "open-test", "open-codex-auth"],
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

    const providerItem = computed(
      () => props.providerItems.find((item) => item.value === String(configValue("provider") || "").trim()) || null,
    );
    const effectiveProviderChoice = computed(() => {
      const explicitProvider = normalizeSetupProviderChoice(fieldValue("provider"), { allowEmpty: true });
      if (explicitProvider !== "") {
        return explicitProvider;
      }
      return normalizeSetupProviderChoice(props.defaultProvider, { allowEmpty: true });
    });
    const showCloudflareAccountField = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_CLOUDFLARE);
    const showCodexOAuthFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_OPENAI_CODEX);
    const showBedrockFields = computed(() => effectiveProviderChoice.value === SETUP_PROVIDER_BEDROCK);
    const showEndpointField = computed(
      () => !showCloudflareAccountField.value && !showBedrockFields.value && !showCodexOAuthFields.value,
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
    const codexAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(String(props.codexAuthState || "").trim()));
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
    const modelLookupDisabled = computed(
      () =>
        props.busy ||
        !props.enableModelPicker ||
        !showOpenAICompatibleHelpers.value ||
        !hasLLMFieldValue(props.config, props.envManaged, "api_key"),
    );
    const credentialHelp = computed(() => {
      const provider = effectiveProviderChoice.value;
      if (
        provider === "" ||
        showBedrockFields.value ||
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
      emit("update-field", { field, value: String(value || "") });
    }

    function onProviderChange(item) {
      if (!item || typeof item !== "object") {
        return;
      }
      const nextProvider = String(item.value || "").trim();
      const currentProvider = String(configValue("provider") || "").trim();
      const previousBaseProvider = currentProvider || props.defaultProvider;
      const previousDefaultEndpoint = defaultEndpointForSetupProvider(previousBaseProvider);
      const currentEndpoint = String(configValue("endpoint") || "").trim();

      updateField("provider", nextProvider);
      if (currentEndpoint !== "" && currentEndpoint !== previousDefaultEndpoint) {
        return;
      }
      if (nextProvider === "" && props.allowProviderInherit) {
        updateField("endpoint", "");
        return;
      }
      updateField("endpoint", defaultEndpointForSetupProvider(nextProvider));
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

    function openExternal(url) {
      const target = String(url || "").trim();
      if (!target) {
        return;
      }
      window.open(target, "_blank", "noopener,noreferrer");
    }

    return {
      t,
      providerItem,
      effectiveProviderChoice,
      showCloudflareAccountField,
      showCodexOAuthFields,
      showBedrockFields,
      showEndpointField,
      credentialLabelKey,
      credentialPlaceholderKey,
      credentialHintPlainKey,
      reasoningEffortItem,
      toolsEmulationItem,
      showOpenAICompatibleHelpers,
      codexAuthNeedsLogin,
      codexAuthActionClass,
      modelLookupDisabled,
      credentialHelp,
      credentialHelpParts,
      fieldEnvName,
      isFieldEnvManaged,
      fieldManagedHeadline,
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
      <div class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_provider_label") }}</span>
        <div v-if="isFieldEnvManaged('provider')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("provider") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <div v-else class="settings-field-control">
          <QDropdownMenu
            :key="String(config.provider || '') || 'provider'"
            :items="providerItems"
            :initialItem="providerItem"
            :placeholder="t(providerPlaceholderKey)"
            @change="onProviderChange"
          />
          <QButton
            v-if="showCodexAuthAction && showCodexOAuthFields"
            type="button"
            :class="codexAuthActionClass"
            :title="codexAuthTitle"
            :aria-label="codexAuthTitle"
            :disabled="busy"
            @click.prevent="$emit('open-codex-auth')"
          >
            <QIconRefresh v-if="codexAuthState === 'loading'" class="icon" />
            <QIconCheckCircle v-else-if="codexAuthState === 'signed-in'" class="icon" />
            <QIconRefresh v-else-if="codexAuthState === 'refreshable'" class="icon" />
            <template v-else-if="codexAuthNeedsLogin">{{ t("settings_codex_auth_login_codex") }}</template>
            <QIconCloseCircle v-else class="icon" />
          </QButton>
        </div>
      </div>

      <label v-if="showEndpointField" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_endpoint_label") }}</span>
        <div v-if="isFieldEnvManaged('endpoint')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("endpoint") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <div v-else class="settings-field-control">
          <QInput
            :modelValue="config.endpoint"
            :placeholder="t('settings_agent_endpoint_placeholder')"
            :disabled="busy"
            @update:modelValue="updateField('endpoint', $event)"
          />
          <QButton
            v-if="enableAPIBasePicker"
            type="button"
            class="outlined icon settings-field-action"
            :title="t('setup_llm_api_base_picker_title')"
            :aria-label="t('setup_llm_api_base_picker_title')"
            :disabled="busy || !showOpenAICompatibleHelpers"
            @click.prevent="$emit('open-api-base-picker')"
          >
            <QIconLink class="icon" />
          </QButton>
        </div>
      </label>

      <label v-if="showCloudflareAccountField" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_cloudflare_account_label") }}</span>
        <div v-if="isFieldEnvManaged('cloudflare_account_id')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("cloudflare_account_id") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.cloudflare_account_id"
          :placeholder="t('settings_agent_cloudflare_account_placeholder')"
          :disabled="busy"
          @update:modelValue="updateField('cloudflare_account_id', $event)"
        />
      </label>

      <label v-if="showBedrockFields" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_aws_key_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_aws_key')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_aws_key") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_aws_key"
          inputType="password"
          :placeholder="t('settings_agent_bedrock_aws_key_placeholder')"
          :disabled="busy"
          @update:modelValue="updateField('bedrock_aws_key', $event)"
        />
      </label>

      <label v-if="showBedrockFields" class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_aws_secret_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_aws_secret')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_aws_secret") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_aws_secret"
          inputType="password"
          :placeholder="t('settings_agent_bedrock_aws_secret_placeholder')"
          :disabled="busy"
          @update:modelValue="updateField('bedrock_aws_secret', $event)"
        />
      </label>

      <label v-if="showBedrockFields" class="settings-field">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_region_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_region')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_region") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_region"
          :placeholder="t('settings_agent_bedrock_region_placeholder')"
          :disabled="busy"
          @update:modelValue="updateField('bedrock_region', $event)"
        />
      </label>

      <label v-if="showBedrockFields" class="settings-field">
        <span class="settings-field-label">{{ t("settings_agent_bedrock_model_arn_label") }}</span>
        <div v-if="isFieldEnvManaged('bedrock_model_arn')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("bedrock_model_arn") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <QInput
          v-else
          :modelValue="config.bedrock_model_arn"
          :placeholder="t('settings_agent_bedrock_model_arn_placeholder')"
          :disabled="busy"
          @update:modelValue="updateField('bedrock_model_arn', $event)"
        />
      </label>

      <label v-if="!showBedrockFields && !showCodexOAuthFields" class="settings-field is-wide">
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
          :placeholder="t(credentialPlaceholderKey)"
          :disabled="busy"
          @update:modelValue="updateField('cloudflare_api_token', $event)"
        />
        <QInput
          v-else
          :modelValue="config.api_key"
          inputType="password"
          :placeholder="t(credentialPlaceholderKey)"
          :disabled="busy"
          @update:modelValue="updateField('api_key', $event)"
        />
        <p v-if="credentialHelp" class="settings-field-hint">
          <button v-if="credentialHelp.url" type="button" class="settings-field-link" @click="openExternal(credentialHelp.url)">
            <span>{{ credentialHelpParts?.before }}</span>
            <span class="settings-field-link-provider">{{ credentialHelp.title }}</span>
            <span>{{ credentialHelpParts?.after }}</span>
            <QIconArrowUpRight class="icon settings-field-link-icon" />
          </button>
          <span v-else class="settings-field-link is-static">
            {{ t(credentialHintPlainKey, { provider: credentialHelp.title }) }}
          </span>
        </p>
      </label>

      <label class="settings-field is-wide">
        <span class="settings-field-label">{{ t("settings_agent_model_label") }}</span>
        <div v-if="isFieldEnvManaged('model')" class="settings-env-managed">
          <code class="settings-env-managed-env">{{ fieldManagedHeadline("model") }}</code>
          <p class="settings-env-managed-body">{{ t("settings_env_managed_body") }}</p>
        </div>
        <div v-else class="settings-field-control">
          <QInput
            :modelValue="config.model"
            :placeholder="t('settings_agent_model_placeholder')"
            :disabled="busy"
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
            <QIconSearch class="icon" />
          </QButton>
        </div>
      </label>

      <label class="settings-field">
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
          @change="onReasoningEffortChange"
        />
      </label>

      <label class="settings-field">
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
          @change="onToolsEmulationChange"
        />
      </label>

      <div v-if="showTestAction" class="settings-agent-actions">
        <QButton type="button" class="outlined settings-aux-action" :disabled="testActionDisabled" @click="$emit('open-test')">
          {{ t("setup_llm_test_button") }}
        </QButton>
      </div>
    </div>
  `,
};

export default LLMConfigForm;
