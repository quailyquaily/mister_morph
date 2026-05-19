import { onBeforeUnmount, onMounted, ref } from "vue";

import { runtimeApiDownloadForEndpoint, runtimeApiFetchForEndpoint } from "../core/context";
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

  function setPersonaAvatarURL(nextURL) {
    if (personaAvatarObjectURL) {
      URL.revokeObjectURL(personaAvatarObjectURL);
    }
    personaAvatarObjectURL = nextURL || "";
    personaAvatarURL.value = personaAvatarObjectURL;
  }

  async function loadPersonaIdentity() {
    try {
      const payload = await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, PERSONA_IDENTITY_ENDPOINT);
      personaName.value = readPersonaName(payload?.content || "");
      return;
    } catch (err) {
      if (!shouldIgnorePersonaLoadError(err)) {
        throw err;
      }
    }

    try {
      const payload = await runtimeApiFetchForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, LEGACY_IDENTITY_ENDPOINT);
      personaName.value = readPersonaName(payload?.content || "");
    } catch (err) {
      if (shouldIgnorePersonaLoadError(err)) {
        personaName.value = "";
        return;
      }
      throw err;
    }
  }

  async function loadPersonaAvatar() {
    try {
      const blob = await runtimeApiDownloadForEndpoint(CONSOLE_LOCAL_ENDPOINT_REF, PERSONA_AVATAR_ENDPOINT);
      setPersonaAvatarURL(URL.createObjectURL(blob));
    } catch (err) {
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
