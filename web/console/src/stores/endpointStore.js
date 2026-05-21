import { defineStore } from "pinia";

import { visibleEndpoints } from "../core/endpoints";
import { pinia } from "./pinia";

const ENDPOINT_STORAGE_KEY = "mistermorph_console_endpoint_ref_v1";

const useEndpointStore = defineStore("endpoint", {
  state: () => ({
    items: [],
    selectedRef: "",
  }),
  actions: {
    saveSelectedEndpointRef() {
      localStorage.setItem(ENDPOINT_STORAGE_KEY, this.selectedRef);
    },
    setSelectedEndpointRef(ref) {
      const next = typeof ref === "string" ? ref.trim() : "";
      if (!next) {
        this.selectedRef = "";
        this.saveSelectedEndpointRef();
        return;
      }

      const items = Array.isArray(this.items) ? this.items : [];
      if (items.length === 0) {
        this.selectedRef = next;
        this.saveSelectedEndpointRef();
        return;
      }

      const canSelect = visibleEndpoints(items, { connectedOnly: true }).some(
        (item) => item.endpoint_ref === next && isConnectedEndpoint(item)
      );
      this.selectedRef = canSelect ? next : firstConnectedEndpointRef(items);
      this.saveSelectedEndpointRef();
    },
    hydrateEndpointSelection() {
      const ref = localStorage.getItem(ENDPOINT_STORAGE_KEY);
      this.selectedRef = typeof ref === "string" ? ref.trim() : "";
    },
    ensureEndpointSelection() {
      const items = Array.isArray(this.items) ? this.items : [];
      const connectedItems = visibleEndpoints(items, { connectedOnly: true }).filter((item) =>
        isConnectedEndpoint(item)
      );
      if (connectedItems.length === 0) {
        this.setSelectedEndpointRef("");
        return;
      }
      const current = this.selectedRef.trim();
      if (current && connectedItems.find((item) => item.endpoint_ref === current)) {
        return;
      }
      this.setSelectedEndpointRef(connectedItems[0].endpoint_ref);
    },
  },
});

function isConnectedEndpoint(item) {
  return Boolean(item && item.endpoint_ref && item.connected);
}

function firstConnectedEndpointRef(items) {
  const connected = visibleEndpoints(items, { connectedOnly: true }).find((item) =>
    isConnectedEndpoint(item)
  );
  return connected ? connected.endpoint_ref : "";
}

const endpointState = useEndpointStore(pinia);

export { endpointState, useEndpointStore };
