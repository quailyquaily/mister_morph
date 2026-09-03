import { computed, ref, watch } from "vue";
import "./SetupPickerDialog.css";

export const setupPickerDialogContentProps = {
  items: {
    type: Array,
    default: () => [],
  },
  loading: Boolean,
  error: {
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
  resetKey: {
    type: String,
    default: "",
  },
};

const SetupPickerDialogContent = {
  props: setupPickerDialogContentProps,
  emits: ["select"],
  setup(props, { emit }) {
    const query = ref("");

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

    function selectItem(item) {
      emit("select", item);
    }

    watch(
      () => props.resetKey,
      () => {
        query.value = "";
      }
    );

    return {
      query,
      filteredItems,
      selectItem,
    };
  },
  template: `
    <section class="setup-picker-dialog">
      <QInput
        v-model="query"
        class="setup-picker-filter"
        :placeholder="filterPlaceholder"
        :disabled="loading"
      />

      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="error" type="danger" icon="PhXCircle" :text="error" />

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
  `,
};

export default SetupPickerDialogContent;
