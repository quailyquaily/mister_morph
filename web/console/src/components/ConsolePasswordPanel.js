import { ref, watch } from "vue";

import SettingDialog from "./SettingDialog";

export default {
  name: "ConsolePasswordPanel",
  components: { SettingDialog },
  props: {
    configured: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
  },
  emits: ["save"],
  setup(props, { emit }) {
    const open = ref(false);
    const password = ref("");
    const confirmation = ref("");
    const error = ref("");

    watch(open, (value) => {
      if (!value) {
        password.value = "";
        confirmation.value = "";
        error.value = "";
      }
    });

    function submit() {
      if (!password.value) {
        error.value = "Password cannot be empty.";
        return;
      }
      if (password.value !== confirmation.value) {
        error.value = "Passwords do not match.";
        return;
      }
      emit("save", { new_password: password.value });
      open.value = false;
    }

    function clear() {
      emit("save", { clear_password: true });
    }

    return { open, password, confirmation, error, submit, clear };
  },
  template: `
    <QCard variant="default" class="config-settings-group">
      <div class="settings-panel-shell">
        <header class="settings-panel-head">
          <div class="settings-panel-copy">
            <h3 class="settings-panel-title workspace-document-title">Web Console sign-in</h3>
            <p class="settings-panel-meta">Protects browser access to this Console. It is separate from the incoming Runtime API access token.</p>
          </div>
          <div class="settings-panel-actions">
            <span class="config-settings-restart">Restart required</span>
            <QButton class="primary" :disabled="saving" @click="open = true">
              {{ configured ? "Change password" : "Set password" }}
            </QButton>
            <QButton v-if="configured" class="danger plain" :disabled="saving" @click="clear">Clear</QButton>
          </div>
        </header>
      </div>

      <SettingDialog
        v-model="open"
        title="Set Web Console password"
        width="460px"
        :saving="saving"
        @save="submit"
      >
        <div class="settings-password-dialog">
          <div v-if="error" class="config-settings-error" role="alert">{{ error }}</div>
          <label class="settings-field">
            <span class="settings-field-label">New password</span>
            <QInput v-model="password" inputType="password" :disabled="saving" />
          </label>
          <label class="settings-field">
            <span class="settings-field-label">Confirm password</span>
            <QInput v-model="confirmation" inputType="password" :disabled="saving" @keyup.enter="submit" />
          </label>
        </div>
      </SettingDialog>
    </QCard>
  `,
};
