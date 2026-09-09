import { computed, onMounted, onUnmounted, ref } from "vue";
import { RouterLink } from "vue-router";
import "./OverviewView.css";
import logoURL from "../assets/images/app_logo_current.svg";

import AppPage from "../components/AppPage";
import { endpointDisplayItem, isConsoleLocalEndpoint, visibleEndpoints } from "../core/endpoints";
import { endpointRoutePath } from "../core/endpoint-routes";
import { endpointState, ensureEndpointsLoaded, loadEndpoints, toBool, translate } from "../core/context";

const OverviewView = {
  components: {
    AppPage,
    RouterLink,
  },
  setup() {
    const t = translate;
    const err = ref("");
    const loading = ref(false);
    let refreshTimer = null;

    const endpointRows = computed(() => {
      const items = visibleEndpoints(endpointState.items);
      return items.map((item) => {
        const display = endpointDisplayItem(item, t);
        const connected = toBool(item.connected, false);
        const pending = !connected && toBool(item.health_pending, false);
        return {
          endpoint_ref: item.endpoint_ref,
          title: String(item.agent_name || "").trim() || display.title,
          detail: items.length > 1
            ? isConsoleLocalEndpoint(item) ? t("overview_location_local") : display.subtitle
            : "",
          avatar_url: String(item.avatar_url || "").trim(),
          connected,
          pending,
          statusLabel: t(pending ? "overview_status_checking"
            : connected ? "endpoint_switcher_online" : "endpoint_switcher_offline"),
          route: connected ? endpointRoutePath(item.endpoint_ref, "/chat") : undefined,
        };
      }).sort((left, right) =>
        Number(right.connected) - Number(left.connected) ||
        Number(right.pending) - Number(left.pending) ||
        left.title.localeCompare(right.title, undefined, { numeric: true, sensitivity: "base" })
      );
    });

    async function load(options = {}) {
      if (loading.value) return;
      loading.value = true;
      err.value = "";
      try {
        if (options.force === true) {
          await loadEndpoints();
        } else {
          await ensureEndpointsLoaded();
        }
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    onMounted(() => {
      void load();
      refreshTimer = window.setInterval(() => {
        void load({ force: true });
      }, 60000);
    });
    onUnmounted(() => {
      window.clearInterval(refreshTimer);
    });

    return { t, err, loading, endpointRows, logoURL };
  },
  template: `
    <AppPage :title="t('nav_overview')">
      <QProgress v-if="loading && endpointRows.length === 0" :infinite="true" />
      <QFence v-if="err" type="danger" icon="PhXCircle" :text="err" />

      <section class="overview-page">
        <QCard v-if="endpointRows.length" variant="default" class="endpoint-overview-panel">
          <ul class="endpoint-overview-list">
            <li v-for="item in endpointRows" :key="item.endpoint_ref">
              <component
                :is="item.connected ? 'RouterLink' : 'div'"
                :to="item.route"
                :class="['endpoint-overview-item', { 'is-offline': !item.connected && !item.pending }]"
                :aria-disabled="item.connected ? undefined : 'true'"
              >
                <span class="endpoint-overview-avatar-mark" aria-hidden="true">
                  <img
                    :class="item.avatar_url ? 'endpoint-overview-avatar' : 'endpoint-overview-logo'"
                    :src="item.avatar_url || logoURL"
                    alt=""
                    @error="$event.target.src = logoURL"
                  />
                </span>
                <span class="endpoint-overview-identity">
                  <span class="endpoint-overview-name" :title="item.title">{{ item.title }}</span>
                  <span v-if="item.detail" class="endpoint-overview-detail" :title="item.detail">{{ item.detail }}</span>
                </span>
                <span
                  :class="['endpoint-overview-status', { 'is-online': item.connected, 'is-pending': item.pending }]"
                  role="img"
                  :aria-label="item.statusLabel"
                  :title="item.statusLabel"
                ></span>
              </component>
            </li>
          </ul>
        </QCard>
        <p v-else-if="!loading && !err" class="muted overview-empty">{{ t('no_endpoints') }}</p>
      </section>
    </AppPage>
  `,
};

export default OverviewView;
