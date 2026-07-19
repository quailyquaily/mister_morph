import { createApp } from "vue";
import { QuailUI, applyTheme } from "quail-ui";
import "quail-ui/dist/index.css";
import "./styles/base.css";

import AppLayout from "./layouts/AppLayout";
import { dismissBootSplash } from "./components/BootSplash";
import { authState, endpointState, localeState } from "./core/context";
import { installDesktopRuntimeMode, reportDesktopFrontendReady } from "./core/desktop-runtime";
import { installExternalLinkHandler } from "./core/external-links";
import { installConsolePerformanceObservers } from "./core/performance";
import { installMacOS26Mode } from "./core/platform";
import { installSystemNotifications } from "./core/system-notifications";
import { router } from "./router";
import { pinia } from "./stores/pinia";

localeState.hydrateLanguage();
authState.hydrate();
endpointState.hydrateEndpointSelection();
installDesktopRuntimeMode();
installExternalLinkHandler();
installConsolePerformanceObservers();
installSystemNotifications();
const platformModeReady = installMacOS26Mode();

const app = createApp(AppLayout);
if (import.meta.env.DEV === true) {
  app.config.performance = true;
}
app.use(pinia);
app.use(router);
app.use(QuailUI);
applyTheme("morph", false);

async function boot() {
  await Promise.all([router.isReady(), platformModeReady]);
  app.mount("#app");
  const bootOverlay = document.getElementById("boot-overlay");
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      void dismissBootSplash(bootOverlay).finally(() => {
        reportDesktopFrontendReady();
      });
    });
  });
}

void boot();
