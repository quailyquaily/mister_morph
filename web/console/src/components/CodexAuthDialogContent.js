import { computed } from "vue";
import { translate } from "../core/context";
import { openExternalURL } from "../core/external-links";
import "./CodexAuthDialog.css";

const CODEX_USAGE_URL = "https://chatgpt.com/codex/settings/usage";

export const codexAuthDialogContentProps = {
  loading: Boolean,
  busy: Boolean,
  error: {
    type: String,
    default: "",
  },
  status: {
    type: Object,
    default: () => ({}),
  },
  summary: {
    type: String,
    default: "",
  },
  loginSession: {
    type: String,
    default: "",
  },
  verificationURL: {
    type: String,
    default: "",
  },
  userCode: {
    type: String,
    default: "",
  },
  loginExpiresLabel: {
    type: String,
    default: "",
  },
};

const CodexAuthDialogContent = {
  props: codexAuthDialogContentProps,
  emits: ["logout"],
  setup(props) {
    const t = translate;
    const loggedIn = computed(() => props.status?.logged_in === true);
    const accountID = computed(() => String(props.status?.account_id || "").trim());
    const introText = computed(() =>
      accountID.value ? t("settings_codex_auth_account_intro", { account: accountID.value }) : "",
    );
    const statusClass = computed(() => {
      if (props.loading) {
        return "is-loading";
      }
      return loggedIn.value ? "is-signed-in" : "is-signed-out";
    });

    function openVerificationURL() {
      const url = String(props.verificationURL || "").trim();
      if (url) {
        openExternalURL(url);
      }
    }

    function openCodexUsage() {
      openExternalURL(CODEX_USAGE_URL);
    }

    async function copyUserCode() {
      const text = String(props.userCode || "").trim();
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
      loggedIn,
      introText,
      statusClass,
      openVerificationURL,
      openCodexUsage,
      copyUserCode,
    };
  },
  template: `
    <section class="codex-auth-dialog">
      <p v-if="introText" class="codex-auth-intro">{{ introText }}</p>

      <QFence
        v-if="error"
        type="danger"
        icon="QIconCloseCircle"
        :text="error"
      />

      <div class="codex-auth-result">
        <article :class="['codex-auth-row', statusClass]">
          <div class="codex-auth-row-summary">
            <div class="codex-auth-row-main">
              <p class="codex-auth-row-title">{{ t("settings_codex_auth_session") }}</p>
              <p class="codex-auth-row-detail">
                {{ loggedIn ? t("settings_codex_auth_status_ready") : t("settings_codex_auth_status_needs_login") }}
              </p>
            </div>
            <div class="codex-auth-row-side">
              <span v-if="loading || busy" class="codex-auth-spinner" aria-hidden="true"></span>
              <strong v-else :class="['codex-auth-row-status', statusClass]">{{ summary }}</strong>
            </div>
          </div>
        </article>
      </div>

      <QFence
        v-if="status?.file_mode_ok === false"
        type="danger"
        icon="QIconCloseCircle"
        :text="status?.file_mode_warning || ''"
      />

      <div v-if="!loggedIn && !loginSession" class="codex-auth-hint">
        <p>{{ t("settings_codex_auth_set_default_note") }}</p>
      </div>

      <div v-if="loginSession" class="codex-auth-device">
        <div class="codex-auth-device-code">
          <span>{{ t("settings_codex_auth_user_code") }}</span>
          <div class="codex-auth-device-code-value">
            <strong>{{ userCode }}</strong>
            <QButton
              type="button"
              class="plain xs icon codex-auth-device-copy"
              :title="t('action_copy')"
              :aria-label="t('action_copy')"
              :disabled="!userCode"
              @click="copyUserCode"
            >
              <QIconCopy class="icon" />
            </QButton>
          </div>
        </div>
        <div class="codex-auth-device-main">
          <p class="codex-auth-device-title">{{ t("settings_codex_auth_login_pending") }}</p>
          <button
            type="button"
            class="codex-auth-device-link"
            :title="verificationURL"
            :aria-label="t('settings_codex_auth_open_verification')"
            @click="openVerificationURL"
          >
            {{ t("settings_codex_auth_open_verification") }}
          </button>
          <p class="codex-auth-device-note">{{ t("settings_codex_auth_login_expires", { time: loginExpiresLabel }) }}</p>
        </div>
      </div>

      <div class="codex-auth-actions">
        <div class="codex-auth-actions-left">
          <QButton
            v-if="loggedIn"
            class="plain xs"
            :loading="busy"
            :disabled="busy || loading"
            @click="$emit('logout')"
          >
            {{ t("action_logout") }}
          </QButton>
        </div>
        <QButton
          type="button"
          class="plain xs codex-auth-usage"
          :title="t('settings_codex_auth_usage')"
          :aria-label="t('settings_codex_auth_usage')"
          @click="openCodexUsage"
        >
          {{ t("settings_codex_auth_usage") }}
          <QIconArrowUpRight class="icon" />
        </QButton>
      </div>
    </section>
  `,
};

export default CodexAuthDialogContent;
