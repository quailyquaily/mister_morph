import { computed, onBeforeUnmount, ref, watch } from "vue";

import {
  createDesktopMessageScheduler,
  logDesktopRuntimeEvent,
  onDesktopWindowMessage,
  sendDesktopWindowMessage,
} from "./desktop-runtime";
import { randomDesktopWindowID } from "./desktop-windows";

function readOption(value) {
  return typeof value === "function" ? value() : value;
}

function normalizePayload(value) {
  return value && typeof value === "object" ? value : {};
}

export function useDesktopPayloadDialog(options = {}) {
  const desktopOpen = ref(false);
  const desktopOpening = ref(false);
  const requestID = ref("");
  const windowID = String(options.windowID || "").trim();
  const webDialogOpen = computed(() => isDialogOpen() && !desktopOpen.value && !desktopOpening.value);

  function isDialogOpen() {
    return readOption(options.open) === true;
  }

  function title() {
    return String(readOption(options.title) || "").trim();
  }

  function close() {
    if (typeof options.close === "function") {
      options.close();
    }
  }

  function payload() {
    return {
      ...normalizePayload(typeof options.payload === "function" ? options.payload(requestID.value) : options.payload),
      request_id: requestID.value,
    };
  }

  function sendClose() {
    if (!desktopOpen.value) {
      return;
    }
    sendDesktopWindowMessage({
      window_id: windowID,
      type: "dialog:close",
      request_id: requestID.value,
    });
  }

  function sendUpdate() {
    if (!desktopOpen.value) {
      return;
    }
    sendDesktopWindowMessage({
      window_id: windowID,
      type: "dialog:update",
      request_id: requestID.value,
      payload: payload(),
    });
  }

  const updateScheduler = createDesktopMessageScheduler(sendUpdate);

  async function openDesktopDialog() {
    if (!windowID || typeof options.openWindow !== "function") {
      return;
    }
    requestID.value = randomDesktopWindowID();
    desktopOpening.value = true;
    try {
      const opened = await options
        .openWindow({
          title: title(),
          payload: payload(),
        })
        .catch(() => false);
      if (opened === true && !isDialogOpen()) {
        sendDesktopWindowMessage({
          window_id: windowID,
          type: "dialog:close",
          request_id: requestID.value,
        });
        updateScheduler.clear();
        return;
      }
      desktopOpen.value = opened === true;
      if (desktopOpen.value) {
        updateScheduler.schedule();
      }
    } finally {
      desktopOpening.value = false;
    }
  }

  watch(
    () => isDialogOpen(),
    (open) => {
      if (open) {
        void openDesktopDialog();
      } else {
        sendClose();
        updateScheduler.clear();
        desktopOpen.value = false;
        desktopOpening.value = false;
      }
    },
    { immediate: true }
  );

  watch(
    payload,
    () => {
      if (desktopOpen.value) {
        updateScheduler.schedule();
      }
    },
    { deep: true }
  );

  const removeDesktopListener = onDesktopWindowMessage((message) => {
    const hiddenWindowID = String(message?.payload?.window_id || message?.window_id || "").trim();
    if (message?.type === "desktop:window-hidden" && hiddenWindowID === windowID) {
      logDesktopRuntimeEvent("payload_dialog_hidden", {
        window_id: windowID,
      });
      close();
      updateScheduler.clear();
      desktopOpen.value = false;
      desktopOpening.value = false;
      return;
    }
    if (message?.request_id !== requestID.value) {
      return;
    }
    if (message?.type === "dialog:ready") {
      updateScheduler.schedule([0]);
      return;
    }
    if (message?.type === "dialog:closed") {
      close();
      updateScheduler.clear();
      desktopOpen.value = false;
      desktopOpening.value = false;
      return;
    }
    if (typeof options.onMessage === "function") {
      options.onMessage(message);
    }
  });

  onBeforeUnmount(() => {
    updateScheduler.clear();
    removeDesktopListener();
  });

  return {
    requestID,
    webDialogOpen,
  };
}
