import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import "./SettingsView.css";

import AppPage from "../components/AppPage";
import {
  apiFetch,
  applyLanguageChange,
  clearAuth,
  localeState,
  translate,
} from "../core/context";

const SettingsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const lang = computed(() => localeState.lang);
    const loggingOut = ref(false);

    async function logout() {
      loggingOut.value = true;
      try {
        await apiFetch("/auth/logout", { method: "POST" });
      } catch {
        // ignore logout failure
      } finally {
        clearAuth();
        router.replace("/login");
        loggingOut.value = false;
      }
    }

    return {
      t,
      lang,
      loggingOut,
      logout,
      onLanguageChange: applyLanguageChange,
    };
  },
  template: `
    <AppPage :title="t('settings_title')">
      <div class="toolbar settings-toolbar">
        <div class="settings-toolbar-left">
          <QLanguageSelector :lang="lang" :presist="true" @change="onLanguageChange" />
        </div>
        <div class="settings-toolbar-right">
          <QButton class="danger" :loading="loggingOut" @click="logout">{{ t("action_logout") }}</QButton>
        </div>
      </div>
    </AppPage>
  `,
};

export default SettingsView;
