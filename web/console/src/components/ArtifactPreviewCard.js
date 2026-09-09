import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import {
  createArtifactPreviewTicket,
  renewArtifactPreviewTicket,
  runtimeApiDownloadForEndpoint,
  translate,
} from "../core/context";

const PREVIEW_RENEW_MARGIN_MS = 60 * 1000;
const PREVIEW_RENEW_FALLBACK_MS = 4 * 60 * 1000;
const IMAGE_PREVIEW_EXTENSIONS = new Set([".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico"]);

function cleanText(value) {
  return String(value || "").trim();
}

function normalizedTopicID(value) {
  const text = cleanText(value);
  if (!text || /^<[^>]+>$/u.test(text)) {
    return "";
  }
  return text;
}

function artifactFilename(path) {
  const name = cleanText(path).replace(/\\/gu, "/").split("/").filter(Boolean).pop();
  return name || "artifact";
}

function artifactPathExtension(path) {
  const filename = artifactFilename(path);
  const index = filename.lastIndexOf(".");
  return index >= 0 ? filename.slice(index).toLowerCase() : "";
}

function triggerBrowserDownload(blob, filename) {
  const objectURL = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = objectURL;
  link.download = cleanText(filename) || "artifact";
  link.rel = "noopener";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);
}

function previewRenewDelay(expiresAt) {
  const expiresAtMs = Date.parse(cleanText(expiresAt));
  if (!Number.isFinite(expiresAtMs)) {
    return PREVIEW_RENEW_FALLBACK_MS;
  }
  return Math.max(1000, expiresAtMs - Date.now() - PREVIEW_RENEW_MARGIN_MS);
}

