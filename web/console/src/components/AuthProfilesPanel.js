import { computed, reactive, ref, watch } from "vue";
import SettingChoices from "./SettingChoices";
import SettingSelect from "./SettingSelect";
import { HTTP_METHOD_OPTIONS } from "../core/config-options";

let profileKey = 0;

function listText(values) {
  return Array.isArray(values) ? values.join("\n") : "";
}

function parseList(value) {
  return String(value || "").split(/\r?\n|,/).map((item) => item.trim()).filter(Boolean);
}

function profileDraft(item = {}) {
  profileKey += 1;
  const name = String(item?.name || "");
  return {
    _key: `auth-profile-${profileKey}`,
    _originalName: String(item?.original_name || name),
    _configured: item?.credential_secret_configured === true,
    name,
    credential_kind: String(item?.credential_kind || "bearer"),
    credential_secret: "",
    url_prefixes_text: listText(item?.url_prefixes),
    methods_text: listText(item?.methods),
    follow_redirects: item?.follow_redirects === true,
    allow_proxy: item?.allow_proxy === true,
    deny_private_ips: item?.deny_private_ips !== false,
    bindings_text: JSON.stringify(item?.bindings && typeof item.bindings === "object" ? item.bindings : {
      url_fetch: { inject: { location: "header", name: "Authorization", format: "bearer" } },
    }, null, 2),
  };
}

