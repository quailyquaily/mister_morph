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
    shortcutClass() {
      const value = typeof this.currentPath === "string" ? this.currentPath.trim() : "";
      if (value === "/overview") {
        return "sidebar-shortcut-link is-active";
      }
      return "sidebar-shortcut-link";
    },
  },
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <div class="sidebar-brand">
          <span class="sidebar-brand-mark" aria-hidden="true">
            <svg class="sidebar-brand-logo" viewBox="0 0 24 24" role="presentation">
              <path d="M3 11h18" />
              <path d="M5 11V7a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v4" />
              <path d="M7 17m-3 0a3 3 0 1 0 6 0a3 3 0 1 0-6 0" />
              <path d="M17 17m-3 0a3 3 0 1 0 6 0a3 3 0 1 0-6 0" />
              <path d="M10 17h4" />
            </svg>
          </span>
          <span class="sidebar-brand-name">mistermorph</span>
        </div>
        <div class="sidebar-shortcuts">
          <a
            :class="shortcutClass()"
            href="/overview"
            :title="t('nav_overview')"
            :aria-label="t('nav_overview')"
            @click.prevent="$emit('go-overview')"
          >
            <QIconGrid class="icon" />
          </a>
        </div>
      </div>
    </section>
  `,
};

export default AppSidebarControls;
