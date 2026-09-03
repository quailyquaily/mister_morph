import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import "./ChatComposer.css";

import {
  buildComposerSuggestionItems,
  composerHighlightSegments,
  composerSuggestionInsertText,
  composerTriggerContext,
  replaceComposerSuggestionToken,
} from "../core/chat-composer-suggestions";

const DEFAULT_MAX_ROWS = 24;
const DEFAULT_SUGGESTION_LABELS = {
  commands: "Commands",
  skills: "Skills",
  loading: "Loading...",
  empty: "No matches",
};
const DEFAULT_FILE_LABELS = {
  files: "Attached files",
  preview: "Preview file",
  remove: "Remove from task",
  uploading: "Uploading...",
  failed: "Upload failed",
};

function sameSuggestionContext(a, b) {
  return (
    a?.type === b?.type &&
    a?.query === b?.query &&
    a?.start === b?.start &&
    a?.end === b?.end
  );
}

function normalizedText(value) {
  return String(value || "");
}

const ChatComposerDialogMenu = {
  name: "ChatComposerDialogMenu",
  props: {
    modelValue: Boolean,
    items: {
      type: Array,
      default: () => [],
    },
    useFilter: {
      type: Boolean,
      default: false,
    },
    scrollHeight: {
      type: String,
      default: "min(42dvh, 320px)",
    },
  },
  emits: ["update:modelValue", "change"],
  setup(props, { emit }) {
    const filterText = ref("");
    const focusedIndex = ref(-1);
    const filteredItems = computed(() => {
      const query = filterText.value.trim().toLowerCase();
      if (!query) {
        return props.items;
      }
      return props.items.filter((item) =>
        normalizedText(item?.title).toLowerCase().includes(query)
      );
    });
    const navigableItems = computed(() =>
      filteredItems.value.filter((item) => !item?.divider && !item?.disabled)
    );

    function close() {
      emit("update:modelValue", false);
    }

    function select(item) {
      if (item?.divider || item?.disabled) {
        return;
      }
      close();
      emit("change", item);
    }

    function handleKeydown(event) {
      const items = navigableItems.value;
      if (!items.length) {
        return;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        focusedIndex.value = Math.min(focusedIndex.value + 1, items.length - 1);
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        focusedIndex.value = Math.max(focusedIndex.value - 1, 0);
      } else if (event.key === "Enter" && focusedIndex.value >= 0) {
        event.preventDefault();
        select(items[focusedIndex.value]);
      }
    }

    watch(
      () => props.modelValue,
      (open) => {
        if (open) {
          filterText.value = "";
          focusedIndex.value = -1;
        }
      }
    );
    watch(filteredItems, () => {
      focusedIndex.value = -1;
    });

    return {
      filterText,
      filteredItems,
      focusedIndex,
      handleKeydown,
      select,
    };
  },
  template: `
    <Teleport to="body">
      <QDialog
        :modelValue="modelValue"
        no-frame
        @update:modelValue="$emit('update:modelValue', $event)"
      >
        <div class="q-menu-popup-body chat-composer-dialog-menu" @keydown="handleKeydown">
          <div v-if="useFilter" class="filter-area">
            <QInput v-model="filterText" class="filter-input" placeholder="Filter" />
          </div>
          <div
            class="scroll-area"
            :style="{ height: scrollHeight, maxHeight: scrollHeight }"
          >
            <QMenu
              v-if="filteredItems.length > 0"
              :items="filteredItems"
              :focused-index="focusedIndex"
              persistent
              no-frame
              no-shadow
              @action="select"
            />
            <div v-else class="empty-hint flow place-center">No item</div>
          </div>
        </div>
      </QDialog>
    </Teleport>
  `,
};

