import { translate } from "../core/context";

const AppDialogShell = {
  props: {
    modelValue: Boolean,
    title: {
      type: String,
      default: "",
    },
    width: {
      type: String,
      default: "560px",
    },
    height: {
      type: String,
      default: "auto",
    },
    position: {
      type: String,
      default: "center",
    },
    closeDisabled: {
      type: Boolean,
      default: false,
    },
  },
  emits: ["update:modelValue", "close"],
  setup(_props, { emit }) {
    const t = translate;

    function close() {
      emit("update:modelValue", false);
      emit("close");
    }

    function updateOpen(open) {
      emit("update:modelValue", open);
      if (!open) {
        emit("close");
      }
    }

    return {
      t,
      close,
      updateOpen,
    };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      :width="width"
      :height="height"
      :position="position"
      @update:modelValue="updateOpen"
      @close="close"
    >
      <template #header>
        <header class="app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ title }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            :disabled="closeDisabled"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
      </template>

      <slot />
    </QDialog>
  `,
};

export default AppDialogShell;
