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

export default {
  name: "ChatComposer",
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
    sendLabel: {
      type: String,
      default: "",
    },
    disclaimer: {
      type: String,
      default: "",
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
  },
  emits: ["update:modelValue", "submit", "attach", "requestCommands", "requestSkills", "heightChange"],
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
    let resizeObserver = null;
    let observedWidth = 0;
    let lastEmittedHeight = 0;
    let syncHeightSeq = 0;

    const labels = computed(() => ({
      ...DEFAULT_SUGGESTION_LABELS,
      ...(props.suggestionLabels || {}),
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
      classes.push(singleLine.value ? "is-single-row" : "is-multi-row");
      return classes.join(" ");
    });
    const attachClass = computed(() =>
      props.attachActive
        ? "plain sm icon chat-composer-workspace is-active"
        : "plain sm icon chat-composer-workspace"
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

    function focus() {
      if (props.disabled) {
        return;
      }
      void nextTick(() => {
        const run = () => {
          const field = textarea();
          if (!field || field.disabled) {
            return;
          }
          field.focus({ preventScroll: true });
          const length = field.value.length;
          field.setSelectionRange(length, length);
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
        }
        syncHeight();
      }
    );
    watch(suggestionItems, (items) => {
      if (!items.length || suggestionIndex.value >= items.length) {
        suggestionIndex.value = 0;
      }
    });

    onMounted(() => {
      syncHeight();
      installResizeObserver();
    });

    onUnmounted(() => {
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
      attachClass,
      suggestionItems,
      suggestionsVisible,
      suggestionTitle,
      suggestionEmptyText,
      suggestionItemClass,
      suggestionItemActive,
      applySuggestion,
      handleKeydown,
      handleKeyup,
      handleInput,
      handleInputScroll,
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
        <div class="chat-composer-grid">
          <div class="chat-composer-toolbar-start">
            <QButton
              :class="attachClass"
              :title="attachLabel"
              :aria-label="attachLabel"
              :disabled="attachDisabled"
              @click="$emit('attach')"
            >
              <QIconPlus class="icon" />
            </QButton>
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
            <QButton
              class="primary sm icon chat-composer-send"
              :loading="sending"
              :disabled="sendDisabled"
              :title="sendLabel"
              :aria-label="sendLabel"
              @click="$emit('submit')"
            >
              <QIconSend class="icon" />
            </QButton>
          </div>
        </div>
      </div>
      <p v-if="disclaimer" class="chat-composer-disclaimer">{{ disclaimer }}</p>
    </div>
  `,
};
