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
  setup(props, { emit }) {
    const t = translate;

    function requestClose() {
      if (props.closeDisabled) {
        return;
      }
      emit("update:modelValue", false);
      emit("close");
    }

    function updateOpen(open) {
      if (!open && props.closeDisabled) {
        emit("update:modelValue", true);
        return;
      }
      emit("update:modelValue", open);
    }

    function dialogClosed() {
      if (props.closeDisabled) {
        emit("update:modelValue", true);
        return;
      }
      emit("close");
    }

    return {
      t,
      dialogClosed,
      requestClose,
      updateOpen,
    };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      :width="width"
      :height="height"
      :position="position"
      :persistent="closeDisabled"
      @update:modelValue="updateOpen"
      @close="dialogClosed"
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
            @click="requestClose"
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
