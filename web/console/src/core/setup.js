import { apiFetch, runtimeApiFetchForEndpoint } from "./context";
import { CONSOLE_LOCAL_ENDPOINT_REF, visibleEndpoints } from "./endpoints";
import {
  LEGACY_IDENTITY_ENDPOINT,
  LEGACY_SOUL_ENDPOINT,
  PERSONA_IDENTITY_ENDPOINT,
  PERSONA_IDENTITY_FILE,
  PERSONA_SOUL_ENDPOINT,
  PERSONA_SOUL_FILE,
} from "./persona-profile";
import { invalidateResource, loadResource, resourceKey } from "./resources";
import { SETUP_REQUIRED_MARKDOWN_FILES } from "./setup-contract";

const SETUP_DEFERRED_STATE_FILES = new Set(["HEARTBEAT.md", "SCRIPTS.md", "cron.yaml"]);
const SETUP_REPAIR_STAGE_BY_KEY = {
  config: "llm",
  identity: "persona",
  soul: "soul",
};

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
  if (stage === "repair") {
    return "/setup/repair";
  }
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

async function fetchConsoleSetupIntegrity(options = {}) {
  return loadResource(
    resourceKey("setup", "integrity"),
    async () => {
      const data = await apiFetch("/setup/integrity");
      return Array.isArray(data?.items) ? data.items : [];
    },
    {
      cache: true,
      force: options.force === true,
    }
  );
}

function blockingSetupIntegrityItems(items) {
  return Array.isArray(items)
    ? items.filter(
        (item) =>
          item &&
          typeof item.key === "string" &&
          typeof item.stage === "string" &&
          (item.status === "malformed" || item.status === "unreadable")
      )
    : [];
}

function repairStageForKey(key) {
  const normalized = typeof key === "string" ? key.trim() : "";
  return SETUP_REPAIR_STAGE_BY_KEY[normalized] || "";
}

function repairRouteForKey(key) {
  return setupStagePath(repairStageForKey(key));
}

