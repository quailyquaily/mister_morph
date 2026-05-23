import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import ProAuthDialogContent, { proAuthDialogContentProps } from "./ProAuthDialogContent";

const ProAuthDialog = {
  components: {
    AppDialogShell,
    ProAuthDialogContent,
  },
  props: {
    modelValue: Boolean,
    ...proAuthDialogContentProps,
  },
  emits: ["update:modelValue", "logout"],
  setup(props, { emit }) {
    const t = translate;

    function close() {
      emit("update:modelValue", false);
    }

    function logout() {
      emit("logout");
    }

    return {
      t,
      close,
      logout,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="modelValue"
      :title="t('settings_pro_auth_title')"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      :closeDisabled="busy"
      @close="close"
    >
      <ProAuthDialogContent
        :loading="loading"
        :busy="busy"
        :error="error"
        :status="status"
        :summary="summary"
        :loginSession="loginSession"
        :verificationURL="verificationURL"
        :userCode="userCode"
        :loginExpiresLabel="loginExpiresLabel"
        @logout="logout"
      />
    </AppDialogShell>
  `,
};

export default ProAuthDialog;
