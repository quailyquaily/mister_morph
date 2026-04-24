import { computed, ref, watch } from "vue";
import { translate } from "../core/context";
import "./SetupPickerDialog.css";

const SetupPickerDialog = {
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
    <QDialog
      :modelValue="modelValue"
      width="560px"
      @update:modelValue="$emit('update:modelValue', $event)"
      @close="close"
    >
      <template #header>
        <header class="app-dialog-header">
          <div class="app-dialog-copy">
            <h3 class="app-dialog-title">{{ resolvedTitle }}</h3>
          </div>
          <QButton
            type="button"
            class="icon border-radius-none app-dialog-close"
            :title="t('action_close')"
            :aria-label="t('action_close')"
            :disabled="loading"
            @click="close"
          >
            <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </QButton>
        </header>
      </template>

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
    </QDialog>
  `,
};

export default SetupPickerDialog;
