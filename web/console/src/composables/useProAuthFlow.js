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

  async function loadProAuthStatus() {
    if (proAuthLoading.value) {
      return;
    }
    proAuthLoading.value = true;
    proAuthError.value = "";
    try {
      applyProAuthStatus(await apiFetch("/auth/pro/status"));
    } catch (e) {
      proAuthError.value = e.message || t("msg_load_failed");
    } finally {
      proAuthLoading.value = false;
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
  }

  function resetProAuthFlow() {
    resetProLoginSession();
    proAuthError.value = "";
  }

  function scheduleProLoginPoll(intervalSeconds = 5) {
    clearProLoginTimer();
    proLoginPollTimer = window.setTimeout(() => {
      void pollProLogin();
    }, delayFromIntervalSeconds(intervalSeconds));
  }

  async function startProLogin(authWindow = null) {
    if (proAuthBusy.value) {
      if (authWindow && !authWindow.closed) {
        authWindow.close();
      }
      return;
    }
    proAuthBusy.value = true;
    proAuthError.value = "";
    resetProLoginSession();
    let authWindowUsed = false;
    try {
      const payload = await apiFetch("/auth/pro/login/start", { method: "POST" });
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
      proAuthError.value = e.message || t("msg_load_failed");
    } finally {
      if (!authWindowUsed && authWindow && !authWindow.closed) {
        authWindow.close();
      }
      proAuthBusy.value = false;
    }
  }

  async function pollProLogin() {
    const sessionID = proLoginSession.value;
    if (!sessionID || proAuthBusy.value) {
      return;
    }
    proAuthBusy.value = true;
    proAuthError.value = "";
    try {
      const payload = await apiFetch("/auth/pro/login/poll", {
        method: "POST",
        body: { session_id: sessionID, set_default: true },
      });
      if (payload?.pending === true) {
        scheduleProLoginPoll(payload?.interval_seconds);
        return;
      }
      applyProAuthStatus(payload);
      resetProLoginSession();
      if (payload?.settings_updated === true) {
        invalidateConsoleSetupReadiness();
        await onSettingsUpdated(payload);
      }
    } catch (e) {
      proAuthError.value = e.message || t("msg_load_failed");
    } finally {
      proAuthBusy.value = false;
    }
  }

  async function logoutProAuth() {
    if (proAuthBusy.value) {
      return;
    }
    proAuthBusy.value = true;
    proAuthError.value = "";
    try {
      applyProAuthStatus(await apiFetch("/auth/pro/logout", { method: "POST" }));
      resetProLoginSession();
    } catch (e) {
      proAuthError.value = e.message || t("msg_delete_failed");
    } finally {
      proAuthBusy.value = false;
    }
  }

  async function openProAuthDialog() {
    const shouldStartLogin = proAuthNeedsLogin.value && !proLoginSession.value && !proAuthBusy.value;
    let authWindow = null;
    if (shouldStartLogin && !canOpenExternalURLInDesktop()) {
      authWindow = openExternalPlaceholder();
    }
    await openReentrantDialog(proAuthDialogOpen);
    void loadProAuthStatus();
    if (shouldStartLogin) {
      void startProLogin(authWindow);
    }
  }

  watch(proAuthDialogOpen, (open) => {
    if (!open) {
      resetProAuthFlow();
    }
  });

  onBeforeUnmount(() => {
    clearProLoginTimer();
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
    applyProAuthStatus,
    loadProAuthStatus,
    openProAuthDialog,
    pollProLogin,
    logoutProAuth,
    resetProLoginSession,
    resetProAuthFlow,
  };
}

export default useProAuthFlow;
