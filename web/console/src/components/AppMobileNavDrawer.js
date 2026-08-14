import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

import "./AppSidebar.css";
import "./AppMobileNavDrawer.css";
import AppSidebarControls from "./AppSidebarControls";
import AppNavList from "./AppNavList";
import { uiSlots } from "../ext/slots";

const AppMobileNavDrawer = {
  components: {
    AppSidebarControls,
    AppNavList,
  },
  props: {
    modelValue: {
      type: Boolean,
      required: true,
    },
    title: {
      type: String,
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
  emits: ["update:modelValue", "close", "navigate", "preload", "endpoint-change", "go-settings"],
  setup(props, { emit }) {
    const sidebarBottomLeftSlot = computed(() => uiSlots["sidebar.bottom_left"] || null);
    const panel = ref(null);
    let focusBeforeOpen = null;
    let bodyOverflowBeforeOpen = "";
    let scrollLocked = false;

    function unlockPageScroll() {
      if (!scrollLocked) {
        return;
      }
      document.body.style.overflow = bodyOverflowBeforeOpen;
      scrollLocked = false;
    }

    function close() {
      emit("update:modelValue", false);
      emit("close");
    }

    function onWindowKeydown(event) {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      close();
    }

    watch(
      () => props.modelValue,
      async (open) => {
        if (open) {
          focusBeforeOpen = document.activeElement;
          bodyOverflowBeforeOpen = document.body.style.overflow;
          document.body.style.overflow = "hidden";
          scrollLocked = true;
          window.addEventListener("keydown", onWindowKeydown);
          await nextTick();
          panel.value?.focus();
          return;
        }

        window.removeEventListener("keydown", onWindowKeydown);
        unlockPageScroll();
        if (focusBeforeOpen instanceof HTMLElement && document.contains(focusBeforeOpen)) {
          focusBeforeOpen.focus();
        }
        focusBeforeOpen = null;
      }
    );

    onBeforeUnmount(() => {
      window.removeEventListener("keydown", onWindowKeydown);
      unlockPageScroll();
    });

    return { close, panel, sidebarBottomLeftSlot };
  },
  template: `
    <Teleport to="body">
      <Transition name="app-mobile-nav">
        <div v-if="modelValue" class="app-mobile-nav-layer">
          <div class="app-mobile-nav-mask" aria-hidden="true" @click="close"></div>
          <aside
            ref="panel"
            class="app-mobile-nav-panel"
            :aria-label="title"
            tabindex="-1"
          >
            <div class="sidebar app-mobile-nav-shell">
              <AppSidebarControls
                :t="t"
                :endpointItems="endpointItems"
                :selectedEndpointItem="selectedEndpointItem"
                @endpoint-change="$emit('endpoint-change', $event)"
                @go-desk="$emit('navigate', { id: '/chat/desk' })"
                @go-overview="$emit('navigate', { id: '/overview' })"
                @go-settings="$emit('go-settings')"
              />
              <AppNavList
                :navItems="navItems"
                :currentPath="currentPath"
                :mobile="true"
                keyPrefix="drawer-"
                :selectedEndpointItem="selectedEndpointItem"
                :t="t"
                @navigate="$emit('navigate', $event)"
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
            </div>
          </aside>
        </div>
      </Transition>
    </Teleport>
  `,
};

export default AppMobileNavDrawer;
