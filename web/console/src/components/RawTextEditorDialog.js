import { computed } from "vue";
import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import "./RawTextEditorDialog.css";

const RawTextEditorDialog = {
  components: {
    AppDialogShell,
  },
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
    <AppDialogShell
      :modelValue="open"
      :title="resolvedTitle"
      width="920px"
      :closeDisabled="saving"
      @close="close"
    >
      <section class="raw-text-dialog">
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
    </AppDialogShell>
  `,
};

export default RawTextEditorDialog;
