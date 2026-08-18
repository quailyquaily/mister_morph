import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";

import defaultEndpointAvatarURL from "../assets/images/app_logo_current.svg";
import { approvalDetailsByID, taskApprovalState } from "../core/chat-approvals";
import {
  buildComposerSubmission,
  composerFileDraftKey,
  composerFileExtension,
} from "../core/chat-composer-files";
import { normalizeComposerCommandItems, normalizeComposerSkillItems } from "../core/chat-composer-suggestions";
import { chatDraft, clearChatDraft, rememberChatDraft } from "../core/chat-draft-memory";
import {
  lastUsedChatLLMProfile,
  normalizeChatLLMProfileMetadata,
  normalizeChatLLMProfiles,
  resolveAvailableChatLLMProfile,
} from "../core/chat-llm-profiles";
import { lastTopicID, rememberLastTopicID } from "../core/chat-topic-memory";
import {
  buildPollingHint,
  chatApprovalReasonText,
  historyPendingSeed,
  historyTimeLabel,
  isContextCompactCommand,
  isTerminalStatus,
  normalizeActivity,
  normalizeHistoryFileReferences,
  normalizePlan,
  normalizeReasoning,
  normalizeTaskStatus,
  normalizeTopicID,
  taskListHistoryItems,
} from "../core/chat-task-history";
import {
  buildConsoleStreamURL,
  createConsoleStreamTicket,
  currentLocale,
  runtimeApiDownloadForEndpoint,
  runtimeApiFetchForEndpoint,
  safeJSON,
  supportsConsoleTaskStream,
  translate,
} from "../core/context";
import { modelVendorMeta } from "../core/model-vendor";
import { loadResource, resourceKey } from "../core/resources";
import AppDialogShell from "./AppDialogShell";
import ChatComposer from "./ChatComposer";
import ChatHistoryList from "./ChatHistoryList";
import WorkspaceDirectoryPicker from "./WorkspaceDirectoryPicker";
import "../views/ChatView.css";
import "./AgentChatPane.css";

const POLL_INTERVAL_MS = 1200;
const HISTORY_LIMIT = 60;
const TOPIC_LIMIT = 60;
const AWARENESS_TOPIC_ID = "_awareness";
const COMPOSER_FILE_IMAGE_EXTENSIONS = new Set([
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".avif",
  ".bmp",
  ".ico",
]);

function cleanText(value) {
  return String(value || "").trim();
}

function topicUpdatedAt(topic) {
  const value = Date.parse(cleanText(topic?.updated_at || topic?.created_at));
  return Number.isFinite(value) ? value : 0;
}

