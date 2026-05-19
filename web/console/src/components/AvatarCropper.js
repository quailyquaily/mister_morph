import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import "./AvatarCropper.css";

import {
  PERSONA_AVATAR_MAX_SOURCE_BYTES,
  PERSONA_AVATAR_SIZE,
  PERSONA_AVATAR_SOURCE_TYPES,
} from "../core/persona-profile";
import { translate } from "../core/context";

const AvatarCropper = {
  props: {
    previewUrl: {
      type: String,
      default: "",
    },
    defaultMarkup: {
      type: String,
      default: "",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    busy: {
      type: Boolean,
      default: false,
    },
  },
  emits: ["save", "delete"],
  setup(props, { emit }) {
    const t = translate;
    const fileInput = ref(null);
    const previewCanvas = ref(null);
    const draftURL = ref("");
    const draftReady = ref(false);
    const cropZoom = ref(1);
    const cropX = ref(0);
    const cropY = ref(0);
    const error = ref("");
    let sourceImage = null;

    function clearDraft() {
      if (draftURL.value) {
        URL.revokeObjectURL(draftURL.value);
      }
      draftURL.value = "";
      draftReady.value = false;
      cropZoom.value = 1;
      cropX.value = 0;
      cropY.value = 0;
      sourceImage = null;
      if (fileInput.value) {
        fileInput.value.value = "";
      }
    }

    function openFilePicker() {
      if (props.disabled || props.busy) {
        return;
      }
      error.value = "";
      fileInput.value?.click();
    }

    function validateFile(file) {
      if (!file) {
        return false;
      }
      if (!PERSONA_AVATAR_SOURCE_TYPES.has(file.type)) {
        error.value = t("persona_avatar_error_type");
        return false;
      }
      if (file.size > PERSONA_AVATAR_MAX_SOURCE_BYTES) {
        error.value = t("persona_avatar_error_size");
        return false;
      }
      return true;
    }

    function loadFile(event) {
      const file = event?.target?.files?.[0] || null;
      error.value = "";
      if (!validateFile(file)) {
        return;
      }
      clearDraft();
      const nextURL = URL.createObjectURL(file);
      const image = new Image();
      image.onload = async () => {
        sourceImage = image;
        draftURL.value = nextURL;
        draftReady.value = true;
        await nextTick();
        renderPreview();
      };
      image.onerror = () => {
        URL.revokeObjectURL(nextURL);
        error.value = t("persona_avatar_error_load");
      };
      image.src = nextURL;
    }

    function drawToCanvas(canvas, size) {
      if (!canvas || !sourceImage) {
        return false;
      }
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        return false;
      }
      canvas.width = size;
      canvas.height = size;
      ctx.clearRect(0, 0, size, size);
      const baseScale = Math.max(size / sourceImage.naturalWidth, size / sourceImage.naturalHeight);
      const scale = baseScale * Number(cropZoom.value || 1);
      const scaledWidth = sourceImage.naturalWidth * scale;
      const scaledHeight = sourceImage.naturalHeight * scale;
      const maxOffsetX = Math.max(0, (scaledWidth - size) / 2);
      const maxOffsetY = Math.max(0, (scaledHeight - size) / 2);
      const drawX = (size - scaledWidth) / 2 + (Number(cropX.value || 0) / 100) * maxOffsetX;
      const drawY = (size - scaledHeight) / 2 + (Number(cropY.value || 0) / 100) * maxOffsetY;
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(sourceImage, drawX, drawY, scaledWidth, scaledHeight);
      return true;
    }

    function renderPreview() {
      if (!draftReady.value) {
        return;
      }
      drawToCanvas(previewCanvas.value, 256);
    }

    async function saveDraft() {
      if (!draftReady.value || props.disabled || props.busy) {
        return;
      }
      const canvas = document.createElement("canvas");
      if (!drawToCanvas(canvas, PERSONA_AVATAR_SIZE)) {
        error.value = t("persona_avatar_error_crop");
        return;
      }
      const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/webp", 0.9));
      if (!blob) {
        error.value = t("persona_avatar_error_crop");
        return;
      }
      emit("save", blob);
    }

    function deleteAvatar() {
      if (props.disabled || props.busy) {
        return;
      }
      clearDraft();
      emit("delete");
    }

    watch([cropZoom, cropX, cropY], renderPreview);
    onBeforeUnmount(clearDraft);

    return {
      clearDraft,
      cropX,
      cropY,
      cropZoom,
      deleteAvatar,
      draftReady,
      error,
      fileInput,
      loadFile,
      openFilePicker,
      previewCanvas,
      saveDraft,
      t,
    };
  },
  template: `
    <div class="avatar-cropper">
      <div class="avatar-cropper-preview">
        <canvas v-show="draftReady" ref="previewCanvas" class="avatar-cropper-canvas"></canvas>
        <img v-if="!draftReady && previewUrl" class="avatar-cropper-image" :src="previewUrl" alt="" />
        <span
          v-else-if="!draftReady && defaultMarkup"
          class="avatar-cropper-logo"
          v-html="defaultMarkup"
          aria-hidden="true"
        ></span>
        <span v-else-if="!draftReady" class="avatar-cropper-placeholder" aria-hidden="true">
          <QIconEcosystem class="icon" />
        </span>
      </div>

      <input
        ref="fileInput"
        class="avatar-cropper-input"
        type="file"
        accept="image/png,image/jpeg,image/webp"
        :disabled="disabled || busy"
        @change="loadFile"
      />

      <div class="avatar-cropper-actions">
        <QButton
          type="button"
          class="outlined icon"
          :title="t('persona_avatar_action_upload')"
          :aria-label="t('persona_avatar_action_upload')"
          :disabled="disabled || busy"
          @click="openFilePicker"
        >
          <QIconPlus class="icon" />
        </QButton>
        <QButton
          type="button"
          class="primary icon"
          :title="t('action_save')"
          :aria-label="t('action_save')"
          :loading="busy"
          :disabled="disabled || busy || !draftReady"
          @click="saveDraft"
        >
          <QIconCheckCircle class="icon" />
        </QButton>
        <QButton
          type="button"
          class="outlined icon"
          :title="t('action_cancel')"
          :aria-label="t('action_cancel')"
          :disabled="disabled || busy || !draftReady"
          @click="clearDraft"
        >
          <QIconCloseCircle class="icon" />
        </QButton>
        <QButton
          type="button"
          class="outlined icon danger"
          :title="t('action_delete')"
          :aria-label="t('action_delete')"
          :disabled="disabled || busy || (!previewUrl && !draftReady)"
          @click="deleteAvatar"
        >
          <QIconTrash class="icon" />
        </QButton>
      </div>

      <div v-if="draftReady" class="avatar-cropper-controls">
        <label class="avatar-cropper-control">
          <span>{{ t("persona_avatar_zoom") }}</span>
          <input v-model.number="cropZoom" type="range" min="1" max="3" step="0.01" :disabled="disabled || busy" />
        </label>
        <label class="avatar-cropper-control">
          <span>{{ t("persona_avatar_x") }}</span>
          <input v-model.number="cropX" type="range" min="-100" max="100" step="1" :disabled="disabled || busy" />
        </label>
        <label class="avatar-cropper-control">
          <span>{{ t("persona_avatar_y") }}</span>
          <input v-model.number="cropY" type="range" min="-100" max="100" step="1" :disabled="disabled || busy" />
        </label>
      </div>

      <QFence v-if="error" type="danger" icon="QIconCloseCircle" :text="error" />
    </div>
  `,
};

export default AvatarCropper;
