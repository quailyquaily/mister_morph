import "./AppTabs.css";

function tabID(tab, index) {
  return String(tab?.id ?? index);
}

const AppTabs = {
  props: {
    tabs: { type: Array, default: () => [] },
    modelValue: { type: Object, default: null },
    disabled: { type: Boolean, default: false },
    ariaLabel: { type: String, default: "" },
  },
  emits: ["update:modelValue", "change"],
  setup(props, { emit }) {
    function isActive(tab, index) {
      return tabID(tab, index) === tabID(props.modelValue, -1);
    }

    function selectTab(tab, index) {
      if (props.disabled || tab?.disabled) return;
      emit("update:modelValue", tab);
      emit("change", { tab, index });
    }

    function onKeydown(event) {
      const direction = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      if (!direction && event.key !== "Home" && event.key !== "End") return;

      const buttons = [...event.currentTarget.closest('[role="tablist"]')?.querySelectorAll('[role="tab"]:not(:disabled)') || []];
      if (!buttons.length) return;
      event.preventDefault();

      const current = buttons.indexOf(event.currentTarget);
      const target = event.key === "Home"
        ? buttons[0]
        : event.key === "End"
          ? buttons[buttons.length - 1]
          : buttons[(current + direction + buttons.length) % buttons.length];
      target?.focus();
      const nextIndex = Number(target?.dataset.tabIndex);
      if (Number.isInteger(nextIndex)) selectTab(props.tabs[nextIndex], nextIndex);
    }

    return { isActive, selectTab, onKeydown };
  },
  template: `
    <div class="app-tabs" role="tablist" :aria-label="ariaLabel || undefined">
      <QButton
        v-for="(tab, index) in tabs"
        :key="tab.id ?? index"
        type="button"
        :class="['plain', 'sm', 'app-tabs-option', { 'is-active': isActive(tab, index), 'toggle-on': isActive(tab, index) }]"
        role="tab"
        :aria-selected="isActive(tab, index)"
        :tabindex="isActive(tab, index) ? 0 : -1"
        :data-tab-index="index"
        :disabled="disabled || tab.disabled"
        @click="selectTab(tab, index)"
        @keydown="onKeydown($event)"
      >
        <component v-if="tab.icon" :is="tab.icon" class="icon app-tabs-option-icon" />
        <span class="app-tabs-option-label">{{ tab.title }}</span>
      </QButton>
    </div>
  `,
};

export default AppTabs;
