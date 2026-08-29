import { nextTick, onBeforeUnmount, ref, watch } from "vue";

import "./AppMobileBottomMenu.css";

const AppMobileBottomMenu = {
  props: {
    modelValue: {
      type: Boolean,
      required: true,
    },
    label: {
      type: String,
      required: true,
    },
    panelClass: {
      type: String,
      default: "",
    },
  },
  emits: ["close"],
  setup(props, { emit }) {
    const panel = ref(null);
    let focusBeforeOpen = null;

    function onWindowKeydown(event) {
      if (event.key !== "Escape" || !props.modelValue) {
        return;
      }
      event.preventDefault();
      emit("close");
    }

    watch(
      () => props.modelValue,
      async (open) => {
        if (open) {
          focusBeforeOpen = document.activeElement;
          window.addEventListener("keydown", onWindowKeydown);
          await nextTick();
          const firstAction = panel.value?.querySelector(
            "input, [aria-current='page'], [aria-selected='true'], a, button:not(:disabled)",
          );
          (firstAction || panel.value)?.focus();
          return;
        }
        window.removeEventListener("keydown", onWindowKeydown);
        if (focusBeforeOpen instanceof HTMLElement && document.contains(focusBeforeOpen)) {
          focusBeforeOpen.focus();
        }
        focusBeforeOpen = null;
      },
    );

    onBeforeUnmount(() => {
      window.removeEventListener("keydown", onWindowKeydown);
    });

    return {
      panel,
    };
  },
  template: `
    <Teleport to="body">
      <Transition name="mobile-bottom-menu">
        <div v-if="modelValue" class="mobile-bottom-menu-layer">
          <div class="mobile-bottom-menu-mask" aria-hidden="true" @click="$emit('close')"></div>
          <section
            ref="panel"
            class="mobile-bottom-menu-panel"
            :class="panelClass"
            role="dialog"
            :aria-label="label"
            tabindex="-1"
          >
            <span class="mobile-bottom-menu-handle" aria-hidden="true"></span>
            <slot />
          </section>
        </div>
      </Transition>
    </Teleport>
  `,
};

export default AppMobileBottomMenu;
