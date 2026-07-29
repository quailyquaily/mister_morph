import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";

import { apiFetch, formatTime, translate } from "../core/context";
import {
  canOpenExternalURLInDesktop,
  openExternalPlaceholder,
  openExternalURL,
} from "../core/external-links";
import { invalidateConsoleSetupReadiness } from "../core/setup";
import { openReentrantDialog } from "../core/reentrant-dialog";

function emptyXAIAuthStatus() {
  return {
    logged_in: false,
    access_token_present: false,
    refresh_token_present: false,
    access_token_expired: false,
    expires_at: "",
    file_mode_ok: true,
    file_mode_warning: "",
  };
}

function normalizeXAIAuthStatus(payload) {
  const status = payload && typeof payload.status === "object" ? payload.status : payload;
  return {
    logged_in: status?.logged_in === true,
    access_token_present: status?.access_token_present === true,
    refresh_token_present: status?.refresh_token_present === true,
    access_token_expired: status?.access_token_expired === true,
    expires_at: typeof status?.expires_at === "string" ? status.expires_at : "",
    file_mode_ok: status?.file_mode_ok !== false,
    file_mode_warning: typeof status?.file_mode_warning === "string" ? status.file_mode_warning : "",
  };
}

export function useXAIAuthFlow(options = {}) {
  const t = translate;
  const onSettingsUpdated =
    typeof options.onSettingsUpdated === "function" ? options.onSettingsUpdated : async () => {};

  const xaiAuthLoading = ref(false);
  const xaiAuthBusy = ref(false);
  const xaiAuthError = ref("");
  const xaiAuthDialogOpen = ref(false);
  const xaiSetDefault = ref(false);
  const xaiLoginSession = ref("");
  const xaiLoginVerificationURL = ref("");
  const xaiLoginUserCode = ref("");
  const xaiLoginExpiresAt = ref("");
  const xaiAuthStatus = reactive(emptyXAIAuthStatus());
  let xaiLoginPollTimer = 0;

  const xaiAuthSummary = computed(() => {
    if (xaiAuthLoading.value) {
      return t("settings_xai_auth_loading");
    }
    if (!xaiAuthStatus.logged_in) {
      return t("settings_xai_auth_signed_out");
    }
    if (xaiAuthStatus.access_token_expired && xaiAuthStatus.refresh_token_present) {
      return t("settings_xai_auth_refreshable");
    }
    if (xaiAuthStatus.access_token_expired) {
      return t("settings_xai_auth_expired");
    }
    return t("settings_xai_auth_signed_in");
  });

  const xaiAuthButtonState = computed(() => {
    if (xaiAuthLoading.value) {
      return "loading";
    }
    if (!xaiAuthStatus.logged_in) {
      return "signed-out";
    }
    if (xaiAuthStatus.access_token_expired && xaiAuthStatus.refresh_token_present) {
      return "refreshable";
    }
    if (xaiAuthStatus.access_token_expired) {
      return "expired";
    }
    return "signed-in";
  });

  const xaiAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(xaiAuthButtonState.value));
  const xaiAuthReady = computed(() => xaiAuthStatus.logged_in && xaiAuthStatus.file_mode_ok !== false);
  const xaiAuthButtonTitle = computed(() => `${t("settings_xai_auth_title")}: ${xaiAuthSummary.value}`);
  const xaiLoginExpiresLabel = computed(() =>
    xaiLoginExpiresAt.value ? formatTime(xaiLoginExpiresAt.value) : t("ttl_unknown")
  );

  function applyXAIAuthStatus(payload) {
    Object.assign(xaiAuthStatus, normalizeXAIAuthStatus(payload));
  }

  async function loadXAIAuthStatus() {
    if (xaiAuthLoading.value) {
      return;
    }
    xaiAuthLoading.value = true;
    xaiAuthError.value = "";
    try {
      applyXAIAuthStatus(await apiFetch("/auth/xai/status"));
    } catch (e) {
      xaiAuthError.value = e.message || t("msg_load_failed");
    } finally {
      xaiAuthLoading.value = false;
    }
  }

  function clearXAILoginTimer() {
    if (xaiLoginPollTimer) {
      window.clearTimeout(xaiLoginPollTimer);
      xaiLoginPollTimer = 0;
    }
  }

  function resetXAILoginSession() {
    clearXAILoginTimer();
    xaiLoginSession.value = "";
    xaiLoginVerificationURL.value = "";
    xaiLoginUserCode.value = "";
    xaiLoginExpiresAt.value = "";
  }

  function resetXAIAuthFlow() {
    resetXAILoginSession();
    xaiAuthError.value = "";
    xaiSetDefault.value = false;
  }

  function scheduleXAILoginPoll(intervalSeconds = 5) {
    clearXAILoginTimer();
    const delay = Math.max(2, Number(intervalSeconds) || 5) * 1000;
    xaiLoginPollTimer = window.setTimeout(() => {
      void pollXAILogin();
    }, delay);
  }

  async function startXAILogin(authWindow = null) {
    if (xaiAuthBusy.value) {
      if (authWindow && !authWindow.closed) {
        authWindow.close();
      }
      return;
    }
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    resetXAILoginSession();
    let authWindowUsed = false;
    try {
      const payload = await apiFetch("/auth/xai/login/start", { method: "POST" });
      xaiLoginSession.value = String(payload?.session_id || "").trim();
      xaiLoginVerificationURL.value = String(
        payload?.verification_url_complete || payload?.verification_url || ""
      ).trim();
      xaiLoginUserCode.value = String(payload?.user_code || "").trim();
      xaiLoginExpiresAt.value = String(payload?.expires_at || "").trim();
      if (xaiLoginVerificationURL.value) {
        if (authWindow && !authWindow.closed) {
          authWindow.location.href = xaiLoginVerificationURL.value;
          authWindowUsed = true;
        } else {
          openExternalURL(xaiLoginVerificationURL.value);
        }
      }
      scheduleXAILoginPoll(payload?.interval_seconds);
    } catch (e) {
      xaiAuthError.value = e.message || t("msg_load_failed");
    } finally {
      if (!authWindowUsed && authWindow && !authWindow.closed) {
        authWindow.close();
      }
      xaiAuthBusy.value = false;
    }
  }

  async function pollXAILogin() {
    const sessionID = xaiLoginSession.value;
    if (!sessionID || xaiAuthBusy.value) {
      return;
    }
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    try {
      const payload = await apiFetch("/auth/xai/login/poll", {
        method: "POST",
        body: { session_id: sessionID, set_default: xaiSetDefault.value },
      });
      if (payload?.pending === true) {
        scheduleXAILoginPoll(payload?.interval_seconds);
        return;
      }
      applyXAIAuthStatus(payload);
      resetXAILoginSession();
      if (payload?.settings_updated === true) {
        invalidateConsoleSetupReadiness();
        await onSettingsUpdated(payload);
      }
    } catch (e) {
      xaiAuthError.value = e.message || t("msg_load_failed");
    } finally {
      xaiAuthBusy.value = false;
    }
  }

  async function logoutXAIAuth() {
    if (xaiAuthBusy.value) {
      return;
    }
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    try {
      const payload = await apiFetch("/auth/xai/logout", { method: "POST" });
      applyXAIAuthStatus(payload);
      resetXAILoginSession();
      if (typeof payload?.revocation_warning === "string") {
        xaiAuthError.value = payload.revocation_warning;
      }
    } catch (e) {
      xaiAuthError.value = e.message || t("msg_delete_failed");
    } finally {
      xaiAuthBusy.value = false;
    }
  }

  async function openXAIAuthDialog() {
    const shouldStartLogin = xaiAuthNeedsLogin.value && !xaiLoginSession.value && !xaiAuthBusy.value;
    let authWindow = null;
    if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
      authWindow = openExternalPlaceholder();
    }
    await openReentrantDialog(xaiAuthDialogOpen);
    void loadXAIAuthStatus();
    if (shouldStartLogin) {
      void startXAILogin(authWindow);
    }
  }

  function reloginXAIAuth() {
    if (xaiAuthBusy.value) {
      return;
    }
    const authWindow = canOpenExternalURLInDesktop() ? null : openExternalPlaceholder();
    void startXAILogin(authWindow);
  }

  watch(xaiAuthDialogOpen, (open) => {
    if (!open) {
      resetXAIAuthFlow();
    }
  });

  onBeforeUnmount(() => {
    clearXAILoginTimer();
  });

  return {
    xaiAuthLoading,
    xaiAuthBusy,
    xaiAuthError,
    xaiAuthDialogOpen,
    xaiSetDefault,
    xaiAuthStatus,
    xaiAuthSummary,
    xaiAuthButtonState,
    xaiAuthNeedsLogin,
    xaiAuthReady,
    xaiAuthButtonTitle,
    xaiLoginSession,
    xaiLoginVerificationURL,
    xaiLoginUserCode,
    xaiLoginExpiresLabel,
    loadXAIAuthStatus,
    openXAIAuthDialog,
    reloginXAIAuth,
    pollXAILogin,
    logoutXAIAuth,
    resetXAIAuthFlow,
  };
}

export default useXAIAuthFlow;
