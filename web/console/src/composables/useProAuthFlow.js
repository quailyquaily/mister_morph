import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";

import { apiFetch, formatTime, translate } from "../core/context";
import {
  canOpenExternalURLInDesktop,
  openExternalPlaceholder,
  openExternalURL,
} from "../core/external-links";
import { invalidateConsoleSetupReadiness } from "../core/setup";
import { openReentrantDialog } from "../core/reentrant-dialog";

function emptyProAuthStatus() {
  return {
    logged_in: false,
    access_token_present: false,
    refresh_token_present: false,
    access_token_expired: false,
    subscription_api_key_present: false,
    subscription: "",
    expires_at: "",
    scope: "",
    user: {},
    file_mode_ok: true,
    file_mode_warning: "",
  };
}

function normalizeProAuthStatus(payload) {
  const status = payload && typeof payload.status === "object" ? payload.status : payload;
  return {
    logged_in: status?.logged_in === true,
    access_token_present: status?.access_token_present === true,
    refresh_token_present: status?.refresh_token_present === true,
    access_token_expired: status?.access_token_expired === true,
    subscription_api_key_present: status?.subscription_api_key_present === true,
    subscription: typeof status?.subscription === "string" ? status.subscription : "",
    expires_at: typeof status?.expires_at === "string" ? status.expires_at : "",
    scope: typeof status?.scope === "string" ? status.scope : "",
    user: status?.user && typeof status.user === "object" ? status.user : {},
    file_mode_ok: status?.file_mode_ok !== false,
    file_mode_warning: typeof status?.file_mode_warning === "string" ? status.file_mode_warning : "",
  };
}

function delayFromIntervalSeconds(intervalSeconds = 5) {
  return Math.max(2, Number(intervalSeconds) || 5) * 1000;
}

