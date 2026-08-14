import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";

import { translate } from "../core/context";
import AppDialogShell from "./AppDialogShell";
import "./AgentSwitcher.css";

function normalizeText(value) {
  return String(value || "").trim();
}

const AgentOptionGroups = {
  props: {
    onlineItems: {
      type: Array,
      default: () => [],
    },
    offlineItems: {
      type: Array,
      default: () => [],
    },
    selectedValue: {
      type: String,
      default: "",
    },
    onlineLabel: {
      type: String,
      required: true,
    },
    offlineLabel: {
      type: String,
      required: true,
    },
  },
  emits: ["select"],
  methods: {
    isSelected(item) {
      return normalizeText(item?.value) === this.selectedValue;
    },
  },
  template: `
    <section
      v-if="onlineItems.length > 0"
      class="agent-switcher-group"
      role="group"
      :aria-label="onlineLabel"
    >
      <button
        v-for="item in onlineItems"
        :key="item.value"
        type="button"
        class="agent-switcher-option"
        :class="{ 'is-selected': isSelected(item) }"
        role="option"
        :aria-selected="isSelected(item) ? 'true' : 'false'"
        data-agent-switcher-action
        @click="$emit('select', item)"
      >
        <img class="agent-switcher-avatar" :src="item.image" alt="" />
        <span class="agent-switcher-name">{{ item.title }}</span>
        <span class="agent-switcher-status-dot is-online" aria-hidden="true"></span>
      </button>
    </section>

    <section
      v-if="offlineItems.length > 0"
      class="agent-switcher-group is-offline"
      role="group"
      :aria-label="offlineLabel"
    >
      <button
        v-for="item in offlineItems"
        :key="item.value"
        type="button"
        class="agent-switcher-option is-offline"
        role="option"
        aria-selected="false"
        aria-disabled="true"
        disabled
      >
        <img class="agent-switcher-avatar" :src="item.image" alt="" />
        <span class="agent-switcher-name">{{ item.title }}</span>
        <span class="agent-switcher-status-dot is-offline" aria-hidden="true"></span>
      </button>
    </section>
  `,
};

