import { computed, ref } from "vue";

import AppMobileBottomMenu from "./AppMobileBottomMenu";

function normalizeText(value) {
  return String(value || "").trim();
}

const AppMobileAgentSwitcher = {
  components: {
    AppMobileBottomMenu,
  },
  props: {
    modelValue: {
      type: Boolean,
      required: true,
    },
    items: {
      type: Array,
      default: () => [],
    },
    selectedItem: {
      type: Object,
      default: null,
    },
    selectedAvatar: {
      type: String,
      default: "",
    },
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["update:modelValue", "change", "overview"],
  setup(props, { emit }) {
    const filter = ref("");

    const normalizedItems = computed(() =>
      props.items
        .map((item) => {
          const value = normalizeText(item?.value);
          return {
            ...item,
            value,
            title: normalizeText(item?.title) || value,
            image: normalizeText(item?.image),
            connected: item?.connected === true,
          };
        })
        .filter((item) => item.value),
    );
    const selectedValue = computed(() => normalizeText(props.selectedItem?.value));
    const selectedAvatar = computed(
      () => normalizeText(props.selectedAvatar) || normalizeText(props.selectedItem?.image),
    );
    const showFilter = computed(() => normalizedItems.value.length > 8);
    const filteredItems = computed(() => {
      const query = normalizeText(filter.value).toLocaleLowerCase();
      if (!query) {
        return normalizedItems.value;
      }
      return normalizedItems.value.filter((item) =>
        [item.title, item.value].some((value) => value.toLocaleLowerCase().includes(query)),
      );
    });
    const onlineItems = computed(() => filteredItems.value.filter((item) => item.connected));
    const offlineItems = computed(() => filteredItems.value.filter((item) => !item.connected));

    function close() {
      if (!props.modelValue) {
        return;
      }
      emit("update:modelValue", false);
    }

    function toggle() {
      if (props.modelValue) {
        close();
        return;
      }
      filter.value = "";
      emit("update:modelValue", true);
    }

    function select(item) {
      if (!item?.connected) {
        return;
      }
      close();
      emit("change", item);
    }

    function openOverview() {
      close();
      emit("overview");
    }

    return {
      filter,
      selectedValue,
      selectedAvatar,
      showFilter,
      onlineItems,
      offlineItems,
      close,
      toggle,
      select,
      openOverview,
    };
  },
  template: `
    <button
      type="button"
      class="mobile-bottom-nav-item mobile-agent-switcher-trigger"
      :class="{ 'is-active': modelValue }"
      :title="t('nav_agent')"
      :aria-label="t('nav_agent')"
      :aria-expanded="modelValue ? 'true' : 'false'"
      aria-haspopup="dialog"
      @click="toggle"
    >
      <img v-if="selectedAvatar" class="mobile-agent-switcher-avatar" :src="selectedAvatar" alt="" />
      <span v-else class="mobile-agent-switcher-avatar is-empty" aria-hidden="true"></span>
    </button>

    <AppMobileBottomMenu
      :modelValue="modelValue"
      :label="t('endpoint_switcher_title')"
      @close="close"
    >
      <div
        class="mobile-agent-switcher-content"
        :class="{ 'has-filter': showFilter }"
      >
        <QInput
          v-if="showFilter"
          class="mobile-agent-switcher-filter"
          :modelValue="filter"
          :placeholder="t('endpoint_switcher_filter_placeholder')"
          @update:modelValue="filter = String($event || '')"
        />
        <div
          class="mobile-bottom-menu-list mobile-agent-switcher-options"
          role="listbox"
          :aria-label="t('endpoint_switcher_title')"
        >
          <section
            v-if="onlineItems.length > 0"
            class="mobile-agent-switcher-group"
            role="group"
            :aria-label="t('endpoint_switcher_online')"
          >
            <button
              v-for="item in onlineItems"
              :key="item.value"
              type="button"
              class="mobile-bottom-menu-item mobile-agent-switcher-option"
              :class="{ 'is-selected': item.value === selectedValue }"
              role="option"
              :aria-selected="item.value === selectedValue ? 'true' : 'false'"
              @click="select(item)"
            >
              <img v-if="item.image" class="mobile-agent-switcher-avatar" :src="item.image" alt="" />
              <span v-else class="mobile-agent-switcher-avatar is-empty" aria-hidden="true"></span>
              <span class="mobile-agent-switcher-name">{{ item.title }}</span>
              <span class="mobile-agent-switcher-status is-online" aria-hidden="true"></span>
            </button>
          </section>

          <section
            v-if="offlineItems.length > 0"
            class="mobile-agent-switcher-group is-offline"
            role="group"
            :aria-label="t('endpoint_switcher_offline')"
          >
            <button
              v-for="item in offlineItems"
              :key="item.value"
              type="button"
              class="mobile-bottom-menu-item mobile-agent-switcher-option is-offline"
              role="option"
              aria-selected="false"
              disabled
            >
              <img v-if="item.image" class="mobile-agent-switcher-avatar" :src="item.image" alt="" />
              <span v-else class="mobile-agent-switcher-avatar is-empty" aria-hidden="true"></span>
              <span class="mobile-agent-switcher-name">{{ item.title }}</span>
              <span class="mobile-agent-switcher-status is-offline" aria-hidden="true"></span>
            </button>
          </section>

          <p
            v-if="onlineItems.length === 0 && offlineItems.length === 0"
            class="mobile-agent-switcher-empty"
          >
            {{ t('endpoint_switcher_empty') }}
          </p>
        </div>

        <button
          type="button"
          class="mobile-bottom-menu-item mobile-agent-switcher-route"
          @click="openOverview"
        >
          <PhNetwork class="mobile-agent-switcher-route-icon" />
          <span class="mobile-agent-switcher-name">{{ t('nav_overview') }}</span>
          <PhCaretRight class="mobile-agent-switcher-route-arrow" />
        </button>
      </div>
    </AppMobileBottomMenu>
  `,
};

export default AppMobileAgentSwitcher;
