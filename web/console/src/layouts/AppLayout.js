import { useAppShell } from "../composables/useAppShell";
import AppMobileBottomNav from "../components/AppMobileBottomNav";
import AppSidebar from "../components/AppSidebar";
import "./AppLayout.css";

const AppLayout = {
  components: {
    AppSidebar,
    AppMobileBottomNav,
  },
  setup() {
    return useAppShell();
  },
  template: `
    <div>
      <section v-if="inShellless">
        <RouterView :key="endpointViewKey" />
      </section>
      <section
        v-else
        class="app-shell"
        :style="{ '--app-viewport-height': appViewportHeight }"
      >
        <div
          :class="[
            'workspace',
            {
              'is-mobile': mobileMode || inStandalone,
            },
          ]"
        >
          <AppSidebar
            v-if="!mobileMode && !inStandalone"
            :t="t"
            :endpointItems="endpointItems"
            :selectedEndpointItem="selectedEndpointItem"
            :navItems="navItems"
            :currentPath="currentPath"
            @navigate="goTo"
            @preload="preloadNavItem"
            @endpoint-change="onEndpointChange"
            @go-settings="goSettings"
          />
          <main
            :class="[
              'content',
              {
                'content-overview': inStandalone,
                'content-page': inWorkspacePage,
              },
            ]"
          >
            <RouterView :key="endpointViewKey" />
          </main>
        </div>
        <AppMobileBottomNav
          v-if="mobileBottomNavVisible"
          v-model="mobileMoreOpen"
          :t="t"
          :endpointItems="endpointItems"
          :selectedEndpointItem="selectedEndpointItem"
          :navItems="navItems"
          :currentPath="currentPath"
          @navigate="goTo($event, false)"
          @preload="preloadNavItem($event, false)"
          @endpoint-change="onEndpointChange"
          @close="closeMobileMore"
        />
      </section>
    </div>
  `,
};

export default AppLayout;
