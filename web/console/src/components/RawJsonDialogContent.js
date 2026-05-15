import { translate } from "../core/context";
import "./RawJsonDialog.css";

export const rawJsonDialogContentProps = {
  json: {
    type: String,
    default: "",
  },
};

const RawJsonDialogContent = {
  props: rawJsonDialogContentProps,
  setup(props) {
    const t = translate;

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
      copy,
    };
  },
  template: `
    <section class="raw-json-dialog">
      <div class="raw-json-tools">
        <QButton class="outlined xs" @click="copy">{{ t("action_copy") }}</QButton>
      </div>
      <pre class="raw-json-body">{{ json }}</pre>
    </section>
  `,
};

export default RawJsonDialogContent;
