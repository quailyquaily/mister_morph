import { computed } from "vue";
import "./SettingOptions.css";

export default {
  props: {
    modelValue: { type: Array, default: () => [] },
    options: { type: Array, default: () => [] },
    disabled: Boolean,
    label: { type: String, default: "" },
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    const items = computed(() => [...new Set([...props.options, ...props.modelValue])]);
    function toggle(value, checked) {
      if (props.disabled) return;
      emit("update:modelValue", checked
        ? [...new Set([...props.modelValue, value])]
        : props.modelValue.filter(item => item !== value));
    }
    return { items, toggle };
  },
  template: `
    <div class="settings-enum-choices" role="group" :aria-label="label || undefined">
      <label v-for="item in items" :key="item" class="settings-enum-choice">
        <input type="checkbox" :checked="modelValue.includes(item)" :disabled="disabled" @change="toggle(item, $event.target.checked)" />
        <span>{{ item }}</span>
      </label>
    </div>
  `,
};
