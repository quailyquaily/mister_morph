import { computed } from "vue";
import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import DeviceAuthDialogContent, { deviceAuthStateProps } from "./DeviceAuthDialogContent";

function proUserLabel(status) {
  const user = status && typeof status.user === "object" && status.user ? status.user : {};
  return (
    (typeof user.name === "string" && user.name.trim()) ||
    (typeof user.email === "string" && user.email.trim()) ||
    (typeof user.union_id === "string" && user.union_id.trim()) ||
    ""
  );
}

const ProAuthDialog = {
  components: {
    AppDialogShell,
    DeviceAuthDialogContent,
  },
  props: {
    modelValue: Boolean,
    ...deviceAuthStateProps,
  },
  emits: ["update:modelValue", "logout"],
  setup(props, { emit }) {
    const t = translate;
    const accountLabel = computed(() => proUserLabel(props.status));

    function close() {
      emit("update:modelValue", false);
    }

    function logout() {
      emit("logout");
    }

    return {
      t,
      accountLabel,
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
        :accountLabel="accountLabel"
        accountIntroKey="settings_pro_auth_account_intro"
        sessionKey="settings_pro_auth_session"
        statusReadyKey="settings_pro_auth_status_ready"
        statusNeedsLoginKey="settings_pro_auth_status_needs_login"
        setDefaultNoteKey="settings_pro_auth_set_default_note"
        loginPendingKey="settings_pro_auth_login_pending"
        loginExpiresKey="settings_codex_auth_login_expires"
        openVerificationKey="settings_pro_auth_open_verification"
        userCodeKey="settings_codex_auth_user_code"
        @logout="logout"
      />
    </AppDialogShell>
  `,
};

export default ProAuthDialog;
