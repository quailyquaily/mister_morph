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
      status: String(raw?.status || "").trim().toLowerCase(),
      toolName: String(raw?.tool_name || "").trim(),
      reasons: Array.isArray(raw?.reasons)
        ? raw.reasons.map((reason) => String(reason || "").trim()).filter(Boolean)
        : [],
      toolParams: params && typeof params === "object" && !Array.isArray(params) ? params : null,
    });
  }
  return details;
}

function taskApprovalState(task) {
  const taskStatus = String(task?.status || "").trim().toLowerCase();
  const output = task?.result?.final?.output;
  const approvalRequestID = String(task?.approval_request_id || output?.approval_request_id || "").trim();
  if (!approvalRequestID) {
    return null;
  }
  if (taskStatus === "pending") {
    return {
      approvalRequestID,
      message: String(output?.message || "").trim(),
      status: "pending",
    };
  }
  if (taskStatus !== "canceled") {
    return null;
  }
  const message = String(task?.error || "").trim();
  if (message === "Approval denied. Task canceled.") {
    return { approvalRequestID, message, status: "denied" };
  }
  if (message === "Approval expired. Task canceled.") {
    return { approvalRequestID, message, status: "expired" };
  }
  return null;
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

export { approvalDetailsByID, approvalParameterEntries, taskApprovalState };
