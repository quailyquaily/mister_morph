import { useAppShell } from "../composables/useAppShell";
import AppMobileNavDrawer from "../components/AppMobileNavDrawer";
import AppSidebar from "../components/AppSidebar";
import AppTopbar from "../components/AppTopbar";
import "./AppLayout.css";

const AppLayout = {
  components: {
    AppTopbar,
    AppSidebar,
    AppMobileNavDrawer,
  },
  setup() {
    return useAppShell();
  },
  template: `
    <div>
      <section v-if="inLogin">
        <RouterView />
      </section>
      <section v-else class="app-shell">
        <AppTopbar
          :t="t"
          :mobileMode="mobileMode"
          :inOverview="inOverview"
          :endpointItems="endpointItems"
          :selectedEndpointItem="selectedEndpointItem"
          @open-mobile-nav="openMobileNav"
          @endpoint-change="onEndpointChange"
          @go-overview="goOverview"
          @go-settings="goSettings"
        />
        <div :class="mobileMode || inOverview ? 'workspace is-mobile' : 'workspace'">
          <AppSidebar
            v-if="!mobileMode && !inOverview"
            :navItems="navItems"
            :currentPath="currentPath"
            @navigate="goTo"
          />
          <main :class="inOverview ? 'content content-overview' : 'content'">
            <RouterView />
          </main>
        </div>
        <AppMobileNavDrawer
          v-if="mobileMode && !inOverview"
          v-model="mobileNavOpen"
          :title="t('drawer_nav')"
          :navItems="navItems"
          :currentPath="currentPath"
          @navigate="goTo"
          @close="closeMobileNav"
        />
      </section>
    </div>
  `,
};

export default AppLayout;
