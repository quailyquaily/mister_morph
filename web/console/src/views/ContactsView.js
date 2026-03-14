import { computed, onMounted, ref, watch } from "vue";
import "./ContactsView.css";

import AppPage from "../components/AppPage";
import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";

function normalizeStatus(raw) {
  const value = String(raw || "").trim().toLowerCase();
  if (value === "active") {
    return "active";
  }
  if (value === "inactive") {
    return "inactive";
  }
  return "";
}

function normalizeKind(raw) {
  const value = String(raw || "").trim().toLowerCase();
  if (value === "agent") {
    return "agent";
  }
  return "human";
}

function compactStrings(items) {
  if (!Array.isArray(items)) {
    return [];
  }
  return items
    .map((item) => String(item || "").trim())
    .filter((item) => item !== "");
}

function shortenIdentifier(raw) {
  const value = String(raw || "").trim();
  if (!value) {
    return "";
  }
  if (value.startsWith("@") && value.length <= 20) {
    return value;
  }
  if (value.length <= 14) {
    return value;
  }
  return `${value.slice(0, 5)}…${value.slice(-4)}`;
}

function fallbackHandleFromContactID(item, channel) {
  const contactID = String(item?.contact_id || "").trim();
  if (!contactID) {
    return "";
  }
  const parts = contactID.split(":").map((part) => part.trim()).filter(Boolean);
  if (parts.length < 2) {
    return "";
  }
  const prefix = parts[0].toLowerCase();
  switch (channel) {
    case "Telegram":
      if (prefix === "tg" || prefix === "telegram") {
        return parts[parts.length - 1];
      }
      return "";
    case "Slack":
      if (prefix === "slack") {
        return parts[parts.length - 1];
      }
      return "";
    case "Line":
      if (prefix === "line") {
        return parts[parts.length - 1];
      }
      return "";
    case "Lark":
      if (prefix === "lark") {
        return parts[parts.length - 1];
      }
      return "";
    default:
      return "";
  }
}

function channelHandles(item) {
  const out = [];
  const seen = new Set();

  function push(channel, raw) {
    const full = String(raw || "").trim();
    if (!full) {
      return;
    }
    const key = `${channel}:${full}`;
    if (seen.has(key)) {
      return;
    }
    seen.add(key);
    out.push({
      key,
      channel,
      full,
      short: shortenIdentifier(full),
    });
  }

  const telegramUsername = String(item?.tg_username || "").trim();
  push("Telegram", telegramUsername ? `@${telegramUsername}` : fallbackHandleFromContactID(item, "Telegram"));
  push("Slack", String(item?.slack_user_id || "").trim() || fallbackHandleFromContactID(item, "Slack"));
  push("Line", String(item?.line_user_id || "").trim() || fallbackHandleFromContactID(item, "Line"));
  push("Lark", String(item?.lark_open_id || "").trim() || fallbackHandleFromContactID(item, "Lark"));

  return out;
}

const ContactsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const err = ref("");
    const items = ref([]);
    const filterText = ref("");

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const data = await runtimeApiFetch("/contacts/list");
        items.value = Array.isArray(data.items) ? data.items : [];
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function displayName(item) {
      const nickname = String(item?.nickname || "").trim();
      if (nickname) {
        return nickname;
      }
      return t("contacts_unnamed");
    }

    function statusClass(item) {
      return normalizeStatus(item?.status) === "inactive"
        ? "contact-badge contact-status contact-status-inactive"
        : "contact-badge contact-status contact-status-active";
    }

    function statusText(item) {
      return normalizeStatus(item?.status) === "inactive"
        ? t("contacts_status_inactive")
        : t("contacts_status_active");
    }

    function kindText(item) {
      return normalizeKind(item?.kind) === "agent" ? t("contacts_kind_agent") : t("contacts_kind_human");
    }

    function topicList(item) {
      return compactStrings(item?.topic_preferences);
    }

    function timeOrDash(value) {
      return String(value || "").trim() ? formatTime(value) : "-";
    }

    function hasValue(value) {
      return String(value || "").trim() !== "";
    }

    function cardClass(item) {
      return normalizeStatus(item?.status) === "inactive" ? "contact-card frame is-inactive" : "contact-card frame";
    }

    function matchesFilter(item) {
      const query = String(filterText.value || "").trim().toLowerCase();
      if (!query) {
        return true;
      }
      const haystack = [
        displayName(item),
        String(item?.contact_id || "").trim(),
        String(item?.persona_brief || "").trim(),
        ...topicList(item),
        ...channelHandles(item).map((handle) => `${handle.channel} ${handle.full} ${handle.short}`),
      ]
        .join("\n")
        .toLowerCase();
      return haystack.includes(query);
    }

    const filteredItems = computed(() => items.value.filter((item) => matchesFilter(item)));

    onMounted(() => {
      void load();
    });
    watch(
      () => endpointState.selectedRef,
      () => {
        void load();
      }
    );

    return {
      t,
      loading,
      err,
      items,
      filterText,
      filteredItems,
      displayName,
      cardClass,
      statusClass,
      statusText,
      kindText,
      channelHandles,
      topicList,
      hasValue,
      timeOrDash,
    };
  },
  template: `
    <AppPage :title="t('contacts_title')">
      <template #actions>
        <div class="xs contacts-bar-filter">
          <QInput
            v-model="filterText"
            class="xs contacts-filter-input"
            :placeholder="t('contacts_filter_placeholder')"
          />
        </div>
      </template>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="contacts-list">
        <article
          v-for="item in filteredItems"
          :key="item.contact_id"
          :class="cardClass(item)"
          :title="item.contact_id"
        >
          <header class="contact-head">
            <div class="contact-identity">
              <div class="contact-badges">
                <code class="contact-badge">{{ kindText(item) }}</code>
                <code :class="statusClass(item)">{{ statusText(item) }}</code>
              </div>
              <h3 class="contact-name">{{ displayName(item) }}</h3>
              <div v-if="item.persona_brief || topicList(item).length > 0" class="contact-body">
                <p v-if="item.persona_brief" class="contact-brief">{{ item.persona_brief }}</p>
                <div v-if="topicList(item).length > 0" class="topic-list">
                  <span class="topic-list-label">{{ t("contacts_field_topics") }}</span>
                  <div class="topic-list-items">
                    <span v-for="topic in topicList(item)" :key="item.contact_id + '-' + topic" class="topic-tag">{{ topic }}</span>
                  </div>
                </div>
              </div>
              <div v-if="channelHandles(item).length > 0" class="channel-list channel-list-primary">
                <div
                  v-for="handle in channelHandles(item)"
                  :key="handle.key"
                  class="channel-handle"
                >
                  <span class="channel-handle-name">{{ handle.channel }}</span>
                  <code class="channel-handle-value" :title="handle.full">{{ handle.short }}</code>
                </div>
              </div>
            </div>
            <div class="contact-timeline">
              <div class="contact-timeline-item">
                <strong class="contact-timeline-value">{{ timeOrDash(item.last_interaction_at) }}</strong>
                <span class="contact-timeline-note">{{ t("contacts_field_last_interaction") }}</span>
              </div>
              <div v-if="hasValue(item.cooldown_until)" class="contact-timeline-item">
                <strong class="contact-timeline-value">{{ timeOrDash(item.cooldown_until) }}</strong>
                <span class="contact-timeline-note">{{ t("contacts_field_cooldown") }}</span>
              </div>
            </div>
          </header>
        </article>
        <p v-if="filteredItems.length === 0 && !loading" class="muted contacts-empty">
          {{ items.length === 0 ? t("contacts_empty") : t("contacts_empty_filtered") }}
        </p>
      </div>
    </AppPage>
  `,
};

export default ContactsView;
