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
  },
  emits: ["navigate"],
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
    onNavigate(item) {
      this.$emit("navigate", item);
    },
  },
  template: `
    <div :class="mobile ? 'sidebar-nav mobile-drawer-nav' : 'sidebar-nav'">
      <template v-for="item in navItems" :key="keyPrefix + item.id">
        <QDivider v-if="item.separator" class="nav-divider" aria-hidden="true" />
        <a
          v-else
          :href="navHref(item)"
          :class="navClass(item)"
          :aria-current="navCurrent(item)"
          @click.prevent="onNavigate(item)"
        >
          <component :is="item.icon" v-if="item.icon" class="nav-icon icon" />
          <span class="nav-label">{{ item.title }}</span>
        </a>
      </template>
    </div>
  `,
};

export default AppNavList;
