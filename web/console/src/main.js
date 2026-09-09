import { createApp } from "vue";
import { applyTheme, closePopupMenu } from "quail-ui";
import "quail-ui/dist/index.css";
import "./styles/base.css";

import AppLayout from "./layouts/AppLayout";
import { dismissBootSplash } from "./components/BootSplash";
import * as quailComponents from "./components/quail";
import { authState, endpointState, localeState } from "./core/context";
import { installDesktopRuntimeMode, reportDesktopFrontendReady } from "./core/desktop-runtime";
import { installExternalLinkHandler } from "./core/external-links";
import { installConsolePerformanceObservers } from "./core/performance";
import { installMacOS26Mode } from "./core/platform";
import { installSystemNotifications } from "./core/system-notifications";
import { phosphorIcons } from "./icons/phosphor";
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
for (const [name, component] of Object.entries({ ...quailComponents, ...phosphorIcons })) {
  app.component(name, component);
}
// Preserve the full plugin's outside-click behavior without registering it.
if (!window.__quailui_click_handler_installed) {
  document.body.addEventListener("click", closePopupMenu);
  window.__quailui_click_handler_installed = true;
}
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
