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
      return prefix === "tg" || prefix === "telegram" ? parts[parts.length - 1] : "";
    case "Slack":
      return prefix === "slack" ? parts[parts.length - 1] : "";
    case "Line":
      return prefix === "line" ? parts[parts.length - 1] : "";
    case "Lark":
      return prefix === "lark" ? parts[parts.length - 1] : "";
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
    const ok = ref("");
    const items = ref([]);
    const filterText = ref("");

    const editingContactID = ref("");
    const editorYAML = ref("");
    const editorLoading = ref(false);
    const editorSaving = ref(false);
    const editorErr = ref("");
    const editorOk = ref("");
    const deleteDialogOpen = ref(false);
    const deleteTarget = ref(null);
    const deleting = ref(false);

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
      return normalizeStatus(item?.status) === "inactive" ? "default" : "success";
    }

    function statusText(item) {
      return normalizeStatus(item?.status) === "inactive"
        ? t("contacts_status_inactive")
        : t("contacts_status_active");
    }

    function kindText(item) {
      return normalizeKind(item?.kind) === "agent" ? t("contacts_kind_agent") : t("contacts_kind_human");
    }

    function isInactive(item) {
      return normalizeStatus(item?.status) === "inactive";
    }

    function cardMarker(item) {
      return kindText(item);
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
      const classes = ["contact-card"];
      if (isInactive(item)) {
        classes.push("is-inactive");
      }
      classes.push(normalizeKind(item?.kind) === "agent" ? "is-agent" : "is-human");
      return classes.join(" ");
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

    function isEditing(item) {
      return String(item?.contact_id || "").trim() === editingContactID.value;
    }

    function toggleEdit(item) {
      if (isEditing(item)) {
        stopEdit();
        return;
      }
      void startEdit(item);
    }

    async function startEdit(item) {
      const contactID = String(item?.contact_id || "").trim();
      if (!contactID) {
        return;
      }
      editingContactID.value = contactID;
      editorLoading.value = true;
      editorErr.value = "";
      editorOk.value = "";
      editorYAML.value = "";
      try {
        const data = await runtimeApiFetch(`/contacts/item?contact_id=${encodeURIComponent(contactID)}`);
        editorYAML.value = String(data?.yaml || "").trim();
      } catch (e) {
        editorErr.value = e.message || t("msg_load_failed");
      } finally {
        editorLoading.value = false;
      }
    }

    function stopEdit() {
      editingContactID.value = "";
      editorYAML.value = "";
      editorLoading.value = false;
      editorSaving.value = false;
      editorErr.value = "";
      editorOk.value = "";
    }

    async function saveEdit() {
      if (!editingContactID.value) {
        return;
      }
      editorSaving.value = true;
      err.value = "";
      ok.value = "";
      editorErr.value = "";
      editorOk.value = "";
      try {
        const data = await runtimeApiFetch("/contacts/item", {
          method: "PUT",
          body: {
            contact_id: editingContactID.value,
            yaml: editorYAML.value,
          },
        });
        editorYAML.value = String(data?.yaml || editorYAML.value || "").trim();
        editorOk.value = t("msg_save_success");
        await load();
      } catch (e) {
        editorErr.value = e.message || t("msg_save_failed");
      } finally {
        editorSaving.value = false;
      }
    }

    function confirmDelete(item) {
      deleteTarget.value = item || null;
      deleteDialogOpen.value = true;
    }

    function closeDeleteDialog() {
      deleteDialogOpen.value = false;
      deleteTarget.value = null;
    }

    async function deleteContact() {
      if (deleting.value) {
        return;
      }
      const contactID = String(deleteTarget.value?.contact_id || "").trim();
      if (!contactID) {
        closeDeleteDialog();
        return;
      }
      deleting.value = true;
      deleteDialogOpen.value = false;
      err.value = "";
      ok.value = "";
      try {
        await runtimeApiFetch(`/contacts/item?contact_id=${encodeURIComponent(contactID)}`, {
          method: "DELETE",
        });
        if (editingContactID.value === contactID) {
          stopEdit();
        }
        ok.value = t("msg_delete_success");
        await load();
      } catch (e) {
        err.value = e.message || t("msg_delete_failed");
      } finally {
        deleting.value = false;
        deleteTarget.value = null;
      }
    }

    const filteredItems = computed(() => items.value.filter((item) => matchesFilter(item)));
    const saveDisabled = computed(
      () => editorLoading.value || editorSaving.value || !editingContactID.value || !String(editorYAML.value || "").trim()
    );
    const deleteDialogText = computed(() =>
      t("contacts_delete_confirm", { name: displayName(deleteTarget.value || null) })
    );
    const deleteDialogActions = computed(() => [
      {
        name: "cancel",
        label: t("action_cancel"),
        class: "outlined",
        action: closeDeleteDialog,
      },
      {
        name: "delete",
        label: t("action_delete"),
        class: "danger",
        action: deleteContact,
      },
    ]);

    onMounted(() => {
      void load();
    });
    watch(
      () => endpointState.selectedRef,
      () => {
        stopEdit();
        closeDeleteDialog();
        void load();
      }
    );

    return {
      t,
      loading,
      err,
      ok,
      items,
      filterText,
      filteredItems,
      displayName,
      cardClass,
      statusClass,
      statusText,
      kindText,
      isInactive,
      cardMarker,
      channelHandles,
      topicList,
      hasValue,
      timeOrDash,
      isEditing,
      toggleEdit,
      startEdit,
      stopEdit,
      saveEdit,
      confirmDelete,
      deleteDialogOpen,
      deleteDialogText,
      deleteDialogActions,
      editorYAML,
      editorLoading,
      editorSaving,
      editorErr,
      editorOk,
      saveDisabled,
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
      <QFence v-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
      <div class="contacts-list">
        <QCard
          v-for="item in filteredItems"
          :key="item.contact_id"
          variant="default"
          :hoverable="true"
          :dashed="isInactive(item)"
          :title="displayName(item)"
          :marker="cardMarker(item)"
          marker-style="plate"
          :class="cardClass(item)"
        >
          <header class="contact-head">
            <div class="contact-identity">
              <div class="contact-topline">
                <div class="contact-badges">
                  <QBadge
                    :type="statusClass(item)"
                    size="sm"
                  >
                    {{ statusText(item) }}
                  </QBadge>
                </div>
                <div class="contact-actions">
                  <QButton
                    class="plain xs icon contact-action-button"
                    :title="isEditing(item) ? t('action_close') : t('action_edit')"
                    :aria-label="isEditing(item) ? t('action_close') : t('action_edit')"
                    @click="toggleEdit(item)"
                  >
                    <QIconCode class="icon" />
                  </QButton>
                  <QButton
                    class="plain xs icon contact-action-button contact-action-delete"
                    :title="t('action_delete')"
                    :aria-label="t('action_delete')"
                    @click="confirmDelete(item)"
                  >
                    <QIconTrash class="icon" />
                  </QButton>
                </div>
              </div>
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
          <section v-if="isEditing(item)" class="contact-editor">
            <QProgress v-if="editorLoading" :infinite="true" />
            <QFence v-if="editorErr" type="danger" icon="QIconCloseCircle" :text="editorErr" />
            <QFence v-if="editorOk" type="success" icon="QIconCheckCircle" :text="editorOk" />
            <QTextarea v-model="editorYAML" class="contact-editor-textarea" :rows="14" />
            <p class="contact-editor-note">{{ t("contacts_editor_hint") }}</p>
            <div class="contact-editor-actions">
              <QButton class="primary" :loading="editorSaving" :disabled="saveDisabled" @click="saveEdit">{{ t("action_save") }}</QButton>
              <QButton class="plain" @click="stopEdit">{{ t("action_close") }}</QButton>
            </div>
          </section>
        </QCard>
        <p v-if="filteredItems.length === 0 && !loading" class="muted contacts-empty">
          {{ items.length === 0 ? t("contacts_empty") : t("contacts_empty_filtered") }}
        </p>
      </div>
      <QMessageDialog
        v-model="deleteDialogOpen"
        icon="QIconTrash"
        iconColor="red"
        :title="t('action_delete')"
        :text="deleteDialogText"
        :actions="deleteDialogActions"
      />
    </AppPage>
  `,
};

export default ContactsView;
