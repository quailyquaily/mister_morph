import { computed } from "vue";

import { uiSlots } from "../ext/slots";
import "./AppNavList.css";

const AppNavList = {
  props: {
    navItems: {
      type: Array,
      required: true,
    },
    currentPath: {
      type: String,
      required: true,
    },
    mobile: {
      type: Boolean,
      default: false,
    },
    keyPrefix: {
      type: String,
      default: "",
    },
    selectedEndpointItem: {
      type: Object,
      default: null,
    },
    t: {
      type: Function,
      default: null,
    },
  },
  emits: ["navigate", "preload"],
  setup() {
    const sidebarBeforeRuntimeSlot = computed(() => uiSlots["sidebar.before_runtime"] || null);
    return { sidebarBeforeRuntimeSlot };
  },
  methods: {
    normalizePath(path) {
      if (typeof path !== "string" || !path) {
        return "/";
      }
      const normalized = path.replace(/\/+$/, "");
      return normalized || "/";
    },
    isActive(item) {
      if (!item || typeof item.id !== "string") {
        return false;
      }
      const current = this.normalizePath(this.currentPath);
      const target = this.normalizePath(item.id);
      return current === target || current.startsWith(`${target}/`);
    },
    navClass(item) {
      return this.isActive(item) ? "nav-link is-active" : "nav-link";
    },
    navCurrent(item) {
      return this.isActive(item) ? "page" : undefined;
    },
    navHref(item) {
      const value = typeof item?.id === "string" ? item.id.trim() : "";
      return value || "/";
    },
    shouldRenderBeforeRuntimeSlot(item) {
      return !!this.sidebarBeforeRuntimeSlot && item?.pagePath === "/settings";
    },
    onNavigate(item) {
      this.$emit("navigate", item);
    },
    onPreload(item) {
      this.$emit("preload", item);
    },
  },
  template: `
    <div :class="mobile ? 'sidebar-nav mobile-nav-list' : 'sidebar-nav'">
      <template v-for="item in navItems" :key="keyPrefix + item.id">
        <QDivider v-if="item.separator" class="nav-divider" aria-hidden="true" />
        <template v-else>
          <div v-if="shouldRenderBeforeRuntimeSlot(item)" class="sidebar-slot sidebar-slot-before-runtime">
            <component
              :is="sidebarBeforeRuntimeSlot"
              :selectedEndpointItem="selectedEndpointItem"
              :currentPath="currentPath"
              :mobile="mobile"
              :t="t"
            />
          </div>
          <a
            :href="navHref(item)"
            :class="navClass(item)"
            :aria-current="navCurrent(item)"
            @focus="onPreload(item)"
            @pointerenter="onPreload(item)"
            @click.prevent="onNavigate(item)"
          >
            <component :is="item.icon" v-if="item.icon" class="nav-icon icon" />
            <span class="nav-label">{{ item.title }}</span>
          </a>
        </template>
      </template>
    </div>
  `,
};

export default AppNavList;
