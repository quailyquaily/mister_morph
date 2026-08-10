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
  const request = typeof options.request === "function" ? options.request : apiFetch;
  const getEndpointRef = typeof options.getEndpointRef === "function" ? options.getEndpointRef : () => "";
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
  let xaiRequestGeneration = 0;
  let xaiLoginEndpointRef = "";

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

  function currentEndpointRef() {
    return String(getEndpointRef() || "").trim();
  }

  function isCurrentRequest(generation, endpointRef) {
    return generation === xaiRequestGeneration && endpointRef === currentEndpointRef();
  }

  async function loadXAIAuthStatus(endpointRef = currentEndpointRef()) {
    if (xaiAuthLoading.value) {
      return;
    }
    const targetEndpointRef = String(endpointRef || "").trim();
    const generation = xaiRequestGeneration;
    xaiAuthLoading.value = true;
    xaiAuthError.value = "";
    try {
      const payload = await request("/auth/xai/status", undefined, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      applyXAIAuthStatus(payload);
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        xaiAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        xaiAuthLoading.value = false;
      }
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
    xaiLoginEndpointRef = "";
  }

  function resetXAIAuthFlow() {
    xaiRequestGeneration += 1;
    xaiAuthLoading.value = false;
    xaiAuthBusy.value = false;
    resetXAILoginSession();
    xaiAuthError.value = "";
    xaiSetDefault.value = false;
  }

  function resetXAIAuthEndpointState() {
    resetXAIAuthFlow();
    Object.assign(xaiAuthStatus, emptyXAIAuthStatus());
  }

  function scheduleXAILoginPoll(intervalSeconds = 5) {
    clearXAILoginTimer();
    const delay = Math.max(2, Number(intervalSeconds) || 5) * 1000;
    xaiLoginPollTimer = window.setTimeout(() => {
      void pollXAILogin();
    }, delay);
  }

  async function startXAILogin(authWindow = null, endpointRef = currentEndpointRef()) {
    if (xaiAuthBusy.value) {
      if (authWindow && !authWindow.closed) {
        authWindow.close();
      }
      return;
    }
    const targetEndpointRef = String(endpointRef || "").trim();
    const generation = ++xaiRequestGeneration;
    xaiAuthLoading.value = false;
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    resetXAILoginSession();
    let authWindowUsed = false;
    try {
      const payload = await request("/auth/xai/login/start", { method: "POST" }, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      xaiLoginEndpointRef = targetEndpointRef;
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
      if (isCurrentRequest(generation, targetEndpointRef)) {
        xaiAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (!authWindowUsed && authWindow && !authWindow.closed) {
        authWindow.close();
      }
      if (generation === xaiRequestGeneration) {
        xaiAuthBusy.value = false;
      }
    }
  }

  async function pollXAILogin() {
    const sessionID = xaiLoginSession.value;
    const targetEndpointRef = xaiLoginEndpointRef;
    if (!sessionID || xaiAuthBusy.value) {
      return;
    }
    if (targetEndpointRef !== currentEndpointRef()) {
      resetXAIAuthFlow();
      return;
    }
    const generation = ++xaiRequestGeneration;
    xaiAuthLoading.value = false;
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    try {
      const payload = await request(
        "/auth/xai/login/poll",
        {
          method: "POST",
          body: { session_id: sessionID, set_default: xaiSetDefault.value },
        },
        targetEndpointRef
      );
      if (
        !isCurrentRequest(generation, targetEndpointRef) ||
        targetEndpointRef !== xaiLoginEndpointRef
      ) {
        return;
      }
      if (payload?.pending === true) {
        scheduleXAILoginPoll(payload?.interval_seconds);
        return;
      }
      applyXAIAuthStatus(payload);
      resetXAILoginSession();
      if (payload?.settings_updated === true) {
        invalidateConsoleSetupReadiness();
        await onSettingsUpdated(payload, targetEndpointRef);
      }
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        xaiAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (generation === xaiRequestGeneration) {
        xaiAuthBusy.value = false;
      }
    }
  }

  async function logoutXAIAuth() {
    if (xaiAuthBusy.value) {
      return;
    }
    const targetEndpointRef = currentEndpointRef();
    const generation = ++xaiRequestGeneration;
    xaiAuthLoading.value = false;
    xaiAuthBusy.value = true;
    xaiAuthError.value = "";
    try {
      const payload = await request("/auth/xai/logout", { method: "POST" }, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      applyXAIAuthStatus(payload);
      resetXAILoginSession();
      if (typeof payload?.revocation_warning === "string") {
        xaiAuthError.value = payload.revocation_warning;
      }
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        xaiAuthError.value = e.message || t("msg_delete_failed");
      }
    } finally {
      if (generation === xaiRequestGeneration) {
        xaiAuthBusy.value = false;
      }
    }
  }

  async function openXAIAuthDialog() {
    const targetEndpointRef = currentEndpointRef();
    const shouldStartLogin = xaiAuthNeedsLogin.value && !xaiLoginSession.value && !xaiAuthBusy.value;
    let authWindow = null;
    if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
      authWindow = openExternalPlaceholder();
    }
    await openReentrantDialog(xaiAuthDialogOpen);
    void loadXAIAuthStatus(targetEndpointRef);
    if (shouldStartLogin) {
      void startXAILogin(authWindow, targetEndpointRef);
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
    resetXAIAuthFlow();
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
    resetXAIAuthEndpointState,
  };
}

export default useXAIAuthFlow;
