import { onUnmounted, ref, watch } from "vue";

const resourceCache = new Map();
const resourceInflight = new Map();
const resourceVersions = new Map();

function resourceKey(...parts) {
  return parts.map((part) => encodeURIComponent(String(part ?? ""))).join(":");
}

function normalizeResourceKey(value) {
  return String(value || "").trim();
}

function resolveValue(value) {
  if (typeof value === "function") {
    return value();
  }
  if (value && typeof value === "object" && "value" in value) {
    return value.value;
  }
  return value;
}

function resolveBool(value) {
  return Boolean(resolveValue(value));
}

function resourceVersion(key) {
  return resourceVersions.get(key) || 0;
}

function bumpResourceVersion(key) {
  resourceVersions.set(key, resourceVersion(key) + 1);
}

async function loadResource(key, loader, options = {}) {
  const normalizedKey = normalizeResourceKey(key);
  if (!normalizedKey) {
    throw new Error("missing resource key");
  }
  if (typeof loader !== "function") {
    throw new Error("missing resource loader");
  }

  const force = options.force === true;
  const cache = options.cache === true;
  if (force) {
    bumpResourceVersion(normalizedKey);
    resourceCache.delete(normalizedKey);
    resourceInflight.delete(normalizedKey);
  }
  if (!force && cache && resourceCache.has(normalizedKey)) {
    return resourceCache.get(normalizedKey);
  }

  const inflight = resourceInflight.get(normalizedKey);
  if (inflight) {
    return inflight;
  }

  const version = resourceVersion(normalizedKey);
  const promise = Promise.resolve()
    .then(loader)
    .then((value) => {
      if (cache && version === resourceVersion(normalizedKey)) {
        resourceCache.set(normalizedKey, value);
      }
      return value;
    })
    .finally(() => {
      if (resourceInflight.get(normalizedKey) === promise) {
        resourceInflight.delete(normalizedKey);
      }
    });

  resourceInflight.set(normalizedKey, promise);
  return promise;
}

function invalidateResource(keyOrPrefix = "") {
  const value = normalizeResourceKey(keyOrPrefix);
  if (!value) {
    for (const key of new Set([...resourceCache.keys(), ...resourceInflight.keys()])) {
      bumpResourceVersion(key);
    }
    resourceCache.clear();
    resourceInflight.clear();
    return;
  }
  for (const key of new Set([...resourceCache.keys(), ...resourceInflight.keys()])) {
    if (key === value || key.startsWith(`${value}:`)) {
      bumpResourceVersion(key);
      resourceCache.delete(key);
      resourceInflight.delete(key);
    }
  }
}

function clearResourceStateForTests() {
  resourceCache.clear();
  resourceInflight.clear();
  resourceVersions.clear();
}

function useResource(options) {
  const data = ref(options?.initialData ?? null);
  const error = ref(null);
  const loading = ref(false);
  const key = ref("");
  const enabled = options?.enabled ?? true;
  const immediate = options?.immediate !== false;
  const cache = options?.cache === true;
  let version = 0;
  let disposed = false;

  async function refresh(refreshOptions = {}) {
    const nextKey = normalizeResourceKey(resolveValue(options?.key));
    key.value = nextKey;
    const isEnabled = resolveBool(enabled);
    if (!isEnabled || !nextKey) {
      version += 1;
      loading.value = false;
      error.value = null;
      if (options?.clearOnDisabled !== false) {
        data.value = options?.initialData ?? null;
      }
      return null;
    }

    const currentVersion = version + 1;
    version = currentVersion;
    loading.value = true;
    error.value = null;
    try {
      const value = await loadResource(
        nextKey,
        () =>
          options.load({
            key: nextKey,
            force: refreshOptions.force === true,
          }),
        {
          force: refreshOptions.force === true,
          cache,
        }
      );
      if (!disposed && currentVersion === version) {
        data.value = value;
      }
      return value;
    } catch (e) {
      if (!disposed && currentVersion === version) {
        error.value = e;
      }
      return null;
    } finally {
      if (!disposed && currentVersion === version) {
        loading.value = false;
      }
    }
  }

  watch(
    () => `${resolveBool(enabled) ? "1" : "0"}\u0000${normalizeResourceKey(resolveValue(options?.key))}`,
    () => {
      void refresh();
    },
    { immediate }
  );

  onUnmounted(() => {
    disposed = true;
    version += 1;
  });

  return {
    data,
    error,
    loading,
    key,
    refresh,
  };
}

export {
  clearResourceStateForTests,
  invalidateResource,
  loadResource,
  resourceKey,
  useResource,
};
