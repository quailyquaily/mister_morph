import "./AppSidebarControls.css";

const AppSidebarControls = {
  props: {
    endpointItems: {
      type: Array,
      required: true,
    },
    selectedEndpointItem: {
      type: Object,
      default: null,
    },
    currentPath: {
      type: String,
      required: true,
    },
    mobile: {
      type: Boolean,
      default: false,
    },
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["endpoint-change", "go-overview", "go-settings"],
  methods: {
    shortcutClass(kind) {
      if (kind === "settings" && this.currentPath === "/settings") {
        return "outlined xs icon sidebar-shortcut is-active";
      }
      return "outlined xs icon sidebar-shortcut";
    },
  },
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <div class="sidebar-endpoint">
          <QDropdownMenu
            class="xs"
            :items="endpointItems"
            :initialItem="selectedEndpointItem"
            :placeholder="t('endpoint_placeholder')"
            :hideSelected="true"
            @change="$emit('endpoint-change', $event)"
          >
            <div v-if="selectedEndpointItem" class="sidebar-endpoint-selected">
              <span class="sidebar-endpoint-name">{{ selectedEndpointItem.title }}</span>
            </div>
            <span v-else class="sidebar-endpoint-placeholder">{{ t('endpoint_placeholder') }}</span>
          </QDropdownMenu>
        </div>
        <div class="sidebar-shortcuts">
          <QButton
            :class="shortcutClass('overview')"
            :title="t('nav_overview')"
            :aria-label="t('nav_overview')"
            @click="$emit('go-overview')"
          >
            <QIconGrid class="icon" />
          </QButton>
          <QButton
            :class="shortcutClass('settings')"
            :title="t('nav_settings')"
            :aria-label="t('nav_settings')"
            @click="$emit('go-settings')"
          >
            <QIconSettings class="icon" />
          </QButton>
        </div>
      </div>
    </section>
  `,
};

export default AppSidebarControls;
