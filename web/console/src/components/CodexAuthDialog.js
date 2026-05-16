import { translate } from "../core/context";
import { useDesktopPayloadDialog } from "../core/desktop-payload-dialog";
import AppDialogShell from "./AppDialogShell";
import CodexAuthDialogContent, { codexAuthDialogContentProps } from "./CodexAuthDialogContent";
import { CODEX_AUTH_WINDOW_ID, openCodexAuthDesktopWindow } from "../core/desktop-windows";

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
        @logout="logout"
      />
    </AppDialogShell>
  `,
};

export default CodexAuthDialog;
