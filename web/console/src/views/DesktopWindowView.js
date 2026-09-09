import { useToast } from "quail-ui";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { runtimeApiFetch, translate } from "../core/context";
import {
  hideDesktopWindow,
  logDesktopRuntimeEvent,
  onDesktopWindowMessage,
  sendDesktopWindowMessage,
  summarizeDesktopPayload,
} from "../core/desktop-runtime";
import { takeDesktopWindowPayload } from "../core/desktop-windows";
import { getDesktopWindowDialog, listDesktopWindowDialogs } from "./desktop-window-dialog-registry";
import "./DesktopWindowView.css";

const POKE_BODY_LIMIT = 10 * 1024;

function utf8ByteLength(value) {
  return new TextEncoder().encode(String(value || "")).length;
}

function queryFlag(value, fallback = true) {
  const text = typeof value === "string" ? value.trim().toLowerCase() : "";
  if (!text) {
    return fallback;
  }
  if (text === "0" || text === "false" || text === "none" || text === "off") {
    return false;
  }
  if (text === "1" || text === "true" || text === "window" || text === "on") {
    return true;
  }
  return fallback;
}

const DesktopWindowView = {
  setup() {
    const route = useRoute();
    const toast = useToast();
    const t = translate;
    const rawJsonPayload = ref(null);
    const setupPickerPayload = ref(null);
    const connectionTestPayload = ref(null);
    const codexAuthPayload = ref(null);
    const rawTextPayload = ref(null);
    const rawTextValue = ref("");
    const pokeBody = ref("");
    const pokeError = ref("");
    const poking = ref(false);
    const windowID = computed(() =>
      typeof route.params.window_id === "string" ? route.params.window_id.trim() : ""
    );
    const dialogContext = {};
    const activeDialog = computed(() => getDesktopWindowDialog(windowID.value));
    const noPadding = computed(() => {
      const padding = typeof route.query.padding === "string" ? route.query.padding.trim().toLowerCase() : "";
      return padding === "none" || padding === "0" || padding === "false";
    });
    const contentScroll = computed(() => queryFlag(route.query.scroll, true));
    const pokeBodyBytes = computed(() => utf8ByteLength(pokeBody.value));
    const pokeBodyTooLarge = computed(() => pokeBodyBytes.value > POKE_BODY_LIMIT);
    const pokeSubmitDisabled = computed(
      () => poking.value || !String(pokeBody.value || "").trim() || pokeBodyTooLarge.value
    );
    const activeWindowReady = computed(() => {
      const dialog = activeDialog.value;
      return !!(dialog && typeof dialog.ready === "function" && dialog.ready(dialogContext));
    });
    const activeWindowComponent = computed(() => (activeWindowReady.value ? activeDialog.value.component : null));
    const activeWindowContentClass = computed(() => activeDialog.value?.contentClass || "");
    const activeWindowProps = computed(() => {
      const dialog = activeDialog.value;
      return dialog && typeof dialog.props === "function" ? dialog.props(dialogContext) : {};
    });
    const activeWindowEvents = computed(() => {
      const dialog = activeDialog.value;
      return dialog && typeof dialog.events === "function" ? dialog.events(dialogContext) : {};
    });

    function resetPoke() {
      pokeBody.value = "";
      pokeError.value = "";
    }

    function normalizePayload(payload) {
      return payload && typeof payload === "object" ? payload : {};
    }

    function requestIDFromPayload(payload) {
      return String(normalizePayload(payload).request_id || "").trim();
    }

    function requestIDFromQuery() {
      return typeof route.query.request_id === "string" ? route.query.request_id.trim() : "";
    }

    function resetDialogPayloadState() {
      listDesktopWindowDialogs().forEach((dialog) => {
        const key = dialog.stateRef;
        if (key && dialogContext[key]) {
          dialogContext[key].value = null;
        }
      });
      rawTextValue.value = "";
    }

    function currentDialogPayloadRef() {
      const key = activeDialog.value?.stateRef;
      return key && dialogContext[key] ? dialogContext[key] : null;
    }

    function currentDialogRequestID() {
      const payloadRef = currentDialogPayloadRef();
      return payloadRef ? requestIDFromPayload(payloadRef.value) : "";
    }

    function loadDialogPayload(expectedKind) {
      const payloadID = typeof route.query.payload_id === "string" ? route.query.payload_id.trim() : "";
      const payload = takeDesktopWindowPayload(payloadID, expectedKind);
      logDesktopRuntimeEvent("desktop_window_load_payload", {
        window_id: windowID.value,
        expected_kind: expectedKind,
        payload_id: payloadID,
        found: payload !== null,
        payload: summarizeDesktopPayload(payload),
      });
      return payload && typeof payload === "object" ? payload : null;
    }

    function notifyDialogReady() {
      const requestID = currentDialogRequestID();
      if (!requestID) {
        return;
      }
      sendDesktopWindowMessage({
        target: "parent",
        type: "dialog:ready",
        request_id: requestID,
        payload: {
          window_id: windowID.value,
        },
      });
    }

    function applyDialogPayload(payload) {
      if (!payload) {
        return;
      }
      const dialog = activeDialog.value;
      if (!dialog || typeof dialog.applyPayload !== "function") {
        return;
      }
      const value = normalizePayload(payload);
      logDesktopRuntimeEvent("desktop_window_apply_payload", {
        window_id: windowID.value,
        payload: summarizeDesktopPayload(value),
      });
      dialog.applyPayload(dialogContext, value);
    }

    function loadPayload() {
      logDesktopRuntimeEvent("desktop_window_route_load", {
        window_id: windowID.value,
        full_path: route.fullPath,
      });
      resetDialogPayloadState();
      const dialog = activeDialog.value;
      if (!dialog) {
        return;
      }
      if (typeof dialog.onRouteLoad === "function") {
        dialog.onRouteLoad(dialogContext);
      }
      if (!dialog.storedPayload) {
        return;
      }
      const payload = loadDialogPayload(dialog.id);
      if (dialog.statefulPayload) {
        applyDialogPayload(payload || { request_id: requestIDFromQuery() });
        notifyDialogReady();
        return;
      }
      if (payload) {
        applyDialogPayload(payload);
      }
    }

    function hideWindow() {
      if (hideDesktopWindow()) {
        return;
      }
      if (typeof window !== "undefined" && typeof window.close === "function") {
        window.close();
      }
    }

    function closeDialogWindow(payload) {
      sendDesktopWindowMessage({
        target: "parent",
        type: "dialog:closed",
        request_id: requestIDFromPayload(payload),
        payload: {
          window_id: windowID.value,
        },
      });
      hideWindow();
    }

    function selectSetupPickerItem(item) {
      sendDesktopWindowMessage({
        target: "parent",
        type: "setup-picker:selected",
        request_id: requestIDFromPayload(setupPickerPayload.value),
        payload: {
          item,
        },
      });
      hideWindow();
    }

    function retryConnectionTest() {
      logDesktopRuntimeEvent("desktop_window_retry_connection_test", {
        window_id: windowID.value,
        request_id: requestIDFromPayload(connectionTestPayload.value),
      });
      sendDesktopWindowMessage({
        target: "parent",
        type: "setup-connection-test:retry",
        request_id: requestIDFromPayload(connectionTestPayload.value),
      });
    }

    function logoutCodexAuth() {
      logDesktopRuntimeEvent("desktop_window_logout_codex_auth", {
        window_id: windowID.value,
        request_id: requestIDFromPayload(codexAuthPayload.value),
      });
      sendDesktopWindowMessage({
        target: "parent",
        type: "codex-auth:logout",
        request_id: requestIDFromPayload(codexAuthPayload.value),
      });
    }

    function updateRawTextValue(value) {
      rawTextValue.value = String(value || "");
    }

    function saveRawText() {
      sendDesktopWindowMessage({
        target: "parent",
        type: "raw-text-editor:save",
        request_id: requestIDFromPayload(rawTextPayload.value),
        payload: {
          content: rawTextValue.value,
        },
      });
    }

    function updatePokeBody(value) {
      pokeBody.value = String(value || "");
      if (pokeError.value) {
        pokeError.value = "";
      }
    }

    async function submitPoke() {
      const body = String(pokeBody.value || "").trim();
      if (!body) {
        pokeError.value = t("runtime_poke_empty");
        return;
      }
      if (utf8ByteLength(body) > POKE_BODY_LIMIT) {
        pokeError.value = t("runtime_poke_too_large");
        return;
      }
      poking.value = true;
      try {
        const data = await runtimeApiFetch("/poke", {
          method: "POST",
          headers: { "Content-Type": "text/plain; charset=utf-8" },
          body,
        });
        sendDesktopWindowMessage({
          target: "parent",
          type: "runtime:poke-submitted",
          payload: {
            poked_at: typeof data?.poked_at === "string" ? data.poked_at : "",
          },
        });
        resetPoke();
        toast.success(t("runtime_poke_ok"));
        hideWindow();
      } catch (e) {
        const message = e.message || t("msg_load_failed");
        if (e?.status === 400 || e?.status === 413) {
          pokeError.value = message;
        } else {
          toast.error(message);
        }
      } finally {
        poking.value = false;
      }
    }

    Object.assign(dialogContext, {
      closeDialogWindow,
      codexAuthPayload,
      connectionTestPayload,
      logoutCodexAuth,
      pokeBody,
      pokeBodyTooLarge,
      pokeError,
      pokeSubmitDisabled,
      poking,
      rawJsonPayload,
      rawTextPayload,
      rawTextValue,
      resetPoke,
      retryConnectionTest,
      saveRawText,
      selectSetupPickerItem,
      setupPickerPayload,
      submitPoke,
      updatePokeBody,
      updateRawTextValue,
    });

    const removeDesktopListener = onDesktopWindowMessage((message) => {
      if (message?.type === "dialog:close") {
        const messageRequestID = String(message?.request_id || "").trim();
        if (messageRequestID && messageRequestID !== currentDialogRequestID()) {
          return;
        }
        hideWindow();
        return;
      }
      if (message?.type !== "dialog:update") {
        return;
      }
      if (!activeDialog.value?.statefulPayload) {
        return;
      }
      const payload = normalizePayload(message?.payload);
      if (message?.request_id !== requestIDFromPayload(payload)) {
        return;
      }
      applyDialogPayload(payload);
    });

    watch(() => route.fullPath, loadPayload, { immediate: true });

    onBeforeUnmount(removeDesktopListener);

    return {
      activeWindowComponent,
      activeWindowContentClass,
      activeWindowEvents,
      activeWindowProps,
      activeWindowReady,
      t,
      windowID,
      noPadding,
      contentScroll,
    };
  },
  template: `
    <main
      class="desktop-window-view"
      :class="{
        'desktop-window-view--no-padding': noPadding,
        'desktop-window-view--content-scroll': contentScroll
      }"
    >
      <section
        v-if="activeWindowReady"
        class="desktop-window-view__content"
        :class="activeWindowContentClass"
      >
        <component
          :is="activeWindowComponent"
          v-bind="activeWindowProps"
          v-on="activeWindowEvents"
        />
      </section>
      <section v-else class="desktop-window-view__empty" aria-live="polite">
        <p>{{ t("desktop_window_unavailable") }}</p>
      </section>
    </main>
  `,
};

export default DesktopWindowView;
