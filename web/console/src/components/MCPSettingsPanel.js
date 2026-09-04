import { computed } from "vue";

import { translate } from "../core/context";
import "./MCPSettingsPanel.css";

let mcpServerKeySeed = 0;
let mcpPairKeySeed = 0;

function nextServerKey() {
  mcpServerKeySeed += 1;
  return `mcp-server-${mcpServerKeySeed}`;
}

function nextPairKey() {
  mcpPairKeySeed += 1;
  return `mcp-pair-${mcpPairKeySeed}`;
}

function clonePair(row) {
  return {
    _key: String(row?._key || nextPairKey()),
    key: String(row?.key || ""),
    value: String(row?.value || ""),
  };
}

function cloneServer(server) {
  return {
    ...server,
    _key: String(server?._key || nextServerKey()),
    env_rows: Array.isArray(server?.env_rows) ? server.env_rows.map(clonePair) : [],
    header_rows: Array.isArray(server?.header_rows) ? server.header_rows.map(clonePair) : [],
  };
}

function emptyServer() {
  return {
    _key: nextServerKey(),
    name: "",
    enable: true,
    type: "stdio",
    command: "",
    args_text: "",
    env_rows: [],
    url: "",
    header_rows: [],
    allowed_tools_text: "",
  };
}

const MCPSettingsPanel = {
  props: {
    modelValue: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
    readOnly: { type: Boolean, default: false },
    readOnlyMessage: { type: String, default: "" },
    validationError: { type: String, default: "" },
    saveDisabled: { type: Boolean, default: false },
  },
  emits: ["update:modelValue", "save"],
  setup(props, { emit }) {
    const t = translate;
    const servers = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []));
    const busy = computed(() => props.loading || props.saving || props.readOnly);

    function updateServers(mutator) {
      const next = servers.value.map(cloneServer);
      mutator(next);
      emit("update:modelValue", next);
    }

    function addServer() {
      updateServers((next) => next.push(emptyServer()));
    }

    function removeServer(index) {
      updateServers((next) => next.splice(index, 1));
    }

    function updateServer(index, field, value) {
      updateServers((next) => {
        if (!next[index]) {
          return;
        }
        next[index][field] = field === "enable" ? !!value : String(value ?? "");
      });
    }

    function addPair(index, field) {
      updateServers((next) => {
        next[index]?.[field]?.push({ _key: nextPairKey(), key: "", value: "" });
      });
    }

    function updatePair(index, field, pairIndex, key, value) {
      updateServers((next) => {
        const row = next[index]?.[field]?.[pairIndex];
        if (row) {
          row[key] = String(value ?? "");
        }
      });
    }

    function removePair(index, field, pairIndex) {
      updateServers((next) => next[index]?.[field]?.splice(pairIndex, 1));
    }

    return {
      t,
      servers,
      busy,
      addServer,
      removeServer,
      updateServer,
      addPair,
      updatePair,
      removePair,
      save: () => emit("save"),
    };
  },
  template: `
    <div class="settings-panel-body settings-panel-body-plain mcp-settings-panel">
      <QCard variant="default">
        <div class="settings-panel-shell">
          <header class="settings-panel-head">
            <div class="settings-panel-copy">
              <h3 class="settings-panel-title workspace-document-title">{{ t("settings_mcp_title") }}</h3>
              <p class="settings-panel-meta">{{ t("settings_section_mcp_meta") }}</p>
            </div>
            <div class="settings-panel-actions">
              <QButton class="outlined" :disabled="busy" @click="addServer">
                <PhPlus class="icon" />
                {{ t("settings_mcp_add_server") }}
              </QButton>
              <QButton class="primary" :loading="saving" :disabled="saveDisabled" @click="save">
                {{ t("action_save") }}
              </QButton>
            </div>
          </header>

          <p v-if="readOnly" class="mcp-settings-message is-warning">{{ readOnlyMessage }}</p>
          <p v-else-if="validationError" class="mcp-settings-message is-error">{{ validationError }}</p>

          <div class="settings-panel-body mcp-settings-body">
            <div v-if="loading" class="mcp-settings-skeleton" aria-hidden="true">
              <QSkeleton height="154px" />
              <QSkeleton height="154px" />
            </div>

            <div v-else-if="!servers.length" class="mcp-settings-empty">
              <PhPlugsConnected class="mcp-settings-empty-icon" />
              <strong>{{ t("settings_mcp_empty") }}</strong>
              <span>{{ t("settings_mcp_empty_note") }}</span>
            </div>

            <div v-else class="mcp-server-list">
              <section v-for="(server, index) in servers" :key="server._key" class="mcp-server">
                <div class="mcp-server-head">
                  <div class="mcp-server-enabled">
                    <QSwitch
                      :modelValue="server.enable"
                      :disabled="busy"
                      :aria-label="t('settings_mcp_enabled')"
                      @update:modelValue="updateServer(index, 'enable', $event)"
                    />
                    <span>{{ t("settings_mcp_enabled") }}</span>
                  </div>

                  <div class="mcp-transport" role="group" :aria-label="t('settings_mcp_transport')">
                    <QButton
                      class="plain sm mcp-transport-option"
                      :class="{ 'is-active': server.type === 'stdio' }"
                      :aria-pressed="server.type === 'stdio'"
                      :disabled="busy"
                      @click="updateServer(index, 'type', 'stdio')"
                    >
                      <PhTerminalWindow class="icon" />
                      {{ t("settings_mcp_stdio") }}
                    </QButton>
                    <QButton
                      class="plain sm mcp-transport-option"
                      :class="{ 'is-active': server.type === 'http' }"
                      :aria-pressed="server.type === 'http'"
                      :disabled="busy"
                      @click="updateServer(index, 'type', 'http')"
                    >
                      <PhGlobe class="icon" />
                      {{ t("settings_mcp_http") }}
                    </QButton>
                  </div>

                  <QButton
                    class="plain sm icon mcp-server-delete"
                    :title="t('settings_mcp_delete_server')"
                    :aria-label="t('settings_mcp_delete_server')"
                    :disabled="busy"
                    @click="removeServer(index)"
                  >
                    <PhTrash class="icon" />
                  </QButton>
                </div>

                <div class="mcp-server-fields">
                  <label class="settings-field is-wide">
                    <span class="settings-field-label">{{ t("settings_mcp_name") }}</span>
                    <QInput
                      :modelValue="server.name"
                      :placeholder="t('settings_mcp_name_placeholder')"
                      :disabled="busy"
                      @update:modelValue="updateServer(index, 'name', $event)"
                    />
                  </label>

                  <template v-if="server.type === 'http'">
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_mcp_url") }}</span>
                      <QInput
                        :modelValue="server.url"
                        placeholder="https://mcp.example.com/mcp"
                        :disabled="busy"
                        @update:modelValue="updateServer(index, 'url', $event)"
                      />
                    </label>

                    <div class="settings-field is-wide mcp-pairs-field">
                      <div class="mcp-field-head">
                        <span class="settings-field-label">{{ t("settings_mcp_headers") }}</span>
                        <QButton class="plain sm" :disabled="busy" @click="addPair(index, 'header_rows')">
                          <PhPlus class="icon" />
                          {{ t("settings_mcp_add_header") }}
                        </QButton>
                      </div>
                      <div v-for="(row, pairIndex) in server.header_rows" :key="row._key" class="mcp-pair-row">
                        <QInput
                          :modelValue="row.key"
                          :placeholder="t('settings_mcp_header_name')"
                          :disabled="busy"
                          @update:modelValue="updatePair(index, 'header_rows', pairIndex, 'key', $event)"
                        />
                        <QInput
                          :modelValue="row.value"
                          :placeholder="t('settings_mcp_header_value')"
                          autocomplete="off"
                          :disabled="busy"
                          @update:modelValue="updatePair(index, 'header_rows', pairIndex, 'value', $event)"
                        />
                        <QButton
                          class="plain sm icon"
                          :title="t('action_delete')"
                          :aria-label="t('action_delete')"
                          :disabled="busy"
                          @click="removePair(index, 'header_rows', pairIndex)"
                        >
                          <PhTrash class="icon" />
                        </QButton>
                      </div>
                    </div>
                  </template>

                  <template v-else>
                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_mcp_command") }}</span>
                      <QInput
                        :modelValue="server.command"
                        placeholder="node"
                        :disabled="busy"
                        @update:modelValue="updateServer(index, 'command', $event)"
                      />
                    </label>

                    <label class="settings-field is-wide">
                      <span class="settings-field-label">{{ t("settings_mcp_arguments") }}</span>
                      <QTextarea
                        :modelValue="server.args_text"
                        :rows="3"
                        :placeholder="t('settings_mcp_arguments_placeholder')"
                        :disabled="busy"
                        @update:modelValue="updateServer(index, 'args_text', $event)"
                      />
                      <span class="settings-panel-meta">{{ t("settings_mcp_arguments_note") }}</span>
                    </label>

                    <div class="settings-field is-wide mcp-pairs-field">
                      <div class="mcp-field-head">
                        <span class="settings-field-label">{{ t("settings_mcp_environment") }}</span>
                        <QButton class="plain sm" :disabled="busy" @click="addPair(index, 'env_rows')">
                          <PhPlus class="icon" />
                          {{ t("settings_mcp_add_variable") }}
                        </QButton>
                      </div>
                      <div v-for="(row, pairIndex) in server.env_rows" :key="row._key" class="mcp-pair-row">
                        <QInput
                          :modelValue="row.key"
                          :placeholder="t('settings_mcp_variable_name')"
                          :disabled="busy"
                          @update:modelValue="updatePair(index, 'env_rows', pairIndex, 'key', $event)"
                        />
                        <QInput
                          :modelValue="row.value"
                          :placeholder="t('settings_mcp_variable_value')"
                          autocomplete="off"
                          :disabled="busy"
                          @update:modelValue="updatePair(index, 'env_rows', pairIndex, 'value', $event)"
                        />
                        <QButton
                          class="plain sm icon"
                          :title="t('action_delete')"
                          :aria-label="t('action_delete')"
                          :disabled="busy"
                          @click="removePair(index, 'env_rows', pairIndex)"
                        >
                          <PhTrash class="icon" />
                        </QButton>
                      </div>
                    </div>
                  </template>

                  <label class="settings-field is-wide">
                    <span class="settings-field-label">{{ t("settings_mcp_allowed_tools") }}</span>
                    <QTextarea
                      :modelValue="server.allowed_tools_text"
                      :rows="3"
                      :placeholder="t('settings_mcp_allowed_tools_placeholder')"
                      :disabled="busy"
                      @update:modelValue="updateServer(index, 'allowed_tools_text', $event)"
                    />
                    <span class="settings-panel-meta">{{ t("settings_mcp_allowed_tools_note") }}</span>
                  </label>
                </div>
              </section>
            </div>
          </div>
        </div>
      </QCard>
    </div>
  `,
};

export default MCPSettingsPanel;
