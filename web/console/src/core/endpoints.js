const CONSOLE_LOCAL_ENDPOINT_REF = "ep_console_local";

function normalizeEndpointRef(ref) {
  return typeof ref === "string" ? ref.trim() : "";
}

function normalizeEndpointMode(mode) {
  return String(mode || "").trim().toLowerCase();
}

function isConsoleLocalEndpoint(item) {
  return normalizeEndpointRef(item?.endpoint_ref) === CONSOLE_LOCAL_ENDPOINT_REF;
}

function visibleEndpoints(items, options = {}) {
  const connectedOnly = Boolean(options.connectedOnly);
  const rows = Array.isArray(items)
    ? items.filter((item) => normalizeEndpointRef(item?.endpoint_ref))
    : [];
  let visibleRows = rows;
  if (connectedOnly) {
    visibleRows = visibleRows.filter((item) => Boolean(item?.connected));
  }
  return visibleRows;
}

function endpointChannelLabel(mode, t) {
  switch (normalizeEndpointMode(mode)) {
    case "console":
      return t("endpoint_channel_console");
    case "serve":
      return t("endpoint_channel_serve");
    case "telegram":
      return t("endpoint_channel_telegram");
    case "slack":
      return t("endpoint_channel_slack");
    case "line":
      return t("endpoint_channel_line");
    case "lark":
      return t("endpoint_channel_lark");
    default:
      return String(mode || "").trim() || t("chat_readonly_unknown_channel");
  }
}

function endpointChannelTone(mode) {
  switch (normalizeEndpointMode(mode)) {
    case "console":
      return "console";
    case "serve":
      return "serve";
    case "telegram":
      return "telegram";
    case "slack":
      return "slack";
    case "line":
      return "line";
    case "lark":
      return "lark";
    default:
      return "default";
  }
}

function endpointChannelBadges(item, t) {
  return [
    {
      label: endpointChannelLabel(item?.mode, t),
      tone: endpointChannelTone(item?.mode),
    },
  ];
}

function endpointLocationLabel(item, t) {
  const raw = String(item?.url || "").trim();
  return raw || t("endpoint_location_local");
}

function endpointCompactLocationLabel(item, t) {
  const raw = String(item?.url || "").trim();
  if (!raw) {
    return t("endpoint_location_local");
  }
  try {
    const parsed = new URL(raw);
    return parsed.host || raw;
  } catch {
    return raw;
  }
}

function endpointDisplayItem(item, t) {
  const channelBadges = endpointChannelBadges(item, t);
  const channelLabel = channelBadges.map((item) => item.label).join(" + ");
  const compactLocation = endpointCompactLocationLabel(item, t);
  let title = String(item?.name || "").trim();
  if (!title) {
    title = isConsoleLocalEndpoint(item) ? channelLabel : compactLocation || channelLabel;
  }
  return {
    value: normalizeEndpointRef(item?.endpoint_ref),
    endpoint_ref: normalizeEndpointRef(item?.endpoint_ref),
    title,
    subtitle: `${channelLabel} · ${compactLocation}`,
    meta: compactLocation,
    location: endpointLocationLabel(item, t),
    channelLabel,
    channelTone: channelBadges[0]?.tone || endpointChannelTone(item?.mode),
    channelBadges,
    connected: Boolean(item?.connected),
  };
}

export {
  CONSOLE_LOCAL_ENDPOINT_REF,
  endpointChannelLabel,
  endpointDisplayItem,
  isConsoleLocalEndpoint,
  visibleEndpoints,
};
