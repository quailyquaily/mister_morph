import { computed, onMounted, ref, watch } from "vue";
import "./ContactsView.css";

import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";

const CONTACT_STATUS_META = [
  { value: "all", titleKey: "status_all" },
  { value: "active", titleKey: "contacts_status_active" },
  { value: "inactive", titleKey: "contacts_status_inactive" },
];

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

function summarizeChannelTargets(item) {
  const channel = String(item?.channel || "").trim().toLowerCase();
  switch (channel) {
    case "telegram": {
      const parts = [];
      const username = String(item?.tg_username || "").trim();
      if (username) {
        parts.push(`@${username}`);
      }
      if (item?.tg_private_chat_id) {
        parts.push(`private:${item.tg_private_chat_id}`);
      }
      const groupIDs = compactStrings(item?.tg_group_chat_ids);
      if (groupIDs.length > 0) {
        parts.push(`groups:${groupIDs.join(", ")}`);
      }
      return parts.join(" | ");
    }
    case "slack": {
      const parts = [];
      const team = String(item?.slack_team_id || "").trim();
      const user = String(item?.slack_user_id || "").trim();
      const dm = String(item?.slack_dm_channel_id || "").trim();
      const channelIDs = compactStrings(item?.slack_channel_ids);
      if (team) {
        parts.push(`team:${team}`);
      }
      if (user) {
        parts.push(`user:${user}`);
      }
      if (dm) {
        parts.push(`dm:${dm}`);
      }
      if (channelIDs.length > 0) {
        parts.push(`channels:${channelIDs.join(", ")}`);
      }
      return parts.join(" | ");
    }
    case "line": {
      const parts = [];
      const userID = String(item?.line_user_id || "").trim();
      const chatIDs = compactStrings(item?.line_chat_ids);
      if (userID) {
        parts.push(`user:${userID}`);
      }
      if (chatIDs.length > 0) {
        parts.push(`chats:${chatIDs.join(", ")}`);
      }
      return parts.join(" | ");
    }
    case "lark": {
      const parts = [];
      const openID = String(item?.lark_open_id || "").trim();
      const chatIDs = compactStrings(item?.lark_chat_ids);
      if (openID) {
        parts.push(`open:${openID}`);
      }
      if (chatIDs.length > 0) {
        parts.push(`chats:${chatIDs.join(", ")}`);
      }
      return parts.join(" | ");
    }
    default:
      return "";
  }
}

const ContactsView = {
  setup() {
    const t = translate;
    const loading = ref(false);
    const err = ref("");
    const statusValue = ref("all");
    const items = ref([]);

    const statusItems = computed(() =>
      CONTACT_STATUS_META.map((item) => ({
        value: item.value,
        title: t(item.titleKey),
      }))
    );
    const selectedStatusItem = computed(() => {
      return statusItems.value.find((item) => item.value === statusValue.value) || statusItems.value[0] || null;
    });

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const q = new URLSearchParams();
        q.set("status", statusValue.value || "all");
        const data = await runtimeApiFetch(`/contacts/list?${q.toString()}`);
        items.value = Array.isArray(data.items) ? data.items : [];
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function onStatusChange(item) {
      if (item && typeof item === "object" && typeof item.value === "string") {
        statusValue.value = item.value;
        void load();
      }
    }

    function displayName(item) {
      const nickname = String(item?.nickname || "").trim();
      if (nickname) {
        return nickname;
      }
      return String(item?.contact_id || "-");
    }

    function statusClass(item) {
      return normalizeStatus(item?.status) === "inactive"
        ? "contact-status contact-status-inactive"
        : "contact-status contact-status-active";
    }

    function statusText(item) {
      return normalizeStatus(item?.status) === "inactive"
        ? t("contacts_status_inactive")
        : t("contacts_status_active");
    }

    function kindText(item) {
      return normalizeKind(item?.kind) === "agent" ? t("contacts_kind_agent") : t("contacts_kind_human");
    }

    function channelTargets(item) {
      const value = summarizeChannelTargets(item);
      return value || "-";
    }

    function topicList(item) {
      return compactStrings(item?.topic_preferences);
    }

    function timeOrDash(value) {
      return String(value || "").trim() ? formatTime(value) : "-";
    }

    onMounted(load);
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
      statusItems,
      selectedStatusItem,
      onStatusChange,
      load,
      displayName,
      statusClass,
      statusText,
      kindText,
      channelTargets,
      topicList,
      timeOrDash,
    };
  },
  template: `
    <section>
      <h2 class="title">{{ t("contacts_title") }}</h2>
      <div class="toolbar wrap">
        <div class="tool-item">
          <QDropdownMenu
            :items="statusItems"
            :initialItem="selectedStatusItem"
            :placeholder="t('placeholder_status')"
            @change="onStatusChange"
          />
        </div>
        <QButton class="outlined" :loading="loading" @click="load">{{ t("action_refresh") }}</QButton>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <div class="stack">
        <article v-for="item in items" :key="item.contact_id" class="contact-card">
          <header class="contact-head">
            <div class="contact-head-left">
              <h3 class="contact-name">{{ displayName(item) }}</h3>
              <code class="contact-id">{{ item.contact_id }}</code>
            </div>
            <code :class="statusClass(item)">{{ statusText(item) }}</code>
          </header>
          <div class="contact-grid">
            <div class="contact-field">
              <span class="contact-label">{{ t("contacts_field_kind") }}</span>
              <code>{{ kindText(item) }}</code>
            </div>
            <div class="contact-field">
              <span class="contact-label">{{ t("contacts_field_channel") }}</span>
              <code>{{ item.channel || "-" }}</code>
            </div>
            <div class="contact-field contact-field-full">
              <span class="contact-label">{{ t("contacts_field_targets") }}</span>
              <code>{{ channelTargets(item) }}</code>
            </div>
            <div class="contact-field">
              <span class="contact-label">{{ t("contacts_field_last_interaction") }}</span>
              <code>{{ timeOrDash(item.last_interaction_at) }}</code>
            </div>
            <div class="contact-field">
              <span class="contact-label">{{ t("contacts_field_cooldown") }}</span>
              <code>{{ timeOrDash(item.cooldown_until) }}</code>
            </div>
          </div>
          <p v-if="item.persona_brief" class="contact-brief">{{ item.persona_brief }}</p>
          <div v-if="topicList(item).length > 0" class="contact-topics">
            <span class="contact-label">{{ t("contacts_field_topics") }}</span>
            <div class="topic-list">
              <code v-for="topic in topicList(item)" :key="item.contact_id + '-' + topic" class="topic-tag">{{ topic }}</code>
            </div>
          </div>
        </article>
        <p v-if="items.length === 0 && !loading" class="muted">{{ t("contacts_empty") }}</p>
      </div>
    </section>
  `,
};

export default ContactsView;
