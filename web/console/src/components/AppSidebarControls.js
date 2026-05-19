import { onBeforeUnmount, onMounted, ref } from "vue";
import "./AppSidebarControls.css";
import sidebarLogoMarkup from "../assets/images/app_logo_current.svg?raw";
import { runtimeApiDownloadForEndpoint } from "../core/context";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import { PERSONA_AVATAR_ENDPOINT, PERSONA_AVATAR_UPDATED_EVENT } from "../core/persona-profile";

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
    const avatarURL = ref("");
    let objectURL = "";

    function setAvatarURL(nextURL) {
      if (objectURL) {
        URL.revokeObjectURL(objectURL);
      }
      objectURL = nextURL || "";
      avatarURL.value = objectURL;
    }

    async function loadAvatar() {
      try {
        const blob = await runtimeApiDownloadForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, PERSONA_AVATAR_ENDPOINT);
        setAvatarURL(URL.createObjectURL(blob));
      } catch (err) {
        if (err?.status === 404 || err?.status === 401) {
          setAvatarURL("");
        }
      }
    }

    onMounted(() => {
      void loadAvatar();
      window.addEventListener(PERSONA_AVATAR_UPDATED_EVENT, loadAvatar);
    });

    onBeforeUnmount(() => {
      window.removeEventListener(PERSONA_AVATAR_UPDATED_EVENT, loadAvatar);
      setAvatarURL("");
    });

    return {
      avatarURL,
      sidebarLogoMarkup,
    };
  },
  template: `
    <section :class="mobile ? 'sidebar-controls sidebar-controls-mobile' : 'sidebar-controls'">
      <div class="sidebar-controls-row">
        <div class="sidebar-brand">
          <span class="sidebar-brand-mark" aria-hidden="true">
            <img v-if="avatarURL" class="sidebar-brand-avatar" :src="avatarURL" alt="" />
            <span v-else class="sidebar-brand-logo" v-html="sidebarLogoMarkup"></span>
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
