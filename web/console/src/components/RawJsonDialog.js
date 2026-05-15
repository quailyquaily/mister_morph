import AppDialogShell from "./AppDialogShell";
import RawJsonDialogContent, { rawJsonDialogContentProps } from "./RawJsonDialogContent";

const RawJsonDialog = {
  components: {
    AppDialogShell,
    RawJsonDialogContent,
  },
  emits: ["close"],
  props: {
    open: {
      type: Boolean,
      default: false,
    },
    title: {
      type: String,
      default: "",
    },
    ...rawJsonDialogContentProps,
  },
  setup(_props, { emit }) {
    function close() {
      emit("close");
    }

    return {
      close,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="open"
      :title="title || 'RAW JSON'"
      width="860px"
      @close="close"
    >
      <RawJsonDialogContent :json="json" />
    </AppDialogShell>
  `,
};

export default RawJsonDialog;
