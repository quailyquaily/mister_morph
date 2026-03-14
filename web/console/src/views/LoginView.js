import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./LoginView.css";

import {
  apiFetch,
  applyLanguageChange,
  authState,
  endpointState,
  loadEndpoints,
  localeState,
  saveAuth,
  translate,
} from "../core/context";

const LoginView = {
  setup() {
    const router = useRouter();
    const route = useRoute();
    const t = translate;
    const lang = computed(() => localeState.lang);
    const password = ref("");
    const busy = ref(false);
    const err = ref("");

    async function submit() {
      if (busy.value) {
        return;
      }
      if (!password.value.trim()) {
        err.value = t("login_required_password");
        return;
      }
      busy.value = true;
      err.value = "";
      try {
        const body = await apiFetch("/auth/login", {
          method: "POST",
          body: { password: password.value },
          noAuth: true,
        });
        authState.token = body.access_token || "";
        authState.expiresAt = body.expires_at || "";
        authState.account = "console";
        saveAuth();
        await loadEndpoints();

        const connected = endpointState.items.filter((item) => item && item.connected === true);
        const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/overview";
        if (redirect && redirect !== "/overview" && redirect !== "/") {
          router.replace(redirect);
          return;
        }
        if (connected.length >= 1) {
          router.replace("/chat");
          return;
        }
        router.replace("/overview");
      } catch (e) {
        err.value = e.message || t("login_failed");
      } finally {
        busy.value = false;
      }
    }

    return { t, lang, password, busy, err, submit, onLanguageChange: applyLanguageChange };
  },
  template: `
    <section class="login-box">
      <div class="login-brand">
        <span class="login-brand-mark" aria-hidden="true">
          <svg class="login-brand-logo" viewBox="0 0 24 24" role="presentation">
            <path d="M3 11h18" />
            <path d="M5 11V7a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v4" />
            <path d="M7 17m-3 0a3 3 0 1 0 6 0a3 3 0 1 0-6 0" />
            <path d="M17 17m-3 0a3 3 0 1 0 6 0a3 3 0 1 0-6 0" />
            <path d="M10 17h4" />
          </svg>
        </span>
        <h1 class="login-title">Mister Morph Console</h1>
      </div>
      <form class="stack" @submit.prevent="submit">
        <QInput
          v-model="password"
          inputType="password"
          :placeholder="t('login_password_placeholder')"
          :disabled="busy"
          @keydown.enter.prevent="submit"
        />
        <QButton :loading="busy" class="primary" @click="submit">{{ t("login_button") }}</QButton>
        <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      </form>
      <div class="login-footer">
        <div class="login-divider" aria-hidden="true"></div>
        <div class="login-language">
          <QLanguageSelector :lang="lang" :presist="true" @change="onLanguageChange" />
        </div>
      </div>
    </section>
  `,
};


export default LoginView;
