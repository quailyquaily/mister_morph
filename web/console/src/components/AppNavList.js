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
      return this.isActive(item) ? "nav-btn nav-btn-active" : "nav-btn";
    },
    navCurrent(item) {
      return this.isActive(item) ? "page" : undefined;
    },
    onNavigate(item) {
      this.$emit("navigate", item);
    },
  },
  template: `
    <div :class="mobile ? 'sidebar-nav mobile-drawer-nav' : 'sidebar-nav'">
      <template v-for="item in navItems" :key="keyPrefix + item.id">
        <div v-if="item.separator" class="nav-divider" aria-hidden="true"></div>
        <div
          v-else
          :class="navClass(item)"
          role="button"
          tabindex="0"
          :aria-current="navCurrent(item)"
          @click="onNavigate(item)"
          @keydown.enter.prevent="onNavigate(item)"
          @keydown.space.prevent="onNavigate(item)"
        >
          <component :is="item.icon" v-if="item.icon" class="nav-icon icon" />
          <span class="nav-label">{{ item.title }}</span>
        </div>
      </template>
    </div>
  `,
};

export default AppNavList;
