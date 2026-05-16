import { computed } from "vue";
import AppDialogShell from "./AppDialogShell";
import SetupPickerDialogContent, { setupPickerDialogContentProps } from "./SetupPickerDialogContent";
import { useDesktopPayloadDialog } from "../core/desktop-payload-dialog";
import {
  openSetupPickerDesktopWindow,
  SETUP_PICKER_WINDOW_ID,
} from "../core/desktop-windows";

const SetupPickerDialog = {
  components: {
    AppDialogShell,
    SetupPickerDialogContent,
  },
  props: {
    modelValue: Boolean,
    title: {
      type: String,
      default: "",
    },
    ...setupPickerDialogContentProps,
  },
  emits: ["update:modelValue", "select"],
  setup(props, { emit }) {
    const resolvedTitle = computed(() => String(props.title || "").trim());

    function payload() {
      return {
        items: Array.isArray(props.items) ? props.items : [],
        loading: props.loading === true,
        error: String(props.error || ""),
        title: resolvedTitle.value,
        filterPlaceholder: String(props.filterPlaceholder || ""),
        emptyText: String(props.emptyText || ""),
        showValue: props.showValue !== false,
      };
    }

    function close() {
      emit("update:modelValue", false);
    }

    function selectItem(item) {
      emit("select", item);
      close();
    }

    const desktopDialog = useDesktopPayloadDialog({
      open: () => props.modelValue,
      windowID: SETUP_PICKER_WINDOW_ID,
      title: () => resolvedTitle.value,
      payload,
      openWindow: openSetupPickerDesktopWindow,
      close,
      onMessage(message) {
        if (message?.type === "setup-picker:selected") {
          selectItem(message?.payload?.item || null);
        }
      },
    });

    return {
      resolvedTitle,
      webDialogOpen: desktopDialog.webDialogOpen,
      close,
      selectItem,
      desktopRequestID: desktopDialog.requestID,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="webDialogOpen"
      :title="resolvedTitle"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      :closeDisabled="loading"
      @close="close"
    >
      <SetupPickerDialogContent
        :items="items"
        :loading="loading"
        :error="error"
        :filterPlaceholder="filterPlaceholder"
        :emptyText="emptyText"
        :showValue="showValue"
        :resetKey="desktopRequestID"
        @select="selectItem"
      />
    </AppDialogShell>
  `,
};

export default SetupPickerDialog;
