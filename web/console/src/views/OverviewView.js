import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import "./OverviewView.css";
import logoMarkup from "../assets/images/app_logo_current.svg?raw";

import AppKicker from "../components/AppKicker";
import AppPage from "../components/AppPage";
import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import { endpointState, loadEndpoints, toBool, translate } from "../core/context";

function endpointSortKey(item) {
  return String(item?.title || item?.url || item?.location || item?.endpoint_ref || "").trim();
}

function sortOverviewItems(items) {
  return [...items].sort((left, right) =>
    endpointSortKey(left).localeCompare(endpointSortKey(right), undefined, {
      numeric: true,
      sensitivity: "base",
    })
  );
}

const OverviewView = {
  components: {
    AppKicker,
    AppPage,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const err = ref("");
    const loading = ref(false);
    let refreshTimer = null;

    const endpointRows = computed(() =>
      sortOverviewItems(
        visibleEndpoints(endpointState.items).map((item) => {
          const display = endpointDisplayItem(item, t);
          return {
            ...display,
            url: item.url || "",
            connected: toBool(item.connected, false),
            can_submit: toBool(item.can_submit, false),
            agent_name: String(item.agent_name || "").trim(),
            avatar_url: String(item.avatar_url || "").trim(),
          };
        })
      )
    );
    const activeEndpointRows = computed(() => endpointRows.value.filter((item) => item.connected));
    const inactiveEndpointRows = computed(() => endpointRows.value.filter((item) => !item.connected));
    const endpointGroups = computed(() => [
      {
        key: "active",
        tone: "success",
        items: activeEndpointRows.value,
        emptyText: t("overview_empty_active"),
        kickerRight: "Active",
      },
      {
        key: "inactive",
        tone: "default",
        items: inactiveEndpointRows.value,
        emptyText: t("overview_empty_inactive"),
        kickerRight: "Inactive",
      },
    ]);

    function openEndpoint(item) {
      if (
        !item ||
        typeof item.endpoint_ref !== "string" ||
        !item.endpoint_ref ||
        item.connected !== true
      ) {
        return;
      }
      endpointState.setSelectedEndpointRef(item.endpoint_ref);
      router.push("/chat");
    }

    function channelBadgeType(badge) {
      switch (String(badge?.tone || "").trim()) {
        case "console":
          return "primary";
        case "telegram":
          return "info";
        case "slack":
          return "danger";
        case "line":
          return "success";
        case "lark":
          return "warning";
        case "serve":
        default:
          return "default";
      }
    }

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        await loadEndpoints();
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    onMounted(() => {
      void load();
      refreshTimer = window.setInterval(() => {
        void load();
      }, 60000);
    });
    onUnmounted(() => {
      if (refreshTimer !== null) {
        window.clearInterval(refreshTimer);
        refreshTimer = null;
      }
    });

    return {
      t,
      err,
      loading,
      endpointGroups,
      logoMarkup,
      openEndpoint,
      channelBadgeType,
    };
  },
  template: `
    <AppPage :title="t('nav_overview')">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

      <section class="overview-page">
        <div class="stat-groups overview-groups">
          <section
            v-for="group in endpointGroups"
            :key="group.key"
            :class="'stat-group overview-group overview-group-' + group.key"
          >
            <header class="overview-group-head">
              <AppKicker as="p" left="Endpoints" :right="group.kickerRight" />
              <QBadge :type="group.tone" size="sm">{{ group.items.length }}</QBadge>
            </header>

            <div class="endpoint-overview-list">
              <QCard
                v-for="item in group.items"
                :key="item.endpoint_ref"
                variant="default"
                :hoverable="item.connected"
                :dashed="!item.connected"
                :class="item.connected ? 'endpoint-overview-item clickable' : 'endpoint-overview-item is-disabled'"
                :tabindex="item.connected ? 0 : -1"
                :role="item.connected ? 'button' : undefined"
                :aria-disabled="item.connected ? undefined : 'true'"
                @click="item.connected && openEndpoint(item)"
                @keydown.enter.prevent="item.connected && openEndpoint(item)"
                @keydown.space.prevent="item.connected && openEndpoint(item)"
              >
                <template #header>
                  <div class="endpoint-overview-head">
                    <span class="endpoint-overview-avatar-mark" aria-hidden="true">
                      <img v-if="item.avatar_url" class="endpoint-overview-avatar" :src="item.avatar_url" alt="" />
                      <span v-else class="endpoint-overview-logo" v-html="logoMarkup"></span>
                    </span>
                    <div class="endpoint-overview-title">
                      <div class="endpoint-overview-name-row">
                        <span class="channel-runtime-dot">
                          <QBadge
                            :type="item.connected ? 'success' : 'default'"
                            size="md"
                            variant="filled"
                            :dot="true"
                          />
                        </span>
                        <code class="endpoint-overview-name">{{ item.title }}</code>
                      </div>
                      <code v-if="item.agent_name" class="endpoint-overview-agent">{{ item.agent_name }}</code>
                    </div>
                  </div>
                </template>
                <code class="endpoint-overview-url">{{ item.url || item.location }}</code>
                <template #footer>
                  <span class="endpoint-channel-badge-list">
                    <QBadge
                      v-for="badge in item.channelBadges"
                      :key="badge.tone + ':' + badge.label"
                      :type="channelBadgeType(badge)"
                      size="sm"
                    >
                      {{ badge.label }}
                    </QBadge>
                  </span>
                </template>
              </QCard>
              <p v-if="group.items.length === 0 && !loading" class="muted overview-group-empty">{{ group.emptyText }}</p>
            </div>
          </section>
        </div>
      </section>
    </AppPage>
  `,
};

export default OverviewView;
