import { computed, ref } from "vue";

import sidebarLogoURL from "../assets/images/app_logo_current.svg";
import { usePersonaSummary } from "../composables/usePersonaSummary";
import { uiSlots } from "../ext/slots";
import AppMobileAgentSwitcher from "./AppMobileAgentSwitcher";
import AppMobileBottomMenu from "./AppMobileBottomMenu";
import AppNavList from "./AppNavList";
import "./AppMobileBottomNav.css";

const PRIMARY_PAGE_PATHS = ["/chat", "/contacts", "/todo"];

function normalizePath(path) {
  const normalized = String(path || "").trim().replace(/\/+$/, "");
  return normalized || "/";
}

const AppMobileBottomNav = {
  components: {
    AppMobileAgentSwitcher,
    AppMobileBottomMenu,
    AppNavList,
  },
  props: {
    modelValue: {
      type: Boolean,
      required: true,
    },
    navItems: {
      type: Array,
      required: true,
    },
    currentPath: {
      type: String,
      required: true,
    },
    endpointItems: {
      type: Array,
      required: true,
    },
    selectedEndpointItem: {
      type: Object,
      default: null,
    },
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["update:modelValue", "close", "navigate", "preload", "endpoint-change"],
  setup(props, { emit }) {
    const { personaAvatarURL } = usePersonaSummary();
    const agentMenuOpen = ref(false);
    const sidebarBottomLeftSlot = computed(() => uiSlots["sidebar.bottom_left"] || null);
    const primaryItems = computed(() =>
      PRIMARY_PAGE_PATHS.map((pagePath) =>
        props.navItems.find((item) => item?.pagePath === pagePath),
      ).filter(Boolean),
    );
    const moreItems = computed(() =>
      props.navItems.filter(
        (item) =>
          !item?.separator &&
          typeof item?.pagePath === "string" &&
          !PRIMARY_PAGE_PATHS.includes(item.pagePath),
      ),
    );
    const moreActive = computed(() => moreItems.value.some((item) => isActive(item)));
    function isActive(item) {
      if (!item || typeof item.id !== "string") {
        return false;
      }
      const current = normalizePath(props.currentPath);
      const target = normalizePath(item.id);
      return current === target || current.startsWith(`${target}/`);
    }

    function navHref(item) {
      const value = typeof item?.id === "string" ? item.id.trim() : "";
      return value || "/";
    }

    function closeMore() {
      if (!props.modelValue) {
        return;
      }
      emit("update:modelValue", false);
      emit("close");
    }

    function toggleMore() {
      if (props.modelValue) {
        closeMore();
        return;
      }
      agentMenuOpen.value = false;
      emit("update:modelValue", true);
    }

    function setAgentMenuOpen(open) {
      if (open) {
        closeMore();
      }
      agentMenuOpen.value = open;
    }

    function navigate(item) {
      closeMore();
      agentMenuOpen.value = false;
      emit("navigate", item);
    }

    return {
      sidebarLogoURL,
      personaAvatarURL,
      sidebarBottomLeftSlot,
      primaryItems,
      moreItems,
      moreActive,
      agentMenuOpen,
      isActive,
      navHref,
      closeMore,
      toggleMore,
      setAgentMenuOpen,
      navigate,
    };
  },
  template: `
    <AppMobileBottomMenu
      :modelValue="modelValue"
      :label="t('nav_more')"
      panelClass="mobile-bottom-more-panel"
      @close="closeMore"
    >
      <AppNavList
        class="mobile-bottom-menu-list"
        :navItems="moreItems"
        :currentPath="currentPath"
        :mobile="true"
        keyPrefix="mobile-more-"
        :selectedEndpointItem="selectedEndpointItem"
        :t="t"
        @navigate="navigate"
        @preload="$emit('preload', $event)"
      />
      <div v-if="sidebarBottomLeftSlot" class="sidebar-slot sidebar-slot-bottom-left">
        <component
          :is="sidebarBottomLeftSlot"
          :selectedEndpointItem="selectedEndpointItem"
          :currentPath="currentPath"
          :t="t"
        />
      </div>
    </AppMobileBottomMenu>

    <nav class="mobile-bottom-nav" :aria-label="t('mobile_nav')">
      <a
        v-for="item in primaryItems"
        :key="item.id"
        :href="navHref(item)"
        class="mobile-bottom-nav-item"
        :class="{ 'is-active': !modelValue && !agentMenuOpen && isActive(item) }"
        :title="item.title"
        :aria-label="item.title"
        :aria-current="isActive(item) ? 'page' : undefined"
        @focus="$emit('preload', item)"
        @pointerenter="$emit('preload', item)"
        @click.prevent="navigate(item)"
      >
        <component :is="item.icon" v-if="item.icon" class="mobile-bottom-nav-icon icon" />
      </a>

      <button
        type="button"
        class="mobile-bottom-nav-item"
        :class="{ 'is-active': modelValue || (!agentMenuOpen && moreActive) }"
        :title="t('nav_more')"
        :aria-label="t('nav_more')"
        :aria-expanded="modelValue ? 'true' : 'false'"
        aria-haspopup="dialog"
        @click="toggleMore"
      >
        <PhSquaresFour class="mobile-bottom-nav-icon icon" />
      </button>

      <AppMobileAgentSwitcher
        :modelValue="agentMenuOpen"
        :items="endpointItems"
        :selectedItem="selectedEndpointItem"
        :selectedAvatar="personaAvatarURL || (selectedEndpointItem && selectedEndpointItem.image) || sidebarLogoURL"
        :t="t"
        @update:modelValue="setAgentMenuOpen"
        @change="$emit('endpoint-change', $event)"
        @overview="$emit('navigate', { id: '/overview' })"
      />
    </nav>
  `,
};

export default AppMobileBottomNav;
