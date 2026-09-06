import { computed, ref } from "vue";

import { translate } from "../core/context";
import { inferenceProviderLogo } from "../core/inference-provider-logos";
import AppDialogShell from "./AppDialogShell";
import AppTabs from "./AppTabs";
import "./InferenceProviderPicker.css";

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
    return { src: "", className: "is-empty" };
  }
  return inferenceProviderLogo(value);
}

function providerLogoClass(item) {
  return providerLogo(item).className || "is-fallback";
}

const DEFAULT_PROVIDER_GROUP = "api";
const PROVIDER_GROUPS = [
  { id: "api", labelKey: "settings_inference_provider_group_api" },
  { id: "account", labelKey: "settings_inference_provider_group_account" },
  { id: "compatible", labelKey: "settings_inference_provider_group_compatible" },
];

const InferenceProviderPicker = {
  components: {
    AppDialogShell,
    AppTabs,
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
    disabledReason: {
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
    const activeGroup = ref(DEFAULT_PROVIDER_GROUP);

    const normalizedItems = computed(() =>
      (Array.isArray(props.items) ? props.items : [])
        .map((item) => {
          const group = normalizeText(item?.group);
          return {
            title: normalizeText(item?.title),
            value: normalizeText(item?.value),
            note: normalizeText(item?.note),
            group: PROVIDER_GROUPS.some((candidate) => candidate.id === group) ? group : DEFAULT_PROVIDER_GROUP,
          };
        })
        .filter((item) => item.title !== "" || item.value !== ""),
    );
    const selectedItem = computed(
      () => normalizedItems.value.find((item) => item.value === normalizeText(props.modelValue)) || null,
    );
    const selectedTitle = computed(
      () => selectedItem.value?.title || normalizeText(props.placeholder) || t("settings_inference_provider_picker_placeholder"),
    );
    const groupTabs = computed(() =>
      PROVIDER_GROUPS.map((group) => ({ id: group.id, title: t(group.labelKey) })),
    );
    const selectedGroupTab = computed(
      () => groupTabs.value.find((item) => item.id === activeGroup.value) || groupTabs.value[0] || null,
    );
    const isFiltering = computed(() => normalizeText(filter.value) !== "");
    const filteredItems = computed(() => {
      const query = normalizeText(filter.value).toLowerCase();
      if (!query) {
        return normalizedItems.value.filter((item) => item.group === activeGroup.value);
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
      activeGroup.value = selectedItem.value?.group || DEFAULT_PROVIDER_GROUP;
      open.value = true;
      console.info("[InferenceProviderPicker] dialog open requested", {
        provider: normalizeText(props.modelValue),
        itemCount: normalizedItems.value.length,
      });
    }

    function reportBlockedDialogOpen() {
      if (!props.disabled && !props.readOnly) {
        return;
      }
      console.warn("[InferenceProviderPicker] dialog open blocked", {
        reason: normalizeText(props.disabledReason) || (props.readOnly ? "readOnly" : "disabled"),
        disabled: props.disabled,
        readOnly: props.readOnly,
        provider: normalizeText(props.modelValue),
        itemCount: normalizedItems.value.length,
      });
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

    function changeGroup(detail) {
      const nextGroup = normalizeText(detail?.tab?.id);
      if (PROVIDER_GROUPS.some((group) => group.id === nextGroup)) {
        activeGroup.value = nextGroup;
      }
    }

    return {
      t,
      open,
      filter,
      selectedItem,
      selectedTitle,
      groupTabs,
      selectedGroupTab,
      isFiltering,
      filteredItems,
      fallbackLogoText,
      providerLogo,
      providerLogoClass,
      openDialog,
      reportBlockedDialogOpen,
      closeDialog,
      isSelected,
      selectItem,
      updateFilter,
      changeGroup,
    };
  },
  template: `
    <div class="inference-provider-picker" @pointerdown.capture="reportBlockedDialogOpen">
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
        </span>
        <span class="inference-provider-trigger-copy">
          <span class="inference-provider-trigger-title">{{ selectedTitle }}</span>
        </span>
        <span class="inference-provider-trigger-chevron" aria-hidden="true">
          <PhCaretDown class="icon" />
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
          <AppTabs
            v-if="!isFiltering"
            class="inference-provider-tabs"
            :tabs="groupTabs"
            :modelValue="selectedGroupTab"
            :ariaLabel="t('settings_inference_provider_group_label')"
            @change="changeGroup"
          />
          <div class="inference-provider-grid" role="listbox" :aria-label="t('settings_inference_provider_dialog_title')">
            <button
              v-for="item in filteredItems"
              :key="item.value || '__empty__'"
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
