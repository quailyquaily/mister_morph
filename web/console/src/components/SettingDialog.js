import { translate } from "../core/context";
import "./SettingDialog.css";

const SettingDialog = {
  name: "SettingDialog",
  props: {
    modelValue: { type: Boolean, default: false },
    title: { type: String, default: "" },
    width: { type: String, default: "720px" },
    saving: { type: Boolean, default: false },
    saveDisabled: { type: Boolean, default: false },
  },
  emits: ["update:modelValue", "cancel", "save"],
  setup(props, { emit }) {
    const t = translate;

    function cancel() {
      if (props.saving) return;
      emit("update:modelValue", false);
      emit("cancel");
    }

    function updateOpen(open) {
      if (open) {
        emit("update:modelValue", true);
        return;
      }
      cancel();
    }

    return { cancel, t, updateOpen };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      :width="width"
      :persistent="saving"
      @update:modelValue="updateOpen"
    >
      <template #header>
        <header class="setting-dialog-header">
          <h3 class="setting-dialog-title">{{ title }}</h3>
        </header>
      </template>

      <section class="setting-dialog">
        <div class="setting-dialog-scroll">
          <slot />
        </div>
        <footer class="setting-dialog-actions">
          <QButton type="button" class="outlined" :disabled="saving" @click="cancel">
            {{ t("action_cancel") }}
          </QButton>
          <QButton
            type="button"
            class="primary"
            :loading="saving"
            :disabled="saving || saveDisabled"
            @click="$emit('save')"
          >
            {{ t("action_save") }}
          </QButton>
        </footer>
      </section>
    </QDialog>
  `,
};

export default SettingDialog;
