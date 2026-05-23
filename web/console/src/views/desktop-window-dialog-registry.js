import DeviceAuthDialogContent from "../components/DeviceAuthDialogContent";
import PokeDialogContent from "../components/PokeDialogContent";
import RawJsonDialogContent from "../components/RawJsonDialogContent";
import RawTextEditorDialogContent from "../components/RawTextEditorDialogContent";
import SetupConnectionTestDialogContent from "../components/SetupConnectionTestDialogContent";
import SetupPickerDialogContent from "../components/SetupPickerDialogContent";
import {
  CODEX_AUTH_WINDOW_ID,
  POKE_WINDOW_ID,
  RAW_JSON_WINDOW_ID,
  RAW_TEXT_EDITOR_WINDOW_ID,
  SETUP_CONNECTION_TEST_WINDOW_ID,
  SETUP_PICKER_WINDOW_ID,
} from "../core/desktop-windows";

const CODEX_USAGE_URL = "https://chatgpt.com/codex/settings/usage";

export const DESKTOP_WINDOW_DIALOGS = {
  [RAW_JSON_WINDOW_ID]: {
    id: RAW_JSON_WINDOW_ID,
    component: RawJsonDialogContent,
    contentClass: "desktop-window-view__content--raw",
    storedPayload: true,
    stateRef: "rawJsonPayload",
    applyPayload(ctx, payload) {
      ctx.rawJsonPayload.value = {
        title: String(payload.title || "").trim(),
        json: String(payload.json || ""),
      };
    },
    ready(ctx) {
      return !!ctx.rawJsonPayload.value;
    },
    props(ctx) {
      return {
        json: ctx.rawJsonPayload.value?.json || "",
      };
    },
  },
  [POKE_WINDOW_ID]: {
    id: POKE_WINDOW_ID,
    component: PokeDialogContent,
    contentClass: "desktop-window-view__content--form",
    onRouteLoad(ctx) {
      ctx.resetPoke();
    },
    ready() {
      return true;
    },
    props(ctx) {
      return {
        inputId: "desktop-poke-body",
        body: ctx.pokeBody.value,
        bodyTooLarge: ctx.pokeBodyTooLarge.value,
        disabled: ctx.poking.value,
        error: ctx.pokeError.value,
        helperText: ctx.pokeHelperText.value,
        sizeLabel: ctx.pokeSizeLabel.value,
        submitDisabled: ctx.pokeSubmitDisabled.value,
        submitting: ctx.poking.value,
      };
    },
    events(ctx) {
      return {
        cancel: ctx.closePokeWindow,
        submit: ctx.submitPoke,
        "update:body": ctx.updatePokeBody,
      };
    },
  },
  [SETUP_PICKER_WINDOW_ID]: {
    id: SETUP_PICKER_WINDOW_ID,
    component: SetupPickerDialogContent,
    contentClass: "desktop-window-view__content--form",
    stateRef: "setupPickerPayload",
    storedPayload: true,
    statefulPayload: true,
    applyPayload(ctx, payload) {
      ctx.setupPickerPayload.value = payload;
    },
    ready(ctx) {
      return !!ctx.setupPickerPayload.value;
    },
    props(ctx) {
      const payload = ctx.setupPickerPayload.value || {};
      return {
        items: payload.items || [],
        loading: payload.loading === true,
        error: payload.error || "",
        filterPlaceholder: payload.filterPlaceholder || "",
        emptyText: payload.emptyText || "",
        showValue: payload.showValue !== false,
        resetKey: payload.request_id || "",
      };
    },
    events(ctx) {
      return {
        select: ctx.selectSetupPickerItem,
      };
    },
  },
  [SETUP_CONNECTION_TEST_WINDOW_ID]: {
    id: SETUP_CONNECTION_TEST_WINDOW_ID,
    component: SetupConnectionTestDialogContent,
    contentClass:
      "desktop-window-view__content--form desktop-window-view__content--dialog-body desktop-window-view__content--setup",
    stateRef: "connectionTestPayload",
    storedPayload: true,
    statefulPayload: true,
    applyPayload(ctx, payload) {
      ctx.connectionTestPayload.value = payload;
    },
    ready(ctx) {
      return !!ctx.connectionTestPayload.value;
    },
    props(ctx) {
      const payload = ctx.connectionTestPayload.value || {};
      return {
        loading: payload.loading === true,
        error: payload.error || "",
        benchmarks: payload.benchmarks || [],
        provider: payload.provider || "",
        apiBase: payload.apiBase || "",
        model: payload.model || "",
        showIntro: payload.showIntro !== false,
      };
    },
    events(ctx) {
      return {
        retry: ctx.retryConnectionTest,
        close: () => ctx.closeDialogWindow(ctx.connectionTestPayload.value),
      };
    },
  },
  [CODEX_AUTH_WINDOW_ID]: {
    id: CODEX_AUTH_WINDOW_ID,
    component: DeviceAuthDialogContent,
    contentClass: "desktop-window-view__content--form",
    stateRef: "codexAuthPayload",
    storedPayload: true,
    statefulPayload: true,
    applyPayload(ctx, payload) {
      ctx.codexAuthPayload.value = payload;
    },
    ready(ctx) {
      return !!ctx.codexAuthPayload.value;
    },
    props(ctx) {
      const payload = ctx.codexAuthPayload.value || {};
      return {
        loading: payload.loading === true,
        busy: payload.busy === true,
        error: payload.error || "",
        status: payload.status || {},
        summary: payload.summary || "",
        loginSession: payload.loginSession || "",
        verificationURL: payload.verificationURL || "",
        userCode: payload.userCode || "",
        loginExpiresLabel: payload.loginExpiresLabel || "",
        accountLabel: payload.accountLabel || String(payload.status?.account_id || "").trim(),
        accountIntroKey: "settings_codex_auth_account_intro",
        sessionKey: "settings_codex_auth_session",
        statusReadyKey: "settings_codex_auth_status_ready",
        statusNeedsLoginKey: "settings_codex_auth_status_needs_login",
        setDefaultNoteKey: "settings_codex_auth_set_default_note",
        loginPendingKey: "settings_codex_auth_login_pending",
        loginExpiresKey: "settings_codex_auth_login_expires",
        openVerificationKey: "settings_codex_auth_open_verification",
        userCodeKey: "settings_codex_auth_user_code",
        extraActionKey: "settings_codex_auth_usage",
        extraActionURL: CODEX_USAGE_URL,
      };
    },
    events(ctx) {
      return {
        logout: ctx.logoutCodexAuth,
      };
    },
  },
  [RAW_TEXT_EDITOR_WINDOW_ID]: {
    id: RAW_TEXT_EDITOR_WINDOW_ID,
    component: RawTextEditorDialogContent,
    contentClass: "desktop-window-view__content--wide",
    stateRef: "rawTextPayload",
    storedPayload: true,
    statefulPayload: true,
    applyPayload(ctx, payload) {
      ctx.rawTextPayload.value = payload;
      ctx.rawTextValue.value = String(payload.modelValue || "");
    },
    ready(ctx) {
      return !!ctx.rawTextPayload.value;
    },
    props(ctx) {
      const payload = ctx.rawTextPayload.value || {};
      return {
        path: payload.path || "",
        modelValue: ctx.rawTextValue.value,
        loading: payload.loading === true,
        saving: payload.saving === true,
      };
    },
    events(ctx) {
      return {
        "update:modelValue": ctx.updateRawTextValue,
        save: ctx.saveRawText,
      };
    },
  },
};

export function getDesktopWindowDialog(id) {
  return DESKTOP_WINDOW_DIALOGS[String(id || "").trim()] || null;
}

export function listDesktopWindowDialogs() {
  return Object.values(DESKTOP_WINDOW_DIALOGS);
}
