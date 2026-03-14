import AppSidebarControls from "./AppSidebarControls";
import AppNavList from "./AppNavList";
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
  emits: ["navigate", "endpoint-change", "go-overview", "go-settings"],
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
      <AppNavList :navItems="navItems" :currentPath="currentPath" @navigate="$emit('navigate', $event)" />
    </aside>
  `,
};

export default AppSidebar;
