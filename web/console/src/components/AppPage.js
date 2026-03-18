import { inject } from "vue";

import "./AppPage.css";

const AppPage = {
  props: {
    title: {
      type: String,
      default: "",
    },
    showMobileNavTrigger: {
      type: Boolean,
      default: true,
    },
  },
  setup() {
    const chrome = inject("app-shell-chrome", null);
    return { chrome };
  },
  template: `
    <section class="page-view">
      <header class="page-bar">
        <div class="page-bar-leading">
          <QButton
            v-if="chrome && showMobileNavTrigger && chrome.shouldShowMobileNavTrigger()"
            class="outlined xs icon page-bar-nav-trigger"
            :title="chrome.drawerNavLabel()"
            :aria-label="chrome.drawerNavLabel()"
            @click="chrome.openMobileNav"
          >
            <QIconMenu class="icon" />
          </QButton>
          <slot name="leading">
            <h2 class="title page-bar-title">{{ title }}</h2>
          </slot>
        </div>
        <div v-if="$slots.actions" class="page-bar-actions">
          <slot name="actions" />
        </div>
      </header>
      <div class="page-body">
        <slot />
      </div>
    </section>
  `,
};

export default AppPage;
