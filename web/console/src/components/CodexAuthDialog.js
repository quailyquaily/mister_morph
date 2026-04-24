import { computed } from "vue";
import { translate } from "../core/context";
import "./CodexAuthDialog.css";

const CODEX_USAGE_URL = "https://chatgpt.com/codex/settings/usage";

const CodexAuthDialog = {
  props: {
    modelValue: Boolean,
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
  },
  emits: ["update:modelValue", "logout"],
  setup(props, { emit }) {
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

    function close() {
      emit("update:modelValue", false);
    }

    function openVerificationURL() {
      const url = String(props.verificationURL || "").trim();
      if (url) {
        window.open(url, "_blank", "noopener,noreferrer");
      }
    }

    function openCodexUsage() {
      window.open(CODEX_USAGE_URL, "_blank", "noopener,noreferrer");
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
      close,
      openVerificationURL,
      openCodexUsage,
      copyUserCode,
    };
  },
  template: `
    <QDialog
      :modelValue="modelValue"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      @close="close"
    >
      <template #header>
        <header class="app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ t("settings_codex_auth_title") }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            :disabled="busy"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
      </template>

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
    </QDialog>
  `,
};

export default CodexAuthDialog;
