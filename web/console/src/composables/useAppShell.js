import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { lastTopicID } from "../core/chat-topic-memory";
import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import {
  authValid,
  endpointState,
  ensureEndpointsLoaded,
  runtimeEndpointByRef,
  translate,
} from "../core/context";
import { NAV_ITEMS_META } from "../router";
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
  const inStandalone = computed(() => inOverview.value || inSetup.value);
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
    visibleEndpoints(endpointState.items, { connectedOnly: true }).map((item) => {
      const display = endpointDisplayItem(item, t);
      return {
        title: display.title,
        value: display.value,
      };
    })
  );
  const selectedEndpointItem = computed(() => {
    return endpointItems.value.find((item) => item.value === endpointState.selectedRef) || null;
  });

  function syncViewport() {
    mobileMode.value = window.innerWidth <= 980;
    if (!mobileMode.value) {
      mobileNavOpen.value = false;
    }
  }

  function preloadSharedResources() {
    if (inShellless.value || inSetup.value || !authValid.value) {
      return;
    }
    const endpointRef = String(endpointState.selectedRef || "").trim();
    if (!endpointRef) {
      return;
    }
    void contactsStore.load({ endpointRef }).catch(() => {});
    void personaStore.loadSummary({ endpointRef }).catch(() => {});
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

  onMounted(() => {
    syncViewport();
    window.addEventListener("resize", syncViewport);
    void refreshEndpointsAndPreload();
  });
  onUnmounted(() => {
    window.removeEventListener("resize", syncViewport);
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
    if (!item || typeof item.id !== "string" || !item.id) {
      return;
    }
    mobileNavOpen.value = false;
    let nextPath = item.id;
    if (item.id === "/chat") {
      nextPath = chatRoutePath(lastTopicID(chatSubmitEndpointRef(endpointState.selectedRef)));
    }
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
    if (item && typeof item === "object" && typeof item.value === "string") {
      const canSelect = visibleEndpoints(endpointState.items, { connectedOnly: true }).some(
        (endpoint) => endpoint.endpoint_ref === item.value && endpoint.connected === true
      );
      endpointState.setSelectedEndpointRef(canSelect ? item.value : "");
      return;
    }
    endpointState.setSelectedEndpointRef("");
  }

  function goOverview() {
    mobileNavOpen.value = false;
    if (route.path !== "/overview") {
      router.push("/overview");
    }
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
    openMobileNav,
    closeMobileNav,
    mobileMode,
    mobileNavOpen,
    endpointItems,
    selectedEndpointItem,
    onEndpointChange,
    goOverview,
    goSettings,
  };
}

export { useAppShell };
