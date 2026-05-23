import { computed } from "vue";
import { translate } from "../core/context";
import { useDesktopPayloadDialog } from "../core/desktop-payload-dialog";
import AppDialogShell from "./AppDialogShell";
import DeviceAuthDialogContent, { deviceAuthStateProps } from "./DeviceAuthDialogContent";
import { CODEX_AUTH_WINDOW_ID, openCodexAuthDesktopWindow } from "../core/desktop-windows";

export const CODEX_USAGE_URL = "https://chatgpt.com/codex/settings/usage";

const CodexAuthDialog = {
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
    const accountLabel = computed(() => String(props.status?.account_id || "").trim());

    function payload() {
      return {
        loading: props.loading === true,
        busy: props.busy === true,
        error: String(props.error || ""),
        status: props.status && typeof props.status === "object" ? props.status : {},
        summary: String(props.summary || ""),
        loginSession: String(props.loginSession || ""),
        verificationURL: String(props.verificationURL || ""),
        userCode: String(props.userCode || ""),
        loginExpiresLabel: String(props.loginExpiresLabel || ""),
        accountLabel: accountLabel.value,
      };
    }

    function close() {
      emit("update:modelValue", false);
    }

    function logout() {
      emit("logout");
    }

    const desktopDialog = useDesktopPayloadDialog({
      open: () => props.modelValue,
      windowID: CODEX_AUTH_WINDOW_ID,
      title: () => t("settings_codex_auth_title"),
      payload,
      openWindow: openCodexAuthDesktopWindow,
      close,
      onMessage(message) {
        if (message?.type === "codex-auth:logout") {
          logout();
        }
      },
    });

    return {
      t,
      CODEX_USAGE_URL,
      accountLabel,
      close,
      logout,
      webDialogOpen: desktopDialog.webDialogOpen,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="webDialogOpen"
      :title="t('settings_codex_auth_title')"
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
        accountIntroKey="settings_codex_auth_account_intro"
        sessionKey="settings_codex_auth_session"
        statusReadyKey="settings_codex_auth_status_ready"
        statusNeedsLoginKey="settings_codex_auth_status_needs_login"
        setDefaultNoteKey="settings_codex_auth_set_default_note"
        loginPendingKey="settings_codex_auth_login_pending"
        loginExpiresKey="settings_codex_auth_login_expires"
        openVerificationKey="settings_codex_auth_open_verification"
        userCodeKey="settings_codex_auth_user_code"
        extraActionKey="settings_codex_auth_usage"
        :extraActionURL="CODEX_USAGE_URL"
        @logout="logout"
      />
    </AppDialogShell>
  `,
};

export default CodexAuthDialog;
