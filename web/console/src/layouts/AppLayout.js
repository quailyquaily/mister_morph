import { provide } from "vue";

import { useAppShell } from "../composables/useAppShell";
import AppMobileNavDrawer from "../components/AppMobileNavDrawer";
import AppSidebar from "../components/AppSidebar";
import "./AppLayout.css";

const AppLayout = {
  components: {
    AppSidebar,
    AppMobileNavDrawer,
  },
  setup() {
    const shell = useAppShell();
    provide("app-shell-chrome", {
      shouldShowMobileNavTrigger: () => shell.mobileMode.value && shell.inWorkspacePage.value,
      openMobileNav: shell.openMobileNav,
      drawerNavLabel: () => shell.t("drawer_nav"),
    });
    return shell;
  },
  template: `
    <div>
      <section v-if="inLogin">
        <RouterView />
      </section>
      <section v-else class="app-shell">
        <div :class="mobileMode || inOverview ? 'workspace is-mobile' : 'workspace'">
          <AppSidebar
            v-if="!mobileMode && !inOverview"
            :t="t"
            :endpointItems="endpointItems"
            :selectedEndpointItem="selectedEndpointItem"
            :navItems="navItems"
            :currentPath="currentPath"
            @navigate="goTo"
            @endpoint-change="onEndpointChange"
            @go-overview="goOverview"
            @go-settings="goSettings"
          />
          <main
            :class="[
              'content',
              {
                'content-overview': inOverview,
                'content-page': inWorkspacePage,
              },
            ]"
          >
            <RouterView />
          </main>
        </div>
        <AppMobileNavDrawer
          v-if="mobileMode && !inOverview"
          v-model="mobileNavOpen"
          :t="t"
          :title="t('drawer_nav')"
          :endpointItems="endpointItems"
          :selectedEndpointItem="selectedEndpointItem"
          :navItems="navItems"
          :currentPath="currentPath"
          @navigate="goTo"
          @endpoint-change="onEndpointChange"
          @go-overview="goOverview"
          @go-settings="goSettings"
          @close="closeMobileNav"
        />
      </section>
    </div>
  `,
};

export default AppLayout;
