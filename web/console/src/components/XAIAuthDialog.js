import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import DeviceAuthDialogContent, { deviceAuthStateProps } from "./DeviceAuthDialogContent";

const XAIAuthDialog = {
  components: {
    AppDialogShell,
    DeviceAuthDialogContent,
  },
  props: {
    modelValue: Boolean,
    setDefault: Boolean,
    ...deviceAuthStateProps,
  },
  emits: ["update:modelValue", "update:setDefault", "login", "logout"],
  setup(_props, { emit }) {
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
      :title="t('settings_xai_auth_title')"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      :closeDisabled="busy"
      @close="close"
    >
      <DeviceAuthDialogContent
        :loading="loading"
        :busy="busy"
        :error="error"
        :status="status"
        :summary="summary"
        :loginSession="loginSession"
        :verificationURL="verificationURL"
        :userCode="userCode"
        :loginExpiresLabel="loginExpiresLabel"
        sessionKey="settings_xai_auth_session"
        statusReadyKey="settings_xai_auth_status_ready"
        statusNeedsLoginKey="settings_xai_auth_status_needs_login"
        setDefaultNoteKey="settings_xai_auth_login_note"
        loginPendingKey="settings_xai_auth_login_pending"
        loginExpiresKey="settings_xai_auth_login_expires"
        openVerificationKey="settings_xai_auth_open_verification"
        userCodeKey="settings_xai_auth_user_code"
        :showSetDefaultToggle="true"
        :setDefault="setDefault"
        setDefaultKey="settings_xai_auth_set_default"
        reloginKey="settings_xai_auth_relogin"
        @update:setDefault="$emit('update:setDefault', $event)"
        @login="$emit('login')"
        @logout="$emit('logout')"
      />
    </AppDialogShell>
  `,
};

export default XAIAuthDialog;
