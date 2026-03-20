import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import "./OverviewView.css";

import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import { endpointState, loadEndpoints, setSelectedEndpointRef, toBool, translate } from "../core/context";

const OverviewView = {
  setup() {
    const t = translate;
    const router = useRouter();
    const err = ref("");
    const loading = ref(false);
    let refreshTimer = null;
    const endpointRows = computed(() =>
      visibleEndpoints(endpointState.items).map((item) => ({
        ...endpointDisplayItem(item, t),
        url: item.url || "",
        connected: toBool(item.connected, false),
        can_submit: toBool(item.can_submit, false),
        agent_name: String(item.agent_name || "").trim(),
      }))
    );

    function tuiKicker(left, right) {
      const lhs = String(left || "").trim();
      const rhs = String(right || "").trim();
      if (lhs && rhs) {
        return `[ ${lhs} // ${rhs} ]`;
      }
      return `[ ${lhs || rhs} ]`;
    }

    function openEndpoint(item) {
      if (
        !item ||
        typeof item.endpoint_ref !== "string" ||
        !item.endpoint_ref ||
        item.connected !== true
      ) {
        return;
      }
      setSelectedEndpointRef(item.endpoint_ref);
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
      endpointRows,
      tuiKicker,
      openEndpoint,
      channelBadgeType,
    };
  },
  template: `
    <section>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="stat-groups">
        <section class="stat-group">
          <h3 class="ui-kicker">{{ tuiKicker(t("runtime_title"), t("group_endpoints")) }}</h3>
          <div class="endpoint-overview-list">
            <QCard
              v-for="item in endpointRows"
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
            <p v-if="endpointRows.length === 0 && !loading" class="muted">{{ t("no_endpoints") }}</p>
          </div>
        </section>
      </div>
    </section>
  `,
};


export default OverviewView;
