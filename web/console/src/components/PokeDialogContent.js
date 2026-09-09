import { translate } from "../core/context";
import { computed } from "vue";
import "./PokeDialogContent.css";

function bindTextareaAccessibility(el, { value }) {
  // QTextarea places fallthrough attributes on its wrapper, not its textarea.
  const input = el.querySelector("textarea");
  if (!input) return;
  input.id = value.id;
  if (value.invalid) input.setAttribute("aria-describedby", value.id + "-help");
  else input.removeAttribute("aria-describedby");
  input.setAttribute("aria-invalid", String(value.invalid));
}

const PokeDialogContent = {
  directives: {
    textareaAccessibility: {
      mounted: bindTextareaAccessibility,
      updated: bindTextareaAccessibility,
    },
  },
  emits: ["submit", "update:body"],
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
    inputId: {
      type: String,
      default: "runtime-poke-body",
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
  setup(props, { emit }) {
    const t = translate;
    const message = computed(() => props.error || (props.bodyTooLarge ? t("runtime_poke_too_large") : ""));

    function updateBody(value) {
      emit("update:body", value);
    }

    function submit() {
      emit("submit");
    }

    return {
      t,
      message,
      submit,
      updateBody,
    };
  },
  template: `
    <section class="runtime-poke-dialog">
      <label class="runtime-poke-label" :for="inputId">{{ t("runtime_poke_body_label") }}</label>
      <QTextarea
        v-textarea-accessibility="{ id: inputId, invalid: !!error || bodyTooLarge }"
        class="runtime-poke-textarea"
        :modelValue="body"
        :rows="8"
        :placeholder="t('runtime_poke_body_placeholder')"
        :disabled="disabled"
        @update:modelValue="updateBody"
      />
      <p v-if="message" :id="inputId + '-help'" class="runtime-poke-error" role="alert">{{ message }}</p>
      <div class="runtime-poke-actions">
        <QButton class="primary sm" :loading="submitting" :disabled="submitDisabled" @click="submit">
          {{ t("runtime_poke_submit") }}
        </QButton>
      </div>
    </section>
  `,
};

export default PokeDialogContent;
