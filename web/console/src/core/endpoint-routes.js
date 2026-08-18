import { CONSOLE_LOCAL_ENDPOINT_REF } from "./endpoints.js";

const DEFAULT_ENDPOINT_ROUTE_REF = "default";
const ENDPOINT_ROUTE_PREFIX = "/e";

function normalizeRouteParam(value) {
  const raw = Array.isArray(value) ? value[0] : value;
  return String(raw || "").trim();
}

function endpointRouteRef(endpointRef) {
  const ref = normalizeRouteParam(endpointRef);
  if (!ref) {
    return "";
  }
  return ref === CONSOLE_LOCAL_ENDPOINT_REF ? DEFAULT_ENDPOINT_ROUTE_REF : ref;
}

function endpointRefFromRouteParam(routeRef) {
  const ref = normalizeRouteParam(routeRef);
  if (!ref) {
    return "";
  }
  return ref.toLowerCase() === DEFAULT_ENDPOINT_ROUTE_REF
    ? CONSOLE_LOCAL_ENDPOINT_REF
    : ref;
}

function endpointRoutePath(endpointRef, pagePath) {
  const routeRef = endpointRouteRef(endpointRef);
  if (!routeRef) {
    return "";
  }
  let suffix = String(pagePath || "").trim();
  if (!suffix || suffix === "/") {
    suffix = "";
  } else if (!suffix.startsWith("/")) {
    suffix = `/${suffix}`;
  }
  return `${ENDPOINT_ROUTE_PREFIX}/${encodeURIComponent(routeRef)}${suffix}`;
}

function endpointPagePath(path) {
  const value = String(path || "").trim();
  const match = value.match(/^\/e\/[^/]+(?=\/|$)/);
  if (!match) {
    return "";
  }
  return value.slice(match[0].length) || "/";
}

function endpointSwitchPath(endpointRef, currentPath, chatTopicID = "") {
  let pagePath = endpointPagePath(currentPath);
  if (!pagePath) {
    pagePath = "/chat";
  }
  if (pagePath === "/chat" || pagePath.startsWith("/chat/")) {
    const topicID = String(chatTopicID || "").trim();
    pagePath = topicID ? `/chat/${encodeURIComponent(topicID)}` : "/chat";
  }
  return endpointRoutePath(endpointRef, pagePath);
}

export {
  ENDPOINT_ROUTE_PREFIX,
  endpointPagePath,
  endpointRefFromRouteParam,
  endpointRoutePath,
  endpointRouteRef,
  endpointSwitchPath,
};
