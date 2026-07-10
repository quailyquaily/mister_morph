import { computed, ref } from "vue";

import { translate } from "../core/context";
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
import AppDialogShell from "./AppDialogShell";
import "./InferenceProviderPicker.css";

const PROVIDER_LOGOS = {
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

function normalizeText(value) {
  return String(value || "").trim();
}

function fallbackLogoText(item) {
  const title = normalizeText(item?.title);
  if (!title) {
    return "LLM";
  }
  const compact = title
    .split(/\s+/)
    .map((part) => part[0])
    .join("")
    .slice(0, 3)
    .toUpperCase();
  return compact || title.slice(0, 2).toUpperCase();
}

function providerLogo(item) {
  const value = normalizeText(item?.value);
  if (value === "") {
    return { src: "", className: "is-inherit", badge: "" };
  }
  return PROVIDER_LOGOS[value] || { src: "", className: "is-fallback", badge: "" };
}

function providerLogoClass(item) {
  return providerLogo(item).className || "is-fallback";
}

const InferenceProviderPicker = {
  components: {
    AppDialogShell,
  },
  props: {
    modelValue: {
      type: String,
      default: "",
    },
    items: {
      type: Array,
      default: () => [],
    },
    placeholder: {
      type: String,
      default: "",
    },
    disabled: Boolean,
    readOnly: Boolean,
  },
  emits: ["change"],
  setup(props, { emit }) {
    const t = translate;
    const open = ref(false);
    const filter = ref("");

    const normalizedItems = computed(() =>
      (Array.isArray(props.items) ? props.items : [])
        .map((item) => ({
          title: normalizeText(item?.title),
          value: normalizeText(item?.value),
          note: normalizeText(item?.note),
        }))
        .filter((item) => item.title !== "" || item.value !== ""),
    );
    const selectedItem = computed(
      () => normalizedItems.value.find((item) => item.value === normalizeText(props.modelValue)) || null,
    );
    const selectedTitle = computed(
      () => selectedItem.value?.title || normalizeText(props.placeholder) || t("settings_inference_provider_picker_placeholder"),
    );
    const filteredItems = computed(() => {
      const query = normalizeText(filter.value).toLowerCase();
      if (!query) {
        return normalizedItems.value;
      }
      return normalizedItems.value.filter((item) =>
        [item.title, item.value, item.note].some((value) => normalizeText(value).toLowerCase().includes(query)),
      );
    });

    function openDialog() {
      if (props.disabled || props.readOnly) {
        return;
      }
      filter.value = "";
      open.value = true;
    }

    function closeDialog() {
      open.value = false;
    }

    function isSelected(item) {
      return normalizeText(item?.value) === normalizeText(props.modelValue);
    }

    function selectItem(item) {
      if (!item || props.disabled || props.readOnly) {
        return;
      }
      emit("change", item);
      closeDialog();
    }

    function updateFilter(value) {
      filter.value = String(value || "");
    }

    return {
      t,
      open,
      filter,
      selectedItem,
      selectedTitle,
      filteredItems,
      fallbackLogoText,
      providerLogo,
      providerLogoClass,
      openDialog,
      closeDialog,
      isSelected,
      selectItem,
      updateFilter,
    };
  },
  template: `
    <div class="inference-provider-picker">
      <button
        type="button"
        class="inference-provider-trigger"
        :class="{ 'is-disabled': disabled || readOnly }"
        :disabled="disabled || readOnly"
        aria-haspopup="dialog"
        :aria-expanded="open ? 'true' : 'false'"
        @click="openDialog"
      >
        <span
          class="inference-provider-logo"
          :class="selectedItem ? providerLogoClass(selectedItem) : 'is-empty'"
          aria-hidden="true"
        >
          <img
            v-if="selectedItem && providerLogo(selectedItem).src"
            class="inference-provider-logo-image"
            :src="providerLogo(selectedItem).src"
            alt=""
          />
          <span v-else class="inference-provider-logo-fallback">
            {{ selectedItem ? fallbackLogoText(selectedItem) : "LLM" }}
          </span>
          <span v-if="selectedItem && providerLogo(selectedItem).badge" class="inference-provider-logo-badge">
            {{ providerLogo(selectedItem).badge }}
          </span>
        </span>
        <span class="inference-provider-trigger-copy">
          <span class="inference-provider-trigger-title">{{ selectedTitle }}</span>
        </span>
        <span class="inference-provider-trigger-chevron" aria-hidden="true">
          <QIconChevronDown class="icon" />
        </span>
      </button>

      <AppDialogShell
        :modelValue="open"
        :title="t('settings_inference_provider_dialog_title')"
        width="760px"
        @update:modelValue="open = $event"
        @close="closeDialog"
      >
        <section class="inference-provider-dialog">
          <QInput
            class="inference-provider-filter"
            :modelValue="filter"
            :placeholder="t('settings_inference_provider_filter_placeholder')"
            @update:modelValue="updateFilter"
          />
          <div class="inference-provider-current">
            <span class="inference-provider-current-label">{{ t("settings_inference_provider_current") }}</span>
            <span class="inference-provider-current-value">{{ selectedTitle }}</span>
          </div>
          <div class="inference-provider-grid" role="listbox" :aria-label="t('settings_inference_provider_dialog_title')">
            <button
              v-for="item in filteredItems"
              :key="item.value || '__inherit__'"
              type="button"
              class="inference-provider-card"
              :class="{ 'is-selected': isSelected(item) }"
              role="option"
              :aria-selected="isSelected(item) ? 'true' : 'false'"
              @click="selectItem(item)"
            >
              <span class="inference-provider-logo is-card" :class="providerLogoClass(item)" aria-hidden="true">
                <img
                  v-if="providerLogo(item).src"
                  class="inference-provider-logo-image"
                  :src="providerLogo(item).src"
                  alt=""
                />
                <span v-else class="inference-provider-logo-fallback">{{ fallbackLogoText(item) }}</span>
                <span v-if="providerLogo(item).badge" class="inference-provider-logo-badge">
                  {{ providerLogo(item).badge }}
                </span>
              </span>
              <span class="inference-provider-card-copy">
                <span class="inference-provider-card-title">{{ item.title }}</span>
              </span>
              <span v-if="isSelected(item)" class="inference-provider-selected-mark">
                {{ t("settings_inference_provider_selected") }}
              </span>
            </button>
            <p v-if="filteredItems.length === 0" class="inference-provider-empty">
              {{ t("settings_inference_provider_empty") }}
            </p>
          </div>
        </section>
      </AppDialogShell>
    </div>
  `,
};

export default InferenceProviderPicker;
