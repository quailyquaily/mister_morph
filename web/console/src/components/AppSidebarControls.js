import "./AppSidebarControls.css";
import sidebarLogoMarkup from "../assets/images/app_logo_current.svg?raw";
import { usePersonaSummary } from "../composables/usePersonaSummary";

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
  setup() {
    const { personaAvatarURL, personaName } = usePersonaSummary();

    return {
      avatarURL: personaAvatarURL,
      personaName,
      sidebarLogoMarkup,
    };
  },
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <button
          class="sidebar-brand"
          type="button"
          :title="t('nav_overview')"
          :aria-label="t('nav_overview')"
          @click="$emit('go-overview')"
        >
          <span class="sidebar-brand-mark" aria-hidden="true">
            <img v-if="avatarURL" class="sidebar-brand-avatar" :src="avatarURL" alt="" />
            <span v-else class="sidebar-brand-logo" v-html="sidebarLogoMarkup"></span>
          </span>
          <span v-if="personaName" class="sidebar-brand-name">{{ personaName }}</span>
        </button>
      </div>
    </section>
  `,
};

export default AppSidebarControls;
