import { onBeforeUnmount, onMounted, watch } from "vue";
import { storeToRefs } from "pinia";

import { endpointState } from "../core/context";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import {
  PERSONA_AVATAR_UPDATED_EVENT,
  PERSONA_IDENTITY_UPDATED_EVENT,
} from "../core/persona-profile";
import { usePersonaStore } from "../stores/personaStore";

export function usePersonaSummary() {
  const personaStore = usePersonaStore();
  const { avatarURL: personaAvatarURL, name: personaName } = storeToRefs(personaStore);

  function selectedPersonaEndpointRef() {
    return String(endpointState.selectedRef || "").trim() || CONSOLE_LOCAL_ENDPOINT_REF;
  }

  async function loadPersonaSummary(options = {}) {
    await personaStore
      .loadSummary({
        endpointRef: selectedPersonaEndpointRef(),
        force: options.force === true,
        perfSource: "shared-preload",
      })
      .catch(() => {});
  }

  const handlePersonaIdentityUpdated = () => {
    void personaStore
      .loadIdentity({
        endpointRef: selectedPersonaEndpointRef(),
        force: true,
        perfSource: "shared-preload",
      })
      .catch(() => {});
  };
  const handlePersonaAvatarUpdated = () => {
    void personaStore
      .loadAvatar({
        endpointRef: selectedPersonaEndpointRef(),
        force: true,
        perfSource: "shared-preload",
      })
      .catch(() => {});
  };

  onMounted(() => {
    void loadPersonaSummary();
    window.addEventListener(PERSONA_IDENTITY_UPDATED_EVENT, handlePersonaIdentityUpdated);
    window.addEventListener(PERSONA_AVATAR_UPDATED_EVENT, handlePersonaAvatarUpdated);
  });

  watch(
    () => endpointState.selectedRef,
    () => {
      void loadPersonaSummary();
    }
  );

  onBeforeUnmount(() => {
    window.removeEventListener(PERSONA_IDENTITY_UPDATED_EVENT, handlePersonaIdentityUpdated);
    window.removeEventListener(PERSONA_AVATAR_UPDATED_EVENT, handlePersonaAvatarUpdated);
  });

  return {
    personaAvatarURL,
    personaName,
    loadPersonaSummary,
  };
}
