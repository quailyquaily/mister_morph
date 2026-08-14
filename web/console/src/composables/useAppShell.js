import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import defaultEndpointAvatarURL from "../assets/images/app_logo_current.svg";
import { lastTopicID } from "../core/chat-topic-memory";
import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import {
  authValid,
  endpointState,
  ensureEndpointsLoaded,
  runtimeEndpointByRef,
  translate,
} from "../core/context";
import { NAV_ITEMS_META, preloadRouteComponent } from "../router";
import { useContactsStore } from "../stores/contactsStore";
import { usePersonaStore } from "../stores/personaStore";

function chatRoutePath(topicID = "") {
  const normalizedTopicID = String(topicID || "").trim();
  return normalizedTopicID ? `/chat/${encodeURIComponent(normalizedTopicID)}` : "/chat";
}

function chatSubmitEndpointRef(endpointRef) {
  const selected = runtimeEndpointByRef(endpointRef);
  if (!selected) {
    return "";
  }
  const mapped = String(selected.submit_endpoint_ref || "").trim();
  if (mapped) {
    return mapped;
  }
  return selected.can_submit ? String(selected.endpoint_ref || "").trim() : "";
}

function useAppShell() {
  const t = translate;
  const router = useRouter();
  const route = useRoute();
  const contactsStore = useContactsStore();
  const personaStore = usePersonaStore();
  const inLogin = computed(() => route.path === "/login");
  const inShellless = computed(() => route.meta && route.meta.shellless === true);
  const inOverview = computed(() => route.path === "/overview");
  const inSetup = computed(() => route.path === "/setup" || route.path.startsWith("/setup/"));
  const inAgentDesk = computed(() => route.path === "/chat/desk");
  const inStandalone = computed(() => inOverview.value || inSetup.value || inAgentDesk.value);
  const inWorkspacePage = computed(() => !inShellless.value && !inStandalone.value);
  const currentPath = computed(() => route.path);
  const navItems = computed(() =>
    NAV_ITEMS_META.map((item) =>
      item.separator
        ? { id: item.id, separator: true }
        : {
            id: item.id,
            title: t(item.titleKey),
            icon: item.icon || "",
          }
    )
  );
  const mobileNavOpen = ref(false);
  const mobileMode = ref(window.innerWidth <= 980);
  const endpointItems = computed(() =>
    visibleEndpoints(endpointState.items).map((item) => {
      const display = endpointDisplayItem(item, t);
      return {
        id: display.value,
        title: String(item?.agent_name || "").trim() || display.title,
        value: display.value,
        image: String(item?.avatar_url || "").trim() || defaultEndpointAvatarURL,
        connected: item?.connected === true,
      };
    })
  );
  const selectedEndpointItem = computed(() => {
    return endpointItems.value.find((item) => item.value === endpointState.selectedRef) || null;
  });
  const navPreloadCancels = new Map();

  function syncViewport() {
    mobileMode.value = window.innerWidth <= 980;
    if (!mobileMode.value) {
      mobileNavOpen.value = false;
    }
  }

  function preloadSharedResources() {
    if (inShellless.value || inSetup.value || inAgentDesk.value || !authValid.value) {
      return;
    }
    const endpointRef = String(endpointState.selectedRef || "").trim();
    if (!endpointRef) {
      return;
    }
    if (!(contactsStore.loaded && contactsStore.endpointRef === endpointRef)) {
      void contactsStore.load({ endpointRef, perfSource: "shared-preload" }).catch(() => {});
    }
    if (
      !(
        personaStore.endpointRef === endpointRef &&
        personaStore.identityLoaded &&
        personaStore.avatarLoaded
      )
    ) {
      void personaStore.loadSummary({ endpointRef, perfSource: "shared-preload" }).catch(() => {});
    }
  }

  async function refreshEndpointsIfNeeded() {
    if (inShellless.value || !authValid.value) {
      return;
    }
    if (endpointState.items.length > 0) {
      endpointState.ensureEndpointSelection();
      return;
    }
    try {
      await ensureEndpointsLoaded();
    } catch {
      endpointState.items = [];
    }
  }

  async function refreshEndpointsAndPreload() {
    await refreshEndpointsIfNeeded();
    preloadSharedResources();
  }

  function runWhenIdle(callback, timeout = 1500) {
    if (typeof window.requestIdleCallback === "function") {
      const handle = window.requestIdleCallback(callback, { timeout });
      return () => window.cancelIdleCallback(handle);
    }
    const handle = window.setTimeout(callback, 120);
    return () => window.clearTimeout(handle);
  }

  function navTargetPath(item) {
    if (!item || typeof item.id !== "string" || !item.id) {
      return "";
    }
    if (item.id === "/chat") {
      return chatRoutePath(lastTopicID(chatSubmitEndpointRef(endpointState.selectedRef)));
    }
    return item.id;
  }

  function preloadNavItem(item) {
    const nextPath = navTargetPath(item);
    if (!nextPath || nextPath === route.path || navPreloadCancels.has(nextPath)) {
      return;
    }
    const cancel = runWhenIdle(() => {
      navPreloadCancels.delete(nextPath);
      void preloadRouteComponent(nextPath)?.catch(() => {});
    });
    navPreloadCancels.set(nextPath, cancel);
  }

  onMounted(() => {
    syncViewport();
    window.addEventListener("resize", syncViewport);
    void refreshEndpointsAndPreload();
  });
  onUnmounted(() => {
    window.removeEventListener("resize", syncViewport);
    for (const cancel of navPreloadCancels.values()) {
      cancel();
    }
    navPreloadCancels.clear();
  });

  watch(
    () => route.fullPath,
    () => {
      mobileNavOpen.value = false;
      void refreshEndpointsAndPreload();
    }
  );

  watch(
    () => endpointState.selectedRef,
    () => {
      preloadSharedResources();
    }
  );

  function goTo(item) {
    const nextPath = navTargetPath(item);
    if (!nextPath) {
      return;
    }
    mobileNavOpen.value = false;
    if (route.path !== nextPath) {
      router.push(nextPath);
    }
  }

  function openMobileNav() {
    mobileNavOpen.value = true;
  }

  function closeMobileNav() {
    mobileNavOpen.value = false;
  }

  function onEndpointChange(item) {
    mobileNavOpen.value = false;
    if (item && typeof item === "object" && typeof item.value === "string") {
      const canSelect = visibleEndpoints(endpointState.items, { connectedOnly: true }).some(
        (endpoint) => endpoint.endpoint_ref === item.value && endpoint.connected === true
      );
      endpointState.setSelectedEndpointRef(canSelect ? item.value : "");
      return;
    }
    endpointState.setSelectedEndpointRef("");
  }

  function goSettings() {
    mobileNavOpen.value = false;
    if (route.path !== "/settings") {
      router.push("/settings");
    }
  }

  return {
    t,
    inLogin,
    inShellless,
    inOverview,
    inSetup,
    inStandalone,
    inWorkspacePage,
    currentPath,
    navItems,
    goTo,
    preloadNavItem,
    openMobileNav,
    closeMobileNav,
    mobileMode,
    mobileNavOpen,
    endpointItems,
    selectedEndpointItem,
    onEndpointChange,
    goSettings,
  };
}

export { useAppShell };
