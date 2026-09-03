import { computed, ref, watch } from "vue";
import { useToast } from "quail-ui";
import "./ImageUploadField.css";

import AppDialogShell from "./AppDialogShell";
import ImageAdjustDialog from "./ImageAdjustDialog";
import { translate } from "../core/context";

const DEFAULT_SOURCE_TYPES = ["image/png", "image/jpeg", "image/webp"];
const DEFAULT_MAX_BYTES = 10 * 1024 * 1024;

const ImageUploadField = {
  components: {
    AppDialogShell,
    ImageAdjustDialog,
  },
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
    dialogTitle: {
      type: String,
      default: "",
    },
    accept: {
      type: String,
      default: "image/png,image/jpeg,image/webp",
    },
    allowedTypes: {
      type: Array,
      default: () => DEFAULT_SOURCE_TYPES,
    },
    maxBytes: {
      type: Number,
      default: DEFAULT_MAX_BYTES,
    },
  },
  emits: ["save", "delete"],
  setup(props, { emit }) {
    const t = translate;
    const toast = useToast();
    const fileInput = ref(null);
    const selectedFile = ref(null);
    const adjustDialogOpen = ref(false);
    const previewDialogOpen = ref(false);
    const previewTitle = computed(() => String(props.dialogTitle || "").trim() || t("image_upload_preview_title"));

    function clearSelection() {
      selectedFile.value = null;
      if (fileInput.value) {
        fileInput.value.value = "";
      }
    }

    function openPreviewDialog() {
      if (props.disabled || props.busy) {
        return;
      }
      previewDialogOpen.value = true;
    }

    function openFilePicker() {
      if (props.disabled || props.busy) {
        return;
      }
      fileInput.value?.click();
    }

    function validateFile(file) {
      if (!file) {
        return false;
      }
      const allowed = Array.isArray(props.allowedTypes) ? props.allowedTypes : [];
      if (allowed.length > 0 && !allowed.includes(file.type)) {
        toast.error(t("image_upload_error_type"));
        return false;
      }
      if (Number(props.maxBytes) > 0 && file.size > Number(props.maxBytes)) {
        toast.error(t("image_upload_error_size"));
        return false;
      }
      return true;
    }

    function loadFile(event) {
      const file = event?.target?.files?.[0] || null;
      if (!validateFile(file)) {
        clearSelection();
        return;
      }
      selectedFile.value = file;
      previewDialogOpen.value = false;
      adjustDialogOpen.value = true;
      if (fileInput.value) {
        fileInput.value.value = "";
      }
    }

    function saveAdjustedImage(blob) {
      emit("save", blob);
      clearSelection();
    }

    function deleteImage() {
      if (props.disabled || props.busy) {
        return;
      }
      clearSelection();
      previewDialogOpen.value = false;
      emit("delete");
    }

    watch(adjustDialogOpen, (open) => {
      if (!open) {
        clearSelection();
      }
    });

    return {
      adjustDialogOpen,
      deleteImage,
      fileInput,
      loadFile,
      openFilePicker,
      openPreviewDialog,
      previewDialogOpen,
      previewTitle,
      saveAdjustedImage,
      selectedFile,
      t,
    };
  },
  template: `
    <div class="image-upload-field">
      <button
        type="button"
        class="image-upload-preview"
        :title="previewTitle"
        :aria-label="previewTitle"
        :disabled="disabled || busy"
        @click="openPreviewDialog"
      >
        <img v-if="previewUrl" class="image-upload-image" :src="previewUrl" alt="" />
        <span
          v-else-if="defaultMarkup"
          class="image-upload-logo"
          v-html="defaultMarkup"
          aria-hidden="true"
        ></span>
        <span v-else class="image-upload-placeholder" aria-hidden="true">
          <PhCube class="icon" />
        </span>
      </button>

      <input
        ref="fileInput"
        class="image-upload-input"
        type="file"
        :accept="accept"
        :disabled="disabled || busy"
        @change="loadFile"
      />

      <AppDialogShell
        v-model="previewDialogOpen"
        :title="previewTitle"
        width="420px"
        :closeDisabled="busy"
      >
        <div class="image-upload-preview-dialog">
          <div class="image-upload-preview-dialog-media">
            <img v-if="previewUrl" class="image-upload-preview-dialog-image" :src="previewUrl" alt="" />
            <span
              v-else-if="defaultMarkup"
              class="image-upload-preview-dialog-logo"
              v-html="defaultMarkup"
              aria-hidden="true"
            ></span>
            <span v-else class="image-upload-preview-dialog-placeholder" aria-hidden="true">
              <PhCube class="icon" />
            </span>
          </div>

          <div class="image-upload-preview-dialog-actions">
            <QButton
              type="button"
              class="outlined danger"
              :disabled="disabled || busy || !previewUrl"
              @click="deleteImage"
            >
              {{ t("action_delete") }}
            </QButton>
            <QButton
              type="button"
              class="outlined"
              :disabled="disabled || busy"
              @click="openFilePicker"
            >
              {{ t("image_upload_action_replace") }}
            </QButton>
          </div>
        </div>
      </AppDialogShell>

      <ImageAdjustDialog
        v-model="adjustDialogOpen"
        :file="selectedFile"
        :title="dialogTitle"
        :crop="crop"
        :outputSize="outputSize"
        :outputType="outputType"
        :outputQuality="outputQuality"
        :busy="busy"
        @save="saveAdjustedImage"
      />
    </div>
  `,
};

export default ImageUploadField;
