import { computed, ref } from "vue";
import "./SettingOptions.css";

export default {
  props: {
    modelValue: { type: String, default: "" },
    options: { type: Array, default: () => [] },
    disabled: Boolean,
    allowCustom: Boolean,
    placeholder: { type: String, default: "Default" },
    label: { type: String, default: "" },
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    const customSelected = ref(false);
    const presetItems = computed(() => props.options.map(option => typeof option === "string"
      ? { title: option || props.placeholder, value: option }
      : option));
    const custom = computed(() => props.allowCustom && (customSelected.value ||
      !presetItems.value.some(item => item.value === props.modelValue)));
    const items = computed(() => {
      const result = [...presetItems.value];
      if (props.allowCustom) result.push({ title: "Custom duration", custom: true });
      else if (!result.some(item => item.value === props.modelValue)) {
        result.push({ title: props.modelValue || props.placeholder, value: props.modelValue });
      }
      return result;
    });
    const selectedItem = computed(() => items.value.find(item => custom.value
      ? item.custom
      : !item.custom && item.value === props.modelValue));
    function select(item) {
      if (props.disabled) return;
      customSelected.value = item.custom === true;
      if (!item.custom) emit("update:modelValue", item.value);
    }
    function updateCustom(value) {
      if (props.disabled) return;
      customSelected.value = true;
      emit("update:modelValue", value);
    }
    return { items, selectedItem, custom, select, updateCustom };
  },
  template: `
    <div class="settings-enum-select">
      <QDropdownMenu
        :key="custom ? 'custom' : modelValue"
        :items="items"
        :initialItem="selectedItem"
        :disabled="disabled"
        :aria-label="label || undefined"
        @change="select"
      />
      <QInput
        v-if="custom"
        :modelValue="modelValue"
        :disabled="disabled"
        :aria-label="label + ': custom duration'"
        placeholder="e.g. 5m or 1h"
        @update:modelValue="updateCustom"
      />
    </div>
  `,
};
