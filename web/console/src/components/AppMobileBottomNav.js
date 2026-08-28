import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

import sidebarLogoURL from "../assets/images/app_logo_current.svg";
import { usePersonaSummary } from "../composables/usePersonaSummary";
import { uiSlots } from "../ext/slots";
import AgentSwitcher from "./AgentSwitcher";
import AppNavList from "./AppNavList";
import "./AppMobileBottomNav.css";

const PRIMARY_PAGE_PATHS = ["/chat", "/contacts", "/todo"];

function normalizePath(path) {
  const normalized = String(path || "").trim().replace(/\/+$/, "");
  return normalized || "/";
}

const AppMobileBottomNav = {
  components: {
    AgentSwitcher,
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
    const morePanel = ref(null);
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
    let focusBeforeOpen = null;
    let restoreFocusOnClose = true;

    function isActive(item) {
      if (!item || typeof item.id !== "string") {
        return false;
      }
      const current = normalizePath(props.currentPath);
      const target = normalizePath(item.id);
      return current === target || current.startsWith(`${target}/`);
    }

    function navClass(item) {
      return isActive(item) ? "mobile-bottom-nav-item is-active" : "mobile-bottom-nav-item";
    }

    function navCurrent(item) {
      return isActive(item) ? "page" : undefined;
    }

    function navHref(item) {
      const value = typeof item?.id === "string" ? item.id.trim() : "";
      return value || "/";
    }

    function closeMore(restoreFocus = true) {
      if (!props.modelValue) {
        return;
      }
      restoreFocusOnClose = restoreFocus;
      emit("update:modelValue", false);
      emit("close");
    }

    function toggleMore() {
      if (props.modelValue) {
        closeMore();
        return;
      }
      restoreFocusOnClose = true;
      emit("update:modelValue", true);
    }

    function navigate(item) {
      closeMore(false);
      emit("navigate", item);
    }

    function onWindowKeydown(event) {
      if (event.key !== "Escape" || !props.modelValue) {
        return;
      }
      event.preventDefault();
      closeMore();
    }

    watch(
      () => props.modelValue,
      async (open) => {
        if (open) {
          focusBeforeOpen = document.activeElement;
          window.addEventListener("keydown", onWindowKeydown);
          await nextTick();
          const firstAction = morePanel.value?.querySelector("a, button, [tabindex='0']");
          (firstAction || morePanel.value)?.focus();
          return;
        }
        window.removeEventListener("keydown", onWindowKeydown);
        if (
          restoreFocusOnClose &&
          focusBeforeOpen instanceof HTMLElement &&
          document.contains(focusBeforeOpen)
        ) {
          focusBeforeOpen.focus();
        }
        focusBeforeOpen = null;
      },
    );

    onBeforeUnmount(() => {
      window.removeEventListener("keydown", onWindowKeydown);
    });

    return {
      sidebarLogoURL,
      personaAvatarURL,
      sidebarBottomLeftSlot,
      primaryItems,
      moreItems,
      moreActive,
      morePanel,
      navClass,
      navCurrent,
      navHref,
      closeMore,
      toggleMore,
      navigate,
    };
  },
  template: `
    <Teleport to="body">
      <Transition name="mobile-bottom-more">
        <div v-if="modelValue" class="mobile-bottom-more-layer">
          <div class="mobile-bottom-more-mask" aria-hidden="true" @click="closeMore"></div>
          <section
            ref="morePanel"
            class="mobile-bottom-more-panel"
            role="dialog"
            :aria-label="t('nav_more')"
            tabindex="-1"
          >
            <span class="mobile-bottom-more-handle" aria-hidden="true"></span>
            <AppNavList
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
          </section>
        </div>
      </Transition>
    </Teleport>

    <nav class="mobile-bottom-nav" :aria-label="t('mobile_nav')">
      <a
        v-for="item in primaryItems"
        :key="item.id"
        :href="navHref(item)"
        :class="navClass(item)"
        :title="item.title"
        :aria-label="item.title"
        :aria-current="navCurrent(item)"
        @focus="$emit('preload', item)"
        @pointerenter="$emit('preload', item)"
        @click.prevent="navigate(item)"
      >
        <component :is="item.icon" v-if="item.icon" class="mobile-bottom-nav-icon icon" />
      </a>

      <button
        type="button"
        class="mobile-bottom-nav-item"
        :class="{ 'is-active': modelValue || moreActive }"
        :title="t('nav_more')"
        :aria-label="t('nav_more')"
        :aria-expanded="modelValue ? 'true' : 'false'"
        aria-haspopup="dialog"
        @click="toggleMore"
      >
        <QIconGrid class="mobile-bottom-nav-icon icon" />
      </button>

      <div
        class="mobile-bottom-nav-agent"
        :title="t('nav_agent')"
        @pointerdown.capture="closeMore(false)"
      >
        <AgentSwitcher
          class="mobile-bottom-agent-switcher"
          :compact="true"
          :items="endpointItems"
          :selectedItem="selectedEndpointItem"
          :selectedAvatar="personaAvatarURL || (selectedEndpointItem && selectedEndpointItem.image) || sidebarLogoURL"
          :selectedName="t('nav_agent')"
          :placeholder="t('endpoint_placeholder')"
          @change="$emit('endpoint-change', $event)"
          @desk="$emit('navigate', { id: '/chat/desk' })"
          @overview="$emit('navigate', { id: '/overview' })"
        />
      </div>
    </nav>
  `,
};

export default AppMobileBottomNav;
