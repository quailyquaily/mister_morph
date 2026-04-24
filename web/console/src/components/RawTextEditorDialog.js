import { computed } from "vue";
import { translate } from "../core/context";
import "./RawTextEditorDialog.css";

const RawTextEditorDialog = {
  emits: ["close", "save", "update:modelValue"],
  props: {
    open: {
      type: Boolean,
      default: false,
    },
    modelValue: {
      type: String,
      default: "",
    },
    title: {
      type: String,
      default: "",
    },
    path: {
      type: String,
      default: "",
    },
    loading: {
      type: Boolean,
      default: false,
    },
    saving: {
      type: Boolean,
      default: false,
    },
  },
  setup(props, { emit }) {
    const t = translate;
    const resolvedTitle = computed(() => props.title || t("repair_editor_title"));

    function close() {
      emit("close");
    }

    function save() {
      emit("save");
    }

    function onInput(value) {
      emit("update:modelValue", String(value || ""));
    }

    return {
      t,
      close,
      save,
      onInput,
      resolvedTitle,
    };
  },
  template: `
    <div v-if="open" class="raw-text-overlay" @click.self="close">
      <section class="raw-text-dialog frame">
        <header class="raw-text-head app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ resolvedTitle }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            :disabled="saving"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
        <code v-if="path" class="raw-text-path">{{ path }}</code>
        <QProgress v-if="loading" :infinite="true" />
        <QTextarea
          v-else
          class="raw-text-body"
          :modelValue="modelValue"
          :rows="20"
          :disabled="saving"
          @update:modelValue="onInput"
        />
        <div class="raw-text-actions">
          <QButton class="primary sm" :loading="saving" :disabled="loading" @click="save">{{ t("action_save") }}</QButton>
        </div>
      </section>
    </div>
  `,
};

export default RawTextEditorDialog;
