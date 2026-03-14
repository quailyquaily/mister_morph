import AppSidebarControls from "./AppSidebarControls";
import AppNavList from "./AppNavList";

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
  emits: ["update:modelValue", "close", "navigate", "endpoint-change", "go-overview", "go-settings"],
  template: `
    <QDrawer
      :modelValue="modelValue"
      @update:modelValue="$emit('update:modelValue', $event)"
      :title="title"
      placement="left"
      size="272px"
      :showMask="true"
      :maskClosable="true"
      :lockScroll="true"
      @close="$emit('close')"
    >
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
        @navigate="$emit('navigate', $event)"
      />
    </QDrawer>
  `,
};

export default AppMobileNavDrawer;
