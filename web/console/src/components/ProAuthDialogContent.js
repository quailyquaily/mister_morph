import { computed } from "vue";
import { translate } from "../core/context";
import { openExternalURL } from "../core/external-links";
import "./CodexAuthDialog.css";

export const proAuthDialogContentProps = {
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

function proUserLabel(status) {
  const user = status && typeof status.user === "object" && status.user ? status.user : {};
  return (
    (typeof user.name === "string" && user.name.trim()) ||
    (typeof user.email === "string" && user.email.trim()) ||
    (typeof user.union_id === "string" && user.union_id.trim()) ||
    ""
  );
}

const ProAuthDialogContent = {
  props: proAuthDialogContentProps,
  emits: ["logout"],
  setup(props) {
    const t = translate;
    const loggedIn = computed(() => props.status?.logged_in === true);
    const accountLabel = computed(() => proUserLabel(props.status));
    const introText = computed(() =>
      accountLabel.value ? t("settings_pro_auth_account_intro", { account: accountLabel.value }) : ""
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
              <p class="codex-auth-row-title">{{ t("settings_pro_auth_session") }}</p>
              <p class="codex-auth-row-detail">
                {{ loggedIn ? t("settings_pro_auth_status_ready") : t("settings_pro_auth_status_needs_login") }}
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
        <p>{{ t("settings_pro_auth_set_default_note") }}</p>
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
          <p class="codex-auth-device-title">{{ t("settings_pro_auth_login_pending") }}</p>
          <button
            type="button"
            class="codex-auth-device-link"
            :title="verificationURL"
            :aria-label="t('settings_pro_auth_open_verification')"
            @click="openVerificationURL"
          >
            {{ t("settings_pro_auth_open_verification") }}
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
      </div>
    </section>
  `,
};

export default ProAuthDialogContent;
