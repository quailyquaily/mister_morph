import { translate } from "../core/context";
import "./PokeDialogContent.css";

const PokeDialogContent = {
  emits: ["cancel", "submit", "update:body"],
  props: {
    body: {
      type: String,
      default: "",
    },
    bodyTooLarge: {
      type: Boolean,
      default: false,
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    error: {
      type: String,
      default: "",
    },
    helperText: {
      type: String,
      default: "",
    },
    inputId: {
      type: String,
      default: "runtime-poke-body",
    },
    sizeLabel: {
      type: String,
      default: "",
    },
    submitDisabled: {
      type: Boolean,
      default: false,
    },
    submitting: {
      type: Boolean,
      default: false,
    },
  },
  setup(_props, { emit }) {
    const t = translate;

    function updateBody(value) {
      emit("update:body", value);
    }

    function cancel() {
      emit("cancel");
    }

    function submit() {
      emit("submit");
    }

    return {
      t,
      cancel,
      submit,
      updateBody,
    };
  },
  template: `
    <section class="runtime-poke-dialog">
      <p class="runtime-poke-hint">{{ t("runtime_poke_dialog_hint") }}</p>
      <label class="runtime-poke-label" :for="inputId">{{ t("runtime_poke_body_label") }}</label>
      <QTextarea
        :id="inputId"
        class="runtime-poke-textarea"
        :modelValue="body"
        :rows="8"
        :placeholder="t('runtime_poke_body_placeholder')"
        :disabled="disabled"
        @update:modelValue="updateBody"
      />
      <div class="runtime-poke-meta">
        <span :class="{ 'is-danger': error || bodyTooLarge }">{{ helperText }}</span>
        <span>{{ sizeLabel }}</span>
      </div>
      <div class="runtime-poke-actions">
        <QButton class="outlined sm" :disabled="submitting" @click="cancel">{{ t("action_cancel") }}</QButton>
        <QButton class="primary sm" :loading="submitting" :disabled="submitDisabled" @click="submit">
          {{ t("runtime_poke_submit") }}
        </QButton>
      </div>
    </section>
  `,
};

export default PokeDialogContent;
