import { translate } from "../core/context";
import CodexAuthDialogContent, { codexAuthDialogContentProps } from "./CodexAuthDialogContent";

const CodexAuthDialog = {
  components: {
    CodexAuthDialogContent,
  },
  props: {
    modelValue: Boolean,
    ...codexAuthDialogContentProps,
  },
  emits: ["update:modelValue", "logout"],
  setup(props, { emit }) {
    const t = translate;

    function close() {
      emit("update:modelValue", false);
    }

    return {
      t,
      close,
    };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      @close="close"
    >
      <template #header>
        <header class="app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ t("settings_codex_auth_title") }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            :disabled="busy"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
      </template>

      <CodexAuthDialogContent
        :loading="loading"
        :busy="busy"
        :error="error"
        :status="status"
        :summary="summary"
        :loginSession="loginSession"
        :verificationURL="verificationURL"
        :userCode="userCode"
        :loginExpiresLabel="loginExpiresLabel"
        @logout="$emit('logout')"
      />
    </QDialog>
  `,
};

export default CodexAuthDialog;