function newHistoryID() {
  return `${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

const AgentChatPane = {
  name: "AgentChatPane",
  components: {
    AppDialogShell,
    ChatComposer,
    ChatHistoryList,
    WorkspaceDirectoryPicker,
  },
  props: {
    paneId: {
      type: String,
      required: true,
    },
    endpoint: {
      type: Object,
      required: true,
    },
    endpointOptions: {
      type: Array,
      default: () => [],
    },
    initialTopicId: {
      type: String,
      default: "",
    },
    canClose: {
      type: Boolean,
      default: true,
    },
  },
  emits: [
    "activate",
    "close",
    "endpoint-change",
    "split",
    "topic-change",
    "topic-missing",
  ],
  setup(props, { emit }) {
    const t = translate;
    const topics = ref([]);
    const topicsSupported = ref(true);
    const selectedTopicID = ref("");
    const creatingTopic = ref(false);
    const endpointDialogOpen = ref(false);
    const endpointFilter = ref("");
    const topicDialogOpen = ref(false);
    const topicFilter = ref("");
    const historyItems = ref([]);
    const historyLoading = ref(false);
    const taskInput = ref("");
    const sending = ref(false);
    const composerRef = ref(null);
    const composerUploading = ref(false);
    const composerFileInput = ref(null);
    const composerFiles = ref([]);
    const composerFilePreviewOpen = ref(false);
    const composerFilePreviewLoading = ref(false);
    const composerFilePreviewID = ref("");
    const composerFilePreviewName = ref("");
    const composerFilePreviewKind = ref("");
    const composerFilePreviewURL = ref("");
    const composerFilePreviewText = ref("");
    const composerFilePreviewError = ref("");
    const composerFilePreviewItems = ref([]);
    const composerFilePreviewIndex = ref(-1);
    const composerCommands = ref([]);
    const composerCommandsLoading = ref(false);
    const composerDefaultLLMProfile = ref(null);
    const composerLLMProfiles = ref([]);
    const composerTopicLLMProfile = ref("");
    const composerLLMProfile = ref("");
    const composerSkills = ref([]);
    const composerSkillsLoading = ref(false);
    const composerSkillsError = ref("");
    const pendingWorkspaceDir = ref("");
    const workspacePickerOpen = ref(false);
    const error = ref("");
    const historyViewport = ref(null);
    const copiedItemID = ref("");
    const expandedState = ref({});
    const approvalDetailAttempts = new Set();
    const pollTimers = new Map();
    const pollInFlight = new Set();
    const streamSockets = new Map();
    const composerFileDrafts = new Map();
    let alive = true;
    let loadVersion = 0;
    let copiedTimerID = 0;
    let historyAutoStick = true;
    let skipNextDraftPersist = false;
    let composerCommandsLoadSeq = 0;
    let composerLLMProfilesLoadSeq = 0;
    let composerSkillsLoadSeq = 0;
    let composerFileSequence = 0;
    let composerFilePreviewSequence = 0;
    let composerFilePreviewObjectURL = "";

    const endpointRef = computed(() => cleanText(props.endpoint?.endpoint_ref));
    let watchedEndpointRef = endpointRef.value;
    const submitEndpointRef = computed(() => {
      const mapped = cleanText(props.endpoint?.submit_endpoint_ref);
      if (mapped) {
        return mapped;
      }
      return props.endpoint?.can_submit === true ? endpointRef.value : "";
    });
    let watchedSubmitEndpointRef = submitEndpointRef.value;
    const available = computed(
      () => props.endpoint?.connected === true && submitEndpointRef.value !== ""
    );
    const agentName = computed(
      () =>
        cleanText(props.endpoint?.agent_name) ||
        cleanText(props.endpoint?.name) ||
        endpointRef.value ||
        t("chat_agent_name_fallback")
    );
    const avatarURL = computed(
      () => cleanText(props.endpoint?.avatar_url) || defaultEndpointAvatarURL
    );
    const filteredEndpointOptions = computed(() => {
      const query = cleanText(endpointFilter.value).toLowerCase();
      if (!query) {
        return props.endpointOptions;
      }
      return props.endpointOptions.filter((item) =>
        [item?.title, item?.value].some((value) =>
          cleanText(value).toLowerCase().includes(query)
        )
      );
    });
    const topicOptions = computed(() =>
      topics.value.map((topic) => ({
        id: normalizeTopicID(topic?.id),
        value: normalizeTopicID(topic?.id),
        title: topicTitle(topic),
      }))
    );
    const filteredTopicOptions = computed(() => {
      const query = cleanText(topicFilter.value).toLowerCase();
      if (!query) {
        return topicOptions.value;
      }
      return topicOptions.value.filter((item) =>
        cleanText(item?.title).toLowerCase().includes(query)
      );
    });
    const topicLabel = computed(() => {
      if (creatingTopic.value || !selectedTopicID.value) {
        return t("agent_desk_new_topic");
      }
      return (
        topicOptions.value.find((item) => item.value === selectedTopicID.value)?.title ||
        t("chat_topic_untitled")
      );
    });
    const activeTaskItem = computed(() => {
      for (let index = historyItems.value.length - 1; index >= 0; index -= 1) {
        const item = historyItems.value[index];
        if (
          cleanText(item?.role) === "agent" &&
          cleanText(item?.taskId) &&
          !isTerminalStatus(normalizeTaskStatus(item?.status))
        ) {
          return item;
        }
      }
      return null;
    });
    const composerStopMode = computed(
      () => Boolean(activeTaskItem.value) && cleanText(taskInput.value) === ""
    );
    const composerActionLabel = computed(() =>
      composerStopMode.value ? t("chat_action_stop") : `${t("chat_action_send")} (Enter)`
    );
    const sendDisabled = computed(
      () =>
        !available.value ||
        sending.value ||
        (!composerStopMode.value && (composerUploading.value || !cleanText(taskInput.value)))
    );
    const composerDraftScope = computed(() => ({
      endpointRef: submitEndpointRef.value,
      topicID: creatingTopic.value ? "" : normalizeTopicID(selectedTopicID.value),
    }));
    const composerAddDisabled = computed(
      () => !available.value || sending.value || composerUploading.value
    );
    const composerAttachActive = computed(() => Boolean(cleanText(pendingWorkspaceDir.value)));
    const composerDisclaimer = computed(
      () => `${agentName.value} can make mistakes. Check important info.`
    );
    const composerSuggestionLabels = computed(() => ({
      commands: t("chat_composer_suggestions_commands"),
      skills: t("chat_composer_suggestions_skills"),
      loading: t("chat_composer_suggestions_loading"),
      empty: t("chat_composer_suggestions_empty"),
    }));
    const composerFileLabels = computed(() => ({
      files: t("chat_composer_files"),
      preview: t("chat_composer_file_preview"),
      remove: t("chat_composer_file_remove"),
      uploading: t("chat_composer_file_uploading"),
      failed: t("chat_composer_upload_failed"),
    }));
    const composerLLMProfileItems = computed(() => {
      const defaultProfile = composerDefaultLLMProfile.value || {};
      const defaultVendor = modelVendorMeta(defaultProfile.modelName);
      return [
        {
          id: "chat-llm-profile-default-route",
          title: t("chat_llm_profile_default"),
          subtitle: defaultProfile.modelName,
          value: "",
          image: defaultVendor.icon || undefined,
          icon: defaultVendor.icon ? undefined : "QIconCpuChip",
        },
        ...composerLLMProfiles.value.map((profile) => {
          const vendor = modelVendorMeta(profile.modelName);
          return {
            id: `chat-llm-profile-${profile.name}`,
            title: profile.name,
            subtitle: profile.modelName,
            value: profile.name,
            image: vendor.icon || undefined,
            icon: vendor.icon ? undefined : "QIconCpuChip",
          };
        }),
      ];
    });
    const composerFilePreviewHasPrevious = computed(() => composerFilePreviewIndex.value > 0);
    const composerFilePreviewHasNext = computed(
      () =>
        composerFilePreviewIndex.value >= 0 &&
        composerFilePreviewIndex.value < composerFilePreviewItems.value.length - 1
    );
    const composerInputHistory = computed(() => {
      const values = [];
      for (let index = historyItems.value.length - 1; index >= 0; index -= 1) {
        const item = historyItems.value[index];
        if (cleanText(item?.role) === "user" && cleanText(item?.text)) {
          values.push(cleanText(item.text));
        }
      }
      return values;
    });

    function topicTitle(topic) {
      const title = cleanText(topic?.title);
      if (title) {
        return title;
      }
      return normalizeTopicID(topic?.id) === "default"
        ? t("chat_topic_default")
        : t("chat_topic_untitled");
    }

    function sortTopics(items) {
      return (Array.isArray(items) ? [...items] : [])
        .filter((topic) => {
          const id = normalizeTopicID(topic?.id);
          return id && id !== AWARENESS_TOPIC_ID;
        })
        .sort((left, right) => topicUpdatedAt(right) - topicUpdatedAt(left));
    }

    function draftTopicID() {
      return creatingTopic.value ? "" : normalizeTopicID(selectedTopicID.value);
    }

    function persistDraft() {
      rememberChatDraft(submitEndpointRef.value, draftTopicID(), taskInput.value);
    }

    function restoreDraft() {
      taskInput.value = chatDraft(submitEndpointRef.value, draftTopicID());
    }

    async function ensureComposerCommandsLoaded() {
      if (composerCommands.value.length > 0 || composerCommandsLoading.value) {
        return;
      }
      const targetEndpointRef = submitEndpointRef.value;
      if (!targetEndpointRef) {
        return;
      }
      const seq = composerCommandsLoadSeq + 1;
      composerCommandsLoadSeq = seq;
      composerCommandsLoading.value = true;
      try {
        const payload = await loadResource(
          resourceKey("chat", "composer-commands", targetEndpointRef),
          () => runtimeApiFetchForEndpoint(targetEndpointRef, "/commands")
        );
        if (!alive || seq !== composerCommandsLoadSeq || targetEndpointRef !== submitEndpointRef.value) {
          return;
        }
        const rawItems = Array.isArray(payload?.items)
          ? payload.items
          : Array.isArray(payload?.commands)
            ? payload.commands
            : [];
        composerCommands.value = normalizeComposerCommandItems(rawItems);
      } catch {
        if (alive && seq === composerCommandsLoadSeq) {
          composerCommands.value = [];
        }
      } finally {
        if (alive && seq === composerCommandsLoadSeq) {
          composerCommandsLoading.value = false;
        }
      }
    }

    async function ensureComposerSkillsLoaded() {
      if (composerSkills.value.length > 0 || composerSkillsLoading.value) {
        return;
      }
      const targetEndpointRef = submitEndpointRef.value;
      if (!targetEndpointRef) {
        return;
      }
      const seq = composerSkillsLoadSeq + 1;
      composerSkillsLoadSeq = seq;
      composerSkillsLoading.value = true;
      composerSkillsError.value = "";
      try {
        const payload = await loadResource(
          resourceKey("chat", "composer-skills", targetEndpointRef),
          () => runtimeApiFetchForEndpoint(targetEndpointRef, "/settings/agent")
        );
        if (!alive || seq !== composerSkillsLoadSeq || targetEndpointRef !== submitEndpointRef.value) {
          return;
        }
        const skills = payload?.skills && typeof payload.skills === "object" ? payload.skills : {};
        composerSkills.value = normalizeComposerSkillItems([
          ...(Array.isArray(skills.loaded) ? skills.loaded : []),
          ...(Array.isArray(skills.available) ? skills.available : []),
        ]);
      } catch (cause) {
        if (alive && seq === composerSkillsLoadSeq) {
          composerSkills.value = [];
          composerSkillsError.value =
            cause?.message || t("chat_composer_suggestions_load_error");
        }
      } finally {
        if (alive && seq === composerSkillsLoadSeq) {
          composerSkillsLoading.value = false;
        }
      }
    }

    function syncComposerLLMProfile() {
      composerLLMProfile.value = resolveAvailableChatLLMProfile(
        composerTopicLLMProfile.value,
        composerLLMProfiles.value
      );
    }

    function applyComposerTopicLLMProfile(value) {
      composerTopicLLMProfile.value = cleanText(value);
      syncComposerLLMProfile();
    }

    async function loadComposerLLMProfiles() {
      const targetEndpointRef = submitEndpointRef.value;
      const seq = composerLLMProfilesLoadSeq + 1;
      composerLLMProfilesLoadSeq = seq;
      if (!targetEndpointRef) {
        composerDefaultLLMProfile.value = null;
        composerLLMProfiles.value = [];
        applyComposerTopicLLMProfile("");
        return;
      }
      try {
        const payload = await loadResource(
          resourceKey("chat", "composer-llm-profiles", targetEndpointRef),
          () => runtimeApiFetchForEndpoint(targetEndpointRef, "/llm/profiles")
        );
        if (!alive || seq !== composerLLMProfilesLoadSeq || targetEndpointRef !== submitEndpointRef.value) {
          return;
        }
        composerDefaultLLMProfile.value = normalizeChatLLMProfileMetadata(payload?.default);
        composerLLMProfiles.value = normalizeChatLLMProfiles(payload?.items);
        syncComposerLLMProfile();
      } catch {
        if (alive && seq === composerLLMProfilesLoadSeq) {
          composerDefaultLLMProfile.value = null;
          composerLLMProfiles.value = [];
          syncComposerLLMProfile();
        }
      }
    }

    function setComposerFileDraft(scope, items) {
      const key = composerFileDraftKey(scope);
      if (!key) {
        composerFiles.value = [];
        return;
      }
      const nextItems = Array.isArray(items) ? [...items] : [];
      if (nextItems.length > 0) {
        composerFileDrafts.set(key, nextItems);
      } else {
        composerFileDrafts.delete(key);
      }
      if (key === composerFileDraftKey(composerDraftScope.value)) {
        composerFiles.value = nextItems;
      }
    }

    function updateComposerFileDraft(scope, update) {
      const key = composerFileDraftKey(scope);
      if (!key || typeof update !== "function") {
        return;
      }
      setComposerFileDraft(scope, update([...(composerFileDrafts.get(key) || [])]));
    }

    function restoreComposerFileDraft() {
      const key = composerFileDraftKey(composerDraftScope.value);
      composerFiles.value = key ? [...(composerFileDrafts.get(key) || [])] : [];
    }

    function clearComposerFileDraft(scope = composerDraftScope.value) {
      setComposerFileDraft(scope, []);
    }

    function openComposerFilePicker() {
      if (composerAddDisabled.value || !composerFileInput.value) {
        return;
      }
      composerFileInput.value.value = "";
      composerFileInput.value.click();
    }

    async function uploadComposerFiles(event) {
      const input = event?.target;
      const files = Array.from(input?.files || []);
      if (input) {
        input.value = "";
      }
      if (files.length === 0 || composerUploading.value || !submitEndpointRef.value) {
        return;
      }

      const uploadScope = { ...composerDraftScope.value };
      const uploadItems = files.map((file) => {
        composerFileSequence += 1;
        return {
          id: `composer-file-${Date.now()}-${composerFileSequence}`,
          name: cleanText(file?.name) || "file",
          status: "uploading",
          dirName: "",
          path: "",
          error: "",
          sourceFile: file,
          endpointRef: uploadScope.endpointRef,
          topicID: uploadScope.topicID,
        };
      });
      const uploadIDs = new Set(uploadItems.map((item) => item.id));
      updateComposerFileDraft(uploadScope, (current) => [...current, ...uploadItems]);

      const form = new FormData();
      for (const file of files) {
        form.append("files", file, file.name);
      }
      const pendingWorkspace = cleanText(pendingWorkspaceDir.value);
      if (pendingWorkspace) {
        form.append("workspace_dir", pendingWorkspace);
      } else if (uploadScope.topicID) {
        form.append("topic_id", uploadScope.topicID);
      }

      composerUploading.value = true;
      error.value = "";
      try {
        const payload = await runtimeApiFetchForEndpoint(uploadScope.endpointRef, "/files/upload", {
          method: "POST",
          body: form,
        });
        const uploadedFiles = Array.isArray(payload?.files) ? payload.files : [];
        if (uploadedFiles.length !== uploadItems.length) {
          throw new Error(t("chat_composer_upload_failed"));
        }
        updateComposerFileDraft(uploadScope, (current) =>
          current.map((item) => {
            if (!uploadIDs.has(item.id)) {
              return item;
            }
            const uploadIndex = uploadItems.findIndex((candidate) => candidate.id === item.id);
            const uploaded = uploadedFiles[uploadIndex] || {};
            const dirName = cleanText(uploaded?.dir_name);
            const path = cleanText(uploaded?.path);
            if (!path || (dirName !== "workspace_dir" && dirName !== "file_cache_dir")) {
              return {
                ...item,
                status: "failed",
                error: t("chat_composer_upload_failed"),
              };
            }
            return {
              ...item,
              name: cleanText(uploaded?.name) || item.name,
              status: "ready",
              dirName,
              path,
              error: "",
            };
          })
        );
      } catch (cause) {
        const message = cause?.message || t("chat_composer_upload_failed");
        updateComposerFileDraft(uploadScope, (current) =>
          current.map((item) =>
            uploadIDs.has(item.id)
              ? { ...item, status: "failed", error: message }
              : item
          )
        );
      } finally {
        composerUploading.value = false;
        if (composerFileDraftKey(uploadScope) === composerFileDraftKey(composerDraftScope.value)) {
          void nextTick(() => composerRef.value?.focus?.({ preserveSelection: true }));
        }
      }
    }

    function releaseComposerFilePreviewURL() {
      if (composerFilePreviewObjectURL) {
        URL.revokeObjectURL(composerFilePreviewObjectURL);
        composerFilePreviewObjectURL = "";
      }
      composerFilePreviewURL.value = "";
    }

    async function resolveFilePreviewSource(item) {
      if (item?.sourceFile && typeof item.sourceFile.arrayBuffer === "function") {
        return item.sourceFile;
      }
      const targetEndpointRef = cleanText(item?.endpointRef || submitEndpointRef.value);
      const dirName = cleanText(item?.dirName || item?.dir_name);
      const path = cleanText(item?.path);
      if (!targetEndpointRef || !path || !["workspace_dir", "file_cache_dir"].includes(dirName)) {
        throw new Error(t("chat_composer_file_preview_unavailable"));
      }
      const query = new URLSearchParams({ dir_name: dirName, path });
      if (dirName === "workspace_dir") {
        const topicID = normalizeTopicID(item?.topicID || item?.topic_id || selectedTopicID.value);
        if (!topicID) {
          throw new Error(t("chat_composer_file_preview_unavailable"));
        }
        query.set("topic_id", topicID);
      }
      return runtimeApiDownloadForEndpoint(
        targetEndpointRef,
        `/files/download?${query.toString()}`
      );
    }

    function closeComposerFilePreview() {
      composerFilePreviewSequence += 1;
      releaseComposerFilePreviewURL();
      composerFilePreviewOpen.value = false;
      composerFilePreviewLoading.value = false;
      composerFilePreviewID.value = "";
      composerFilePreviewName.value = "";
      composerFilePreviewKind.value = "";
      composerFilePreviewText.value = "";
      composerFilePreviewError.value = "";
      composerFilePreviewItems.value = [];
      composerFilePreviewIndex.value = -1;
    }

    async function loadComposerFilePreview(item) {
      if (cleanText(item?.status) !== "ready") {
        return;
      }
      releaseComposerFilePreviewURL();
      composerFilePreviewSequence += 1;
      const sequence = composerFilePreviewSequence;
      composerFilePreviewOpen.value = true;
      composerFilePreviewLoading.value = true;
      composerFilePreviewID.value = cleanText(item?.id);
      composerFilePreviewName.value = cleanText(item?.name);
      composerFilePreviewKind.value = "";
      composerFilePreviewText.value = "";
      composerFilePreviewError.value = "";
      const extension = composerFileExtension(item?.name);
      try {
        const sourceFile = await resolveFilePreviewSource(item);
        if (sequence !== composerFilePreviewSequence) {
          return;
        }
        const mimeType = cleanText(sourceFile?.type).toLowerCase();
        if (
          COMPOSER_FILE_IMAGE_EXTENSIONS.has(extension) ||
          (mimeType.startsWith("image/") && mimeType !== "image/svg+xml")
        ) {
          composerFilePreviewObjectURL = URL.createObjectURL(sourceFile);
          composerFilePreviewURL.value = composerFilePreviewObjectURL;
          composerFilePreviewKind.value = "image";
        } else if (extension === ".pdf" || mimeType === "application/pdf") {
          composerFilePreviewObjectURL = URL.createObjectURL(sourceFile);
          composerFilePreviewURL.value = composerFilePreviewObjectURL;
          composerFilePreviewKind.value = "pdf";
        } else {
          const buffer = await sourceFile.arrayBuffer();
          if (sequence !== composerFilePreviewSequence) {
            return;
          }
          composerFilePreviewText.value = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
          composerFilePreviewKind.value = "text";
        }
      } catch {
        if (sequence === composerFilePreviewSequence) {
          composerFilePreviewError.value = t("chat_composer_file_preview_unavailable");
        }
      } finally {
        if (sequence === composerFilePreviewSequence) {
          composerFilePreviewLoading.value = false;
        }
      }
    }

    function previewComposerFile(item) {
      const previewItems = (Array.isArray(item?.previewItems) ? item.previewItems : [item]).filter(
        (candidate) => cleanText(candidate?.status) === "ready"
      );
      if (previewItems.length === 0) {
        return;
      }
      const selectedID = cleanText(item?.id);
      composerFilePreviewItems.value = previewItems;
      composerFilePreviewIndex.value = Math.max(
        0,
        previewItems.findIndex((candidate) => cleanText(candidate?.id) === selectedID)
      );
      void loadComposerFilePreview(previewItems[composerFilePreviewIndex.value]);
    }

    function navigateComposerFilePreview(offset) {
      const nextIndex = composerFilePreviewIndex.value + Number(offset || 0);
      if (nextIndex < 0 || nextIndex >= composerFilePreviewItems.value.length) {
        return;
      }
      composerFilePreviewIndex.value = nextIndex;
      void loadComposerFilePreview(composerFilePreviewItems.value[nextIndex]);
    }

    function removeComposerFile(item) {
      const itemID = cleanText(item?.id);
      if (!itemID) {
        return;
      }
      updateComposerFileDraft(composerDraftScope.value, (current) =>
        current.filter((candidate) => candidate.id !== itemID)
      );
      if (composerFilePreviewID.value === itemID) {
        closeComposerFilePreview();
      }
    }

    function openComposerWorkspaceBrowser() {
      if (!composerAddDisabled.value) {
        workspacePickerOpen.value = true;
      }
    }

    function selectComposerWorkspace(path) {
      pendingWorkspaceDir.value = cleanText(path);
      void nextTick(() => composerRef.value?.focus?.({ preserveSelection: true }));
    }

    function patchHistoryItem(itemID, patch) {
      const index = historyItems.value.findIndex((item) => cleanText(item?.id) === cleanText(itemID));
      if (index < 0) {
        return;
      }
      const next = historyItems.value.slice();
      next[index] = { ...next[index], ...patch };
      historyItems.value = next;
    }

    function taskHistoryItem(taskID) {
      const key = cleanText(taskID);
      return (
        historyItems.value.find(
          (item) => cleanText(item?.role) === "agent" && cleanText(item?.taskId) === key
        ) || null
      );
    }

    function handleHistoryScroll() {
      const viewport = historyViewport.value;
      if (!viewport) {
        return;
      }
      const distance = viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop;
      historyAutoStick = distance <= 32;
    }

    async function scrollToBottom(force = false) {
      if (!force && !historyAutoStick) {
        return;
      }
      await nextTick();
      if (historyViewport.value) {
        historyViewport.value.scrollTop = historyViewport.value.scrollHeight;
        historyAutoStick = true;
      }
    }

    function clearPoll(taskID) {
      const key = cleanText(taskID);
      const timerID = pollTimers.get(key);
      if (timerID) {
        window.clearTimeout(timerID);
      }
      pollTimers.delete(key);
    }

    function closeStream(taskID) {
      const key = cleanText(taskID);
      const entry = streamSockets.get(key);
      if (!entry) {
        return;
      }
      try {
        entry.socket.close();
      } catch {
        // The authoritative task state still comes from polling.
      }
      streamSockets.delete(key);
    }

    function clearTracking() {
      for (const taskID of pollTimers.keys()) {
        clearPoll(taskID);
      }
      pollInFlight.clear();
      for (const taskID of streamSockets.keys()) {
        closeStream(taskID);
      }
    }

    function schedulePoll(taskID) {
      const key = cleanText(taskID);
      if (!key || !alive) {
        return;
      }
      clearPoll(key);
      const timerID = window.setTimeout(() => {
        pollTimers.delete(key);
        void pollTask(key);
      }, POLL_INTERVAL_MS);
      pollTimers.set(key, timerID);
    }

    function applyTaskDetail(detail) {
      const mapped = taskListHistoryItems([detail], t, {
        agentName: agentName.value,
        endpointRef: submitEndpointRef.value,
        locale: currentLocale(),
      });
      let next = historyItems.value.slice();
      for (const item of mapped) {
        const index = next.findIndex((existing) => cleanText(existing?.id) === cleanText(item?.id));
        if (index >= 0) {
          next[index] = { ...next[index], ...item };
        } else {
          next.push(item);
        }
      }
      historyItems.value = next;
    }

    async function pollTask(taskID) {
      const key = cleanText(taskID);
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      if (!key || !targetEndpointRef || !alive || pollInFlight.has(key)) {
        return;
      }
      pollInFlight.add(key);
      try {
        const detail = await runtimeApiFetchForEndpoint(
          targetEndpointRef,
          `/tasks/${encodeURIComponent(key)}`
        );
        if (
          !alive ||
          targetEndpointRef !== submitEndpointRef.value ||
          targetVersion !== loadVersion
        ) {
          return;
        }
        applyTaskDetail(detail);
        const status = normalizeTaskStatus(detail?.status);
        if (taskApprovalState(detail)) {
          void loadApprovalDetails();
        }
        if (isTerminalStatus(status)) {
          clearPoll(key);
          closeStream(key);
        } else {
          schedulePoll(key);
        }
        error.value = "";
        void scrollToBottom();
      } catch (cause) {
        if (alive && targetVersion === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
          schedulePoll(key);
        }
      } finally {
        pollInFlight.delete(key);
      }
    }

    async function startTaskStream(taskID) {
      const key = cleanText(taskID);
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      if (!key || !supportsConsoleTaskStream(targetEndpointRef) || streamSockets.has(key)) {
        return;
      }
      let ticketPayload;
      try {
        ticketPayload = await createConsoleStreamTicket();
      } catch {
        return;
      }
      if (
        !alive ||
        targetEndpointRef !== submitEndpointRef.value ||
        targetVersion !== loadVersion ||
        isTerminalStatus(normalizeTaskStatus(taskHistoryItem(key)?.status))
      ) {
        return;
      }
      const url = buildConsoleStreamURL(cleanText(ticketPayload?.ticket), key, targetEndpointRef);
      if (!url) {
        return;
      }
      const socket = new WebSocket(url);
      const entry = { socket };
      streamSockets.set(key, entry);
      socket.onmessage = (event) => {
        if (
          !alive ||
          targetEndpointRef !== submitEndpointRef.value ||
          targetVersion !== loadVersion ||
          streamSockets.get(key) !== entry
        ) {
          return;
        }
        const frame = safeJSON(event.data, null);
        const existing = taskHistoryItem(key);
        if (!frame || !existing) {
          return;
        }
        const patch = {};
        if (frame.plan && typeof frame.plan === "object") {
          patch.plan = normalizePlan(frame.plan || existing.plan);
        }
        if (frame.activity && typeof frame.activity === "object") {
          patch.activity = normalizeActivity(frame.activity || existing.activity);
        }
        if (typeof frame.reasoning === "string" && frame.reasoning.trim()) {
          patch.reasoning = normalizeReasoning(frame.reasoning);
        }
        if (frame.preview !== true && typeof frame.text === "string" && frame.text) {
          patch.text = frame.text;
        } else if (frame.preview !== true && typeof frame.error === "string" && frame.error) {
          patch.text = frame.error;
        }
        if (typeof frame.status === "string" && frame.status) {
          patch.status = normalizeTaskStatus(frame.status);
        }
        if (Object.keys(patch).length > 0) {
          patchHistoryItem(existing.id, patch);
          void scrollToBottom();
        }
        if (frame.done) {
          closeStream(key);
        }
      };
      socket.onclose = () => {
        if (streamSockets.get(key) === entry) {
          streamSockets.delete(key);
        }
      };
      socket.onerror = () => {
        // Polling remains active.
      };
    }

    function trackTask(taskID, immediate = false) {
      const key = cleanText(taskID);
      if (!key || isTerminalStatus(normalizeTaskStatus(taskHistoryItem(key)?.status))) {
        return;
      }
      void startTaskStream(key);
      if (immediate) {
        void pollTask(key);
      } else {
        schedulePoll(key);
      }
    }

    async function loadApprovalDetails() {
      const targetEndpointRef = submitEndpointRef.value;
      const targetVersion = loadVersion;
      const requestItems = historyItems.value
        .map((item) => ({
          requestID: cleanText(item?.approval?.approvalRequestID),
          status: cleanText(item?.approval?.status || "pending").toLowerCase(),
        }))
        .filter((item) => item.requestID);
      const pending = requestItems.filter(
        (item) => !approvalDetailAttempts.has(`${item.status}:${item.requestID}`)
      );
      if (pending.length === 0 || !targetEndpointRef) {
        return;
      }
      for (const item of pending) {
        approvalDetailAttempts.add(`${item.status}:${item.requestID}`);
      }
      const requests = [];
      if (pending.some((item) => item.status === "pending")) {
        requests.push(
          runtimeApiFetchForEndpoint(targetEndpointRef, "/approvals?status=pending&limit=200")
            .then((payload) => (Array.isArray(payload?.items) ? payload.items : []))
            .catch(() => [])
        );
      }
      for (const item of pending) {
        if (item.status !== "pending") {
          requests.push(
            runtimeApiFetchForEndpoint(
              targetEndpointRef,
              `/approvals/${encodeURIComponent(item.requestID)}`
            )
              .then((payload) => [payload])
              .catch(() => [])
          );
        }
      }
      const details = approvalDetailsByID({ items: (await Promise.all(requests)).flat() });
      if (
        !alive ||
        targetEndpointRef !== submitEndpointRef.value ||
        targetVersion !== loadVersion ||
        details.size === 0
      ) {
        return;
      }
      historyItems.value = historyItems.value.map((item) => {
        const requestID = cleanText(item?.approval?.approvalRequestID);
        const detail = details.get(requestID);
        if (!detail) {
          return item;
        }
        const currentStatus = cleanText(item?.approval?.status).toLowerCase();
        return {
          ...item,
          approval: {
            ...item.approval,
            ...detail,
            status:
              currentStatus === "denied" || currentStatus === "expired"
                ? currentStatus
                : detail.status || currentStatus || "pending",
            reasons: detail.reasons.map((reason) => chatApprovalReasonText(reason, t)),
          },
        };
      });
    }

    async function fetchHistory(version) {
      if (!submitEndpointRef.value || (topicsSupported.value && creatingTopic.value)) {
        historyItems.value = [];
        return;
      }
      let path = `/tasks?limit=${HISTORY_LIMIT}`;
      if (topicsSupported.value && selectedTopicID.value) {
        path += `&topic_id=${encodeURIComponent(selectedTopicID.value)}`;
      }
      const payload = await runtimeApiFetchForEndpoint(submitEndpointRef.value, path);
      if (!alive || version !== loadVersion) {
        return;
      }
      const tasks = Array.isArray(payload?.items) ? payload.items : [];
      applyComposerTopicLLMProfile(lastUsedChatLLMProfile(tasks));
      historyItems.value = taskListHistoryItems(
        tasks,
        t,
        {
          agentName: agentName.value,
          endpointRef: submitEndpointRef.value,
          locale: currentLocale(),
        }
      );
      approvalDetailAttempts.clear();
      void loadApprovalDetails();
      for (const item of historyItems.value) {
        if (
          cleanText(item?.role) === "agent" &&
          cleanText(item?.taskId) &&
          !isTerminalStatus(normalizeTaskStatus(item?.status))
        ) {
          trackTask(item.taskId);
        }
      }
      void scrollToBottom();
    }

    async function fetchTopics(preferredTopicID = "", removeIfMissing = false) {
      const payload = await runtimeApiFetchForEndpoint(
        submitEndpointRef.value,
        `/topics?limit=${TOPIC_LIMIT}`
      );
      topicsSupported.value = true;
      const preferred = normalizeTopicID(preferredTopicID);
      const remembered = normalizeTopicID(lastTopicID(submitEndpointRef.value));
      const candidates = [preferred, selectedTopicID.value, remembered]
        .map((value) => normalizeTopicID(value))
        .filter(Boolean);
      const items = Array.isArray(payload?.items) ? [...payload.items] : [];
      const targetTopicID = candidates[0] || "";
      if (
        targetTopicID &&
        !items.some((topic) => normalizeTopicID(topic?.id) === targetTopicID)
      ) {
        try {
          const target = await runtimeApiFetchForEndpoint(
            submitEndpointRef.value,
            `/topics/${encodeURIComponent(targetTopicID)}`
          );
          if (normalizeTopicID(target?.id) === targetTopicID) {
            items.push(target);
          }
        } catch (cause) {
          if (cause?.status !== 404) {
            throw cause;
          }
          if (removeIfMissing && preferred && targetTopicID === preferred) {
            return { missingTopicID: preferred };
          }
        }
      }
      topics.value = sortTopics(items);
      const nextTopicID = candidates.find((value) =>
        topics.value.some((topic) => normalizeTopicID(topic?.id) === value)
      );
      selectedTopicID.value = nextTopicID || normalizeTopicID(topics.value[0]?.id);
      creatingTopic.value = selectedTopicID.value === "";
      if (selectedTopicID.value) {
        rememberLastTopicID(submitEndpointRef.value, selectedTopicID.value);
      }
      return { missingTopicID: "" };
    }

    async function loadPane(preferredTopicID = "", removeIfMissing = false) {
      const version = loadVersion + 1;
      loadVersion = version;
      historyAutoStick = true;
      clearTracking();
      historyLoading.value = true;
      error.value = "";
      if (!available.value) {
        historyItems.value = [];
        historyLoading.value = false;
        return;
      }
      try {
        try {
          const topicResult = await fetchTopics(preferredTopicID, removeIfMissing);
          if (!alive || version !== loadVersion) {
            return;
          }
          if (topicResult?.missingTopicID) {
            emit("topic-missing", {
              paneID: props.paneId,
              topicID: topicResult.missingTopicID,
            });
            return;
          }
        } catch (cause) {
          if (cause?.status !== 404) {
            throw cause;
          }
          topicsSupported.value = false;
          topics.value = [];
          selectedTopicID.value = "";
          creatingTopic.value = false;
        }
        if (!alive || version !== loadVersion) {
          return;
        }
        emit("topic-change", {
          paneID: props.paneId,
          topicID: normalizeTopicID(selectedTopicID.value),
        });
        restoreDraft();
        restoreComposerFileDraft();
        await fetchHistory(version);
      } catch (cause) {
        if (alive && version === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
          historyItems.value = [];
        }
      } finally {
        if (alive && version === loadVersion) {
          historyLoading.value = false;
        }
      }
    }

    async function selectTopic(item) {
      topicDialogOpen.value = false;
      const topicID = normalizeTopicID(item?.value);
      if (!topicID || topicID === selectedTopicID.value) {
        return;
      }
      persistDraft();
      selectedTopicID.value = topicID;
      creatingTopic.value = false;
      rememberLastTopicID(submitEndpointRef.value, topicID);
      emit("topic-change", { paneID: props.paneId, topicID });
      restoreDraft();
      restoreComposerFileDraft();
      pendingWorkspaceDir.value = "";
      const version = loadVersion + 1;
      loadVersion = version;
      clearTracking();
      historyAutoStick = true;
      historyLoading.value = true;
      error.value = "";
      try {
        await fetchHistory(version);
      } catch (cause) {
        if (alive && version === loadVersion) {
          error.value = cause?.message || t("msg_load_failed");
        }
      } finally {
        if (alive && version === loadVersion) {
          historyLoading.value = false;
        }
      }
    }

    function startNewTopic() {
      topicDialogOpen.value = false;
      persistDraft();
      loadVersion += 1;
      clearTracking();
      historyAutoStick = true;
      selectedTopicID.value = "";
      creatingTopic.value = true;
      emit("topic-change", { paneID: props.paneId, topicID: "" });
      historyItems.value = [];
      error.value = "";
      restoreDraft();
      restoreComposerFileDraft();
      pendingWorkspaceDir.value = "";
      applyComposerTopicLLMProfile("");
      void nextTick(() => emit("activate", props.paneId));
    }

    function openTopicDialog() {
      if (!available.value || historyLoading.value) {
        return;
      }
      topicFilter.value = "";
      topicDialogOpen.value = true;
    }

    async function submitTask() {
      const task = cleanText(taskInput.value);
      if (!task || sending.value || composerUploading.value || !available.value) {
        return;
      }
      const submittedDraftScope = { ...composerDraftScope.value };
      const submittedTopicID = draftTopicID();
      const llmProfile = cleanText(composerLLMProfile.value);
      const pendingWorkspace = cleanText(pendingWorkspaceDir.value);
      const { requestBody, submittedFiles, fileReferences } = buildComposerSubmission({
        task,
        llmProfile,
        files: composerFiles.value,
        topicID: topicsSupported.value ? submittedTopicID : "",
        workspaceDir: topicsSupported.value ? pendingWorkspace : "",
      });
      const provisionalUserID = `provisional:${newHistoryID()}:user`;
      const provisionalAgentID = `provisional:${newHistoryID()}:agent`;
      historyItems.value = [
        ...historyItems.value,
        {
          id: provisionalUserID,
          role: "user",
          text: task,
          files: submittedFiles,
          endpointRef: submitEndpointRef.value,
          topicID: submittedTopicID,
          timeText: historyTimeLabel(new Date().toISOString(), currentLocale()),
        },
        {
          id: provisionalAgentID,
          role: "agent",
          text: buildPollingHint(agentName.value, t, provisionalAgentID),
          status: "queued",
          taskId: "",
          pendingSeed: provisionalAgentID,
          presentation: isContextCompactCommand(task) ? "context-compact" : "",
        },
      ];
      sending.value = true;
      error.value = "";
      historyAutoStick = true;
      taskInput.value = "";
      clearChatDraft(submitEndpointRef.value, submittedTopicID);
      void scrollToBottom();
      try {
        const submitted = await runtimeApiFetchForEndpoint(submitEndpointRef.value, "/tasks", {
          method: "POST",
          body: requestBody,
        });
        const taskID = cleanText(submitted?.id);
        const trackedTaskID = cleanText(submitted?.steer_target_task_id) || taskID;
        if (!taskID || !trackedTaskID) {
          throw new Error(t("chat_missing_task_id"));
        }
        if (!cleanText(submitted?.steer_target_task_id)) {
          applyComposerTopicLLMProfile(llmProfile);
        }
        const createdTopicID = normalizeTopicID(submitted?.topic_id || submittedTopicID);
        if (topicsSupported.value && !createdTopicID) {
          throw new Error(t("chat_missing_topic_id"));
        }
        if (createdTopicID) {
          selectedTopicID.value = createdTopicID;
          creatingTopic.value = false;
          rememberLastTopicID(submitEndpointRef.value, createdTopicID);
          emit("topic-change", { paneID: props.paneId, topicID: createdTopicID });
        }
        patchHistoryItem(provisionalUserID, {
          taskId: taskID,
          files: normalizeHistoryFileReferences(fileReferences),
          endpointRef: submitEndpointRef.value,
          topicID: createdTopicID,
        });
        clearComposerFileDraft(submittedDraftScope);
        pendingWorkspaceDir.value = "";
        await loadPane(createdTopicID);
        trackTask(trackedTaskID, true);
      } catch (cause) {
        const message = cause?.message || t("msg_load_failed");
        error.value = message;
        taskInput.value = task;
        rememberChatDraft(submitEndpointRef.value, submittedTopicID, task);
        patchHistoryItem(provisionalAgentID, {
          status: "failed",
          text: message,
        });
      } finally {
        sending.value = false;
        void nextTick(() => composerRef.value?.focus?.());
      }
    }

    async function stopActiveTask() {
      const taskID = cleanText(activeTaskItem.value?.taskId);
      if (!taskID || sending.value || !submitEndpointRef.value) {
        return;
      }
      sending.value = true;
      error.value = "";
      try {
        await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/tasks/${encodeURIComponent(taskID)}/stop`,
          { method: "POST" }
        );
        await pollTask(taskID);
      } catch (cause) {
        error.value = cause?.message || t("msg_load_failed");
      } finally {
        sending.value = false;
      }
    }

    async function decideApproval(item, decision) {
      const requestID = cleanText(item?.approval?.approvalRequestID);
      const taskID = cleanText(item?.taskId);
      const action = cleanText(decision).toLowerCase();
      if (!requestID || !taskID || (action !== "approve" && action !== "deny")) {
        return;
      }
      patchHistoryItem(item.id, { approvalBusy: true, approvalError: "" });
      try {
        const result = await runtimeApiFetchForEndpoint(
          submitEndpointRef.value,
          `/approvals/${encodeURIComponent(requestID)}/${action}`,
          { method: "POST", body: { actor: "console:user" } }
        );
        const decisionError = cleanText(result?.error);
        if (action === "approve" && result?.resumed === false && decisionError) {
          patchHistoryItem(item.id, {
            approval: null,
            approvalBusy: false,
            status: "failed",
            text: decisionError,
          });
          return;
        }
        if (action === "deny") {
          patchHistoryItem(item.id, {
            approval: { ...item.approval, status: "denied" },
            approvalBusy: false,
            status: "canceled",
            text: t("chat_approval_denied"),
          });
        } else {
          patchHistoryItem(item.id, {
            approval: null,
            approvalBusy: false,
            status: "queued",
            text: buildPollingHint(agentName.value, t, historyPendingSeed(item, taskID)),
          });
          trackTask(taskID, true);
        }
        await pollTask(taskID);
      } catch (cause) {
        patchHistoryItem(item.id, {
          approvalBusy: false,
          approvalError: cause?.message || t("msg_load_failed"),
        });
      }
    }

    async function copyHistoryItem(item) {
      const text = String(item?.text || "");
      if (
        !text ||
        typeof navigator === "undefined" ||
        typeof navigator.clipboard?.writeText !== "function"
      ) {
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        copiedItemID.value = cleanText(item?.id);
        if (copiedTimerID) {
          window.clearTimeout(copiedTimerID);
        }
        copiedTimerID = window.setTimeout(() => {
          copiedItemID.value = "";
          copiedTimerID = 0;
        }, 1200);
      } catch {
        // Clipboard availability depends on the browser security context.
      }
    }

    function toggleStatus(itemID, panel) {
      const key = cleanText(itemID);
      const value = cleanText(panel);
      if (!key || !["plan", "activity", "reasoning"].includes(value)) {
        return;
      }
      const next = { ...expandedState.value };
      if (next[key] === value) {
        delete next[key];
      } else {
        next[key] = value;
      }
      expandedState.value = next;
    }

    function toggleDuration(item) {
      if (!cleanText(item?.durationText)) {
        return;
      }
      patchHistoryItem(item.id, {
        durationVisible: item?.durationVisible !== true,
        durationVisibleManual: true,
      });
    }

    function selectEndpoint(item) {
      if (!cleanText(item?.value) || item?.disabled === true) {
        return;
      }
      endpointDialogOpen.value = false;
      emit("endpoint-change", { paneID: props.paneId, item });
    }

    function openEndpointDialog() {
      if (sending.value) {
        return;
      }
      endpointFilter.value = "";
      endpointDialogOpen.value = true;
    }

    function splitPane(direction) {
      emit("split", { paneID: props.paneId, direction });
    }

    watch(taskInput, () => {
      if (skipNextDraftPersist) {
        skipNextDraftPersist = false;
        return;
      }
      persistDraft();
    });

    watch(
      () =>
        `${endpointRef.value}\u0000${submitEndpointRef.value}\u0000${
          props.endpoint?.connected === true ? "1" : "0"
        }`,
      () => {
        const endpointChanged = watchedEndpointRef !== endpointRef.value;
        const composerEndpointChanged = watchedSubmitEndpointRef !== submitEndpointRef.value;
        watchedEndpointRef = endpointRef.value;
        watchedSubmitEndpointRef = submitEndpointRef.value;
        if (endpointChanged) {
          endpointDialogOpen.value = false;
          topicDialogOpen.value = false;
          topicFilter.value = "";
          topics.value = [];
          topicsSupported.value = true;
          selectedTopicID.value = "";
          creatingTopic.value = false;
          historyItems.value = [];
          if (taskInput.value !== "") {
            skipNextDraftPersist = true;
            taskInput.value = "";
          }
          error.value = "";
          copiedItemID.value = "";
          expandedState.value = {};
          approvalDetailAttempts.clear();
        }
        if (composerEndpointChanged) {
          composerCommandsLoadSeq += 1;
          composerCommands.value = [];
          composerCommandsLoading.value = false;
          composerLLMProfilesLoadSeq += 1;
          composerDefaultLLMProfile.value = null;
          composerLLMProfiles.value = [];
          applyComposerTopicLLMProfile("");
          composerSkillsLoadSeq += 1;
          composerSkills.value = [];
          composerSkillsLoading.value = false;
          composerSkillsError.value = "";
          composerFiles.value = [];
          pendingWorkspaceDir.value = "";
          workspacePickerOpen.value = false;
          closeComposerFilePreview();
        }
        const preferredTopicID = endpointChanged ? "" : selectedTopicID.value;
        const removeIfMissing =
          Boolean(preferredTopicID) &&
          preferredTopicID === normalizeTopicID(props.initialTopicId);
        void loadPane(preferredTopicID, removeIfMissing);
        if (composerEndpointChanged) {
          void loadComposerLLMProfiles();
        }
      }
    );

    onMounted(() => {
      const initialTopicID = normalizeTopicID(props.initialTopicId);
      selectedTopicID.value = initialTopicID;
      void loadPane(initialTopicID, Boolean(initialTopicID));
      void loadComposerLLMProfiles();
    });

    onUnmounted(() => {
      alive = false;
      loadVersion += 1;
      persistDraft();
      clearTracking();
      closeComposerFilePreview();
      if (copiedTimerID) {
        window.clearTimeout(copiedTimerID);
      }
    });

    return {
      t,
      agentName,
      available,
      avatarURL,
      closeComposerFilePreview,
      composerActionLabel,
      composerAddDisabled,
      composerAttachActive,
      composerCommands,
      composerDisclaimer,
      composerFileInput,
      composerFileLabels,
      composerFilePreviewError,
      composerFilePreviewHasNext,
      composerFilePreviewHasPrevious,
      composerFilePreviewItems,
      composerFilePreviewKind,
      composerFilePreviewLoading,
      composerFilePreviewName,
      composerFilePreviewOpen,
      composerFilePreviewText,
      composerFilePreviewURL,
      composerFiles,
      composerInputHistory,
      composerLLMProfile,
      composerLLMProfileItems,
      composerRef,
      composerSkills,
      composerSkillsError,
      composerSkillsLoading,
      composerStopMode,
      composerSuggestionLabels,
      composerUploading,
      copiedItemID,
      creatingTopic,
      decideApproval,
      endpointDialogOpen,
      endpointFilter,
      error,
      expandedState,
      filteredEndpointOptions,
      filteredTopicOptions,
      historyItems,
      historyLoading,
      historyViewport,
      handleHistoryScroll,
      ensureComposerCommandsLoaded,
      ensureComposerSkillsLoaded,
      navigateComposerFilePreview,
      openEndpointDialog,
      openComposerFilePicker,
      openComposerWorkspaceBrowser,
      openTopicDialog,
      previewComposerFile,
      copyHistoryItem,
      removeComposerFile,
      selectedTopicID,
      selectEndpoint,
      selectComposerWorkspace,
      selectTopic,
      sendDisabled,
      sending,
      scrollToBottom,
      startNewTopic,
      stopActiveTask,
      splitPane,
      submitEndpointRef,
      submitTask,
      taskInput,
      toggleDuration,
      toggleStatus,
      topicDialogOpen,
      topicFilter,
      topicLabel,
      topicOptions,
      topicsSupported,
      uploadComposerFiles,
      workspacePickerOpen,
      pendingWorkspaceDir,
    };
  },
  template: `
    <article
      class="agent-chat-pane"
      :class="{ 'is-unavailable': !available }"
      :data-pane-id="paneId"
      :data-endpoint-ref="endpoint.endpoint_ref"
      :aria-label="agentName"
      tabindex="-1"
      @pointerdown="$emit('activate', paneId)"
    >
      <header class="agent-chat-pane-head">
        <div class="agent-chat-pane-identity">
          <button
            type="button"
            class="agent-chat-pane-endpoint-trigger"
            :disabled="sending"
            :title="endpoint.endpoint_ref"
            :aria-label="t('endpoint_switcher_label')"
            aria-haspopup="dialog"
            :aria-expanded="endpointDialogOpen ? 'true' : 'false'"
            @click.stop="openEndpointDialog"
          >
            <img
              class="agent-chat-pane-endpoint-avatar"
              :src="avatarURL"
              :alt="agentName"
            />
          </button>
          <span
            class="agent-chat-pane-status"
            :class="available ? 'is-online' : 'is-offline'"
            aria-hidden="true"
          ></span>
        </div>

        <div class="agent-chat-pane-topicbar">
          <button
            v-if="topicsSupported"
            type="button"
            class="agent-chat-pane-topic-trigger"
            :disabled="!available || historyLoading"
            :title="topicLabel"
            aria-haspopup="dialog"
            :aria-expanded="topicDialogOpen ? 'true' : 'false'"
            @click.stop="openTopicDialog"
          >
            <span class="agent-chat-pane-topic-label">{{ topicLabel }}</span>
            <QIconChevronDown class="icon agent-chat-pane-topic-chevron" aria-hidden="true" />
          </button>
          <span v-else class="agent-chat-pane-topic-label">{{ topicLabel }}</span>
        </div>

        <div class="agent-chat-pane-actions">
          <QButton
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_split_right')"
            :aria-label="t('agent_desk_split_right')"
            @click.stop="splitPane('row')"
          >
            <QIconLayoutRight class="icon" />
          </QButton>
          <QButton
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_split_down')"
            :aria-label="t('agent_desk_split_down')"
            @click.stop="splitPane('column')"
          >
            <QIconLayoutRight class="icon agent-chat-pane-split-down-icon" />
          </QButton>
          <QButton
            v-if="canClose"
            class="plain xs icon agent-chat-pane-action"
            :title="t('agent_desk_close_pane')"
            :aria-label="t('agent_desk_close_pane')"
            @click.stop="$emit('close', paneId)"
          >
            <QIconCloseCircle class="icon" />
          </QButton>
        </div>
      </header>

      <input
        ref="composerFileInput"
        type="file"
        multiple
        hidden
        @change="uploadComposerFiles"
      />

      <section v-if="!available" class="agent-chat-pane-unavailable">
        <span class="agent-chat-pane-unavailable-mark" aria-hidden="true"></span>
        <h3>{{ t('agent_desk_endpoint_unavailable') }}</h3>
        <p>{{ t('agent_desk_endpoint_unavailable_hint') }}</p>
      </section>
      <div
        v-else
        ref="historyViewport"
        class="agent-chat-pane-history chat-history"
        @scroll="handleHistoryScroll"
      >
        <div v-if="historyLoading" class="agent-chat-pane-skeleton" aria-hidden="true">
          <QSkeleton width="42%" height="16px" />
          <QSkeleton width="88%" height="72px" />
          <QSkeleton width="56%" height="16px" />
          <QSkeleton width="76%" height="92px" />
        </div>
        <ChatHistoryList
          v-else
          :items="historyItems"
          :loading="false"
          :emptyText="creatingTopic ? t('chat_new_topic_intro') : t('chat_topic_empty')"
          :submitEndpointRef="submitEndpointRef"
          :selectedTopicId="selectedTopicID"
          :copiedItemId="copiedItemID"
          :expandedState="expandedState"
          :copyLabel="t('action_copy')"
          :filePreviewLabel="t('chat_composer_file_preview')"
          :approvalApproveLabel="t('chat_approval_approve')"
          :approvalDenyLabel="t('chat_approval_deny')"
          :approvalTitle="t('chat_approval_title')"
          @copy="copyHistoryItem"
          @rendered="scrollToBottom()"
          @preview-file="previewComposerFile"
          @toggle-status="toggleStatus"
          @time-click="toggleDuration"
          @approval-approve="decideApproval($event, 'approve')"
          @approval-deny="decideApproval($event, 'deny')"
        />
      </div>

      <p v-if="error && available" class="agent-chat-pane-error" role="alert">{{ error }}</p>
      <ChatComposer
        v-if="available"
        ref="composerRef"
        class="agent-chat-pane-composer"
        :modelValue="taskInput"
        :disabled="sending"
        :sendDisabled="sendDisabled"
        :sending="sending"
        :stopMode="composerStopMode"
        :placeholder="t('chat_input_placeholder', { name: agentName })"
        :sendLabel="composerActionLabel"
        :inputHistory="composerInputHistory"
        :attach-active="composerAttachActive"
        :attach-disabled="composerAddDisabled"
        :add-label="t('chat_composer_add')"
        :attach-label="t('chat_composer_add_workspace')"
        :upload-label="t('chat_composer_upload_files')"
        :uploading="composerUploading"
        :file-items="composerFiles"
        :file-labels="composerFileLabels"
        :disclaimer="composerDisclaimer"
        :commands="composerCommands"
        v-model:llm-profile-value="composerLLMProfile"
        :llm-profile-items="composerLLMProfileItems"
        :llm-profile-label="t('chat_llm_profile_label')"
        :skills="composerSkills"
        :skills-loading="composerSkillsLoading"
        :skills-error="composerSkillsError"
        :suggestion-labels="composerSuggestionLabels"
        @update:modelValue="taskInput = $event"
        @attach="openComposerWorkspaceBrowser"
        @upload="openComposerFilePicker"
        @preview-file="previewComposerFile"
        @remove-file="removeComposerFile"
        @submit="submitTask"
        @stop="stopActiveTask"
        @request-commands="ensureComposerCommandsLoaded"
        @request-skills="ensureComposerSkillsLoaded"
      />

      <WorkspaceDirectoryPicker
        v-model="workspacePickerOpen"
        :endpoint-ref="submitEndpointRef"
        :initial-path="pendingWorkspaceDir"
        @select="selectComposerWorkspace"
      />

      <AppDialogShell
        v-if="composerFilePreviewOpen"
        v-model="composerFilePreviewOpen"
        :title="composerFilePreviewName"
        width="880px"
        height="min(78vh, 760px)"
        @close="closeComposerFilePreview"
      >
        <section class="chat-composer-file-preview">
          <div class="chat-composer-file-preview-stage">
            <p v-if="composerFilePreviewLoading" class="chat-composer-file-preview-status">
              {{ t('chat_composer_file_preview_loading') }}
            </p>
            <div
              v-else-if="composerFilePreviewKind === 'image' && composerFilePreviewURL"
              class="chat-composer-file-preview-image-scroll"
            >
              <img
                class="chat-composer-file-preview-image"
                :src="composerFilePreviewURL"
                :alt="composerFilePreviewName"
              />
            </div>
            <iframe
              v-else-if="composerFilePreviewKind === 'pdf' && composerFilePreviewURL"
              class="chat-composer-file-preview-pdf"
              :src="composerFilePreviewURL"
              :title="composerFilePreviewName"
              sandbox=""
              referrerpolicy="no-referrer"
            ></iframe>
            <pre
              v-else-if="composerFilePreviewKind === 'text'"
              class="chat-composer-file-preview-text"
            >{{ composerFilePreviewText }}</pre>
            <QFence
              v-else-if="composerFilePreviewError"
              class="chat-composer-file-preview-error"
              type="danger"
              icon="QIconCloseCircle"
              :text="composerFilePreviewError"
            />
            <nav
              v-if="composerFilePreviewItems.length > 1"
              class="chat-composer-file-preview-navigation"
              :aria-label="t('chat_composer_file_preview_navigation')"
            >
              <QButton
                class="plain sm icon chat-composer-file-preview-nav-button is-previous"
                :disabled="!composerFilePreviewHasPrevious"
                :title="t('chat_composer_file_preview_previous')"
                :aria-label="t('chat_composer_file_preview_previous')"
                @click="navigateComposerFilePreview(-1)"
              >
                <QIconArrowLeft class="icon" />
              </QButton>
              <QButton
                class="plain sm icon chat-composer-file-preview-nav-button is-next"
                :disabled="!composerFilePreviewHasNext"
                :title="t('chat_composer_file_preview_next')"
                :aria-label="t('chat_composer_file_preview_next')"
                @click="navigateComposerFilePreview(1)"
              >
                <QIconArrowRight class="icon" />
              </QButton>
            </nav>
          </div>
        </section>
      </AppDialogShell>

      <Teleport to="body">
        <AppDialogShell
          :modelValue="endpointDialogOpen"
          :title="t('endpoint_switcher_title')"
          width="420px"
          @update:modelValue="endpointDialogOpen = $event"
          @close="endpointDialogOpen = false"
        >
          <section class="agent-chat-topic-dialog agent-chat-endpoint-dialog">
            <QInput
              v-if="endpointOptions.length > 8"
              v-model="endpointFilter"
              class="agent-chat-topic-dialog-filter"
              :placeholder="t('endpoint_switcher_filter_placeholder')"
            />
            <div
              class="agent-chat-topic-dialog-list"
              role="listbox"
              :aria-label="t('endpoint_switcher_title')"
            >
              <button
                v-for="item in filteredEndpointOptions"
                :key="item.value"
                type="button"
                class="agent-chat-topic-dialog-option agent-chat-endpoint-dialog-option"
                :class="{ 'is-selected': item.value === endpoint.endpoint_ref }"
                :disabled="item.disabled === true"
                role="option"
                :aria-selected="item.value === endpoint.endpoint_ref ? 'true' : 'false'"
                @click="selectEndpoint(item)"
              >
                <span class="agent-chat-endpoint-dialog-main">
                  <img
                    class="agent-chat-endpoint-dialog-avatar"
                    :src="item.image"
                    alt=""
                  />
                  <span>{{ item.title }}</span>
                </span>
                <QIconCheckCircle
                  v-if="item.value === endpoint.endpoint_ref"
                  class="icon agent-chat-topic-dialog-check"
                  aria-hidden="true"
                />
              </button>
              <p v-if="filteredEndpointOptions.length === 0" class="agent-chat-topic-dialog-empty">
                {{ t('endpoint_switcher_empty') }}
              </p>
            </div>
          </section>
        </AppDialogShell>

        <AppDialogShell
          v-if="topicsSupported"
          :modelValue="topicDialogOpen"
          :title="t('agent_desk_topic')"
          width="420px"
          @update:modelValue="topicDialogOpen = $event"
          @close="topicDialogOpen = false"
        >
          <section class="agent-chat-topic-dialog">
            <div class="agent-chat-topic-dialog-filterbar">
              <QInput
                v-model="topicFilter"
                class="agent-chat-topic-dialog-filter"
                :placeholder="t('agent_desk_topic_filter_placeholder')"
              />
              <QButton
                class="plain icon agent-chat-pane-new-topic"
                :class="{ 'is-active': creatingTopic }"
                :disabled="!available || historyLoading"
                :title="t('agent_desk_new_topic')"
                :aria-label="t('agent_desk_new_topic')"
                @click="startNewTopic"
              >
                <QIconPlus class="icon" />
              </QButton>
            </div>

            <div class="agent-chat-topic-dialog-list" role="listbox" :aria-label="t('agent_desk_topic')">
              <button
                v-for="item in filteredTopicOptions"
                :key="item.value"
                type="button"
                class="agent-chat-topic-dialog-option"
                :class="{ 'is-selected': item.value === selectedTopicID }"
                role="option"
                :aria-selected="item.value === selectedTopicID ? 'true' : 'false'"
                @click="selectTopic(item)"
              >
                <span>{{ item.title }}</span>
                <QIconCheckCircle
                  v-if="item.value === selectedTopicID"
                  class="icon agent-chat-topic-dialog-check"
                  aria-hidden="true"
                />
              </button>
              <p v-if="filteredTopicOptions.length === 0" class="agent-chat-topic-dialog-empty">
                {{ t('agent_desk_topic_filter_empty') }}
              </p>
            </div>
          </section>
        </AppDialogShell>
      </Teleport>
    </article>
  `,
};

export default AgentChatPane;
