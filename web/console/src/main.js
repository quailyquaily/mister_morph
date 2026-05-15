import { createApp } from "vue";
import { QuailUI, applyTheme } from "quail-ui";
import "quail-ui/dist/index.css";
import "./styles/base.css";

import AppLayout from "./layouts/AppLayout";
import { dismissBootSplash } from "./components/BootSplash";
import { hydrateAuth, hydrateEndpointSelection, hydrateLanguage } from "./core/context";
import { installDesktopRuntimeMode } from "./core/desktop-runtime";
import { installExternalLinkHandler } from "./core/external-links";
import { router } from "./router";

hydrateLanguage();
hydrateAuth();
hydrateEndpointSelection();
installDesktopRuntimeMode();
installExternalLinkHandler();

const app = createApp(AppLayout);
app.use(router);
app.use(QuailUI);
applyTheme("morph", false);

async function boot() {
  await router.isReady();
  app.mount("#app");
  const bootOverlay = document.getElementById("boot-overlay");
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      void dismissBootSplash(bootOverlay);
    });
  });
}

void boot();
