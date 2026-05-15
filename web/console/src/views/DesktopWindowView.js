import { computed } from "vue";
import { useRoute } from "vue-router";

import { translate } from "../core/context";
import "./DesktopWindowView.css";

const DesktopWindowView = {
  setup() {
    const route = useRoute();
    const title = computed(() => {
      const queryTitle = typeof route.query.title === "string" ? route.query.title.trim() : "";
      if (queryTitle) {
        return queryTitle;
      }
      const id = typeof route.params.window_id === "string" ? route.params.window_id.trim() : "";
      return id || translate("desktop_window_title");
    });
    return { title, t: translate };
  },
  template: `
    <main class="desktop-window-view">
      <header class="desktop-window-view__header">
        <h1>{{ title }}</h1>
      </header>
      <section class="desktop-window-view__empty" aria-live="polite">
        <p>{{ t("desktop_window_unavailable") }}</p>
      </section>
    </main>
  `,
};

export default DesktopWindowView;
