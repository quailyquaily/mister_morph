import { computed } from "vue";

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
  emits: ["update:modelValue", "close", "navigate", "preload", "endpoint-change", "go-overview", "go-settings"],
  setup() {
    const sidebarBottomLeftSlot = computed(() => uiSlots["sidebar.bottom_left"] || null);
    return { sidebarBottomLeftSlot };
  },
  template: `
    <QDrawer
      class="app-mobile-nav-drawer"
      :modelValue="modelValue"
      @update:modelValue="$emit('update:modelValue', $event)"
      placement="left"
      size="272px"
      :closable="false"
      :showMask="true"
      :maskClosable="true"
      :lockScroll="true"
      @close="$emit('close')"
    >
      <div class="sidebar app-mobile-nav-shell">
        <AppSidebarControls
          :t="t"
          :endpointItems="endpointItems"
          :selectedEndpointItem="selectedEndpointItem"
          :currentPath="currentPath"
          :mobile="true"
          @endpoint-change="$emit('endpoint-change', $event)"
          @go-overview="$emit('go-overview')"
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
    </QDrawer>
  `,
};

export default AppMobileNavDrawer;
