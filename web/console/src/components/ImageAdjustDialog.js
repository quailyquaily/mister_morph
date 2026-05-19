import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useToast } from "quail-ui";
import "./ImageAdjustDialog.css";

import AppDialogShell from "./AppDialogShell";
import { translate } from "../core/context";

const PREVIEW_MAX_SIZE = 360;
const PREVIEW_MIN_SIZE = 180;
const MIN_ZOOM = 1;
const MAX_ZOOM = 4;
const ZOOM_STEP = 1.14;

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

function numericProp(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

const ImageAdjustDialog = {
  components: {
    AppDialogShell,
  },
  props: {
    modelValue: Boolean,
    file: {
      type: [File, Blob],
      default: null,
    },
    title: {
      type: String,
      default: "",
    },
    crop: {
      type: Boolean,
      default: false,
    },
    outputSize: {
      type: Number,
      default: 1024,
    },
    outputType: {
      type: String,
      default: "image/webp",
    },
    outputQuality: {
      type: Number,
      default: 0.9,
    },
    busy: {
      type: Boolean,
      default: false,
    },
  },
  emits: ["update:modelValue", "save"],
  setup(props, { emit }) {
    const t = translate;
    const toast = useToast();
    const canvas = ref(null);
    const loading = ref(false);
    const imageReady = ref(false);
    const zoom = ref(1);
    const offset = reactive({ x: 0, y: 0 });
    const viewport = reactive({ width: 320, height: 320 });
    const dragging = ref(false);
    let sourceImage = null;
    let sourceURL = "";
    let loadToken = 0;
    let dragStartPoint = null;
    let dragStartOffset = null;

    const resolvedTitle = computed(() => String(props.title || "").trim() || t("image_adjust_title"));
    const stageStyle = computed(() => ({
      width: `${viewport.width}px`,
      aspectRatio: `${viewport.width} / ${viewport.height}`,
    }));
    const zoomPercent = computed(() => `${Math.round(zoom.value * 100)}%`);

    function closeDialog() {
      if (props.busy) {
        return;
      }
      emit("update:modelValue", false);
    }

    function updateOpen(open) {
      if (!open) {
        closeDialog();
        return;
      }
      emit("update:modelValue", true);
    }

    function clearSource() {
      loadToken += 1;
      if (sourceURL) {
        URL.revokeObjectURL(sourceURL);
      }
      sourceURL = "";
      sourceImage = null;
      imageReady.value = false;
      loading.value = false;
      zoom.value = 1;
      offset.x = 0;
      offset.y = 0;
      dragging.value = false;
      dragStartPoint = null;
      dragStartOffset = null;
    }

    function updateViewport() {
      if (!sourceImage || props.crop) {
        viewport.width = 320;
        viewport.height = 320;
        return;
      }
      const ratio = sourceImage.naturalWidth / sourceImage.naturalHeight;
      if (!Number.isFinite(ratio) || ratio <= 0) {
        viewport.width = 320;
        viewport.height = 320;
        return;
      }
      if (ratio >= 1) {
        viewport.width = PREVIEW_MAX_SIZE;
        viewport.height = Math.max(PREVIEW_MIN_SIZE, Math.round(PREVIEW_MAX_SIZE / ratio));
      } else {
        viewport.height = PREVIEW_MAX_SIZE;
        viewport.width = Math.max(PREVIEW_MIN_SIZE, Math.round(PREVIEW_MAX_SIZE * ratio));
      }
    }

    function transformBounds(viewWidth = viewport.width, viewHeight = viewport.height) {
      if (!sourceImage) {
        return { x: 0, y: 0 };
      }
      const baseScale = props.crop
        ? Math.max(viewWidth / sourceImage.naturalWidth, viewHeight / sourceImage.naturalHeight)
        : Math.min(viewWidth / sourceImage.naturalWidth, viewHeight / sourceImage.naturalHeight);
      const scale = baseScale * zoom.value;
      const scaledWidth = sourceImage.naturalWidth * scale;
      const scaledHeight = sourceImage.naturalHeight * scale;
      return {
        x: Math.max(0, (scaledWidth - viewWidth) / 2),
        y: Math.max(0, (scaledHeight - viewHeight) / 2),
      };
    }

    function clampOffset() {
      const bounds = transformBounds();
      offset.x = clamp(offset.x, -bounds.x, bounds.x);
      offset.y = clamp(offset.y, -bounds.y, bounds.y);
    }

    function drawAdjustedImage(ctx, viewWidth, viewHeight, offsetScale = 1) {
      if (!sourceImage || !ctx) {
        return false;
      }
      const baseScale = props.crop
        ? Math.max(viewWidth / sourceImage.naturalWidth, viewHeight / sourceImage.naturalHeight)
        : Math.min(viewWidth / sourceImage.naturalWidth, viewHeight / sourceImage.naturalHeight);
      const scale = baseScale * zoom.value;
      const scaledWidth = sourceImage.naturalWidth * scale;
      const scaledHeight = sourceImage.naturalHeight * scale;
      const drawX = (viewWidth - scaledWidth) / 2 + offset.x * offsetScale;
      const drawY = (viewHeight - scaledHeight) / 2 + offset.y * offsetScale;
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = "high";
      ctx.clearRect(0, 0, viewWidth, viewHeight);
      ctx.drawImage(sourceImage, drawX, drawY, scaledWidth, scaledHeight);
      return true;
    }

    function renderPreview() {
      if (!imageReady.value || !canvas.value) {
        return;
      }
      clampOffset();
      const dpr = numericProp(typeof window !== "undefined" ? window.devicePixelRatio : 1, 1);
      const preview = canvas.value;
      preview.width = Math.round(viewport.width * dpr);
      preview.height = Math.round(viewport.height * dpr);
      const ctx = preview.getContext("2d");
      if (!ctx) {
        return;
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      drawAdjustedImage(ctx, viewport.width, viewport.height);
    }

    async function loadFile() {
      clearSource();
      if (!props.modelValue || !props.file) {
        return;
      }
      loading.value = true;
      const token = loadToken;
      sourceURL = URL.createObjectURL(props.file);
      const image = new Image();
      image.onload = async () => {
        if (token !== loadToken) {
          return;
        }
        sourceImage = image;
        zoom.value = 1;
        offset.x = 0;
        offset.y = 0;
        updateViewport();
        imageReady.value = true;
        loading.value = false;
        await nextTick();
        renderPreview();
      };
      image.onerror = () => {
        if (token !== loadToken) {
          return;
        }
        if (sourceURL) {
          URL.revokeObjectURL(sourceURL);
          sourceURL = "";
        }
        loading.value = false;
        toast.error(t("image_adjust_error_load"));
      };
      image.src = sourceURL;
    }

    function canvasPoint(event) {
      const target = canvas.value;
      if (!target) {
        return { x: 0, y: 0 };
      }
      const rect = target.getBoundingClientRect();
      const width = rect.width || viewport.width;
      const height = rect.height || viewport.height;
      return {
        x: (event.clientX - rect.left) * (viewport.width / width),
        y: (event.clientY - rect.top) * (viewport.height / height),
      };
    }

    function beginDrag(event) {
      if (!imageReady.value || props.busy) {
        return;
      }
      event.preventDefault();
      dragging.value = true;
      dragStartPoint = canvasPoint(event);
      dragStartOffset = { x: offset.x, y: offset.y };
      event.currentTarget?.setPointerCapture?.(event.pointerId);
    }

    function dragImage(event) {
      if (!dragging.value || !dragStartPoint || !dragStartOffset) {
        return;
      }
      event.preventDefault();
      const point = canvasPoint(event);
      offset.x = dragStartOffset.x + point.x - dragStartPoint.x;
      offset.y = dragStartOffset.y + point.y - dragStartPoint.y;
      renderPreview();
    }

    function finishDrag(event) {
      if (!dragging.value) {
        return;
      }
      event.currentTarget?.releasePointerCapture?.(event.pointerId);
      dragging.value = false;
      dragStartPoint = null;
      dragStartOffset = null;
      renderPreview();
    }

    function zoomIn() {
      if (!imageReady.value || props.busy) {
        return;
      }
      zoom.value = clamp(zoom.value * ZOOM_STEP, MIN_ZOOM, MAX_ZOOM);
      renderPreview();
    }

    function zoomOut() {
      if (!imageReady.value || props.busy) {
        return;
      }
      zoom.value = clamp(zoom.value / ZOOM_STEP, MIN_ZOOM, MAX_ZOOM);
      renderPreview();
    }

    function outputDimensions() {
      const size = numericProp(props.outputSize, 1024);
      if (props.crop || !sourceImage) {
        return { width: size, height: size };
      }
      const naturalMax = Math.max(sourceImage.naturalWidth, sourceImage.naturalHeight);
      const scale = naturalMax > size ? size / naturalMax : 1;
      return {
        width: Math.max(1, Math.round(sourceImage.naturalWidth * scale)),
        height: Math.max(1, Math.round(sourceImage.naturalHeight * scale)),
      };
    }

    async function saveImage() {
      if (!imageReady.value || props.busy) {
        return;
      }
      const output = outputDimensions();
      const target = document.createElement("canvas");
      target.width = output.width;
      target.height = output.height;
      const ctx = target.getContext("2d");
      if (!drawAdjustedImage(ctx, output.width, output.height, output.width / viewport.width)) {
        toast.error(t("image_adjust_error_process"));
        return;
      }
      const blob = await new Promise((resolve) =>
        target.toBlob(resolve, props.outputType, clamp(Number(props.outputQuality) || 0.9, 0.1, 1))
      );
      if (!blob) {
        toast.error(t("image_adjust_error_process"));
        return;
      }
      emit("save", blob);
      emit("update:modelValue", false);
    }

    watch(
      () => [props.modelValue, props.file, props.crop],
      () => {
        void loadFile();
      },
      { immediate: true }
    );
    watch(zoom, renderPreview);
    onBeforeUnmount(clearSource);

    return {
      beginDrag,
      canvas,
      closeDialog,
      dragImage,
      dragging,
      finishDrag,
      imageReady,
      loading,
      resolvedTitle,
      saveImage,
      stageStyle,
      t,
      updateOpen,
      zoomIn,
      zoomOut,
      zoomPercent,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="modelValue"
      :title="resolvedTitle"
      width="560px"
      :closeDisabled="busy"
      @update:modelValue="updateOpen"
    >
      <div class="image-adjust-dialog">
        <div
          class="image-adjust-stage"
          :class="{ 'is-dragging': dragging }"
          :style="stageStyle"
        >
          <canvas
            ref="canvas"
            class="image-adjust-canvas"
            :aria-label="t('image_adjust_canvas_label')"
            role="img"
            @pointerdown="beginDrag"
            @pointermove="dragImage"
            @pointerup="finishDrag"
            @pointercancel="finishDrag"
            @pointerleave="finishDrag"
          ></canvas>
          <QProgress v-if="loading" class="image-adjust-progress" :infinite="true" />
        </div>

        <div class="image-adjust-controls">
          <div class="image-adjust-zoom-actions">
            <QButton
              type="button"
              class="outlined icon"
              :title="t('image_adjust_zoom_out')"
              :aria-label="t('image_adjust_zoom_out')"
              :disabled="busy || !imageReady"
              @click="zoomOut"
            >
              <span class="image-adjust-zoom-mark" aria-hidden="true">-</span>
            </QButton>
            <span class="image-adjust-zoom-value">{{ zoomPercent }}</span>
            <QButton
              type="button"
              class="outlined icon"
              :title="t('image_adjust_zoom_in')"
              :aria-label="t('image_adjust_zoom_in')"
              :disabled="busy || !imageReady"
              @click="zoomIn"
            >
              <QIconPlus class="icon" />
            </QButton>
          </div>

          <div class="image-adjust-actions">
            <QButton type="button" class="outlined" :disabled="busy" @click="closeDialog">
              {{ t("action_cancel") }}
            </QButton>
            <QButton
              type="button"
              class="primary"
              :loading="busy"
              :disabled="busy || !imageReady"
              @click="saveImage"
            >
              {{ t("action_save") }}
            </QButton>
          </div>
        </div>
      </div>
    </AppDialogShell>
  `,
};

export default ImageAdjustDialog;
