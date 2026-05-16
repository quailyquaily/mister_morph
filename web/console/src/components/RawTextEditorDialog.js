import { computed } from "vue";
import { translate } from "../core/context";
import { useDesktopPayloadDialog } from "../core/desktop-payload-dialog";
import AppDialogShell from "./AppDialogShell";
import RawTextEditorDialogContent, { rawTextEditorDialogContentProps } from "./RawTextEditorDialogContent";
import { openRawTextEditorDesktopWindow, RAW_TEXT_EDITOR_WINDOW_ID } from "../core/desktop-windows";

const RawTextEditorDialog = {
  components: {
    AppDialogShell,
    RawTextEditorDialogContent,
  },
  emits: ["close", "save", "update:modelValue"],
  props: {
    open: {
      type: Boolean,
      default: false,
    },
    title: {
      type: String,
      default: "",
    },
    ...rawTextEditorDialogContentProps,
  },
  setup(props, { emit }) {
    const t = translate;
    const resolvedTitle = computed(() => props.title || t("repair_editor_title"));

    function payload() {
      return {
        title: resolvedTitle.value,
        path: String(props.path || ""),
        modelValue: String(props.modelValue || ""),
        loading: props.loading === true,
        saving: props.saving === true,
      };
    }

    function close() {
      emit("close");
    }

    function save() {
      emit("save");
    }

    function onInput(value) {
      emit("update:modelValue", String(value || ""));
    }

    const desktopDialog = useDesktopPayloadDialog({
      open: () => props.open,
      windowID: RAW_TEXT_EDITOR_WINDOW_ID,
      title: () => resolvedTitle.value,
      payload,
      openWindow: openRawTextEditorDesktopWindow,
      close,
      onMessage(message) {
        if (message?.type === "raw-text-editor:save") {
          emit("update:modelValue", String(message?.payload?.content || ""));
          save();
        }
      },
    });

    return {
      t,
      close,
      save,
      onInput,
      resolvedTitle,
      webDialogOpen: desktopDialog.webDialogOpen,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="webDialogOpen"
      :title="resolvedTitle"
      width="920px"
      :closeDisabled="saving"
      @close="close"
    >
      <RawTextEditorDialogContent
        :path="path"
        :modelValue="modelValue"
        :loading="loading"
        :saving="saving"
        @update:modelValue="onInput"
        @save="save"
      />
    </AppDialogShell>
  `,
};

export default RawTextEditorDialog;
