import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import defaultEndpointAvatarURL from "../assets/images/app_logo_current.svg";
import { lastTopicID } from "../core/chat-topic-memory";
import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import {
  endpointPagePath,
  endpointRefFromRouteParam,
  endpointRoutePath,
  endpointSwitchPath,
} from "../core/endpoint-routes";
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

function chatPagePath(topicID = "") {
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
  const inSetup = computed(() => {
    const pagePath = endpointPagePath(route.path) || route.path;
    return pagePath === "/setup" || pagePath.startsWith("/setup/");
  });
  const inAgentDesk = computed(() => route.path === "/chat/desk");
  const inStandalone = computed(() => inOverview.value || inSetup.value || inAgentDesk.value);
  const inWorkspacePage = computed(() => !inShellless.value && !inStandalone.value);
  const currentPath = computed(() => route.path);
  const endpointViewKey = computed(() =>
    route.meta?.endpointScoped
      ? endpointRefFromRouteParam(route.params.endpoint_ref)
      : String(route.name || route.path),
  );
  const navItems = computed(() =>
    NAV_ITEMS_META.map((item) =>
      item.separator
        ? { id: item.id, separator: true }
        : {
            id: endpointRoutePath(endpointState.selectedRef, item.id),
            pagePath: item.id,
            title: t(item.titleKey),
            icon: item.icon || "",
          }
    )
  );
  const mobileMoreOpen = ref(false);
  const mobileMode = ref(window.innerWidth <= 980);
  const appViewportHeight = ref(
    `${Math.round(window.visualViewport?.height || window.innerHeight)}px`,
  );
  const mobileBottomNavVisible = computed(
    () => mobileMode.value && !inStandalone.value && route.meta?.mobileBottomNav !== false,
  );
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
    appViewportHeight.value = `${Math.round(
      window.visualViewport?.height || window.innerHeight,
    )}px`;
    if (!mobileMode.value) {
      mobileMoreOpen.value = false;
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

  function navTargetPath(item, restoreChatTopic = true) {
    if (!item || typeof item.id !== "string" || !item.id) {
      return "";
    }
    if (restoreChatTopic && item.pagePath === "/chat") {
      return endpointRoutePath(
        endpointState.selectedRef,
        chatPagePath(lastTopicID(chatSubmitEndpointRef(endpointState.selectedRef))),
      );
    }
    return item.id;
  }

  function preloadNavItem(item, restoreChatTopic = true) {
    const nextPath = navTargetPath(item, restoreChatTopic);
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
    window.visualViewport?.addEventListener("resize", syncViewport);
    void refreshEndpointsAndPreload();
  });
  onUnmounted(() => {
    window.removeEventListener("resize", syncViewport);
    window.visualViewport?.removeEventListener("resize", syncViewport);
    for (const cancel of navPreloadCancels.values()) {
      cancel();
    }
    navPreloadCancels.clear();
  });

  watch(
    () => route.fullPath,
    () => {
      mobileMoreOpen.value = false;
      void refreshEndpointsAndPreload();
    }
  );

  watch(
    () => endpointState.selectedRef,
    () => {
      preloadSharedResources();
    }
  );

  function goTo(item, restoreChatTopic = true) {
    const nextPath = navTargetPath(item, restoreChatTopic);
    if (!nextPath) {
      return;
    }
    mobileMoreOpen.value = false;
    if (route.path !== nextPath) {
      router.push(nextPath);
    }
  }

  function closeMobileMore() {
    mobileMoreOpen.value = false;
  }

  function onEndpointChange(item) {
    mobileMoreOpen.value = false;
    const targetRef = typeof item?.value === "string" ? item.value.trim() : "";
    const canSelect = visibleEndpoints(endpointState.items, { connectedOnly: true }).some(
      (endpoint) => endpoint.endpoint_ref === targetRef,
    );
    if (!canSelect) {
      return;
    }
    const chatTopicID = mobileMode.value
      ? ""
      : lastTopicID(chatSubmitEndpointRef(targetRef));
    const nextPath = endpointSwitchPath(
      targetRef,
      route.path,
      chatTopicID,
    );
    if (nextPath && route.path !== nextPath) {
      router.push(nextPath);
    }
  }

  function goSettings() {
    mobileMoreOpen.value = false;
    const nextPath = endpointRoutePath(endpointState.selectedRef, "/settings");
    if (nextPath && route.path !== nextPath) {
      router.push(nextPath);
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
    endpointViewKey,
    navItems,
    goTo,
    preloadNavItem,
    closeMobileMore,
    mobileMode,
    appViewportHeight,
    mobileBottomNavVisible,
    mobileMoreOpen,
    endpointItems,
    selectedEndpointItem,
    onEndpointChange,
    goSettings,
  };
}

export { useAppShell };
