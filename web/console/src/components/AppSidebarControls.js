import "./AppSidebarControls.css";
import sidebarLogoMarkup from "../assets/images/app_logo_current.svg?raw";

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
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <div class="sidebar-brand">
          <span class="sidebar-brand-mark" aria-hidden="true">
            ${sidebarLogoMarkup}
          </span>
        </div>
        <div class="sidebar-shortcuts">
          <QButton
            class="stripe xs icon"
            type="plain"
            :title="t('nav_overview')"
            :aria-label="t('nav_overview')"
            @click="$emit('go-overview')"
          >
            <QIconEcosystem class="icon" />
          </QButton>
        </div>
      </div>
    </section>
  `,
};

export default AppSidebarControls;
