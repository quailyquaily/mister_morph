import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import CodexAuthDialogContent, { codexAuthDialogContentProps } from "./CodexAuthDialogContent";

const CodexAuthDialog = {
  components: {
    AppDialogShell,
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
    <AppDialogShell
      :modelValue="modelValue"
      :title="t('settings_codex_auth_title')"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      :closeDisabled="busy"
      @close="close"
    >
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
    </AppDialogShell>
  `,
};

export default CodexAuthDialog;
