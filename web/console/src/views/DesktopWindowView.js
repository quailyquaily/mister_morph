import { useToast } from "quail-ui";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import PokeDialogContent from "../components/PokeDialogContent";
import RawJsonDialogContent from "../components/RawJsonDialogContent";
import { formatBytes, runtimeApiFetch, translate } from "../core/context";
import { hideDesktopWindow, sendDesktopWindowMessage } from "../core/desktop-runtime";
import { takeDesktopWindowPayload } from "../core/desktop-windows";
import "./DesktopWindowView.css";

const RAW_JSON_WINDOW_ID = "raw-json";
const POKE_WINDOW_ID = "poke";
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
    PokeDialogContent,
    RawJsonDialogContent,
  },
  setup() {
    const route = useRoute();
    const toast = useToast();
    const t = translate;
    const rawJsonPayload = ref(null);
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

    function loadPayload() {
      rawJsonPayload.value = null;
      if (windowID.value === POKE_WINDOW_ID) {
        resetPoke();
        return;
      }
      if (windowID.value !== RAW_JSON_WINDOW_ID) {
        return;
      }
      const payloadID = typeof route.query.payload_id === "string" ? route.query.payload_id.trim() : "";
      const payload = takeDesktopWindowPayload(payloadID, RAW_JSON_WINDOW_ID);
      if (!payload) {
        return;
      }
      rawJsonPayload.value = {
        title: String(payload.title || "").trim(),
        json: String(payload.json || ""),
      };
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

    watch(() => route.fullPath, loadPayload, { immediate: true });

    return {
      closePokeWindow,
      pokeBody,
      pokeBodyTooLarge,
      pokeError,
      pokeHelperText,
      pokeSizeLabel,
      pokeSubmitDisabled,
      poking,
      rawJsonPayload,
      submitPoke,
      t,
      updatePokeBody,
      windowID,
      noPadding,
      contentScroll,
      POKE_WINDOW_ID,
      RAW_JSON_WINDOW_ID,
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
      <section v-else class="desktop-window-view__empty" aria-live="polite">
        <p>{{ t("desktop_window_unavailable") }}</p>
      </section>
    </main>
  `,
};

export default DesktopWindowView;
