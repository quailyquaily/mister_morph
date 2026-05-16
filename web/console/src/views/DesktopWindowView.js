import { useToast } from "quail-ui";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";

import CodexAuthDialogContent from "../components/CodexAuthDialogContent";
import PokeDialogContent from "../components/PokeDialogContent";
import RawJsonDialogContent from "../components/RawJsonDialogContent";
import RawTextEditorDialogContent from "../components/RawTextEditorDialogContent";
import SetupConnectionTestDialogContent from "../components/SetupConnectionTestDialogContent";
import SetupPickerDialogContent from "../components/SetupPickerDialogContent";
import { formatBytes, runtimeApiFetch, translate } from "../core/context";
import {
  hideDesktopWindow,
  logDesktopRuntimeEvent,
  onDesktopWindowMessage,
  sendDesktopWindowMessage,
  summarizeDesktopPayload,
} from "../core/desktop-runtime";
import {
  CODEX_AUTH_WINDOW_ID,
  POKE_WINDOW_ID,
  RAW_JSON_WINDOW_ID,
  RAW_TEXT_EDITOR_WINDOW_ID,
  SETUP_CONNECTION_TEST_WINDOW_ID,
  SETUP_PICKER_WINDOW_ID,
  takeDesktopWindowPayload,
} from "../core/desktop-windows";
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
  components: {
    CodexAuthDialogContent,
    PokeDialogContent,
    RawJsonDialogContent,
    RawTextEditorDialogContent,
    SetupConnectionTestDialogContent,
    SetupPickerDialogContent,
  },
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
    const pokeSizeLabel = computed(() =>
      t("runtime_poke_size", { used: formatBytes(pokeBodyBytes.value), limit: formatBytes(POKE_BODY_LIMIT) })
    );
    const pokeHelperText = computed(() => {
      if (pokeError.value) {
        return pokeError.value;
      }
      if (pokeBodyTooLarge.value) {
        return t("runtime_poke_too_large");
      }
      return t("runtime_poke_limit", { limit: formatBytes(POKE_BODY_LIMIT) });
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

    function currentDialogRequestID() {
      switch (windowID.value) {
        case SETUP_PICKER_WINDOW_ID:
          return requestIDFromPayload(setupPickerPayload.value);
        case SETUP_CONNECTION_TEST_WINDOW_ID:
          return requestIDFromPayload(connectionTestPayload.value);
        case CODEX_AUTH_WINDOW_ID:
          return requestIDFromPayload(codexAuthPayload.value);
        case RAW_TEXT_EDITOR_WINDOW_ID:
          return requestIDFromPayload(rawTextPayload.value);
        default:
          return "";
      }
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
      const value = normalizePayload(payload);
      logDesktopRuntimeEvent("desktop_window_apply_payload", {
        window_id: windowID.value,
        payload: summarizeDesktopPayload(value),
      });
      switch (windowID.value) {
        case SETUP_PICKER_WINDOW_ID:
          setupPickerPayload.value = value;
          break;
        case SETUP_CONNECTION_TEST_WINDOW_ID:
          connectionTestPayload.value = value;
          break;
        case CODEX_AUTH_WINDOW_ID:
          codexAuthPayload.value = value;
          break;
        case RAW_TEXT_EDITOR_WINDOW_ID:
          rawTextPayload.value = value;
          rawTextValue.value = String(value.modelValue || "");
          break;
      }
    }

    function loadPayload() {
      logDesktopRuntimeEvent("desktop_window_route_load", {
        window_id: windowID.value,
        full_path: route.fullPath,
      });
      rawJsonPayload.value = null;
      setupPickerPayload.value = null;
      connectionTestPayload.value = null;
      codexAuthPayload.value = null;
      rawTextPayload.value = null;
      rawTextValue.value = "";
      if (windowID.value === POKE_WINDOW_ID) {
        resetPoke();
        return;
      }
      if (windowID.value === RAW_JSON_WINDOW_ID) {
        const payload = loadDialogPayload(RAW_JSON_WINDOW_ID);
        if (!payload) {
          return;
        }
        rawJsonPayload.value = {
          title: String(payload.title || "").trim(),
          json: String(payload.json || ""),
        };
        return;
      }
      if (
        windowID.value === SETUP_PICKER_WINDOW_ID ||
        windowID.value === SETUP_CONNECTION_TEST_WINDOW_ID ||
        windowID.value === CODEX_AUTH_WINDOW_ID ||
        windowID.value === RAW_TEXT_EDITOR_WINDOW_ID
      ) {
        applyDialogPayload(loadDialogPayload(windowID.value) || { request_id: requestIDFromQuery() });
        notifyDialogReady();
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

    function closePokeWindow() {
      if (poking.value) {
        return;
      }
      pokeError.value = "";
      hideWindow();
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
      const payload = normalizePayload(message?.payload);
      if (message?.request_id !== requestIDFromPayload(payload)) {
        return;
      }
      applyDialogPayload(payload);
    });

    watch(() => route.fullPath, loadPayload, { immediate: true });

    onBeforeUnmount(removeDesktopListener);

    return {
      closeDialogWindow,
      closePokeWindow,
      codexAuthPayload,
      connectionTestPayload,
      logoutCodexAuth,
      pokeBody,
      pokeBodyTooLarge,
      pokeError,
      pokeHelperText,
      pokeSizeLabel,
      pokeSubmitDisabled,
      poking,
      rawJsonPayload,
      rawTextPayload,
      rawTextValue,
      retryConnectionTest,
      saveRawText,
      selectSetupPickerItem,
      setupPickerPayload,
      submitPoke,
      t,
      updateRawTextValue,
      updatePokeBody,
      windowID,
      noPadding,
      contentScroll,
      CODEX_AUTH_WINDOW_ID,
      POKE_WINDOW_ID,
      RAW_JSON_WINDOW_ID,
      RAW_TEXT_EDITOR_WINDOW_ID,
      SETUP_CONNECTION_TEST_WINDOW_ID,
      SETUP_PICKER_WINDOW_ID,
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
        v-if="windowID === RAW_JSON_WINDOW_ID && rawJsonPayload"
        class="desktop-window-view__content desktop-window-view__content--raw"
      >
        <RawJsonDialogContent :json="rawJsonPayload.json" />
      </section>
      <section
        v-else-if="windowID === POKE_WINDOW_ID"
        class="desktop-window-view__content desktop-window-view__content--form"
      >
        <PokeDialogContent
          inputId="desktop-poke-body"
          :body="pokeBody"
          :bodyTooLarge="pokeBodyTooLarge"
          :disabled="poking"
          :error="pokeError"
          :helperText="pokeHelperText"
          :sizeLabel="pokeSizeLabel"
          :submitDisabled="pokeSubmitDisabled"
          :submitting="poking"
          @cancel="closePokeWindow"
          @submit="submitPoke"
          @update:body="updatePokeBody"
        />
      </section>
      <section
        v-else-if="windowID === SETUP_PICKER_WINDOW_ID && setupPickerPayload"
        class="desktop-window-view__content desktop-window-view__content--form"
      >
        <SetupPickerDialogContent
          :items="setupPickerPayload.items || []"
          :loading="setupPickerPayload.loading === true"
          :error="setupPickerPayload.error || ''"
          :filterPlaceholder="setupPickerPayload.filterPlaceholder || ''"
          :emptyText="setupPickerPayload.emptyText || ''"
          :showValue="setupPickerPayload.showValue !== false"
          :resetKey="setupPickerPayload.request_id || ''"
          @select="selectSetupPickerItem"
        />
      </section>
      <section
        v-else-if="windowID === SETUP_CONNECTION_TEST_WINDOW_ID && connectionTestPayload"
        class="desktop-window-view__content desktop-window-view__content--form"
      >
        <SetupConnectionTestDialogContent
          :loading="connectionTestPayload.loading === true"
          :error="connectionTestPayload.error || ''"
          :benchmarks="connectionTestPayload.benchmarks || []"
          :provider="connectionTestPayload.provider || ''"
          :apiBase="connectionTestPayload.apiBase || ''"
          :model="connectionTestPayload.model || ''"
          :showIntro="connectionTestPayload.showIntro !== false"
          @retry="retryConnectionTest"
          @close="closeDialogWindow(connectionTestPayload)"
        />
      </section>
      <section
        v-else-if="windowID === CODEX_AUTH_WINDOW_ID && codexAuthPayload"
        class="desktop-window-view__content desktop-window-view__content--form"
      >
        <CodexAuthDialogContent
          :loading="codexAuthPayload.loading === true"
          :busy="codexAuthPayload.busy === true"
          :error="codexAuthPayload.error || ''"
          :status="codexAuthPayload.status || {}"
          :summary="codexAuthPayload.summary || ''"
          :loginSession="codexAuthPayload.loginSession || ''"
          :verificationURL="codexAuthPayload.verificationURL || ''"
          :userCode="codexAuthPayload.userCode || ''"
          :loginExpiresLabel="codexAuthPayload.loginExpiresLabel || ''"
          @logout="logoutCodexAuth"
        />
      </section>
      <section
        v-else-if="windowID === RAW_TEXT_EDITOR_WINDOW_ID && rawTextPayload"
        class="desktop-window-view__content desktop-window-view__content--wide"
      >
        <RawTextEditorDialogContent
          :path="rawTextPayload.path || ''"
          :modelValue="rawTextValue"
          :loading="rawTextPayload.loading === true"
          :saving="rawTextPayload.saving === true"
          @update:modelValue="updateRawTextValue"
          @save="saveRawText"
        />
      </section>
      <section v-else class="desktop-window-view__empty" aria-live="polite">
        <p>{{ t("desktop_window_unavailable") }}</p>
      </section>
    </main>
  `,
};

export default DesktopWindowView;
