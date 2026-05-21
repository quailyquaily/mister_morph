import { onBeforeUnmount, onMounted, ref, watch } from "vue";

import { endpointState, runtimeApiDownloadForEndpoint, runtimeApiFetchForEndpoint } from "../core/context";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import {
  LEGACY_IDENTITY_ENDPOINT,
  parseIdentityProfile,
  PERSONA_AVATAR_ENDPOINT,
  PERSONA_AVATAR_UPDATED_EVENT,
  PERSONA_IDENTITY_ENDPOINT,
  PERSONA_IDENTITY_UPDATED_EVENT,
} from "../core/persona-profile";

function shouldIgnorePersonaLoadError(err) {
  return err?.status === 401 || err?.status === 404;
}

function readPersonaName(raw) {
  return String(parseIdentityProfile(raw).name || "").trim();
}

export function usePersonaSummary() {
  const personaName = ref("");
  const personaAvatarURL = ref("");
  let personaAvatarObjectURL = "";
  let identityLoadSeq = 0;
  let avatarLoadSeq = 0;

  function selectedPersonaEndpointRef() {
    return String(endpointState.selectedRef || "").trim() || CONSOLE_LOCAL_ENDPOINT_REF;
  }

  function isCurrentIdentityLoad(seq, endpointRef) {
    return seq === identityLoadSeq && endpointRef === selectedPersonaEndpointRef();
  }

  function isCurrentAvatarLoad(seq, endpointRef) {
    return seq === avatarLoadSeq && endpointRef === selectedPersonaEndpointRef();
  }

  function setPersonaAvatarURL(nextURL) {
    if (personaAvatarObjectURL) {
      URL.revokeObjectURL(personaAvatarObjectURL);
    }
    personaAvatarObjectURL = nextURL || "";
    personaAvatarURL.value = personaAvatarObjectURL;
  }

  async function loadPersonaIdentity() {
    const endpointRef = selectedPersonaEndpointRef();
    const seq = ++identityLoadSeq;
    try {
      const payload = await runtimeApiFetchForEndpoint(endpointRef, PERSONA_IDENTITY_ENDPOINT);
      if (!isCurrentIdentityLoad(seq, endpointRef)) {
        return;
      }
      personaName.value = readPersonaName(payload?.content || "");
      return;
    } catch (err) {
      if (!isCurrentIdentityLoad(seq, endpointRef)) {
        return;
      }
      if (!shouldIgnorePersonaLoadError(err)) {
        throw err;
      }
    }

    try {
      const payload = await runtimeApiFetchForEndpoint(endpointRef, LEGACY_IDENTITY_ENDPOINT);
      if (!isCurrentIdentityLoad(seq, endpointRef)) {
        return;
      }
      personaName.value = readPersonaName(payload?.content || "");
    } catch (err) {
      if (!isCurrentIdentityLoad(seq, endpointRef)) {
        return;
      }
      if (shouldIgnorePersonaLoadError(err)) {
        personaName.value = "";
        return;
      }
      throw err;
    }
  }

  async function loadPersonaAvatar() {
    const endpointRef = selectedPersonaEndpointRef();
    const seq = ++avatarLoadSeq;
    try {
      const blob = await runtimeApiDownloadForEndpoint(endpointRef, PERSONA_AVATAR_ENDPOINT);
      const nextURL = URL.createObjectURL(blob);
      if (!isCurrentAvatarLoad(seq, endpointRef)) {
        URL.revokeObjectURL(nextURL);
        return;
      }
      setPersonaAvatarURL(nextURL);
    } catch (err) {
      if (!isCurrentAvatarLoad(seq, endpointRef)) {
        return;
      }
      if (shouldIgnorePersonaLoadError(err)) {
        setPersonaAvatarURL("");
        return;
      }
      throw err;
    }
  }

  async function loadPersonaSummary() {
    await Promise.allSettled([loadPersonaIdentity(), loadPersonaAvatar()]);
  }

  onMounted(() => {
    void loadPersonaSummary();
    window.addEventListener(PERSONA_IDENTITY_UPDATED_EVENT, loadPersonaIdentity);
    window.addEventListener(PERSONA_AVATAR_UPDATED_EVENT, loadPersonaAvatar);
  });

  watch(
    () => endpointState.selectedRef,
    () => {
      personaName.value = "";
      setPersonaAvatarURL("");
      void loadPersonaSummary();
    }
  );

  onBeforeUnmount(() => {
    window.removeEventListener(PERSONA_IDENTITY_UPDATED_EVENT, loadPersonaIdentity);
    window.removeEventListener(PERSONA_AVATAR_UPDATED_EVENT, loadPersonaAvatar);
    setPersonaAvatarURL("");
  });

  return {
    personaAvatarURL,
    personaName,
    loadPersonaSummary,
  };
}
