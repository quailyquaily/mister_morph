import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  authValid,
  endpointState,
  ensureEndpointSelection,
  loadEndpoints,
  setSelectedEndpointRef,
  translate,
} from "../core/context";
import { NAV_ITEMS_META } from "../router";

function useAppShell() {
  const t = translate;
  const router = useRouter();
  const route = useRoute();
  const inLogin = computed(() => route.path === "/login" || route.path === "/setup");
  const inOverview = computed(() => route.path === "/overview");
  const currentPath = computed(() => route.path);
  const navItems = computed(() =>
    NAV_ITEMS_META.map((item) => ({
      id: item.id,
      title: t(item.titleKey),
      icon: item.icon || "",
    }))
  );
  const mobileNavOpen = ref(false);
  const mobileMode = ref(window.innerWidth <= 980);
  const endpointItems = computed(() =>
    endpointState.items
      .filter((item) => item && item.connected === true)
      .map((item) => ({
        title: item.name || item.endpoint_ref,
        subtitle: item.url || "",
        value: item.endpoint_ref,
      }))
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

  async function refreshEndpointsIfNeeded() {
    if (inLogin.value || !authValid.value) {
      return;
    }
    if (endpointState.items.length > 0) {
      ensureEndpointSelection();
      return;
    }
    try {
      await loadEndpoints();
    } catch {
      endpointState.items = [];
    }
  }

  onMounted(() => {
    syncViewport();
    window.addEventListener("resize", syncViewport);
    void refreshEndpointsIfNeeded();
  });
  onUnmounted(() => {
    window.removeEventListener("resize", syncViewport);
  });

  watch(
    () => route.fullPath,
    () => {
      mobileNavOpen.value = false;
      void refreshEndpointsIfNeeded();
    }
  );

  function goTo(item) {
    if (!item || typeof item.id !== "string" || !item.id) {
      return;
    }
    mobileNavOpen.value = false;
    if (route.path !== item.id) {
      router.push(item.id);
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
      const canSelect = endpointState.items.some(
        (endpoint) => endpoint.endpoint_ref === item.value && endpoint.connected === true
      );
      setSelectedEndpointRef(canSelect ? item.value : "");
      return;
    }
    setSelectedEndpointRef("");
  }

  function goOverview() {
    if (route.path !== "/overview") {
      router.push("/overview");
    }
  }

  return {
    t,
    inLogin,
    inOverview,
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
  };
}

export { useAppShell };
