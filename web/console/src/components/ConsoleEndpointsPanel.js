import { computed, reactive, watch } from "vue";

let endpointKey = 0;

function endpointDraft(item = {}) {
  const name = String(item?.name || "");
  endpointKey += 1;
  return {
    _key: `console-endpoint-${endpointKey}`,
    _originalName: String(item?.original_name || name),
    _configured: item?.auth_token_configured === true,
    name,
    url: String(item?.url || ""),
    auth_token: "",
  };
}

export default {
  name: "ConsoleEndpointsPanel",
  props: {
    endpoints: { type: Array, default: () => [] },
    loading: Boolean,
    saving: Boolean,
  },
  emits: ["save"],
  setup(props, { emit }) {
    const draft = reactive([]);

    function replaceDraft() {
      draft.splice(0, draft.length, ...props.endpoints.map(endpointDraft));
    }

    watch(() => props.endpoints, replaceDraft, { deep: true, immediate: true });

    const valid = computed(() => draft.every((item) => {
      if (!item.name.trim() || !item.url.trim()) return false;
      return item._configured || item.auth_token.trim() !== "";
    }));

    function add() {
      draft.push(endpointDraft());
    }

    function remove(index) {
      draft.splice(index, 1);
    }

    function save() {
      emit("save", draft.map((item) => ({
        original_name: item._originalName,
        name: item.name.trim(),
        url: item.url.trim(),
        auth_token: item.auth_token.trim(),
      })));
    }

    return { draft, valid, add, remove, save };
  },
  template: `
    <QCard variant="default" class="config-settings-group">
      <div class="settings-panel-shell">
        <header class="settings-panel-head">
          <div class="settings-panel-copy">
            <h3 class="settings-panel-title workspace-document-title">Remote Morphs</h3>
            <p class="settings-panel-meta">Other Morph instances this Console can control. Each access token must match the remote Morph's incoming access token.</p>
          </div>
          <div class="settings-panel-actions">
            <span class="config-settings-restart">Restart required</span>
            <QButton class="primary" :loading="saving" :disabled="loading || saving || !valid" @click="save">Save</QButton>
          </div>
        </header>
        <div class="settings-panel-body settings-collection-list">
          <div v-for="(endpoint, index) in draft" :key="endpoint._key" class="settings-collection-item">
            <div class="settings-form-grid">
              <div class="settings-field">
                <span class="settings-field-label">Name</span>
                <QInput v-model="endpoint.name" :disabled="loading || saving" />
              </div>
              <div class="settings-field">
                <span class="settings-field-label">Runtime API URL</span>
                <QInput v-model="endpoint.url" placeholder="https://agent.example.com/runtime" :disabled="loading || saving" />
              </div>
              <div class="settings-field is-wide">
                <span class="settings-field-label">Access token</span>
                <QInput
                  v-model="endpoint.auth_token"
                  inputType="password"
                  :placeholder="endpoint._configured ? 'Configured — enter a new value to replace' : 'Required'"
                  :disabled="loading || saving"
                />
              </div>
            </div>
            <QButton class="plain xs danger" :disabled="loading || saving" @click="remove(index)">Remove</QButton>
          </div>
          <QButton class="placeholder" :disabled="loading || saving" @click="add">Add Morph</QButton>
        </div>
      </div>
    </QCard>
  `,
};
