function approvalDetailsByID(payload) {
  const details = new Map();
  const items = Array.isArray(payload?.items) ? payload.items : [];
  for (const raw of items) {
    const approvalRequestID = String(raw?.approval_request_id || "").trim();
    if (!approvalRequestID) {
      continue;
    }
    const params = raw?.tool_params;
    details.set(approvalRequestID, {
      approvalRequestID,
      toolName: String(raw?.tool_name || "").trim(),
      reasons: Array.isArray(raw?.reasons)
        ? raw.reasons.map((reason) => String(reason || "").trim()).filter(Boolean)
        : [],
      toolParams: params && typeof params === "object" && !Array.isArray(params) ? params : null,
    });
  }
  return details;
}

function approvalParameterEntries(params) {
  if (!params || typeof params !== "object" || Array.isArray(params)) {
    return [];
  }
  return Object.entries(params)
    .sort(([left], [right]) => Number(right === "cmd") - Number(left === "cmd"))
    .map(([name, rawValue]) => {
      let value = rawValue;
      if (typeof value !== "string") {
        try {
          value = JSON.stringify(value, null, 2);
        } catch {
          value = String(value);
        }
      }
      return {
        name,
        value: String(value ?? ""),
        command: name === "cmd",
        multiline: String(value ?? "").includes("\n"),
      };
    });
}

export { approvalDetailsByID, approvalParameterEntries };
