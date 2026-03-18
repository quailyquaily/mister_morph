import { createApp } from "vue";
import { QuailUI, applyTheme } from "quail-ui";
import "quail-ui/dist/index.css";
import "./styles/base.css";

import AppLayout from "./layouts/AppLayout";
import { hydrateAuth, hydrateEndpointSelection, hydrateLanguage } from "./core/context";
import { router } from "./router";

hydrateLanguage();
hydrateAuth();
hydrateEndpointSelection();

const app = createApp(AppLayout);
app.use(router);
app.use(QuailUI);
applyTheme("morph", false);
app.mount("#app");
