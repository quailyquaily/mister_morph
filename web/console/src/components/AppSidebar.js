import { computed } from "vue";

import AppSidebarControls from "./AppSidebarControls";
import AppNavList from "./AppNavList";
import { uiSlots } from "../ext/slots";
import "./AppSidebar.css";

const AppSidebar = {
  components: {
    AppSidebarControls,
    AppNavList,
  },
  props: {
    endpointItems: {
      type: Array,
      required: true,
    },
    selectedEndpointItem: {
      type: Object,
      default: null,
    },
    navItems: {
      type: Array,
      required: true,
    },
    currentPath: {
      type: String,
      required: true,
    },
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["navigate", "preload", "endpoint-change", "go-overview", "go-settings"],
  setup() {
    const sidebarBottomLeftSlot = computed(() => uiSlots["sidebar.bottom_left"] || null);
    return { sidebarBottomLeftSlot };
  },
  template: `
    <aside class="sidebar">
      <AppSidebarControls
        :t="t"
        :endpointItems="endpointItems"
        :selectedEndpointItem="selectedEndpointItem"
        :currentPath="currentPath"
        @endpoint-change="$emit('endpoint-change', $event)"
        @go-overview="$emit('go-overview')"
        @go-settings="$emit('go-settings')"
      />
      <AppNavList
        :navItems="navItems"
        :currentPath="currentPath"
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
    </aside>
  `,
};

export default AppSidebar;
