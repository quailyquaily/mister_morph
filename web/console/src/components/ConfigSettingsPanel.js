import { computed, reactive, ref, watch } from "vue";

import { buildConfigUpdate, createConfigDraft } from "../core/config-fields";

export default {
  name: "ConfigSettingsPanel",
  props: {
    groups: { type: Array, default: () => [] },
    values: { type: Object, default: () => ({}) },
    fieldStates: { type: Object, default: () => ({}) },
    loading: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
    embedded: { type: Boolean, default: false },
    hideSingleGroupHeading: { type: Boolean, default: false },
    savePlacement: { type: String, default: "header" },
  },
  emits: ["save", "update:dirty"],
  setup(props, { emit }) {
    const draft = reactive({});
    const original = ref({});
    const reset = reactive({});
    const validationError = ref("");
    const fields = computed(() => props.groups.flatMap((group) => Array.isArray(group.fields) ? group.fields : []));

    function replaceDraft() {
      const next = createConfigDraft(props.values, fields.value);
      for (const key of Object.keys(draft)) {
        delete draft[key];
      }
      Object.assign(draft, next);
      original.value = { ...next };
      for (const key of Object.keys(reset)) {
        delete reset[key];
      }
      validationError.value = "";
    }

    watch(() => props.values, replaceDraft, { deep: true, immediate: true });
    watch(fields, replaceDraft);

    const dirty = computed(() => {
      if (Object.keys(reset).some((path) => reset[path])) {
        return true;
      }
      return fields.value.some((field) => !Object.is(draft[field.path], original.value[field.path]));
    });

    watch(dirty, (value) => emit("update:dirty", value));

    function stateFor(field) {
      return props.fieldStates?.[field.path] || {};
    }

    function fieldDisabled(field) {
      return props.loading || props.saving || stateFor(field).editable === false;
    }

    function updateField(field, value) {
      draft[field.path] = value;
      delete reset[field.path];
      validationError.value = "";
    }

    function resetField(field) {
      reset[field.path] = true;
      validationError.value = "";
    }

    function environmentManaged(field) {
      const source = stateFor(field).source;
      return source === "environment_override" || source === "config_env_ref";
    }

    function environmentManagedName(field) {
      return stateFor(field).env_name || "Environment variable";
    }

    function showClear(field) {
      const state = stateFor(field);
      return field.secret === true && state.explicit === true && state.editable !== false;
    }

    function sourceLabel(field) {
      const state = stateFor(field);
      if (state.source === "runtime_override") return "Managed by a command-line flag";
      if (state.source === "config_aws_ref") return "AWS Secrets Manager reference";
      if (state.source === "config_os_ref") return "System secret store";
      return "";
    }

    function inputType(field) {
      if (field.secret) return "password";
      if (field.type === "int" || field.type === "float") return "number";
      return "text";
    }

    function selectItems(field) {
      return (field.options || []).map((value) => ({
        title: value || field.placeholder || "Default",
        value,
      }));
    }

    function selectedItem(field) {
      return selectItems(field).find((item) => item.value === draft[field.path]) || null;
    }

    function save() {
      try {
        const update = buildConfigUpdate(
          draft,
          original.value,
          Object.keys(reset).filter((path) => reset[path]),
          fields.value,
        );
        validationError.value = "";
        emit("save", update);
      } catch (error) {
        validationError.value = error?.message || "Invalid setting";
      }
    }

    return {
      draft,
      validationError,
      dirty,
      stateFor,
      fieldDisabled,
      updateField,
      resetField,
      environmentManaged,
      environmentManagedName,
      showClear,
      sourceLabel,
      inputType,
      selectItems,
      selectedItem,
      save,
    };
  },
  template: `
    <div class="config-settings-panel">
      <QProgress v-if="loading" :infinite="true" />
      <div v-if="validationError" class="config-settings-error" role="alert">{{ validationError }}</div>

      <component
        :is="embedded ? 'section' : 'QCard'"
        v-for="group in groups"
        :key="group.id"
        :variant="embedded ? undefined : 'default'"
        :class="['config-settings-group', { 'is-embedded': embedded }]"
      >
        <div class="settings-panel-shell">
          <header
            v-if="!hideSingleGroupHeading || groups.length > 1 || (savePlacement === 'header' && group === groups[0])"
            class="settings-panel-head"
          >
            <div v-if="!hideSingleGroupHeading || groups.length > 1" class="settings-panel-copy">
              <h3 class="settings-panel-title workspace-document-title">{{ group.title }}</h3>
              <p v-if="group.note" class="settings-panel-meta">{{ group.note }}</p>
            </div>
            <QButton
              v-if="savePlacement === 'header' && group === groups[0]"
              class="primary"
              :loading="saving"
              :disabled="loading || saving || !dirty"
              @click="save"
            >
              Save
            </QButton>
          </header>

          <div class="settings-panel-body config-settings-fields">
            <div
              v-for="field in group.fields"
              :key="field.path"
              :class="['settings-field', { 'is-wide': field.wide || field.type === 'json' || field.type === 'string_list' }]"
            >
              <div class="config-settings-label-row">
                <span class="settings-field-label">{{ field.label }}</span>
                <span
                  v-if="stateFor(field).apply_mode === 'runtime_restart' || stateFor(field).apply_mode === 'process_restart'"
                  class="config-settings-restart"
                >Restart required</span>
              </div>

              <div v-if="environmentManaged(field)" class="settings-env-managed">
                <code class="settings-env-managed-env">{{ environmentManagedName(field) }}</code>
                <p class="settings-env-managed-body">Managed by the environment variable.</p>
              </div>
              <QSwitch
                v-else-if="field.type === 'bool'"
                :modelValue="draft[field.path]"
                :disabled="fieldDisabled(field)"
                @update:modelValue="updateField(field, $event)"
              />
              <QDropdownMenu
                v-else-if="field.type === 'select'"
                :key="field.path + ':' + String(draft[field.path])"
                :items="selectItems(field)"
                :initialItem="selectedItem(field)"
                :placeholder="field.placeholder || ''"
                :disabled="fieldDisabled(field)"
                @change="updateField(field, $event?.value ?? '')"
              />
              <QTextarea
                v-else-if="field.type === 'string_list' || field.type === 'json'"
                :modelValue="draft[field.path]"
                :rows="field.type === 'json' ? 7 : 4"
                :class="{ 'config-settings-json': field.type === 'json' }"
                :placeholder="field.placeholder || ''"
                :disabled="fieldDisabled(field)"
                @update:modelValue="updateField(field, $event)"
              />
              <QInput
                v-else
                :modelValue="draft[field.path]"
                :inputType="inputType(field)"
                :placeholder="field.secret && stateFor(field).configured ? 'Configured — enter a new value to replace' : field.placeholder || ''"
                :disabled="fieldDisabled(field)"
                @update:modelValue="updateField(field, $event)"
              />

              <p v-if="field.note" class="settings-field-note">{{ field.note }}</p>
              <div v-if="sourceLabel(field) || showClear(field)" class="config-settings-field-meta">
                <span v-if="sourceLabel(field)">{{ sourceLabel(field) }}</span>
                <span v-else></span>
                <QButton
                  v-if="showClear(field)"
                  class="plain xs"
                  :disabled="loading || saving"
                  @click="resetField(field)"
                >Clear</QButton>
              </div>
            </div>
          </div>
        </div>
      </component>

      <div v-if="savePlacement === 'footer'" class="config-settings-actions">
        <QButton class="primary" :loading="saving" :disabled="loading || saving || !dirty" @click="save">
          Save
        </QButton>
      </div>
    </div>
  `,
};
