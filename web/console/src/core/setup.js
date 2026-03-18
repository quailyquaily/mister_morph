import { runtimeApiFetchForEndpoint } from "./context";
import { CONSOLE_LOCAL_ENDPOINT_REF, visibleEndpoints } from "./endpoints";

function normalizeEndpointItem(item) {
  return {
    endpoint_ref: typeof item?.endpoint_ref === "string" ? item.endpoint_ref.trim() : "",
    name: typeof item?.name === "string" ? item.name : "",
    url: typeof item?.url === "string" ? item.url : "",
    mode: typeof item?.mode === "string" ? item.mode : "",
    connected: item?.connected === true,
    can_submit: item?.can_submit === true,
    agent_name: typeof item?.agent_name === "string" ? item.agent_name : "",
    submit_endpoint_ref:
      typeof item?.submit_endpoint_ref === "string" ? item.submit_endpoint_ref.trim() : "",
  };
}

function buildConsoleSetupState(items) {
  const endpoints = visibleEndpoints(items).map(normalizeEndpointItem);
  const connectedEndpoints = endpoints.filter((item) => item.connected === true);
  const chatReadyEndpoints = endpoints.filter((item) => item.connected === true && item.can_submit === true);
  const consoleLocalEndpoint =
    endpoints.find((item) => item.endpoint_ref === CONSOLE_LOCAL_ENDPOINT_REF) || null;
  const requiresSetup =
    chatReadyEndpoints.length === 0 &&
    consoleLocalEndpoint?.connected === true &&
    consoleLocalEndpoint?.can_submit !== true;

  return {
    endpoints,
    connectedEndpoints,
    chatReadyEndpoints,
    consoleLocalEndpoint,
    requiresSetup,
    primaryChatReadyEndpoint: chatReadyEndpoints[0] || null,
  };
}

function consoleSetupTargetEndpointRef(state) {
  const local = state?.consoleLocalEndpoint;
  if (local?.connected === true && local?.can_submit === true && local?.endpoint_ref) {
    return local.endpoint_ref;
  }
  return state?.primaryChatReadyEndpoint?.endpoint_ref || "";
}

function setupStagePath(stage) {
  if (stage === "persona") {
    return "/setup/persona";
  }
  if (stage === "soul") {
    return "/setup/soul";
  }
  if (stage === "done") {
    return "/setup/done";
  }
  return "/setup/llm";
}

async function consoleStateFileInfo(fileName, endpointRef = CONSOLE_LOCAL_ENDPOINT_REF) {
  const ref = typeof endpointRef === "string" ? endpointRef.trim() : "";
  if (!ref) {
    return null;
  }
  try {
    const name = encodeURIComponent(String(fileName || "").trim());
    const data = await runtimeApiFetchForEndpoint(ref, `/state/files/${name}`);
    const content = typeof data?.content === "string" ? data.content : "";
    return {
      exists: true,
      content,
    };
  } catch (err) {
    if (err?.status === 404) {
      return {
        exists: false,
        content: "",
      };
    }
    return null;
  }
}

async function consoleIdentityExists(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF) {
  const info = await consoleStateFileInfo("IDENTITY.md", endpointRef);
  if (!info) {
    return null;
  }
  return info.exists === true;
}

async function consoleSoulExists(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF) {
  const info = await consoleStateFileInfo("SOUL.md", endpointRef);
  if (!info) {
    return null;
  }
  return info.exists === true;
}

async function resolveConsoleSetupStage(items) {
  const setup = buildConsoleSetupState(items);
  if (setup.requiresSetup) {
    return { stage: "llm", setup };
  }
  const local = setup?.consoleLocalEndpoint;
  if (local?.connected === true && local?.can_submit === true) {
    const hasIdentity = await consoleIdentityExists(CONSOLE_LOCAL_ENDPOINT_REF);
    if (hasIdentity === false) {
      return { stage: "persona", setup };
    }
    const soulExists = await consoleSoulExists(CONSOLE_LOCAL_ENDPOINT_REF);
    if (soulExists === false) {
      return { stage: "soul", setup };
    }
  }
  return { stage: "ready", setup };
}

export {
  buildConsoleSetupState,
  consoleIdentityExists,
  consoleSoulExists,
  consoleSetupTargetEndpointRef,
  consoleStateFileInfo,
  resolveConsoleSetupStage,
  setupStagePath,
};
