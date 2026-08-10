import "./AppSidebarControls.css";
import sidebarLogoURL from "../assets/images/app_logo_current.svg";
import { usePersonaSummary } from "../composables/usePersonaSummary";
import AgentSwitcher from "./AgentSwitcher";

const AppSidebarControls = {
  components: {
    AgentSwitcher,
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
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["endpoint-change", "go-overview", "go-settings"],
  setup() {
    const { personaAvatarURL, personaName } = usePersonaSummary();

    return {
      personaAvatarURL,
      personaName,
      sidebarLogoURL,
    };
  },
  template: `
    <AgentSwitcher
      class="sidebar-controls"
      :items="endpointItems"
      :selectedItem="selectedEndpointItem"
      :selectedAvatar="personaAvatarURL || (selectedEndpointItem && selectedEndpointItem.image) || sidebarLogoURL"
      :selectedName="personaName || (selectedEndpointItem && selectedEndpointItem.title) || t('endpoint_placeholder')"
      :placeholder="t('endpoint_placeholder')"
      @change="$emit('endpoint-change', $event)"
      @overview="$emit('go-overview')"
    />
  `,
};

export default AppSidebarControls;