const ArtifactPreviewCard = {
  props: {
    artifact: {
      type: Object,
      default: () => ({}),
    },
    endpointRef: {
      type: String,
      default: "",
    },
    fallbackTopicId: {
      type: String,
      default: "",
    },
    autoPreview: {
      type: Boolean,
      default: false,
    },
  },
  setup(props) {
    const t = translate;
    const expanded = ref(false);
    const loading = ref(false);
    const downloading = ref(false);
    const error = ref("");
    const entryURL = ref("");
    const previewTicket = ref("");
    const frameKey = ref(0);
    const autoStarted = ref(false);
    const previewFullscreen = ref(false);
    let renewTimer = 0;
    let renewing = false;

    const preview = computed(() => {
      const artifact = props.artifact || {};
      const dirName = cleanText(artifact.dir_name || artifact.dirName);
      const explicitTopicID = normalizedTopicID(artifact.topic_id || artifact.topicID);
      return {
        dirName,
        topicID: explicitTopicID || normalizedTopicID(props.fallbackTopicId),
        path: cleanText(artifact.path),
      };
    });

    const displayPath = computed(() => `${preview.value.dirName}/${preview.value.path}`);
    const isImagePreview = computed(() => IMAGE_PREVIEW_EXTENSIONS.has(artifactPathExtension(preview.value.path)));
    const artifactLabel = computed(() =>
      isImagePreview.value ? t("artifact_preview_type_image") : t("artifact_preview_type_web")
    );
    const previewCardClass = computed(() =>
      previewFullscreen.value ? "artifact-preview-card is-fullscreen" : "artifact-preview-card"
    );
    const fullscreenActionLabel = computed(() =>
      previewFullscreen.value
        ? t("artifact_preview_action_exit_fullscreen")
        : t("artifact_preview_action_fullscreen")
    );
    const canPreview = computed(() =>
      Boolean(
        cleanText(props.endpointRef) &&
          preview.value.dirName &&
          preview.value.path &&
          (preview.value.dirName !== "workspace_dir" || preview.value.topicID)
      )
    );

    function previewPayload() {
      return {
        endpoint_ref: cleanText(props.endpointRef),
        dir_name: preview.value.dirName,
        topic_id: preview.value.topicID,
        path: preview.value.path,
      };
    }

    function clearRenewTimer() {
      if (!renewTimer) {
        return;
      }
      window.clearTimeout(renewTimer);
      renewTimer = 0;
    }

    function scheduleRenew(expiresAt) {
      clearRenewTimer();
      if (!expanded.value || !previewTicket.value) {
        return;
      }
      renewTimer = window.setTimeout(() => {
        void renewPreviewTicket();
      }, previewRenewDelay(expiresAt));
    }

    function applyPreviewTicket(data, reloadFrame) {
      const ticket = cleanText(data?.ticket);
      const nextEntryURL = cleanText(data?.entry_url);
      if (!ticket || !nextEntryURL) {
        throw new Error(t("artifact_preview_error"));
      }
      previewTicket.value = ticket;
      entryURL.value = nextEntryURL;
      expanded.value = true;
      error.value = "";
      if (reloadFrame) {
        frameKey.value += 1;
      }
      scheduleRenew(data?.expires_at);
    }

    async function recreatePreviewAfterRenewFailure() {
      if (!canPreview.value || !expanded.value) {
        clearRenewTimer();
        return;
      }
      const data = await createArtifactPreviewTicket(previewPayload());
      applyPreviewTicket(data, true);
    }

    async function renewPreviewTicket() {
      if (!expanded.value || !previewTicket.value || renewing) {
        return;
      }
      renewing = true;
      try {
        const data = await renewArtifactPreviewTicket(previewTicket.value);
        const ticket = cleanText(data?.ticket) || previewTicket.value;
        previewTicket.value = ticket;
        scheduleRenew(data?.expires_at);
      } catch (_) {
        try {
          await recreatePreviewAfterRenewFailure();
        } catch (e) {
          error.value = e?.message || t("artifact_preview_error");
          clearRenewTimer();
        }
      } finally {
        renewing = false;
      }
    }

    async function loadPreview() {
      if (!canPreview.value || loading.value) {
        return;
      }
      loading.value = true;
      error.value = "";
      try {
        const data = await createArtifactPreviewTicket(previewPayload());
        applyPreviewTicket(data, true);
      } catch (e) {
        error.value = e?.message || t("artifact_preview_error");
      } finally {
        loading.value = false;
      }
    }

    async function togglePreview() {
      autoStarted.value = true;
      if (!expanded.value) {
        await loadPreview();
        return;
      }
      expanded.value = false;
      previewFullscreen.value = false;
      previewTicket.value = "";
      entryURL.value = "";
      error.value = "";
      frameKey.value += 1;
      clearRenewTimer();
    }

    function toggleFullscreenPreview() {
      if (!expanded.value || !entryURL.value || loading.value) {
        return;
      }
      previewFullscreen.value = !previewFullscreen.value;
    }

    function handleKeydown(event) {
      if (event.key === "Escape" && previewFullscreen.value) {
        event.preventDefault();
        previewFullscreen.value = false;
      }
    }

    async function downloadArtifact() {
      if (!canPreview.value || downloading.value) {
        return;
      }
      downloading.value = true;
      error.value = "";
      try {
        const query = new URLSearchParams();
        query.set("dir_name", preview.value.dirName);
        query.set("path", preview.value.path);
        if (preview.value.topicID) {
          query.set("topic_id", preview.value.topicID);
        }
        const blob = await runtimeApiDownloadForEndpoint(
          cleanText(props.endpointRef),
          `/files/download?${query.toString()}`
        );
        triggerBrowserDownload(blob, artifactFilename(preview.value.path));
      } catch (e) {
        error.value = e?.message || t("artifact_preview_error");
      } finally {
        downloading.value = false;
      }
    }

    watch(
      () => [props.autoPreview, canPreview.value, preview.value.path, preview.value.dirName, preview.value.topicID],
      ([autoPreview]) => {
        if (!autoPreview || !canPreview.value || autoStarted.value || expanded.value || loading.value) {
          return;
        }
        autoStarted.value = true;
        void loadPreview();
      },
      { immediate: true }
    );

    watch(previewFullscreen, (isFullscreen) => {
      if (typeof document === "undefined") {
        return;
      }
      document.body.classList.toggle("artifact-preview-scroll-lock", isFullscreen);
    });

    watch(
      () => [expanded.value, entryURL.value],
      ([isExpanded, url]) => {
        if (!isExpanded || !url) {
          previewFullscreen.value = false;
        }
      }
    );

    onMounted(() => {
      window.addEventListener("keydown", handleKeydown);
    });

    onBeforeUnmount(() => {
      clearRenewTimer();
      previewFullscreen.value = false;
      if (typeof document !== "undefined") {
        document.body.classList.remove("artifact-preview-scroll-lock");
      }
      window.removeEventListener("keydown", handleKeydown);
    });

    return {
      t,
      expanded,
      loading,
      downloading,
      error,
      entryURL,
      frameKey,
      previewFullscreen,
      previewCardClass,
      fullscreenActionLabel,
      displayPath,
      artifactLabel,
      isImagePreview,
      canPreview,
      togglePreview,
      downloadArtifact,
      toggleFullscreenPreview,
    };
  },
  template: `
    <Teleport to="body" :disabled="!previewFullscreen">
      <section :class="previewCardClass">
        <header v-if="!previewFullscreen" class="artifact-preview-head">
          <div class="artifact-preview-copy">
            <p class="artifact-preview-kicker">{{ artifactLabel }}</p>
          </div>
          <div class="artifact-preview-actions">
            <QButton
              class="plain xs icon"
              :title="expanded ? t('artifact_preview_action_collapse') : t('artifact_preview_action_expand')"
              :aria-label="expanded ? t('artifact_preview_action_collapse') : t('artifact_preview_action_expand')"
              :disabled="!canPreview"
              :loading="loading"
              @click="togglePreview"
            >
              <PhCaretUp v-if="expanded" class="icon" />
              <PhCaretDown v-else class="icon" />
            </QButton>
            <QButton
              class="plain xs icon"
              :title="t('chat_workspace_action_download')"
              :aria-label="t('chat_workspace_action_download')"
              :disabled="!canPreview"
              :loading="downloading"
              @click="downloadArtifact"
            >
              <PhCloudArrowDown class="icon" />
            </QButton>
            <QButton
              class="plain xs icon"
              :title="fullscreenActionLabel"
              :aria-label="fullscreenActionLabel"
              :disabled="!canPreview || !expanded || !entryURL"
              @click="toggleFullscreenPreview"
            >
              <PhXCircle v-if="previewFullscreen" class="icon" />
              <PhCornersOut v-else class="icon" />
            </QButton>
          </div>
        </header>

        <div v-if="previewFullscreen" class="artifact-preview-corner">
          <QButton
            class="outlined xs icon artifact-preview-exit"
            :title="fullscreenActionLabel"
            :aria-label="fullscreenActionLabel"
            @click="toggleFullscreenPreview"
          >
            <PhXCircle class="icon" />
          </QButton>
        </div>

        <QFence v-if="error" type="danger" icon="PhXCircle" :text="error" />

        <div v-if="expanded && entryURL" class="artifact-preview-frame-shell" :class="{ 'is-image': isImagePreview }">
          <img
            v-if="isImagePreview"
            :key="frameKey"
            class="artifact-preview-image"
            :src="entryURL"
            referrerpolicy="no-referrer"
            loading="lazy"
            :alt="displayPath"
          />
          <iframe
            v-else
            :key="frameKey"
            class="artifact-preview-frame"
            :src="entryURL"
            sandbox="allow-scripts allow-forms"
            referrerpolicy="no-referrer"
            loading="lazy"
            :title="displayPath"
          ></iframe>
        </div>
      </section>
    </Teleport>
  `,
};

export default ArtifactPreviewCard;
