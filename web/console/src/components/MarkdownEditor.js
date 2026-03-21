import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import OverType from "overtype";
import "./MarkdownEditor.css";

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

    const surfaceStyle = computed(() => {
      const height = String(props.height || "").trim();
      return height ? { "--markdown-editor-height": height } : {};
    });

    function normalizedAriaLabel() {
      return String(props.ariaLabel || "").trim() || "Markdown editor";
    }

    function applyTextareaState() {
      const instance = editor.value;
      const textarea = instance?.textarea;
      if (!textarea) {
        return;
      }
      const disabled = props.disabled === true;
      textarea.disabled = disabled;
      textarea.readOnly = disabled;
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
          readOnly: props.disabled,
        },
        onChange(value) {
          if (syncingModelValue.value) {
            return;
          }
          emit("update:modelValue", value);
        },
      });
      editor.value = instance || null;
      applyTextareaState();
      applyPlaceholder();
    }

    onMounted(() => {
      initEditor();
    });

    onBeforeUnmount(() => {
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
      surfaceStyle,
    };
  },
  template: `
    <div class="markdown-editor-shell">
      <div ref="host" class="markdown-editor-surface" :style="surfaceStyle"></div>
      <p v-if="hint" class="markdown-editor-hint">{{ hint }}</p>
    </div>
  `,
};

export default MarkdownEditor;
