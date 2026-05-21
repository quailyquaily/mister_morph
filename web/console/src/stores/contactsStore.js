import { defineStore } from "pinia";

import { runtimeApiFetchForEndpoint } from "../core/context";
import { loadResource, resourceKey } from "../core/resources";
import { endpointState } from "./endpointStore";

function selectedEndpointRef() {
  return String(endpointState.selectedRef || "").trim();
}

function normalizeEndpointRef(value) {
  return String(value || "").trim() || selectedEndpointRef();
}

async function fetchContacts(endpointRef) {
  const data = await runtimeApiFetchForEndpoint(endpointRef, "/contacts/list");
  return Array.isArray(data?.items) ? data.items : [];
}

const useContactsStore = defineStore("contacts", {
  state: () => ({
    endpointRef: "",
    items: [],
    loaded: false,
    loading: false,
    error: "",
    requestSeq: 0,
  }),
  actions: {
    async load(options = {}) {
      const endpointRef = normalizeEndpointRef(options.endpointRef);
      if (!endpointRef) {
        this.endpointRef = "";
        this.items = [];
        this.loaded = false;
        this.loading = false;
        this.error = "";
        return [];
      }
      if (!options.force && this.loaded && this.endpointRef === endpointRef) {
        return this.items;
      }

      const seq = this.requestSeq + 1;
      this.requestSeq = seq;
      if (this.endpointRef !== endpointRef) {
        this.endpointRef = endpointRef;
        this.items = [];
        this.loaded = false;
      }
      this.loading = true;
      this.error = "";

      try {
        const items = await loadResource(
          resourceKey("contacts", "list", endpointRef),
          () => fetchContacts(endpointRef),
          {
            cache: true,
            force: options.force === true,
          }
        );
        if (seq !== this.requestSeq || this.endpointRef !== endpointRef) {
          return items;
        }
        this.items = items;
        this.loaded = true;
        this.error = "";
        return this.items;
      } catch (err) {
        if (seq === this.requestSeq && this.endpointRef === endpointRef) {
          this.error = err?.message || "failed to load contacts";
        }
        throw err;
      } finally {
        if (seq === this.requestSeq && this.endpointRef === endpointRef) {
          this.loading = false;
        }
      }
    },
  },
});

export { useContactsStore };
