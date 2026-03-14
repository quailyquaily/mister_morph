import { createApp } from "vue";
import { QuailUI, applyTheme, readStoredTheme } from "quail-ui";
import "quail-ui/dist/index.css";
import "./styles/base.css";

import AppLayout from "./layouts/AppLayout";
import { hydrateAuth, hydrateEndpointSelection, hydrateLanguage } from "./core/context";
import { router } from "./router";

function applyHostRenderingHints() {
  if (typeof document !== "object") {
    return;
  }

  const root = document.documentElement;
  if (window?.go?.main?.App?.RestartApp) {
    root.dataset.host = "desktop";
  }

  const platform = `${navigator.userAgent || ""} ${navigator.platform || ""}`.toLowerCase();
  if (platform.includes("linux")) {
    root.dataset.platform = "linux";
  }
}

applyHostRenderingHints();
hydrateLanguage();
hydrateAuth();
hydrateEndpointSelection();

const app = createApp(AppLayout);
app.use(router);
app.use(QuailUI);
applyTheme(readStoredTheme() || "morph", false);
app.mount("#app");
