import { computed, ref, watch } from "vue";
import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import "./SetupPickerDialog.css";

const SetupPickerDialog = {
  components: {
    AppDialogShell,
  },
  props: {
    modelValue: Boolean,
    items: {
      type: Array,
      default: () => [],
    },
    loading: Boolean,
    error: {
      type: String,
      default: "",
    },
    title: {
      type: String,
      default: "",
    },
    filterPlaceholder: {
      type: String,
      default: "",
    },
    emptyText: {
      type: String,
      default: "",
    },
    showValue: {
      type: Boolean,
      default: true,
    },
  },
  emits: ["update:modelValue", "select"],
  setup(props, { emit }) {
    const t = translate;
    const query = ref("");
    const resolvedTitle = computed(() => String(props.title || "").trim());

    const filteredItems = computed(() => {
      const needle = String(query.value || "").trim().toLowerCase();
      const source = Array.isArray(props.items) ? props.items : [];
      if (!needle) {
        return source;
      }
      return source.filter((item) => {
        const haystack = [item?.title, item?.value, item?.note]
          .map((value) => String(value || "").toLowerCase())
          .join("\n");
        return haystack.includes(needle);
      });
    });

    function close() {
      emit("update:modelValue", false);
    }

    function selectItem(item) {
      emit("select", item);
      close();
    }

    watch(
      () => props.modelValue,
      (open) => {
        if (open) {
          query.value = "";
        }
      }
    );

    return {
      t,
      query,
      resolvedTitle,
      filteredItems,
      close,
      selectItem,
    };
  },
  template: `
    <AppDialogShell
      :modelValue="modelValue"
      :title="resolvedTitle"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      :closeDisabled="loading"
      @close="close"
    >
      <section class="setup-picker-dialog">
        <QInput
          v-model="query"
          class="setup-picker-filter"
          :placeholder="filterPlaceholder"
          :disabled="loading"
        />

        <QProgress v-if="loading" :infinite="true" />
        <QFence v-if="error" type="danger" icon="QIconCloseCircle" :text="error" />

        <div v-if="!loading" class="setup-picker-list">
          <button
            v-for="item in filteredItems"
            :key="item.id || item.value || item.title"
            type="button"
            class="setup-picker-item"
            @click="selectItem(item)"
          >
            <span class="setup-picker-item-copy">
              <strong class="setup-picker-item-title">{{ item.title }}</strong>
              <span v-if="item.note" class="setup-picker-item-note">{{ item.note }}</span>
            </span>
            <code v-if="showValue && item.value" class="setup-picker-item-value">{{ item.value }}</code>
          </button>

          <p v-if="filteredItems.length === 0 && !error" class="setup-picker-empty">{{ emptyText }}</p>
        </div>
      </section>
    </AppDialogShell>
  `,
};

export default SetupPickerDialog;
