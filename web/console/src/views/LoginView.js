import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./LoginView.css";
import loginLogoUrl from "../assets/images/app_logo.svg";

import {
  apiFetch,
  authState,
  ensureConsoleSession,
  endpointState,
  loadEndpoints,
  localeState,
  translate,
} from "../core/context";
import { consoleSetupTargetEndpointRef, resolveConsoleSetupStage, setupStagePath } from "../core/setup";

const LoginView = {
  setup() {
    const router = useRouter();
    const route = useRoute();
    const t = translate;
    const lang = computed(() => localeState.lang);
    const password = ref("");
    const busy = ref(false);
    const err = ref("");

    async function finishLogin() {
      await loadEndpoints();

      const setupState = await resolveConsoleSetupStage(endpointState.items);
      const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/overview";
      if (setupState.stage !== "ready") {
        const next = { path: setupStagePath(setupState.stage) };
        if (redirect && redirect !== "/overview" && redirect !== "/") {
          next.query = { redirect };
        }
        router.replace(next);
        return;
      }
      const targetRef = consoleSetupTargetEndpointRef(setupState.setup);
      if (targetRef) {
        endpointState.setSelectedEndpointRef(targetRef);
      }
      if (redirect && redirect !== "/overview" && redirect !== "/") {
        router.replace(redirect);
        return;
      }
      if (targetRef) {
        router.replace("/chat");
        return;
      }
      router.replace("/overview");
    }

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
        authState.save();
        await finishLogin();
      } catch (e) {
        err.value = e.message || t("login_failed");
      } finally {
        busy.value = false;
      }
    }

    onMounted(async () => {
      if (busy.value) {
        return;
      }
      busy.value = true;
      err.value = "";
      try {
        const ok = await ensureConsoleSession();
        if (ok) {
          await finishLogin();
        }
      } catch {
      } finally {
        busy.value = false;
      }
    });

    return {
      t,
      lang,
      password,
      busy,
      err,
      submit,
      onLanguageChange: localeState.applyLanguageChange,
    };
  },
  template: `
    <section class="login-box">
      <div class="login-brand">
        <span class="login-brand-mark" aria-hidden="true">
          <img class="login-brand-logo" src="${loginLogoUrl}" alt="" role="presentation" />
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
