import AppDialogShell from "./AppDialogShell";
import SetupConnectionTestDialogContent, {
  setupConnectionTestDialogContentProps,
} from "./SetupConnectionTestDialogContent";
import { translate } from "../core/context";
import { useDesktopPayloadDialog } from "../core/desktop-payload-dialog";
import {
  openSetupConnectionTestDesktopWindow,
  SETUP_CONNECTION_TEST_WINDOW_ID,
} from "../core/desktop-windows";

const SetupConnectionTestDialog = {
  components: {
    AppDialogShell,
    SetupConnectionTestDialogContent,
  },
  props: {
    modelValue: Boolean,
    ...setupConnectionTestDialogContentProps,
  },
  emits: ["update:modelValue", "retry"],
  setup(props, { emit }) {
    const t = translate;

    function payload() {
      return {
        loading: props.loading === true,
        error: String(props.error || ""),
        benchmarks: Array.isArray(props.benchmarks) ? props.benchmarks : [],
        provider: String(props.provider || ""),
        apiBase: String(props.apiBase || ""),
        model: String(props.model || ""),
        showIntro: props.showIntro !== false,
      };
    }

    function close() {
      emit("update:modelValue", false);
    }

    function retry() {
      emit("retry");
    }

    const desktopDialog = useDesktopPayloadDialog({
      open: () => props.modelValue,
      windowID: SETUP_CONNECTION_TEST_WINDOW_ID,
      title: () => t("setup_llm_test_title"),
      payload,
      openWindow: openSetupConnectionTestDesktopWindow,
      close,
      onMessage(message) {
        if (message?.type === "setup-connection-test:retry") {
          retry();
        }
      },
    });

    return {
      t,
      close,
      retry,
      webDialogOpen: desktopDialog.webDialogOpen,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="webDialogOpen"
      :title="t('setup_llm_test_title')"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      @close="close"
    >
      <SetupConnectionTestDialogContent
        :loading="loading"
        :error="error"
        :benchmarks="benchmarks"
        :provider="provider"
        :apiBase="apiBase"
        :model="model"
        :showIntro="showIntro"
        @retry="retry"
        @close="close"
      />
    </AppDialogShell>
  `,
};

export default SetupConnectionTestDialog;