export default {
  name: "AuthProfilesPanel",
  components: { SettingChoices, SettingSelect },
  props: {
    profiles: { type: Array, default: () => [] },
    loading: Boolean,
    saving: Boolean,
  },
  emits: ["save"],
  setup(props, { emit }) {
    const draft = reactive([]);
    const error = ref("");
    watch(() => props.profiles, () => {
      draft.splice(0, draft.length, ...props.profiles.map(profileDraft));
      error.value = "";
    }, { deep: true, immediate: true });

    const complete = computed(() => draft.every((item) =>
      item.name.trim() && item.credential_kind.trim() &&
      (item._configured || item.credential_secret.trim()) &&
      parseList(item.url_prefixes_text).length > 0 && parseList(item.methods_text).length > 0,
    ));

    function add() {
      draft.push(profileDraft());
    }

    function remove(index) {
      draft.splice(index, 1);
    }

    function bindingsFor(profile) {
      try {
        const bindings = JSON.parse(profile.bindings_text || "{}");
        return bindings && typeof bindings === "object" && !Array.isArray(bindings) ? bindings : {};
      } catch {
        return {};
      }
    }

    function updateInjection(profile, tool, field, value) {
      const bindings = bindingsFor(profile);
      bindings[tool] = {
        ...bindings[tool],
        inject: { ...bindings[tool]?.inject, [field]: value },
      };
      profile.bindings_text = JSON.stringify(bindings, null, 2);
    }

    function save() {
      try {
        const profiles = draft.map((item) => ({
          original_name: item._originalName,
          name: item.name.trim(),
          credential_kind: item.credential_kind.trim(),
          credential_secret: item.credential_secret.trim(),
          url_prefixes: parseList(item.url_prefixes_text),
          methods: parseList(item.methods_text).map((method) => method.toUpperCase()),
          follow_redirects: item.follow_redirects,
          allow_proxy: item.allow_proxy,
          deny_private_ips: item.deny_private_ips,
          bindings: JSON.parse(item.bindings_text || "{}"),
        }));
        error.value = "";
        emit("save", profiles);
      } catch {
        error.value = "Bindings must be valid JSON.";
      }
    }

    return { draft, error, complete, add, remove, save, parseList, bindingsFor, updateInjection, HTTP_METHOD_OPTIONS };
  },
  template: `
    <QCard variant="default" class="config-settings-group">
      <div class="settings-panel-shell">
        <header class="settings-panel-head">
          <div class="settings-panel-copy">
            <h3 class="settings-panel-title workspace-document-title">Auth profiles</h3>
            <p class="settings-panel-meta">Credentials and request limits used by authenticated HTTP tools.</p>
          </div>
          <QButton class="primary" :loading="saving" :disabled="loading || saving || !complete" @click="save">Save</QButton>
        </header>
        <div class="settings-panel-body settings-collection-list">
          <div v-if="error" class="config-settings-error" role="alert">{{ error }}</div>
          <div v-for="(profile, index) in draft" :key="profile._key" class="settings-collection-item">
            <div class="settings-form-grid">
              <div class="settings-field">
                <span class="settings-field-label">Name</span>
                <QInput v-model="profile.name" :disabled="loading || saving" />
              </div>
              <div class="settings-field">
                <span class="settings-field-label">Credential kind</span>
                <QInput v-model="profile.credential_kind" :disabled="loading || saving" />
              </div>
              <div class="settings-field is-wide">
                <span class="settings-field-label">Credential secret</span>
                <QInput
                  v-model="profile.credential_secret"
                  inputType="password"
                  :placeholder="profile._configured ? 'Configured — enter a new value to replace' : 'Required'"
                  :disabled="loading || saving"
                />
              </div>
              <div class="settings-field">
                <span class="settings-field-label">Allowed URL prefixes</span>
                <QTextarea v-model="profile.url_prefixes_text" :rows="4" :disabled="loading || saving" />
              </div>
              <div class="settings-field">
                <span class="settings-field-label">Allowed methods</span>
                <SettingChoices
                  :modelValue="parseList(profile.methods_text).map(method => method.toUpperCase())"
                  :options="HTTP_METHOD_OPTIONS"
                  label="Allowed methods"
                  :disabled="loading || saving"
                  @update:modelValue="profile.methods_text = $event.join('\\n')"
                />
              </div>
              <div class="settings-field is-wide">
                <span class="settings-field-label">Bindings</span>
                <div v-for="(binding, tool) in bindingsFor(profile)" :key="tool" class="settings-binding">
                  <strong class="settings-field-label">{{ tool }}</strong>
                  <div class="settings-form-grid">
                    <div class="settings-field">
                      <span class="settings-field-label">Injection location</span>
                      <SettingSelect
                        :modelValue="binding?.inject?.location || ''"
                        :options="['header']"
                        label="Injection location"
                        :disabled="loading || saving"
                        @update:modelValue="updateInjection(profile, tool, 'location', $event)"
                      />
                    </div>
                    <div class="settings-field">
                      <span class="settings-field-label">Injection format</span>
                      <SettingSelect
                        :modelValue="binding?.inject?.format || ''"
                        :options="['', 'raw', 'bearer', 'basic']"
                        placeholder="Default (raw)"
                        label="Injection format"
                        :disabled="loading || saving"
                        @update:modelValue="updateInjection(profile, tool, 'format', $event)"
                      />
                    </div>
                    <div class="settings-field is-wide">
                      <span class="settings-field-label">Header name</span>
                      <QInput
                        :modelValue="binding?.inject?.name || ''"
                        :disabled="loading || saving"
                        @update:modelValue="updateInjection(profile, tool, 'name', $event)"
                      />
                    </div>
                  </div>
                </div>
                <details class="settings-bindings-advanced">
                  <summary>Advanced bindings (JSON)</summary>
                  <QTextarea v-model="profile.bindings_text" :rows="8" class="config-settings-json" :disabled="loading || saving" />
                </details>
              </div>
              <div class="settings-field-row is-wide is-three">
                <QSwitch v-model="profile.deny_private_ips" label="Deny private IPs" :disabled="loading || saving" />
                <QSwitch v-model="profile.follow_redirects" label="Follow redirects" :disabled="loading || saving" />
                <QSwitch v-model="profile.allow_proxy" label="Allow proxy" :disabled="loading || saving" />
              </div>
            </div>
            <QButton class="plain xs danger" :disabled="loading || saving" @click="remove(index)">Remove</QButton>
          </div>
          <QButton class="placeholder" :disabled="loading || saving" @click="add">Add auth profile</QButton>
        </div>
      </div>
    </QCard>
  `,
};
