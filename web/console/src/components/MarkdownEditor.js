import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import OverType from "overtype";
import "./MarkdownEditor.css";

const SELECTION_EVENTS = ["click", "input", "keyup", "select"];

function stringValue(value) {
  return typeof value === "string" ? value : String(value ?? "");
}

const MarkdownEditor = {
  props: {
    modelValue: {
      type: String,
      default: "",
    },
    placeholder: {
      type: String,
      default: "",
    },
    hint: {
      type: String,
      default: "",
    },
    height: {
      type: String,
      default: "",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    readOnly: {
      type: Boolean,
      default: false,
    },
    ariaLabel: {
      type: String,
      default: "",
    },
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    const host = ref(null);
    const editor = ref(null);
    const syncingModelValue = ref(false);
    const savedSelection = ref({ start: null, end: null });

    const surfaceStyle = computed(() => {
      const height = String(props.height || "").trim();
      return height ? { "--markdown-editor-height": height } : {};
    });

    function normalizedAriaLabel() {
      return String(props.ariaLabel || "").trim() || "Markdown editor";
    }

    function rememberSelection() {
      const textarea = editor.value?.textarea;
      if (!textarea) {
        return;
      }
      const start = textarea.selectionStart;
      const end = textarea.selectionEnd;
      if (!Number.isInteger(start) || !Number.isInteger(end)) {
        return;
      }
      if (document.activeElement !== textarea && !Number.isInteger(savedSelection.value.start)) {
        return;
      }
      savedSelection.value = { start, end };
    }

    function insertAtCursor(raw, options = {}) {
      const instance = editor.value;
      const textarea = instance?.textarea;
      const text = stringValue(raw);
      if (!instance || !textarea || !text || props.disabled || props.readOnly) {
        return false;
      }

      const current = textarea.value;
      const active = document.activeElement === textarea;
      let start = active ? textarea.selectionStart : savedSelection.value.start;
      let end = active ? textarea.selectionEnd : savedSelection.value.end;
      if (!Number.isInteger(start) || start < 0 || start > current.length) {
        start = current.length;
      }
      if (!Number.isInteger(end) || end < start || end > current.length) {
        end = start;
      }

      const before = current.slice(0, start);
      const after = current.slice(end);
      const prefix = options.spacing === true && before && !/\s$/.test(before) ? " " : "";
      const suffix = options.spacing === true && after && !/^\s/.test(after) ? " " : "";
      const inserted = `${prefix}${text}${suffix}`;
      textarea.focus({ preventScroll: true });
      textarea.setSelectionRange(start, end);
      instance.insertAtCursor(inserted);

      const cursor = start + inserted.length;
      savedSelection.value = { start: cursor, end: cursor };
      nextTick(() => {
        instance.focus();
        textarea.setSelectionRange(cursor, cursor);
      });
      return true;
    }

    function applyTextareaState() {
      const instance = editor.value;
      const textarea = instance?.textarea;
      if (!textarea) {
        return;
      }
      const disabled = props.disabled === true;
      const readOnly = disabled || props.readOnly === true;
      textarea.disabled = disabled;
      textarea.readOnly = readOnly;
      textarea.setAttribute("aria-label", normalizedAriaLabel());
      if (disabled) {
        textarea.blur();
      }
    }

    function applyPlaceholder() {
      const instance = editor.value;
      const nextPlaceholder = String(props.placeholder || "");
      if (instance?.textarea) {
        instance.textarea.placeholder = nextPlaceholder;
      }
      if (instance?.placeholderEl) {
        instance.placeholderEl.textContent = nextPlaceholder;
      }
    }

    function initEditor() {
      if (!host.value) {
        return;
      }
      const [instance] = OverType.init(host.value, {
        value: stringValue(props.modelValue),
        placeholder: String(props.placeholder || ""),
        toolbar: false,
        showStats: false,
        smartLists: true,
        spellcheck: false,
        fontFamily: "var(--font-mono)",
        fontSize: "14px",
        lineHeight: 1.75,
        padding: "16px 18px",
        textareaProps: {
          "aria-label": normalizedAriaLabel(),
          disabled: props.disabled,
          readOnly: props.disabled || props.readOnly,
        },
        onChange(value) {
          if (syncingModelValue.value) {
            return;
          }
          emit("update:modelValue", value);
        },
      });
      editor.value = instance || null;
      for (const eventName of SELECTION_EVENTS) {
        instance?.textarea?.addEventListener(eventName, rememberSelection);
      }
      applyTextareaState();
      applyPlaceholder();
    }

    onMounted(() => {
      initEditor();
    });

    onBeforeUnmount(() => {
      for (const eventName of SELECTION_EVENTS) {
        editor.value?.textarea?.removeEventListener(eventName, rememberSelection);
      }
      editor.value?.destroy();
      editor.value = null;
    });

    watch(
      () => props.modelValue,
      (value) => {
        const instance = editor.value;
        if (!instance) {
          return;
        }
        const next = stringValue(value);
        if (instance.getValue() !== next) {
          savedSelection.value = { start: null, end: null };
          syncingModelValue.value = true;
          instance.setValue(next);
          syncingModelValue.value = false;
        }
      }
    );

    watch(
      () => props.disabled,
      () => {
        applyTextareaState();
      }
    );

    watch(
      () => props.readOnly,
      () => {
        applyTextareaState();
      }
    );

    watch(
      () => props.ariaLabel,
      () => {
        applyTextareaState();
      }
    );

    watch(
      () => props.placeholder,
      () => {
        applyPlaceholder();
      }
    );

    return {
      hint: computed(() => String(props.hint || "").trim()),
      host,
      insertAtCursor,
      rememberSelection,
      surfaceStyle,
      toolbarAriaLabel: computed(() => `${normalizedAriaLabel()} toolbar`),
    };
  },
  template: `
    <div class="markdown-editor-shell">
      <div :class="$slots.toolbar ? 'markdown-editor-frame has-toolbar' : 'markdown-editor-frame'">
        <div
          v-if="$slots.toolbar"
          class="markdown-editor-toolbar"
          role="toolbar"
          :aria-label="toolbarAriaLabel"
          @pointerdown.capture="rememberSelection"
        >
          <slot name="toolbar"></slot>
        </div>
        <div ref="host" class="markdown-editor-surface" :style="surfaceStyle"></div>
      </div>
      <p v-if="hint" class="markdown-editor-hint">{{ hint }}</p>
    </div>
  `,
};

export default MarkdownEditor;
