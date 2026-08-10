import { computed } from "vue";

import "./AppSidebarControls.css";
import sidebarLogoURL from "../assets/images/app_logo_current.svg";
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
  emits: ["endpoint-change", "go-settings"],
  setup(props) {
    const { personaAvatarURL, personaName } = usePersonaSummary();
    const endpointMenuIsLong = computed(() => props.endpointItems.length > 8);

    return {
      endpointMenuIsLong,
      personaAvatarURL,
      personaName,
      sidebarLogoURL,
    };
  },
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <div class="sidebar-endpoint">
          <QDropdownMenu
            class="xs"
            :items="endpointItems"
            :disabled="endpointItems.length === 0"
            :useFilter="endpointMenuIsLong"
            :useDialog="endpointMenuIsLong ? 'always' : 'auto'"
            :scrollHeight="endpointMenuIsLong ? 'min(42dvh, 320px)' : 'auto'"
            variant="plain"
            @change="$emit('endpoint-change', $event)"
          >
            <span class="sidebar-endpoint-selected">
              <img
                class="sidebar-endpoint-avatar"
                :src="personaAvatarURL || (selectedEndpointItem && selectedEndpointItem.image) || sidebarLogoURL"
                alt=""
              />
              <span class="sidebar-endpoint-name">
                {{ personaName || (selectedEndpointItem ? selectedEndpointItem.title : t('endpoint_placeholder')) }}
              </span>
            </span>
          </QDropdownMenu>
        </div>
      </div>
    </section>
  `,
};

export default AppSidebarControls;
