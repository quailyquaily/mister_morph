import { computed } from "vue";
import { translate } from "../core/context";
import { openExternalURL } from "../core/external-links";
import "./CodexAuthDialog.css";

export const deviceAuthStateProps = {
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

export const deviceAuthDialogContentProps = {
  ...deviceAuthStateProps,
  accountLabel: {
    type: String,
    default: "",
  },
  accountIntroKey: {
    type: String,
    default: "",
  },
  sessionKey: {
    type: String,
    default: "",
  },
  statusReadyKey: {
    type: String,
    default: "",
  },
  statusNeedsLoginKey: {
    type: String,
    default: "",
  },
  setDefaultNoteKey: {
    type: String,
    default: "",
  },
  loginPendingKey: {
    type: String,
    default: "",
  },
  loginExpiresKey: {
    type: String,
    default: "",
  },
  openVerificationKey: {
    type: String,
    default: "",
  },
  userCodeKey: {
    type: String,
    default: "settings_codex_auth_user_code",
  },
  extraActionKey: {
    type: String,
    default: "",
  },
  extraActionURL: {
    type: String,
    default: "",
  },
};

const DeviceAuthDialogContent = {
  props: deviceAuthDialogContentProps,
  emits: ["logout"],
  setup(props) {
    const t = translate;
    const loggedIn = computed(() => props.status?.logged_in === true);
    const introText = computed(() => {
      const account = String(props.accountLabel || "").trim();
      return account && props.accountIntroKey ? t(props.accountIntroKey, { account }) : "";
    });
    const statusClass = computed(() => {
      if (props.loading) {
        return "is-loading";
      }
      return loggedIn.value ? "is-signed-in" : "is-signed-out";
    });
    const extraActionLabel = computed(() => (props.extraActionKey ? t(props.extraActionKey) : ""));
    const showExtraAction = computed(() => extraActionLabel.value !== "" && String(props.extraActionURL || "").trim() !== "");

    function openVerificationURL() {
      const url = String(props.verificationURL || "").trim();
      if (url) {
        openExternalURL(url);
      }
    }

    function openExtraAction() {
      const url = String(props.extraActionURL || "").trim();
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
      extraActionLabel,
      showExtraAction,
      openVerificationURL,
      openExtraAction,
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
              <p class="codex-auth-row-title">{{ t(sessionKey) }}</p>
              <p class="codex-auth-row-detail">
                {{ loggedIn ? t(statusReadyKey) : t(statusNeedsLoginKey) }}
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
        <p>{{ t(setDefaultNoteKey) }}</p>
      </div>

      <div v-if="loginSession" class="codex-auth-device">
        <div class="codex-auth-device-code">
          <span>{{ t(userCodeKey) }}</span>
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
          <p class="codex-auth-device-title">{{ t(loginPendingKey) }}</p>
          <button
            type="button"
            class="codex-auth-device-link"
            :title="verificationURL"
            :aria-label="t(openVerificationKey)"
            @click="openVerificationURL"
          >
            {{ t(openVerificationKey) }}
          </button>
          <p class="codex-auth-device-note">{{ t(loginExpiresKey, { time: loginExpiresLabel }) }}</p>
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
          v-if="showExtraAction"
          type="button"
          class="plain xs codex-auth-usage"
          :title="extraActionLabel"
          :aria-label="extraActionLabel"
          @click="openExtraAction"
        >
          {{ extraActionLabel }}
          <QIconArrowUpRight class="icon" />
        </QButton>
      </div>
    </section>
  `,
};

export default DeviceAuthDialogContent;
