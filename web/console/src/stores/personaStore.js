import { defineStore } from "pinia";

import {
  runtimeApiDownloadForEndpoint,
  runtimeApiFetchForEndpoint,
} from "../core/context";
import { CONSOLE_LOCAL_ENDPOINT_REF } from "../core/endpoints";
import {
  LEGACY_IDENTITY_ENDPOINT,
  parseIdentityProfile,
  PERSONA_AVATAR_ENDPOINT,
  PERSONA_IDENTITY_ENDPOINT,
} from "../core/persona-profile";
import { endpointState } from "./endpointStore";

const identityInflightByEndpoint = new Map();
const avatarInflightByEndpoint = new Map();

function selectedEndpointRef() {
  return String(endpointState.selectedRef || "").trim() || CONSOLE_LOCAL_ENDPOINT_REF;
}

function normalizeEndpointRef(value) {
  return String(value || "").trim() || selectedEndpointRef();
}

function shouldIgnorePersonaLoadError(err) {
  return err?.status === 401 || err?.status === 404;
}

function readPersonaName(raw) {
  return String(parseIdentityProfile(raw).name || "").trim();
}

async function fetchPersonaName(endpointRef) {
  try {
    const payload = await runtimeApiFetchForEndpoint(endpointRef, PERSONA_IDENTITY_ENDPOINT);
    return readPersonaName(payload?.content || "");
  } catch (err) {
    if (!shouldIgnorePersonaLoadError(err)) {
      throw err;
    }
  }

  try {
    const payload = await runtimeApiFetchForEndpoint(endpointRef, LEGACY_IDENTITY_ENDPOINT);
    return readPersonaName(payload?.content || "");
  } catch (err) {
    if (shouldIgnorePersonaLoadError(err)) {
      return "";
    }
    throw err;
  }
}

async function fetchPersonaAvatarBlob(endpointRef) {
  try {
    return await runtimeApiDownloadForEndpoint(endpointRef, PERSONA_AVATAR_ENDPOINT);
  } catch (err) {
    if (shouldIgnorePersonaLoadError(err)) {
      return null;
    }
    throw err;
  }
}

const usePersonaStore = defineStore("persona", {
  state: () => ({
    endpointRef: "",
    name: "",
    avatarURL: "",
    identityLoaded: false,
    avatarLoaded: false,
    identityLoading: false,
    avatarLoading: false,
    identityError: "",
    avatarError: "",
    identitySeq: 0,
    avatarSeq: 0,
  }),
  actions: {
    setEndpoint(endpointRef) {
      const ref = normalizeEndpointRef(endpointRef);
      if (this.endpointRef === ref) {
        return ref;
      }
      this.endpointRef = ref;
      this.name = "";
      this.identityLoaded = false;
      this.avatarLoaded = false;
      this.identityError = "";
      this.avatarError = "";
      this.identitySeq += 1;
      this.avatarSeq += 1;
      this.setAvatarURL("");
      return ref;
    },
    setAvatarURL(nextURL) {
      const current = String(this.avatarURL || "");
      if (current && current !== nextURL && current.startsWith("blob:")) {
        URL.revokeObjectURL(current);
      }
      this.avatarURL = nextURL || "";
    },
    async loadIdentity(options = {}) {
      const endpointRef = this.setEndpoint(options.endpointRef);
      if (!options.force && this.identityLoaded && this.endpointRef === endpointRef) {
        return this.name;
      }

      const seq = this.identitySeq + 1;
      this.identitySeq = seq;
      this.identityLoading = true;
      this.identityError = "";
      let promise = null;
      try {
        promise = options.force ? null : identityInflightByEndpoint.get(endpointRef);
        if (!promise) {
          promise = fetchPersonaName(endpointRef);
          identityInflightByEndpoint.set(endpointRef, promise);
        }
        const name = await promise;
        if (seq !== this.identitySeq || this.endpointRef !== endpointRef) {
          return name;
        }
        this.name = name;
        this.identityLoaded = true;
        this.identityError = "";
        return this.name;
      } catch (err) {
        if (seq === this.identitySeq && this.endpointRef === endpointRef) {
          this.identityError = err?.message || "failed to load persona identity";
        }
        throw err;
      } finally {
        if (identityInflightByEndpoint.get(endpointRef) === promise) {
          identityInflightByEndpoint.delete(endpointRef);
        }
        if (seq === this.identitySeq && this.endpointRef === endpointRef) {
          this.identityLoading = false;
        }
      }
    },
    async loadAvatar(options = {}) {
      const endpointRef = this.setEndpoint(options.endpointRef);
      if (!options.force && this.avatarLoaded && this.endpointRef === endpointRef) {
        return this.avatarURL;
      }

      const seq = this.avatarSeq + 1;
      this.avatarSeq = seq;
      this.avatarLoading = true;
      this.avatarError = "";
      let promise = null;
      try {
        promise = options.force ? null : avatarInflightByEndpoint.get(endpointRef);
        if (!promise) {
          promise = fetchPersonaAvatarBlob(endpointRef);
          avatarInflightByEndpoint.set(endpointRef, promise);
        }
        const blob = await promise;
        const nextURL = blob ? URL.createObjectURL(blob) : "";
        if (seq !== this.avatarSeq || this.endpointRef !== endpointRef) {
          if (nextURL) {
            URL.revokeObjectURL(nextURL);
          }
          return nextURL;
        }
        this.setAvatarURL(nextURL);
        this.avatarLoaded = true;
        this.avatarError = "";
        return this.avatarURL;
      } catch (err) {
        if (seq === this.avatarSeq && this.endpointRef === endpointRef) {
          this.avatarError = err?.message || "failed to load persona avatar";
        }
        throw err;
      } finally {
        if (avatarInflightByEndpoint.get(endpointRef) === promise) {
          avatarInflightByEndpoint.delete(endpointRef);
        }
        if (seq === this.avatarSeq && this.endpointRef === endpointRef) {
          this.avatarLoading = false;
        }
      }
    },
    async loadSummary(options = {}) {
      const endpointRef = normalizeEndpointRef(options.endpointRef);
      const [identityResult, avatarResult] = await Promise.allSettled([
        this.loadIdentity({ endpointRef, force: options.force === true }),
        this.loadAvatar({ endpointRef, force: options.force === true }),
      ]);
      if (identityResult.status === "rejected") {
        throw identityResult.reason;
      }
      if (avatarResult.status === "rejected") {
        throw avatarResult.reason;
      }
      return {
        name: this.name,
        avatarURL: this.avatarURL,
      };
    },
  },
});

export { usePersonaStore };
