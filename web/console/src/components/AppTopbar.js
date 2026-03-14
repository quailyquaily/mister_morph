import "./AppTopbar.css";

const AppTopbar = {
  props: {
    mobileMode: {
      type: Boolean,
      required: true,
    },
    inOverview: {
      type: Boolean,
      required: true,
    },
    endpointItems: {
      type: Array,
      required: true,
    },
    selectedEndpointItem: {
      type: Object,
      default: null,
    },
    t: {
      type: Function,
      required: true,
    },
  },
  emits: ["open-mobile-nav", "endpoint-change", "go-overview", "go-settings"],
  template: `
    <header class="topbar">
      <div class="topbar-brand">
        <QButton v-if="mobileMode && !inOverview" class="plain mobile-nav-trigger icon" @click="$emit('open-mobile-nav')">
          <QIconMenu class="icon"/>
        </QButton>
        <div class="brand">
          <h1 class="brand-title">Mistermorph Console</h1>
        </div>
      </div>
      <div v-if="!inOverview" class="topbar-actions">
        <div class="topbar-endpoint">
          <QDropdownMenu
            :items="endpointItems"
            :initialItem="selectedEndpointItem"
            :placeholder="t('endpoint_placeholder')"
            :hideSelected="true"
            @change="$emit('endpoint-change', $event)"
          >
            <div v-if="selectedEndpointItem" class="endpoint-selected">
              <span class="endpoint-selected-name">{{ selectedEndpointItem.title }}</span>
            </div>
            <span v-else class="endpoint-selected-placeholder">{{ t('endpoint_placeholder') }}</span>
          </QDropdownMenu>
        </div>
        <QButton
          class="outlined icon topbar-settings-trigger"
          :title="t('nav_settings')"
          :aria-label="t('nav_settings')"
          @click="$emit('go-settings')"
        >
          <QIconSettings class="icon"/>
        </QButton>
        <QButton
          class="outlined icon topbar-overview-trigger"
          :title="t('nav_overview')"
          :aria-label="t('nav_overview')"
          @click="$emit('go-overview')"
        >
          <QIconGrid class="icon"/>
        </QButton>
      </div>
    </header>
  `,
};

export default AppTopbar;
