import { computed } from "vue";
import { translate } from "../core/context";
import "./RawTextEditorDialog.css";

export const rawTextEditorDialogContentProps = {
  modelValue: {
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
};

const RawTextEditorDialogContent = {
  props: rawTextEditorDialogContentProps,
  emits: ["save", "update:modelValue"],
  setup(props, { emit }) {
    const t = translate;
    const saveDisabled = computed(() => props.loading || props.saving);

    function save() {
      emit("save");
    }

    function onInput(value) {
      emit("update:modelValue", String(value || ""));
    }

    return {
      t,
      save,
      saveDisabled,
      onInput,
    };
  },
  template: `
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
        <QButton class="primary sm" :loading="saving" :disabled="saveDisabled" @click="save">{{ t("action_save") }}</QButton>
      </div>
    </section>
  `,
};

export default RawTextEditorDialogContent;