const AgentSwitcher = {
  components: {
    AgentOptionGroups,
    AppDialogShell,
  },
  props: {
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
    selectedName: {
      type: String,
      default: "",
    },
    placeholder: {
      type: String,
      default: "",
    },
  },
  emits: ["change", "desk", "overview"],
  setup(props, { emit }) {
    const t = translate;
    const root = ref(null);
    const trigger = ref(null);
    const menuOpen = ref(false);
    const dialogOpen = ref(false);
    const filter = ref("");

    const normalizedItems = computed(() =>
      (Array.isArray(props.items) ? props.items : [])
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
    const onlineItems = computed(() => normalizedItems.value.filter((item) => item.connected));
    const offlineItems = computed(() => normalizedItems.value.filter((item) => !item.connected));
    const longMode = computed(() => normalizedItems.value.length > 8);
    const selectedValue = computed(() => normalizeText(props.selectedItem?.value));
    const triggerName = computed(
      () =>
        normalizeText(props.selectedName) ||
        normalizeText(props.selectedItem?.title) ||
        normalizeText(props.placeholder) ||
        t("endpoint_placeholder"),
    );
    const triggerAvatar = computed(
      () => normalizeText(props.selectedAvatar) || normalizeText(props.selectedItem?.image),
    );
    const open = computed(() => menuOpen.value || dialogOpen.value);
    const filteredItems = computed(() => {
      const query = normalizeText(filter.value).toLocaleLowerCase();
      if (!query) {
        return normalizedItems.value;
      }
      return normalizedItems.value.filter((item) =>
        [item.title, item.value].some((value) => normalizeText(value).toLocaleLowerCase().includes(query)),
      );
    });
    const dialogOnlineItems = computed(() => filteredItems.value.filter((item) => item.connected));
    const dialogOfflineItems = computed(() => filteredItems.value.filter((item) => !item.connected));

    function closeMenu(restoreFocus = false) {
      if (!menuOpen.value) {
        return;
      }
      menuOpen.value = false;
      if (restoreFocus) {
        void nextTick(() => trigger.value?.focus());
      }
    }

    function openCompactMenu(focusLast = false) {
      menuOpen.value = true;
      void nextTick(() => {
        const options = root.value?.querySelectorAll(
          ".agent-switcher-panel [data-agent-switcher-action]:not(:disabled)",
        );
        if (!options || options.length === 0) {
          return;
        }
        const selected = root.value?.querySelector(
          '.agent-switcher-panel .agent-switcher-option[aria-selected="true"]',
        );
        const target = focusLast ? options[options.length - 1] : selected || options[0];
        target?.focus();
      });
    }

    function toggleSwitcher() {
      if (longMode.value) {
        closeMenu();
        filter.value = "";
        dialogOpen.value = true;
        return;
      }
      if (menuOpen.value) {
        closeMenu();
        return;
      }
      openCompactMenu();
    }

    function onTriggerKeydown(event) {
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
        return;
      }
      event.preventDefault();
      if (longMode.value) {
        toggleSwitcher();
        return;
      }
      openCompactMenu(event.key === "ArrowUp");
    }

    function selectItem(item) {
      if (!item?.connected) {
        return;
      }
      closeMenu();
      dialogOpen.value = false;
      emit("change", item);
    }

    function openOverview() {
      closeMenu();
      dialogOpen.value = false;
      emit("overview");
    }

    function openDesk() {
      closeMenu();
      dialogOpen.value = false;
      emit("desk");
    }

    function updateFilter(value) {
      filter.value = String(value || "");
    }

    function onDocumentPointerDown(event) {
      if (!menuOpen.value || root.value?.contains(event.target)) {
        return;
      }
      closeMenu();
    }

    function onDocumentKeydown(event) {
      if (!menuOpen.value) {
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        closeMenu(true);
        return;
      }
      if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
        return;
      }

      const options = Array.from(
        root.value?.querySelectorAll("[data-agent-switcher-action]:not(:disabled)") || [],
      );
      if (options.length === 0) {
        return;
      }
      event.preventDefault();
      const current = options.indexOf(document.activeElement);
      let next = 0;
      if (event.key === "End") {
        next = options.length - 1;
      } else if (event.key === "ArrowUp") {
        next = current <= 0 ? options.length - 1 : current - 1;
      } else if (event.key === "ArrowDown") {
        next = current < 0 || current === options.length - 1 ? 0 : current + 1;
      }
      options[next]?.focus();
    }

    onMounted(() => {
      document.addEventListener("pointerdown", onDocumentPointerDown);
      document.addEventListener("keydown", onDocumentKeydown);
    });
    onBeforeUnmount(() => {
      document.removeEventListener("pointerdown", onDocumentPointerDown);
      document.removeEventListener("keydown", onDocumentKeydown);
    });

    return {
      t,
      root,
      trigger,
      menuOpen,
      dialogOpen,
      filter,
      onlineItems,
      offlineItems,
      longMode,
      selectedValue,
      triggerName,
      triggerAvatar,
      open,
      dialogOnlineItems,
      dialogOfflineItems,
      closeMenu,
      toggleSwitcher,
      onTriggerKeydown,
      selectItem,
      openDesk,
      openOverview,
      updateFilter,
    };
  },
  template: `
    <div ref="root" class="agent-switcher" :class="{ 'is-open': open }">
      <button
        ref="trigger"
        type="button"
        class="agent-switcher-trigger"
        :aria-label="t('endpoint_switcher_label')"
        :aria-haspopup="longMode ? 'dialog' : 'listbox'"
        :aria-expanded="open ? 'true' : 'false'"
        @click="toggleSwitcher"
        @keydown="onTriggerKeydown"
      >
        <img v-if="triggerAvatar" class="agent-switcher-avatar" :src="triggerAvatar" alt="" />
        <span v-else class="agent-switcher-avatar is-empty" aria-hidden="true"></span>
        <span class="agent-switcher-name">{{ triggerName }}</span>
        <span class="agent-switcher-chevron" aria-hidden="true">
          <svg viewBox="0 0 16 16" focusable="false">
            <path d="m4 6 4 4 4-4" />
          </svg>
        </span>
      </button>

      <div
        v-if="!longMode"
        class="agent-switcher-panel"
        :class="{ 'is-visible': menuOpen }"
        :aria-hidden="menuOpen ? 'false' : 'true'"
        :inert="!menuOpen"
      >
        <div class="agent-switcher-panel-scroll" role="listbox" :aria-label="t('endpoint_switcher_title')">
          <AgentOptionGroups
            :onlineItems="onlineItems"
            :offlineItems="offlineItems"
            :selectedValue="selectedValue"
            :onlineLabel="t('endpoint_switcher_online')"
            :offlineLabel="t('endpoint_switcher_offline')"
            @select="selectItem"
          />
          <p v-if="onlineItems.length === 0 && offlineItems.length === 0" class="agent-switcher-empty">
            {{ t('endpoint_switcher_empty') }}
          </p>
        </div>
        <button
          type="button"
          class="agent-switcher-route"
          data-agent-switcher-action
          @click="openDesk"
        >
          <span class="agent-switcher-route-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" focusable="false">
              <path d="M2.5 2.5h11v11h-11zM8 2.5v11M2.5 8h11" />
            </svg>
          </span>
          <span class="agent-switcher-name">{{ t('endpoint_switcher_desk') }}</span>
          <span class="agent-switcher-route-arrow" aria-hidden="true">
            <svg viewBox="0 0 16 16" focusable="false">
              <path d="m6 4 4 4-4 4" />
            </svg>
          </span>
        </button>
        <button
          type="button"
          class="agent-switcher-route"
          data-agent-switcher-action
          @click="openOverview"
        >
          <span class="agent-switcher-route-icon" aria-hidden="true">
            <svg viewBox="0 0 16 16" focusable="false">
              <path d="M2.5 2.5h4v4h-4zM9.5 2.5h4v4h-4zM2.5 9.5h4v4h-4zM9.5 9.5h4v4h-4z" />
            </svg>
          </span>
          <span class="agent-switcher-name">{{ t('nav_overview') }}</span>
          <span class="agent-switcher-route-arrow" aria-hidden="true">
            <svg viewBox="0 0 16 16" focusable="false">
              <path d="m6 4 4 4-4 4" />
            </svg>
          </span>
        </button>
      </div>

      <AppDialogShell
        :modelValue="dialogOpen"
        :title="t('endpoint_switcher_title')"
        width="420px"
        @update:modelValue="dialogOpen = $event"
        @close="dialogOpen = false"
      >
        <section class="agent-switcher-dialog">
          <QInput
            class="agent-switcher-filter"
            :modelValue="filter"
            :placeholder="t('endpoint_switcher_filter_placeholder')"
            @update:modelValue="updateFilter"
          />
          <div class="agent-switcher-dialog-list">
            <div class="agent-switcher-dialog-scroll" role="listbox" :aria-label="t('endpoint_switcher_title')">
              <AgentOptionGroups
                :onlineItems="dialogOnlineItems"
                :offlineItems="dialogOfflineItems"
                :selectedValue="selectedValue"
                :onlineLabel="t('endpoint_switcher_online')"
                :offlineLabel="t('endpoint_switcher_offline')"
                @select="selectItem"
              />
              <p
                v-if="dialogOnlineItems.length === 0 && dialogOfflineItems.length === 0"
                class="agent-switcher-empty"
              >
                {{ t('endpoint_switcher_empty') }}
              </p>
            </div>
            <button
              type="button"
              class="agent-switcher-route"
              @click="openDesk"
            >
              <span class="agent-switcher-route-icon" aria-hidden="true">
                <svg viewBox="0 0 16 16" focusable="false">
                  <path d="M2.5 2.5h11v11h-11zM8 2.5v11M2.5 8h11" />
                </svg>
              </span>
              <span class="agent-switcher-name">{{ t('endpoint_switcher_desk') }}</span>
              <span class="agent-switcher-route-arrow" aria-hidden="true">
                <svg viewBox="0 0 16 16" focusable="false">
                  <path d="m6 4 4 4-4 4" />
                </svg>
              </span>
            </button>
            <button type="button" class="agent-switcher-route" @click="openOverview">
              <span class="agent-switcher-route-icon" aria-hidden="true">
                <svg viewBox="0 0 16 16" focusable="false">
                  <path d="M2.5 2.5h4v4h-4zM9.5 2.5h4v4h-4zM2.5 9.5h4v4h-4zM9.5 9.5h4v4h-4z" />
                </svg>
              </span>
              <span class="agent-switcher-name">{{ t('nav_overview') }}</span>
              <span class="agent-switcher-route-arrow" aria-hidden="true">
                <svg viewBox="0 0 16 16" focusable="false">
                  <path d="m6 4 4 4-4 4" />
                </svg>
              </span>
            </button>
          </div>
        </section>
      </AppDialogShell>
    </div>
  `,
};

export default AgentSwitcher;
