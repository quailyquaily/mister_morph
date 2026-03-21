import { computed } from "vue";
import { translate } from "../core/context";
import "./RawJsonDialog.css";

const RawJsonDialog = {
  emits: ["close"],
  props: {
    open: {
      type: Boolean,
      default: false,
    },
    json: {
      type: String,
      default: "",
    },
    title: {
      type: String,
      default: "",
    },
  },
  setup(props, { emit }) {
    const t = translate;
    const resolvedTitle = computed(() => props.title || "RAW JSON");

    function close() {
      emit("close");
    }

    async function copy() {
      const text = String(props.json || "");
      if (!text) {
        return;
      }
      try {
        if (navigator?.clipboard?.writeText) {
          await navigator.clipboard.writeText(text);
          return;
        }
      } catch {}
      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "true");
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      textarea.style.pointerEvents = "none";
      document.body.appendChild(textarea);
      textarea.select();
      try {
        document.execCommand("copy");
      } finally {
        document.body.removeChild(textarea);
      }
    }

    return {
      t,
      close,
      copy,
      resolvedTitle,
    };
  },
  template: `
    <div v-if="open" class="raw-json-overlay" @click.self="close">
      <section class="raw-json-dialog frame">
        <header class="raw-json-head">
          <div class="raw-json-copy">
            <h3 class="raw-json-title">{{ resolvedTitle }}</h3>
          </div>
          <div class="raw-json-actions">
            <QButton class="plain sm" @click="close">{{ t("action_close") }}</QButton>
            <QButton class="plain sm" @click="copy">{{ t("action_copy") }}</QButton>
          </div>
        </header>
        <pre class="raw-json-body">{{ json }}</pre>
      </section>
    </div>
  `,
};

export default RawJsonDialog;
