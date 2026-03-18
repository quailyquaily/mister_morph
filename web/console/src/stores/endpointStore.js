import { reactive } from "vue";

import { visibleEndpoints } from "../core/endpoints";

const ENDPOINT_STORAGE_KEY = "mistermorph_console_endpoint_ref_v1";

const endpointState = reactive({
  items: [],
  selectedRef: "",
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

function saveSelectedEndpointRef() {
  localStorage.setItem(ENDPOINT_STORAGE_KEY, endpointState.selectedRef);
}

function setSelectedEndpointRef(ref) {
  const next = typeof ref === "string" ? ref.trim() : "";
  if (!next) {
    endpointState.selectedRef = "";
    saveSelectedEndpointRef();
    return;
  }

  const items = Array.isArray(endpointState.items) ? endpointState.items : [];
  if (items.length === 0) {
    endpointState.selectedRef = next;
    saveSelectedEndpointRef();
    return;
  }

  const canSelect = visibleEndpoints(items, { connectedOnly: true }).some(
    (item) => item.endpoint_ref === next && isConnectedEndpoint(item)
  );
  endpointState.selectedRef = canSelect ? next : firstConnectedEndpointRef(items);
  saveSelectedEndpointRef();
}

function hydrateEndpointSelection() {
  const ref = localStorage.getItem(ENDPOINT_STORAGE_KEY);
  endpointState.selectedRef = typeof ref === "string" ? ref.trim() : "";
}

function ensureEndpointSelection() {
  const items = Array.isArray(endpointState.items) ? endpointState.items : [];
  const connectedItems = visibleEndpoints(items, { connectedOnly: true }).filter((item) =>
    isConnectedEndpoint(item)
  );
  if (connectedItems.length === 0) {
    setSelectedEndpointRef("");
    return;
  }
  const current = endpointState.selectedRef.trim();
  if (current && connectedItems.find((item) => item.endpoint_ref === current)) {
    return;
  }
  setSelectedEndpointRef(connectedItems[0].endpoint_ref);
}

export {
  endpointState,
  setSelectedEndpointRef,
  hydrateEndpointSelection,
  ensureEndpointSelection,
};