export default {
  name: "ChatComposer",
  components: {
    ChatComposerDialogMenu,
  },
  props: {
    modelValue: {
      type: String,
      default: "",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    placeholder: {
      type: String,
      default: "",
    },
    sendDisabled: {
      type: Boolean,
      default: false,
    },
    sending: {
      type: Boolean,
      default: false,
    },
    stopMode: {
      type: Boolean,
      default: false,
    },
    attachActive: {
      type: Boolean,
      default: false,
    },
    attachDisabled: {
      type: Boolean,
      default: false,
    },
    attachLabel: {
      type: String,
      default: "",
    },
    addLabel: {
      type: String,
      default: "",
    },
    uploadLabel: {
      type: String,
      default: "",
    },
    uploading: {
      type: Boolean,
      default: false,
    },
    fileItems: {
      type: Array,
      default: () => [],
    },
    fileLabels: {
      type: Object,
      default: () => ({}),
    },
    sendLabel: {
      type: String,
      default: "",
    },
    submitOnEnter: {
      type: Boolean,
      default: true,
    },
    inputHistory: {
      type: Array,
      default: () => [],
    },
    skills: {
      type: Array,
      default: () => [],
    },
    commands: {
      type: Array,
      default: () => [],
    },
    llmProfileItems: {
      type: Array,
      default: () => [],
    },
    llmProfileValue: {
      type: String,
      default: "",
    },
    llmProfileLabel: {
      type: String,
      default: "",
    },
    skillsLoading: {
      type: Boolean,
      default: false,
    },
    skillsError: {
      type: String,
      default: "",
    },
    suggestionLabels: {
      type: Object,
      default: () => DEFAULT_SUGGESTION_LABELS,
    },
    maxRows: {
      type: Number,
      default: DEFAULT_MAX_ROWS,
    },
    landing: {
      type: Boolean,
      default: false,
    },
    showAddActions: {
      type: Boolean,
      default: true,
    },
  },
  emits: [
    "update:modelValue",
    "update:llmProfileValue",
    "submit",
    "stop",
    "attach",
    "upload",
    "previewFile",
    "removeFile",
    "requestCommands",
    "requestSkills",
    "heightChange",
  ],
  setup(props, { emit, expose }) {
    const composerRoot = ref(null);
    const composerField = ref(null);
    const composerMirror = ref(null);
    const singleLine = ref(true);
    const suggestionContext = ref(null);
    const suggestionIndex = ref(0);
    const suggestionsOpen = ref(false);
    const historyIndex = ref(-1);
    const applyingHistoryText = ref(false);
    const addDialogOpen = ref(false);
    const llmProfileDialogOpen = ref(false);
    const compactDialogViewport = ref(
      typeof window !== "undefined" && window.innerWidth < 768
    );
    let resizeObserver = null;
    let observedWidth = 0;
    let lastEmittedHeight = 0;
    let syncHeightSeq = 0;

    const labels = computed(() => ({
      ...DEFAULT_SUGGESTION_LABELS,
      ...(props.suggestionLabels || {}),
    }));
    const resolvedFileLabels = computed(() => ({
      ...DEFAULT_FILE_LABELS,
      ...(props.fileLabels || {}),
    }));
    const inputValue = computed(() => normalizedText(props.modelValue));
    const highlightSegments = computed(() =>
      composerHighlightSegments({
        text: inputValue.value,
        commands: props.commands,
      })
    );
    const rootClass = computed(() => {
      const classes = ["chat-composer"];
      if (props.landing) {
        classes.push("chat-composer-landing");
      }
      if (!props.showAddActions) {
        classes.push("without-add-actions");
      }
      classes.push(singleLine.value ? "is-single-row" : "is-multi-row");
      return classes.join(" ");
    });
    const addMenuClass = computed(() =>
      props.attachActive
        ? "chat-composer-add-menu is-active"
        : "chat-composer-add-menu"
    );
    const addActionItems = computed(() => [
      {
        id: "chat-composer-upload-files",
        title: props.uploadLabel,
        value: "upload",
        icon: "PhPaperclip",
      },
      {
        id: "chat-composer-add-workspace",
        title: props.attachLabel,
        value: "workspace",
        icon: "PhCube",
      },
    ]);
    const addUsesDialog = computed(() => compactDialogViewport.value);
    const llmProfileUsesDialog = computed(
      () => compactDialogViewport.value || props.llmProfileItems.length > 4
    );
    const suggestionItems = computed(() =>
      buildComposerSuggestionItems({
        context: suggestionContext.value,
        commands: props.commands,
        skills: props.skills,
      })
    );
    const activeSuggestion = computed(() => {
      const items = suggestionItems.value;
      if (!items.length) {
        return null;
      }
      return items[suggestionIndex.value] || items[0];
    });
    const suggestionsVisible = computed(() => Boolean(suggestionsOpen.value && suggestionContext.value));
    const suggestionTitle = computed(() =>
      suggestionContext.value?.type === "skill" ? labels.value.skills : labels.value.commands
    );
    const suggestionEmptyText = computed(() => {
      if (suggestionContext.value?.type === "skill" && props.skillsLoading) {
        return labels.value.loading;
      }
      if (suggestionContext.value?.type === "skill" && props.skillsError) {
        return props.skillsError;
      }
      return labels.value.empty;
    });
    const historyItems = computed(() =>
      (Array.isArray(props.inputHistory) ? props.inputHistory : [])
        .map((item) => normalizedText(item).trim())
        .filter(Boolean)
    );
    const selectedLLMProfileItem = computed(() => {
      const items = Array.isArray(props.llmProfileItems) ? props.llmProfileItems : [];
      const value = normalizedText(props.llmProfileValue).trim();
      return items.find((item) => normalizedText(item?.value).trim() === value) || items[0] || null;
    });
    const llmProfileTitle = computed(() => {
      const label = normalizedText(props.llmProfileLabel).trim();
      const selected = normalizedText(selectedLLMProfileItem.value?.title).trim();
      return label && selected ? `${label}: ${selected}` : label || selected;
    });

    function fileItemClass(item) {
      const status = normalizedText(item?.status).trim();
      return status ? `chat-composer-file is-${status}` : "chat-composer-file";
    }

    function fileItemMeta(item) {
      const status = normalizedText(item?.status).trim();
      if (status === "uploading") {
        return resolvedFileLabels.value.uploading;
      }
      if (status === "failed") {
        return normalizedText(item?.error).trim() || resolvedFileLabels.value.failed;
      }
      return "";
    }

    function previewFile(item) {
      if (normalizedText(item?.status).trim() !== "ready") {
        return;
      }
      emit("previewFile", item);
    }

    function rootElement() {
      return composerRoot.value?.$el || composerRoot.value;
    }

    function textarea() {
      const root = composerField.value?.$el || composerField.value;
      if (!root) {
        return null;
      }
      if (root.tagName?.toLowerCase?.() === "textarea") {
        return root;
      }
      if (typeof root.querySelector !== "function") {
        return null;
      }
      return root.querySelector("textarea");
    }

    function highlightClass(segment) {
      return segment?.type === "command"
        ? "chat-composer-highlight-token is-command"
        : segment?.type === "skill"
          ? "chat-composer-highlight-token is-skill"
          : "";
    }

    function syncMirrorScroll() {
      const field = textarea();
      const mirror = composerMirror.value?.$el || composerMirror.value;
      if (!field || !mirror) {
        return;
      }
      mirror.scrollTop = field.scrollTop;
      mirror.scrollLeft = field.scrollLeft;
    }

    function emitHeightChange() {
      const root = rootElement();
      if (!root || typeof root.getBoundingClientRect !== "function") {
        return;
      }
      const height = Math.ceil(root.getBoundingClientRect().height || root.offsetHeight || 0);
      if (Math.abs(height - lastEmittedHeight) < 1) {
        return;
      }
      lastEmittedHeight = height;
      emit("heightChange", height);
    }

    function installResizeObserver() {
      if (typeof ResizeObserver !== "function") {
        return;
      }
      const root = rootElement();
      if (!root || typeof root.getBoundingClientRect !== "function") {
        return;
      }
      observedWidth = root.getBoundingClientRect().width || 0;
      resizeObserver = new ResizeObserver((entries) => {
        const entry = entries[0];
        const width = Number(entry?.contentRect?.width) || root.getBoundingClientRect().width || 0;
        if (Math.abs(width - observedWidth) < 1) {
          return;
        }
        observedWidth = width;
        syncHeight();
      });
      resizeObserver.observe(root);
    }

    function closeSuggestions() {
      suggestionsOpen.value = false;
      suggestionContext.value = null;
      suggestionIndex.value = 0;
    }

    function setSuggestionContext(nextContext) {
      if (!nextContext) {
        closeSuggestions();
        return;
      }
      if (!sameSuggestionContext(suggestionContext.value, nextContext)) {
        suggestionIndex.value = 0;
      }
      suggestionContext.value = nextContext;
      suggestionsOpen.value = true;
      if (nextContext.type === "command") {
        emit("requestCommands");
      } else if (nextContext.type === "skill") {
        emit("requestSkills");
      }
    }

    function syncSuggestionsFromTextarea(rawText = null) {
      if (props.disabled) {
        closeSuggestions();
        return;
      }
      const field = textarea();
      const text = rawText === null ? normalizedText(field?.value ?? props.modelValue) : normalizedText(rawText);
      const cursor = typeof field?.selectionStart === "number" ? field.selectionStart : text.length;
      setSuggestionContext(composerTriggerContext(text, cursor));
    }

    function suggestionItemActive(item, index) {
      return activeSuggestion.value?.key === item?.key || suggestionIndex.value === index;
    }

    function suggestionItemClass(item, index) {
      const classes = ["chat-composer-suggestion-item"];
      if (suggestionItemActive(item, index)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function applySuggestion(item = activeSuggestion.value) {
      if (!item) {
        return;
      }
      const field = textarea();
      const text = normalizedText(props.modelValue);
      const cursor = typeof field?.selectionStart === "number" ? field.selectionStart : text.length;
      const context = composerTriggerContext(text, cursor) || suggestionContext.value;
      if (!context) {
        closeSuggestions();
        return;
      }
      const insertText = composerSuggestionInsertText(item.insertText || item.value || item.title);
      const nextText = replaceComposerSuggestionToken(text, context, insertText);
      const nextCursor = context.start + insertText.length;
      resetHistoryNavigation();
      emit("update:modelValue", nextText);
      closeSuggestions();
      syncHeight(nextText);
      void nextTick(() => {
        const nextField = textarea();
        if (!nextField || nextField.disabled) {
          return;
        }
        nextField.focus({ preventScroll: true });
        nextField.setSelectionRange(nextCursor, nextCursor);
      });
    }

    function handleSuggestionKeydown(event) {
      if (!suggestionsOpen.value) {
        return false;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        closeSuggestions();
        return true;
      }
      const items = suggestionItems.value;
      if (!items.length) {
        if (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Enter" || event.key === "Tab") {
          event.preventDefault();
          return true;
        }
        return false;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        suggestionIndex.value = (suggestionIndex.value + 1) % items.length;
        return true;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        suggestionIndex.value = (suggestionIndex.value - 1 + items.length) % items.length;
        return true;
      }
      if ((event.key === "Enter" || event.key === "Tab") && !event.altKey && !event.ctrlKey && !event.metaKey) {
        event.preventDefault();
        applySuggestion(items[suggestionIndex.value] || items[0]);
        return true;
      }
      return false;
    }

    function resetHistoryNavigation() {
      historyIndex.value = -1;
      applyingHistoryText.value = false;
    }

    function applyHistoryText(index, text) {
      historyIndex.value = index;
      applyingHistoryText.value = true;
      emit("update:modelValue", text);
      syncHeight(text);
      focus();
      void nextTick(() => {
        applyingHistoryText.value = false;
      });
    }

    function handleHistoryKeydown(event) {
      if (
        !event ||
        (event.key !== "ArrowUp" && event.key !== "ArrowDown") ||
        event.altKey ||
        event.ctrlKey ||
        event.metaKey ||
        event.shiftKey ||
        event.isComposing ||
        props.disabled
      ) {
        return;
      }
      const history = historyItems.value;
      if (!history.length) {
        return;
      }
      const current = normalizedText(props.modelValue);
      const index = historyIndex.value;
      const browsing = index >= 0 && current === history[index];
      if (!browsing && (current !== "" || event.key === "ArrowDown")) {
        resetHistoryNavigation();
        return;
      }
      const nextIndex = event.key === "ArrowUp"
        ? (browsing ? Math.min(index + 1, history.length - 1) : 0)
        : Math.max(index - 1, -1);
      event.preventDefault();
      if (browsing && nextIndex === index) {
        focus();
        return;
      }
      if (nextIndex < 0) {
        resetHistoryNavigation();
        applyingHistoryText.value = true;
        emit("update:modelValue", "");
        syncHeight("");
        focus();
        void nextTick(() => {
          applyingHistoryText.value = false;
        });
        return;
      }
      applyHistoryText(nextIndex, history[nextIndex]);
    }

    function handleEnter(event) {
      if (event?.isComposing || event?.keyCode === 229) {
        return;
      }
      event?.preventDefault?.();
      emit("submit");
    }

    function handleKeydown(event) {
      if (event?.isComposing || event?.keyCode === 229) {
        return;
      }
      if (handleSuggestionKeydown(event)) {
        return;
      }
      if (
        props.submitOnEnter &&
        event?.key === "Enter" &&
        !event.shiftKey &&
        !event.altKey &&
        !event.ctrlKey &&
        !event.metaKey
      ) {
        handleEnter(event);
        return;
      }
      if (event?.key === "ArrowUp" || event?.key === "ArrowDown") {
        handleHistoryKeydown(event);
      }
    }

    function handleKeyup() {
      syncSuggestionsFromTextarea();
    }

    function handleInput(event) {
      const nextValue = normalizedText(event?.target?.value);
      resetHistoryNavigation();
      emit("update:modelValue", nextValue);
      void syncHeight(nextValue);
      syncSuggestionsFromTextarea(nextValue);
      void nextTick(syncMirrorScroll);
    }

    function handleInputScroll() {
      syncMirrorScroll();
    }

    function selectLLMProfile(item) {
      llmProfileDialogOpen.value = false;
      emit("update:llmProfileValue", normalizedText(item?.value).trim());
    }

    function selectAddAction(item) {
      addDialogOpen.value = false;
      if (normalizedText(item?.value).trim() === "upload") {
        emit("upload");
        return;
      }
      emit("attach");
    }

    function openAddDialog() {
      if (props.attachDisabled || props.uploading) {
        return;
      }
      addDialogOpen.value = true;
    }

    function openLLMProfileDialog() {
      if (props.disabled) {
        return;
      }
      llmProfileDialogOpen.value = true;
    }

    function refreshDialogViewport() {
      const compact = window.innerWidth < 768;
      if (compact === compactDialogViewport.value) {
        return;
      }
      compactDialogViewport.value = compact;
      addDialogOpen.value = false;
      llmProfileDialogOpen.value = false;
    }

    function focus(options = {}) {
      if (props.disabled) {
        return;
      }
      const preserveSelection = options?.preserveSelection === true;
      void nextTick(() => {
        const run = () => {
          const field = textarea();
          if (!field || field.disabled) {
            return;
          }
          if (props.submitOnEnter) {
            field.focus({ preventScroll: true });
          } else {
            field.focus();
          }
          if (!preserveSelection) {
            const length = field.value.length;
            field.setSelectionRange(length, length);
          }
        };
        if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
          window.requestAnimationFrame(run);
        } else {
          run();
        }
      });
    }

    function insertText(rawText) {
      const insertValue = normalizedText(rawText);
      if (!insertValue) {
        return;
      }
      resetHistoryNavigation();
      const current = normalizedText(props.modelValue);
      const field = textarea();
      const active = typeof document !== "undefined" ? document.activeElement : null;
      let start = current.length;
      let end = current.length;
      if (
        field &&
        active === field &&
        typeof field.selectionStart === "number" &&
        typeof field.selectionEnd === "number"
      ) {
        start = field.selectionStart;
        end = field.selectionEnd;
      }
      const nextValue = `${current.slice(0, start)}${insertValue}${current.slice(end)}`;
      emit("update:modelValue", nextValue);
      syncHeight(nextValue);
      void nextTick(() => {
        const nextField = textarea();
        if (!nextField || nextField.disabled) {
          return;
        }
        const nextOffset = start + insertValue.length;
        nextField.focus({ preventScroll: true });
        nextField.setSelectionRange(nextOffset, nextOffset);
      });
    }

    function measureTextarea(field) {
      const styles = window.getComputedStyle(field);
      const lineHeight = Number.parseFloat(styles.lineHeight) || 20;
      const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
      const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
      const borderTop = Number.parseFloat(styles.borderTopWidth) || 0;
      const borderBottom = Number.parseFloat(styles.borderBottomWidth) || 0;
      const maxRows = Math.max(1, Number(props.maxRows) || DEFAULT_MAX_ROWS);
      const minHeight = lineHeight + paddingTop + paddingBottom + borderTop + borderBottom;
      const maxHeight = lineHeight * maxRows + paddingTop + paddingBottom + borderTop + borderBottom;

      field.style.height = "0px";
      const scrollHeight = field.scrollHeight;
      const contentHeight = Math.max(0, scrollHeight - paddingTop - paddingBottom);
      const visualRows = Math.max(1, Math.round(contentHeight / lineHeight));
      return {
        maxHeight,
        minHeight,
        scrollHeight,
        visualRows,
      };
    }

    function singleLineTextareaMetrics(field) {
      const styles = window.getComputedStyle(field);
      const lineHeight = Number.parseFloat(styles.lineHeight) || 20;
      const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
      const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
      const borderTop = Number.parseFloat(styles.borderTopWidth) || 0;
      const borderBottom = Number.parseFloat(styles.borderBottomWidth) || 0;
      const height = lineHeight + paddingTop + paddingBottom + borderTop + borderBottom;
      return {
        maxHeight: height,
        minHeight: height,
        scrollHeight: height,
        visualRows: 1,
      };
    }

    function applyMeasuredTextareaHeight(field, metrics) {
      const nextHeight = Math.max(metrics.minHeight, Math.min(metrics.scrollHeight, metrics.maxHeight));
      field.style.height = `${nextHeight}px`;
      field.style.overflowY = metrics.scrollHeight > metrics.maxHeight ? "auto" : "hidden";
      void nextTick(syncMirrorScroll);
    }

    async function syncHeight(rawValue = props.modelValue) {
      const seq = syncHeightSeq + 1;
      syncHeightSeq = seq;
      const text = normalizedText(rawValue);
      const baselineSingleLine = text === "" || !text.includes("\n");
      if (singleLine.value !== baselineSingleLine) {
        singleLine.value = baselineSingleLine;
      }
      await nextTick();
      if (seq !== syncHeightSeq) {
        return;
      }
      let field = textarea();
      if (!field || typeof window === "undefined") {
        return;
      }
      if (text === "") {
        const metrics = singleLineTextareaMetrics(field);
        applyMeasuredTextareaHeight(field, metrics);
        emitHeightChange();
        return;
      }

      let metrics = measureTextarea(field);
      const nextSingleLine = metrics.visualRows <= 1;
      if (singleLine.value !== nextSingleLine) {
        singleLine.value = nextSingleLine;
        await nextTick();
        if (seq !== syncHeightSeq) {
          return;
        }
        field = textarea();
        if (!field) {
          return;
        }
        metrics = measureTextarea(field);
      }
      applyMeasuredTextareaHeight(field, metrics);
      emitHeightChange();
    }

    function handlePointerDown(event) {
      const target = event?.target;
      if (typeof Element === "undefined" || !(target instanceof Element)) {
        focus();
        return;
      }
      if (target.closest(".chat-composer-send")) {
        return;
      }
      if (target.closest("textarea, input, button, a, [role='button']")) {
        return;
      }
      event.preventDefault();
      focus();
    }

    watch(
      () => props.modelValue,
      () => {
        if (!applyingHistoryText.value) {
          const history = historyItems.value;
          const index = historyIndex.value;
          if (index >= 0 && normalizedText(props.modelValue) !== normalizedText(history[index])) {
            resetHistoryNavigation();
          }
        }
        syncHeight();
        void nextTick(syncSuggestionsFromTextarea);
      }
    );
    watch(
      () => props.disabled,
      (disabled) => {
        if (disabled) {
          closeSuggestions();
          llmProfileDialogOpen.value = false;
        }
        syncHeight();
      }
    );
    watch(
      () => props.attachDisabled || props.uploading,
      (blocked) => {
        if (blocked) {
          addDialogOpen.value = false;
        }
      }
    );
    watch(llmProfileUsesDialog, (usesDialog) => {
      if (!usesDialog) {
        llmProfileDialogOpen.value = false;
      }
    });
    watch(suggestionItems, (items) => {
      if (!items.length || suggestionIndex.value >= items.length) {
        suggestionIndex.value = 0;
      }
    });
    watch(
      () => props.fileItems,
      () => {
        void nextTick(emitHeightChange);
      }
    );

    onMounted(() => {
      syncHeight();
      installResizeObserver();
      window.addEventListener("resize", refreshDialogViewport);
    });

    onUnmounted(() => {
      window.removeEventListener("resize", refreshDialogViewport);
      if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = null;
      }
    });

    expose({ focus, insertText, syncHeight, closeSuggestions });

    return {
      inputValue,
      highlightSegments,
      composerRoot,
      composerField,
      composerMirror,
      rootClass,
      addMenuClass,
      addActionItems,
      addDialogOpen,
      addUsesDialog,
      suggestionItems,
      suggestionsVisible,
      suggestionTitle,
      suggestionEmptyText,
      suggestionItemClass,
      suggestionItemActive,
      selectedLLMProfileItem,
      llmProfileTitle,
      llmProfileDialogOpen,
      llmProfileUsesDialog,
      resolvedFileLabels,
      fileItemClass,
      fileItemMeta,
      previewFile,
      applySuggestion,
      handleKeydown,
      handleKeyup,
      handleInput,
      handleInputScroll,
      selectLLMProfile,
      selectAddAction,
      openAddDialog,
      openLLMProfileDialog,
      handlePointerDown,
      highlightClass,
    };
  },
  template: `
    <div ref="composerRoot" :class="rootClass" @pointerdown="handlePointerDown">
      <div
        v-if="!landing"
        class="chat-composer-gradient-blur"
        aria-hidden="true"
      ></div>
      <div class="chat-composer-surface">
        <div
          v-if="suggestionsVisible"
          class="chat-composer-suggestions"
          role="listbox"
          :aria-label="suggestionTitle"
        >
          <button
            v-for="(item, index) in suggestionItems"
            :key="item.key"
            type="button"
            :class="suggestionItemClass(item, index)"
            role="option"
            :aria-selected="suggestionItemActive(item, index) ? 'true' : 'false'"
            @mousedown.prevent
            @click="applySuggestion(item)"
          >
            <span class="chat-composer-suggestion-title">{{ item.title }}</span>
            <span v-if="item.description" class="chat-composer-suggestion-description">{{ item.description }}</span>
          </button>
          <p v-if="!suggestionItems.length" class="chat-composer-suggestions-empty">
            {{ suggestionEmptyText }}
          </p>
        </div>
        <div
          v-if="fileItems.length"
          class="chat-composer-files"
          :aria-label="resolvedFileLabels.files"
          aria-live="polite"
        >
          <article
            v-for="item in fileItems"
            :key="item.id"
            :class="fileItemClass(item)"
          >
            <button
              type="button"
              class="chat-composer-file-main"
              :disabled="item.status !== 'ready'"
              :title="item.status === 'ready' ? resolvedFileLabels.preview : fileItemMeta(item)"
              @click="previewFile(item)"
            >
              <PhPaperclip class="chat-composer-file-icon" />
              <span class="chat-composer-file-copy">
                <span class="chat-composer-file-name">{{ item.name }}</span>
                <span v-if="fileItemMeta(item)" class="chat-composer-file-meta">{{ fileItemMeta(item) }}</span>
              </span>
            </button>
            <button
              type="button"
              class="chat-composer-file-remove"
              :title="resolvedFileLabels.remove"
              :aria-label="resolvedFileLabels.remove + ': ' + item.name"
              @click="$emit('removeFile', item)"
            >
              <PhXCircle class="icon" />
            </button>
          </article>
        </div>
        <div class="chat-composer-grid">
          <div v-if="showAddActions" class="chat-composer-toolbar-start">
            <QDropdownMenu
              v-if="!addUsesDialog"
              :class="addMenuClass"
              :items="addActionItems"
              :title="addLabel"
              hideSelected
              hideActionLabel
              :useFilter="true"
              useDialog="never"
              scrollHeight="min(42dvh, 320px)"
              variant="plain"
              :disabled="attachDisabled"
              :loading="uploading"
              @change="selectAddAction"
            >
              <PhPlus class="chat-composer-add-icon" />
              <span class="chat-composer-add-label">{{ addLabel }}</span>
            </QDropdownMenu>
            <div v-else :class="['q-dropdown-menu', addMenuClass]">
              <div class="q-dropdown-menu-inner">
                <button
                  type="button"
                  class="q-dropdown-menu-action touchable plain hide-selected no-prepend"
                  :class="{ expanded: addDialogOpen, loading: uploading }"
                  :disabled="attachDisabled || uploading"
                  :title="addLabel"
                  :aria-label="addLabel"
                  :aria-expanded="addDialogOpen ? 'true' : 'false'"
                  :aria-busy="uploading ? 'true' : undefined"
                  aria-haspopup="dialog"
                  @click.stop="openAddDialog"
                >
                  <div v-if="uploading" class="ocean" aria-hidden="true"><div class="wave"></div></div>
                  <PhPlus class="chat-composer-add-icon" />
                  <span class="chat-composer-add-label">{{ addLabel }}</span>
                  <span class="empty-block"></span>
                  <PhCaretDown class="icon chevron-icon" aria-hidden="true" />
                </button>
              </div>
            </div>
          </div>
          <div class="chat-composer-input-shell">
            <div ref="composerMirror" class="chat-composer-highlight" aria-hidden="true">
              <span
                v-for="(segment, index) in highlightSegments"
                :key="index + ':' + segment.type + ':' + segment.text"
                :class="highlightClass(segment)"
              >{{ segment.text }}</span>
            </div>
            <textarea
              ref="composerField"
              class="chat-composer-input"
              :value="inputValue"
              rows="1"
              :disabled="disabled"
              :placeholder="placeholder"
              @input="handleInput"
              @keydown="handleKeydown"
              @keyup="handleKeyup"
              @scroll="handleInputScroll"
            ></textarea>
          </div>
          <div class="chat-composer-actions">
            <template v-if="llmProfileItems.length > 1">
              <QDropdownMenu
                v-if="!llmProfileUsesDialog"
                :key="'llm-profile-' + llmProfileValue + '-' + llmProfileItems.length"
                class="chat-composer-profile llm-profile-dropdown"
                :items="llmProfileItems"
                :initialItem="selectedLLMProfileItem"
                :placeholder="llmProfileLabel"
                :title="llmProfileTitle"
                :useFilter="true"
                useDialog="never"
                scrollHeight="min(42dvh, 320px)"
                variant="plain"
                :disabled="disabled"
                @change="selectLLMProfile"
              />
              <div
                v-else
                class="q-dropdown-menu chat-composer-profile llm-profile-dropdown"
              >
                <div class="q-dropdown-menu-inner">
                  <button
                    type="button"
                    class="q-dropdown-menu-action touchable plain no-prepend"
                    :class="{ expanded: llmProfileDialogOpen }"
                    :disabled="disabled"
                    :title="llmProfileTitle"
                    :aria-label="llmProfileTitle"
                    :aria-expanded="llmProfileDialogOpen ? 'true' : 'false'"
                    aria-haspopup="dialog"
                    @click.stop="openLLMProfileDialog"
                  >
                    <div class="q-dropdown-selected">
                      <div class="menu-title q-text-body-text">
                        {{ selectedLLMProfileItem?.title || llmProfileLabel }}
                      </div>
                    </div>
                    <PhCaretDown class="icon chevron-icon" aria-hidden="true" />
                  </button>
                </div>
              </div>
            </template>
            <QButton
              class="primary sm icon chat-composer-send"
              :loading="sending"
              :disabled="sendDisabled"
              :title="sendLabel"
              :aria-label="sendLabel"
              @click="stopMode ? $emit('stop') : $emit('submit')"
            >
              <PhStop v-if="stopMode" class="icon chat-composer-stop-icon" />
              <PhPaperPlaneTilt v-else class="icon" />
            </QButton>
          </div>
        </div>
      </div>
      <ChatComposerDialogMenu
        v-if="showAddActions && addUsesDialog"
        v-model="addDialogOpen"
        :items="addActionItems"
        :useFilter="true"
        scrollHeight="min(42dvh, 320px)"
        @change="selectAddAction"
      />
      <ChatComposerDialogMenu
        v-if="llmProfileItems.length > 1 && llmProfileUsesDialog"
        v-model="llmProfileDialogOpen"
        :items="llmProfileItems"
        :useFilter="true"
        scrollHeight="min(42dvh, 320px)"
        @change="selectLLMProfile"
      />
    </div>
  `,
};
