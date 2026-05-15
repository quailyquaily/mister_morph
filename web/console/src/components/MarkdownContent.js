import { onBeforeUnmount, onMounted, ref, watch } from "vue";

let rendererModulePromise = null;
const STREAMING_RENDER_DEBOUNCE_MS = 80;

async function loadRendererModule() {
  rendererModulePromise ||= Promise.all([
    import("../vendor/markdown-renderer/index.js"),
    import("../vendor/markdown-renderer/index.css"),
  ]).then(([module]) => module);
  return rendererModulePromise;
}

const MarkdownContent = {
  emits: ["rendered"],
  props: {
    source: {
      type: String,
      default: "",
    },
    format: {
      type: String,
      default: "auto",
    },
    theme: {
      type: String,
      default: "paper",
    },
    streaming: {
      type: Boolean,
      default: false,
    },
    streamMode: {
      type: String,
      default: "balanced",
    },
    streamProfiler: {
      type: Boolean,
      default: false,
    },
  },
  setup(props, { emit }) {
    const host = ref(null);
    const renderer = ref(null);
    let resizeObserver = null;
    let resizeFrameID = 0;
    let renderTimerID = 0;
    let syncPromise = null;
    let syncQueued = false;
    let disposed = false;

    function emitRenderedOnNextFrame() {
      if (resizeFrameID) {
        return;
      }
      resizeFrameID = window.requestAnimationFrame(() => {
        resizeFrameID = 0;
        emit("rendered");
      });
    }

    function observeHostSize(element) {
      if (resizeObserver || typeof ResizeObserver === "undefined") {
        return;
      }
      resizeObserver = new ResizeObserver(() => {
        emitRenderedOnNextFrame();
      });
      resizeObserver.observe(element);
    }

    async function syncRenderer() {
      const element = host.value;
      if (!element) {
        return;
      }
      observeHostSize(element);
      const { MarkdownRenderer } = await loadRendererModule();
      if (!host.value || host.value !== element) {
        return;
      }
      if (!renderer.value) {
        renderer.value = new MarkdownRenderer(element, {
          format: props.format,
          streamMode: props.streamMode,
          streamProfiler: props.streamProfiler,
          streaming: props.streaming,
          theme: props.theme,
        });
      }
      await renderer.value.update(props.source, {
        format: props.format,
        streamMode: props.streamMode,
        streamProfiler: props.streamProfiler,
        streaming: props.streaming,
        theme: props.theme,
      });
      if (host.value === element) {
        emit("rendered");
      }
    }

    function clearScheduledRender() {
      if (!renderTimerID) {
        return;
      }
      window.clearTimeout(renderTimerID);
      renderTimerID = 0;
    }

    async function runRendererSync() {
      if (disposed) {
        return;
      }
      if (syncPromise) {
        syncQueued = true;
        return;
      }
      syncPromise = syncRenderer();
      try {
        await syncPromise;
      } finally {
        syncPromise = null;
        if (syncQueued && !disposed) {
          syncQueued = false;
          requestRendererSync();
        }
      }
    }

    function requestRendererSync() {
      if (disposed) {
        return;
      }
      clearScheduledRender();
      if (props.streaming && renderer.value) {
        renderTimerID = window.setTimeout(() => {
          renderTimerID = 0;
          void runRendererSync();
        }, STREAMING_RENDER_DEBOUNCE_MS);
        return;
      }
      void runRendererSync();
    }

    onMounted(() => {
      requestRendererSync();
    });

    onBeforeUnmount(() => {
      disposed = true;
      clearScheduledRender();
      if (resizeFrameID) {
        window.cancelAnimationFrame(resizeFrameID);
        resizeFrameID = 0;
      }
      resizeObserver?.disconnect();
      resizeObserver = null;
      renderer.value?.destroy();
      renderer.value = null;
    });

    watch(
      () => [props.source, props.format, props.theme, props.streaming, props.streamMode, props.streamProfiler],
      () => {
        requestRendererSync();
      }
    );

    return {
      host,
    };
  },
  template: `<div ref="host" class="chat-markdown-content"></div>`,
};

export default MarkdownContent;