export function useProAuthFlow(options = {}) {
  const t = translate;
  const request = typeof options.request === "function" ? options.request : apiFetch;
  const getEndpointRef = typeof options.getEndpointRef === "function" ? options.getEndpointRef : () => "";
  const onSettingsUpdated =
    typeof options.onSettingsUpdated === "function" ? options.onSettingsUpdated : async () => {};

  const proAuthLoading = ref(false);
  const proAuthBusy = ref(false);
  const proAuthError = ref("");
  const proAuthDialogOpen = ref(false);
  const proLoginSession = ref("");
  const proLoginVerificationURL = ref("");
  const proLoginUserCode = ref("");
  const proLoginExpiresAt = ref("");
  const proAuthStatus = reactive(emptyProAuthStatus());
  let proLoginPollTimer = 0;
  let proRequestGeneration = 0;
  let proLoginEndpointRef = "";

  const proAuthSummary = computed(() => {
    if (proAuthLoading.value) {
      return t("settings_pro_auth_loading");
    }
    if (!proAuthStatus.logged_in) {
      return t("settings_pro_auth_signed_out");
    }
    if (proAuthStatus.access_token_expired && proAuthStatus.refresh_token_present) {
      return t("settings_pro_auth_refreshable");
    }
    if (proAuthStatus.access_token_expired) {
      return t("settings_pro_auth_expired");
    }
    return t("settings_pro_auth_signed_in");
  });

  const proAuthButtonState = computed(() => {
    if (proAuthLoading.value) {
      return "loading";
    }
    if (!proAuthStatus.logged_in) {
      return "signed-out";
    }
    if (proAuthStatus.access_token_expired && proAuthStatus.refresh_token_present) {
      return "refreshable";
    }
    if (proAuthStatus.access_token_expired) {
      return "expired";
    }
    return "signed-in";
  });

  const proAuthNeedsLogin = computed(() => ["signed-out", "expired"].includes(proAuthButtonState.value));
  const proAuthButtonTitle = computed(() => `${t("settings_pro_auth_title")}: ${proAuthSummary.value}`);
  const proLoginExpiresLabel = computed(() =>
    proLoginExpiresAt.value ? formatTime(proLoginExpiresAt.value) : t("ttl_unknown")
  );

  function applyProAuthStatus(payload) {
    Object.assign(proAuthStatus, normalizeProAuthStatus(payload));
  }

  function currentEndpointRef() {
    return String(getEndpointRef() || "").trim();
  }

  function isCurrentRequest(generation, endpointRef) {
    return generation === proRequestGeneration && endpointRef === currentEndpointRef();
  }

  async function loadProAuthStatus(endpointRef = currentEndpointRef()) {
    if (proAuthLoading.value) {
      return;
    }
    const targetEndpointRef = String(endpointRef || "").trim();
    const generation = proRequestGeneration;
    proAuthLoading.value = true;
    proAuthError.value = "";
    try {
      const payload = await request("/auth/pro/status", undefined, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      applyProAuthStatus(payload);
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        proAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        proAuthLoading.value = false;
      }
    }
  }

  function clearProLoginTimer() {
    if (proLoginPollTimer) {
      window.clearTimeout(proLoginPollTimer);
      proLoginPollTimer = 0;
    }
  }

  function resetProLoginSession() {
    clearProLoginTimer();
    proLoginSession.value = "";
    proLoginVerificationURL.value = "";
    proLoginUserCode.value = "";
    proLoginExpiresAt.value = "";
    proLoginEndpointRef = "";
  }

  function resetProAuthFlow() {
    proRequestGeneration += 1;
    proAuthLoading.value = false;
    proAuthBusy.value = false;
    resetProLoginSession();
    proAuthError.value = "";
  }

  function resetProAuthEndpointState() {
    resetProAuthFlow();
    Object.assign(proAuthStatus, emptyProAuthStatus());
  }

  function scheduleProLoginPoll(intervalSeconds = 5) {
    clearProLoginTimer();
    proLoginPollTimer = window.setTimeout(() => {
      void pollProLogin();
    }, delayFromIntervalSeconds(intervalSeconds));
  }

  async function startProLogin(authWindow = null, endpointRef = currentEndpointRef()) {
    if (proAuthBusy.value) {
      if (authWindow && !authWindow.closed) {
        authWindow.close();
      }
      return;
    }
    const targetEndpointRef = String(endpointRef || "").trim();
    const generation = ++proRequestGeneration;
    proAuthLoading.value = false;
    proAuthBusy.value = true;
    proAuthError.value = "";
    resetProLoginSession();
    let authWindowUsed = false;
    try {
      const payload = await request("/auth/pro/login/start", { method: "POST" }, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      proLoginEndpointRef = targetEndpointRef;
      proLoginSession.value = String(payload?.session_id || "").trim();
      proLoginVerificationURL.value = String(payload?.verification_url_complete || payload?.verification_url || "").trim();
      proLoginUserCode.value = String(payload?.user_code || "").trim();
      proLoginExpiresAt.value = String(payload?.expires_at || "").trim();
      if (proLoginVerificationURL.value) {
        if (authWindow && !authWindow.closed) {
          authWindow.location.href = proLoginVerificationURL.value;
          authWindowUsed = true;
        } else {
          openExternalURL(proLoginVerificationURL.value);
        }
      }
      scheduleProLoginPoll(payload?.interval_seconds);
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        proAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (!authWindowUsed && authWindow && !authWindow.closed) {
        authWindow.close();
      }
      if (generation === proRequestGeneration) {
        proAuthBusy.value = false;
      }
    }
  }

  async function pollProLogin() {
    const sessionID = proLoginSession.value;
    const targetEndpointRef = proLoginEndpointRef;
    if (!sessionID || proAuthBusy.value) {
      return;
    }
    if (targetEndpointRef !== currentEndpointRef()) {
      resetProAuthFlow();
      return;
    }
    const generation = ++proRequestGeneration;
    proAuthLoading.value = false;
    proAuthBusy.value = true;
    proAuthError.value = "";
    try {
      const payload = await request(
        "/auth/pro/login/poll",
        {
          method: "POST",
          body: { session_id: sessionID, set_default: true },
        },
        targetEndpointRef
      );
      if (
        !isCurrentRequest(generation, targetEndpointRef) ||
        targetEndpointRef !== proLoginEndpointRef
      ) {
        return;
      }
      if (payload?.pending === true) {
        scheduleProLoginPoll(payload?.interval_seconds);
        return;
      }
      applyProAuthStatus(payload);
      resetProLoginSession();
      if (payload?.settings_updated === true) {
        invalidateConsoleSetupReadiness();
        await onSettingsUpdated(payload, targetEndpointRef);
      }
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        proAuthError.value = e.message || t("msg_load_failed");
      }
    } finally {
      if (generation === proRequestGeneration) {
        proAuthBusy.value = false;
      }
    }
  }

  async function logoutProAuth() {
    if (proAuthBusy.value) {
      return;
    }
    const targetEndpointRef = currentEndpointRef();
    const generation = ++proRequestGeneration;
    proAuthLoading.value = false;
    proAuthBusy.value = true;
    proAuthError.value = "";
    try {
      const payload = await request("/auth/pro/logout", { method: "POST" }, targetEndpointRef);
      if (!isCurrentRequest(generation, targetEndpointRef)) {
        return;
      }
      applyProAuthStatus(payload);
      resetProLoginSession();
    } catch (e) {
      if (isCurrentRequest(generation, targetEndpointRef)) {
        proAuthError.value = e.message || t("msg_delete_failed");
      }
    } finally {
      if (generation === proRequestGeneration) {
        proAuthBusy.value = false;
      }
    }
  }

  async function openProAuthDialog() {
    const targetEndpointRef = currentEndpointRef();
    const shouldStartLogin = proAuthNeedsLogin.value && !proLoginSession.value && !proAuthBusy.value;
    let authWindow = null;
    if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
      authWindow = openExternalPlaceholder();
    }
    await openReentrantDialog(proAuthDialogOpen);
    void loadProAuthStatus(targetEndpointRef);
    if (shouldStartLogin) {
      void startProLogin(authWindow, targetEndpointRef);
    }
  }

  watch(proAuthDialogOpen, (open) => {
    if (!open) {
      resetProAuthFlow();
    }
  });

  onBeforeUnmount(() => {
    resetProAuthFlow();
  });

  return {
    proAuthLoading,
    proAuthBusy,
    proAuthError,
    proAuthDialogOpen,
    proAuthStatus,
    proAuthSummary,
    proAuthButtonState,
    proAuthNeedsLogin,
    proAuthButtonTitle,
    proLoginSession,
    proLoginVerificationURL,
    proLoginUserCode,
    proLoginExpiresLabel,
    loadProAuthStatus,
    openProAuthDialog,
    pollProLogin,
    logoutProAuth,
    resetProAuthFlow,
    resetProAuthEndpointState,
  };
}

export default useProAuthFlow;