function isAllowedRepairSetupRoute(routeLike, items) {
  const path = typeof routeLike?.path === "string" ? routeLike.path.trim() : "";
  const repairKey = typeof routeLike?.query?.repair === "string" ? routeLike.query.repair.trim() : "";
  if (!path || !repairKey) {
    return false;
  }
  const expectedPath = repairRouteForKey(repairKey);
  if (!expectedPath || path !== expectedPath) {
    return false;
  }
  return blockingSetupIntegrityItems(items).some((item) => item.key === repairKey);
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

async function consoleRuntimeTextFileInfo(endpointPath, endpointRef = CONSOLE_LOCAL_ENDPOINT_REF) {
  const ref = typeof endpointRef === "string" ? endpointRef.trim() : "";
  if (!ref) {
    return null;
  }
  try {
    const data = await runtimeApiFetchForEndpoint(ref, endpointPath);
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

async function consoleStateFilesIndex(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF) {
  const ref = typeof endpointRef === "string" ? endpointRef.trim() : "";
  if (!ref) {
    return null;
  }
  try {
    const data = await runtimeApiFetchForEndpoint(ref, "/state/files");
    const items = Array.isArray(data?.items) ? data.items : [];
    const index = new Map();
    for (const item of items) {
      const name = typeof item?.name === "string" ? item.name.trim() : "";
      if (!name) {
        continue;
      }
      index.set(name, {
        exists: item?.exists === true,
        path: typeof item?.path === "string" ? item.path : "",
        group: typeof item?.group === "string" ? item.group : "",
      });
    }
    return index;
  } catch {
    return null;
  }
}

async function ensureConsoleDeferredSetupFiles(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex) {
  const ref = typeof endpointRef === "string" ? endpointRef.trim() : "";
  if (!ref) {
    return null;
  }
  const index = stateFilesIndex === undefined ? await consoleStateFilesIndex(ref) : stateFilesIndex;
  if (!index) {
    return null;
  }
  for (const file of SETUP_REQUIRED_MARKDOWN_FILES) {
    const name = typeof file?.name === "string" ? file.name.trim() : "";
    if (!name) {
      continue;
    }
    if (!SETUP_DEFERRED_STATE_FILES.has(name)) {
      continue;
    }
    if (index.get(name)?.exists === true) {
      continue;
    }
    try {
      await runtimeApiFetchForEndpoint(ref, `/state/files/${encodeURIComponent(name)}`, {
        method: "PUT",
        body: {
          content: typeof file?.content === "string" ? file.content : "",
        },
      });
      index.set(name, { ...(index.get(name) || {}), exists: true });
    } catch {
      // Leave missing if the runtime cannot write yet.
    }
  }
  return index;
}

async function consoleIdentityExists(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex) {
  const index =
    stateFilesIndex === undefined ? await consoleStateFilesIndex(endpointRef) : stateFilesIndex;
  if (index) {
    if (index.get(PERSONA_IDENTITY_FILE)?.exists === true) {
      return true;
    }
    if (index.get("IDENTITY.md")?.exists === true) {
      return true;
    }
  }
  const info = await consoleRuntimeTextFileInfo(PERSONA_IDENTITY_ENDPOINT, endpointRef);
  if (info?.exists === true) {
    return true;
  }
  const legacy = await consoleRuntimeTextFileInfo(LEGACY_IDENTITY_ENDPOINT, endpointRef);
  if (legacy) {
    return legacy.exists === true;
  }
  return info ? false : null;
}

async function consoleSoulExists(endpointRef = CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex) {
  const index =
    stateFilesIndex === undefined ? await consoleStateFilesIndex(endpointRef) : stateFilesIndex;
  if (index) {
    if (index.get(PERSONA_SOUL_FILE)?.exists === true) {
      return true;
    }
    if (index.get("SOUL.md")?.exists === true) {
      return true;
    }
  }
  const info = await consoleRuntimeTextFileInfo(PERSONA_SOUL_ENDPOINT, endpointRef);
  if (info?.exists === true) {
    return true;
  }
  const legacy = await consoleRuntimeTextFileInfo(LEGACY_SOUL_ENDPOINT, endpointRef);
  if (legacy) {
    return legacy.exists === true;
  }
  return info ? false : null;
}

function setupReadinessSignature(items) {
  return JSON.stringify(
    visibleEndpoints(items).map((item) => {
      const normalized = normalizeEndpointItem(item);
      return [
        normalized.endpoint_ref,
        normalized.connected,
        normalized.can_submit,
        normalized.submit_endpoint_ref,
      ];
    })
  );
}

async function resolveConsoleSetupStageUncached(items) {
  const setup = buildConsoleSetupState(items);
  if (setup.requiresSetup) {
    return { stage: "llm", setup };
  }
  const local = setup?.consoleLocalEndpoint;
  if (local?.connected === true && local?.can_submit === true) {
    const stateFilesIndex = await consoleStateFilesIndex(CONSOLE_LOCAL_ENDPOINT_REF);
    const hasIdentity = await consoleIdentityExists(CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex);
    if (hasIdentity !== true) {
      return { stage: "persona", setup };
    }
    const hasSoul = await consoleSoulExists(CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex);
    if (hasSoul !== true) {
      return { stage: "soul", setup };
    }
    await ensureConsoleDeferredSetupFiles(CONSOLE_LOCAL_ENDPOINT_REF, stateFilesIndex);
  }
  return { stage: "ready", setup };
}

async function resolveConsoleSetupStage(items, options = {}) {
  return loadResource(
    resourceKey("setup", "stage", setupReadinessSignature(items)),
    () => resolveConsoleSetupStageUncached(items),
    {
      cache: true,
      force: options.force === true,
    }
  );
}

function invalidateConsoleSetupReadiness() {
  invalidateResource(resourceKey("setup"));
}

export {
  buildConsoleSetupState,
  blockingSetupIntegrityItems,
  consoleIdentityExists,
  consoleSoulExists,
  consoleSetupTargetEndpointRef,
  consoleStateFileInfo,
  fetchConsoleSetupIntegrity,
  invalidateConsoleSetupReadiness,
  isAllowedRepairSetupRoute,
  repairRouteForKey,
  resolveConsoleSetupStage,
  setupStagePath,
};
