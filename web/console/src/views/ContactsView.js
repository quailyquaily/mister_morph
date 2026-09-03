import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { useToast } from "quail-ui";
import "./ContactsView.css";

import AppPage from "../components/AppPage";
import ContactAvatar from "../components/ContactAvatar";
import { endpointState, formatTime, runtimeApiFetch, translate } from "../core/context";
import { useContactsStore } from "../stores/contactsStore";

function normalizeStatus(raw) {
  return String(raw || "").trim().toLowerCase() === "inactive" ? "inactive" : "active";
}

function normalizeKind(raw) {
  return String(raw || "").trim().toLowerCase() === "agent" ? "agent" : "human";
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
  if (!value || value.length <= 22) {
    return value;
  }
  return `${value.slice(0, 10)}…${value.slice(-7)}`;
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
    case "telegram":
      return prefix === "tg" || prefix === "telegram" ? parts[parts.length - 1] : "";
    case "slack":
      return prefix === "slack" ? parts[parts.length - 1] : "";
    case "line":
      return prefix === "line" ? parts[parts.length - 1] : "";
    case "lark":
      return prefix === "lark" || prefix === "lark_user" ? parts[parts.length - 1] : "";
	case "mixin":
	  return prefix === "mixin" ? parts[parts.length - 1] : "";
    default:
      return "";
  }
}

function channelLabel(t, raw) {
  const channel = String(raw || "").trim().toLowerCase();
  switch (channel) {
    case "telegram":
    case "tg":
      return t("endpoint_channel_telegram");
    case "slack":
      return t("endpoint_channel_slack");
    case "line":
      return t("endpoint_channel_line");
    case "lark":
      return t("endpoint_channel_lark");
	case "mixin":
	  return t("endpoint_channel_mixin");
    case "console":
      return t("endpoint_channel_console");
    default:
      return String(raw || "").trim() || "—";
  }
}

function channelHandles(t, item) {
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
      channel: channelLabel(t, channel),
      full,
      short: shortenIdentifier(full),
    });
  }

  const telegramUsername = String(item?.tg_username || "").trim().replace(/^@+/, "");
  push("telegram", telegramUsername ? `@${telegramUsername}` : fallbackHandleFromContactID(item, "telegram"));
  push("slack", String(item?.slack_user_id || "").trim() || fallbackHandleFromContactID(item, "slack"));
  push("line", String(item?.line_user_id || "").trim() || fallbackHandleFromContactID(item, "line"));
  push("lark", String(item?.lark_open_id || "").trim() || fallbackHandleFromContactID(item, "lark"));
	const mixinIdentity = String(item?.mixin_identity_number || "").trim();
	push("mixin", mixinIdentity ? `@${mixinIdentity}` : String(item?.mixin_user_id || "").trim() || fallbackHandleFromContactID(item, "mixin"));

  return out;
}

const ContactsView = {
  components: {
    AppPage,
    ContactAvatar,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const contactsStore = useContactsStore();
    const { items, loading } = storeToRefs(contactsStore);

    const err = ref("");
    const filterText = ref("");
    const selectedContactID = ref("");
    const isMobile = ref(false);
    const mobileEditorVisible = ref(false);

    const editingContactID = ref("");
    const editorYAML = ref("");
    const loadedEditorYAML = ref("");
    const editorLoading = ref(false);
    const editorSaving = ref(false);
    const editorErr = ref("");

    const deleteDialogOpen = ref(false);
    const deleteTarget = ref(null);
    const deleting = ref(false);

    function displayName(item) {
      const nickname = String(item?.nickname || "").trim();
      if (nickname) {
        return nickname;
      }
      const telegramUsername = String(item?.tg_username || "").trim().replace(/^@+/, "");
      if (telegramUsername) {
        return `@${telegramUsername}`;
      }
      return t("contacts_unnamed");
    }

    function isAgent(item) {
      return normalizeKind(item?.kind) === "agent";
    }

    function isActive(item) {
      return normalizeStatus(item?.status) === "active";
    }

    function statusText(item) {
      return isActive(item) ? t("contacts_status_active") : t("contacts_status_inactive");
    }

    function kindText(item) {
      return isAgent(item) ? t("contacts_kind_agent") : t("contacts_kind_human");
    }

    function topicList(item) {
      return compactStrings(item?.topic_preferences);
    }

    function timeOrDash(value) {
      return String(value || "").trim() ? formatTime(value) : "—";
    }

    function primaryContactMeta(item) {
      const handle = channelHandles(t, item)[0];
      if (handle) {
        return `${handle.channel} · ${handle.short}`;
      }
      const channel = channelLabel(t, item?.channel);
      const contactID = shortenIdentifier(item?.contact_id);
      return contactID ? `${channel} · ${contactID}` : channel;
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
        channelLabel(t, item?.channel),
        kindText(item),
        statusText(item),
        ...topicList(item),
        ...channelHandles(t, item).map((handle) => `${handle.channel} ${handle.full}`),
      ]
        .join("\n")
        .toLowerCase();
      return haystack.includes(query);
    }

    const filteredItems = computed(() => items.value.filter((item) => matchesFilter(item)));
    const selectedContact = computed(() => {
      const contactID = String(selectedContactID.value || "").trim();
      if (!contactID) {
        return null;
      }
      return items.value.find((item) => String(item?.contact_id || "").trim() === contactID) || null;
    });
    const selectedHandles = computed(() => channelHandles(t, selectedContact.value));
    const selectedTopics = computed(() => topicList(selectedContact.value));
    const editing = computed(
      () => Boolean(selectedContact.value) && editingContactID.value === selectedContactID.value
    );
    const saveDisabled = computed(
      () =>
        editorLoading.value ||
        editorSaving.value ||
        !editing.value ||
        !String(editorYAML.value || "").trim() ||
        editorYAML.value === loadedEditorYAML.value
    );

    const showIndexPane = computed(() => !isMobile.value || !mobileEditorVisible.value);
    const showEditorPane = computed(() => !isMobile.value || mobileEditorVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobileEditorVisible.value);
    const mobileBarTitle = computed(() =>
      mobileShowBack.value && selectedContact.value ? displayName(selectedContact.value) : t("contacts_title")
    );
    const pageClass = computed(() => (isMobile.value ? "contacts-page contacts-page-mobile-split" : "contacts-page"));

    const deleteDialogText = computed(() =>
      t("contacts_delete_confirm", { name: displayName(deleteTarget.value || selectedContact.value) })
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

    function refreshMobileMode() {
      isMobile.value = typeof window !== "undefined" && window.innerWidth <= 920;
      if (!isMobile.value) {
        mobileEditorVisible.value = false;
      }
    }

    function showIndexView() {
      stopEdit();
      mobileEditorVisible.value = false;
    }

    function contactItemClass(item) {
      const classes = ["contacts-index-item", "workspace-sidebar-item"];
      if (!isMobile.value && String(item?.contact_id || "").trim() === selectedContactID.value) {
        classes.push("is-active");
      }
      if (isAgent(item)) {
        classes.push("is-agent");
      }
      return classes.join(" ");
    }

    function contactAriaLabel(item) {
      return `${displayName(item)}, ${kindText(item)}, ${statusText(item)}`;
    }

    function stopEdit() {
      editingContactID.value = "";
      editorYAML.value = "";
      loadedEditorYAML.value = "";
      editorLoading.value = false;
      editorSaving.value = false;
      editorErr.value = "";
    }

    function clearSelection() {
      stopEdit();
      selectedContactID.value = "";
      mobileEditorVisible.value = false;
    }

    function selectContact(item) {
      const contactID = String(item?.contact_id || "").trim();
      if (!contactID) {
        return;
      }
      if (contactID !== selectedContactID.value) {
        stopEdit();
        selectedContactID.value = contactID;
      }
      if (isMobile.value) {
        mobileEditorVisible.value = true;
      }
    }

    async function load() {
      err.value = "";
      try {
        await contactsStore.load({ force: true });
        if (selectedContactID.value && !selectedContact.value) {
          clearSelection();
        }
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      }
    }

    async function startEdit() {
      const contactID = String(selectedContact.value?.contact_id || "").trim();
      if (!contactID || editorLoading.value) {
        return;
      }
      editingContactID.value = contactID;
      editorLoading.value = true;
      editorErr.value = "";
      editorYAML.value = "";
      loadedEditorYAML.value = "";
      try {
        const data = await runtimeApiFetch(`/contacts/item?contact_id=${encodeURIComponent(contactID)}`);
        if (editingContactID.value !== contactID) {
          return;
        }
        const yaml = String(data?.yaml || "").trim();
        editorYAML.value = yaml;
        loadedEditorYAML.value = yaml;
      } catch (e) {
        if (editingContactID.value === contactID) {
          editorErr.value = e.message || t("msg_load_failed");
        }
      } finally {
        if (editingContactID.value === contactID) {
          editorLoading.value = false;
        }
      }
    }

    async function saveEdit() {
      const contactID = String(editingContactID.value || "").trim();
      if (!contactID || saveDisabled.value) {
        return;
      }
      editorSaving.value = true;
      editorErr.value = "";
      try {
        await runtimeApiFetch("/contacts/item", {
          method: "PUT",
          body: {
            contact_id: contactID,
            yaml: editorYAML.value,
          },
        });
        await load();
        if (selectedContactID.value === contactID) {
          stopEdit();
        }
        toast.success(t("msg_save_success"));
      } catch (e) {
        toast.error(e.message || t("msg_save_failed"));
      } finally {
        editorSaving.value = false;
      }
    }

    function confirmDelete() {
      if (!selectedContact.value) {
        return;
      }
      deleteTarget.value = selectedContact.value;
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
      try {
        await runtimeApiFetch(`/contacts/item?contact_id=${encodeURIComponent(contactID)}`, {
          method: "DELETE",
        });
        if (selectedContactID.value === contactID) {
          clearSelection();
        }
        await load();
        toast.success(t("msg_delete_success"));
      } catch (e) {
        toast.error(e.message || t("msg_delete_failed"));
      } finally {
        deleting.value = false;
        deleteTarget.value = null;
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      void load();
    });
    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
    });
    watch(
      () => endpointState.selectedRef,
      () => {
        clearSelection();
        closeDeleteDialog();
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
      selectedContact,
      selectedHandles,
      selectedTopics,
      editing,
      editorYAML,
      editorLoading,
      editorSaving,
      editorErr,
      saveDisabled,
      isMobile,
      showIndexPane,
      showEditorPane,
      mobileShowBack,
      mobileBarTitle,
      pageClass,
      displayName,
      isAgent,
      statusText,
      kindText,
      channelLabel,
      timeOrDash,
      primaryContactMeta,
      contactItemClass,
      contactAriaLabel,
      endpointState,
      showIndexView,
      selectContact,
      startEdit,
      stopEdit,
      saveEdit,
      confirmDelete,
      deleteDialogOpen,
      deleteDialogText,
      deleteDialogActions,
    };
  },
  template: `
    <AppPage
      :title="t('contacts_title')"
      :class="pageClass"
      :hideDesktopBar="true"
      :hideMobileBar="showIndexPane"
      :overlayBar="true"
    >
      <template #leading>
        <div class="contacts-page-bar">
          <QButton
            v-if="mobileShowBack"
            class="plain xs icon contacts-page-bar-back"
            :title="t('contacts_title')"
            :aria-label="t('contacts_title')"
            @click="showIndexView"
          >
            <PhArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ mobileBarTitle }}</h2>
        </div>
      </template>

      <div class="contacts-workbench">
        <aside v-if="showIndexPane" class="contacts-index workspace-sidebar-section" :aria-label="t('contacts_title')">
          <div class="contacts-index-head workspace-sidebar-head">
            <h3 class="contacts-index-title workspace-section-title">{{ t("contacts_title") }}</h3>
          </div>

          <div class="contacts-index-body">
            <div class="contacts-index-filter">
              <QInput
                v-model="filterText"
                class="xs contacts-filter-input"
                :placeholder="t('contacts_filter_placeholder')"
                :aria-label="t('contacts_filter_placeholder')"
              />
            </div>

            <div v-if="loading" class="contacts-index-loading" aria-hidden="true">
              <QSkeleton variant="card" height="62px" :count="4" />
            </div>
            <QFence v-else-if="err" class="contacts-index-error" type="danger" icon="PhXCircle" :text="err" />

            <div
              v-if="!loading && filteredItems.length > 0"
              class="contacts-index-list workspace-sidebar-list"
              :role="isMobile ? undefined : 'listbox'"
            >
              <button
                v-for="item in filteredItems"
                :key="item.contact_id"
                type="button"
                :class="contactItemClass(item)"
                :role="isMobile ? undefined : 'option'"
                :aria-selected="isMobile ? undefined : selectedContact?.contact_id === item.contact_id"
                :aria-label="contactAriaLabel(item)"
                @click="selectContact(item)"
              >
                <span class="contacts-index-kind" :title="kindText(item)" aria-hidden="true">
                  <ContactAvatar
                    :item="item"
                    :name="displayName(item)"
                    :endpointRef="endpointState.selectedRef"
                  />
                  <span :class="isAgent(item) ? 'contacts-kind-badge is-agent' : 'contacts-kind-badge'">
                    <PhCpu v-if="isAgent(item)" class="icon" />
                    <PhUserCircle v-else class="icon" />
                  </span>
                </span>
                <span class="workspace-sidebar-item-copy">
                  <span class="workspace-sidebar-item-title">{{ displayName(item) }}</span>
                  <span class="contacts-index-item-meta workspace-sidebar-item-meta">{{ primaryContactMeta(item) }}</span>
                </span>
              </button>
            </div>

            <p v-else-if="!loading" class="contacts-index-empty muted">
              {{ items.length === 0 ? t("contacts_empty") : t("contacts_empty_filtered") }}
            </p>
          </div>
        </aside>

        <QCard v-if="showEditorPane && selectedContact" class="contacts-detail-card" variant="default">
          <div class="contacts-detail-shell">
            <header class="contacts-detail-head">
              <div class="contacts-detail-identity">
                <span class="contacts-detail-kind" :title="kindText(selectedContact)" aria-hidden="true">
                  <ContactAvatar
                    :item="selectedContact"
                    :name="displayName(selectedContact)"
                    :endpointRef="endpointState.selectedRef"
                    size="detail"
                  />
                  <span :class="isAgent(selectedContact) ? 'contacts-kind-badge is-agent' : 'contacts-kind-badge'">
                    <PhCpu v-if="isAgent(selectedContact)" class="icon" />
                    <PhUserCircle v-else class="icon" />
                  </span>
                </span>
                <div class="contacts-detail-copy">
                  <h3 class="contacts-detail-title workspace-document-title">{{ displayName(selectedContact) }}</h3>
                </div>
              </div>

              <div v-if="editing" class="contacts-detail-actions">
                <QButton class="primary" :loading="editorSaving" :disabled="saveDisabled" @click="saveEdit">
                  {{ t("action_save") }}
                </QButton>
                <QButton class="outlined" :disabled="editorSaving" @click="stopEdit">{{ t("action_cancel") }}</QButton>
              </div>
              <div v-else class="contacts-detail-actions">
                <QButton class="outlined contacts-edit-action" @click="startEdit">
                  <PhPencilSimple class="icon contacts-detail-action-icon" />
                  <span>{{ t("action_edit") }}</span>
                </QButton>
                <QButton
                  class="plain icon contacts-delete-action"
                  :title="t('action_delete')"
                  :aria-label="t('action_delete')"
                  @click="confirmDelete"
                >
                  <PhTrash class="icon" />
                </QButton>
              </div>
            </header>

            <div v-if="editing" class="contacts-editor-body">
              <QSkeleton v-if="editorLoading" variant="card" height="360px" :count="1" />
              <template v-else>
                <QFence v-if="editorErr" type="danger" icon="PhXCircle" :text="editorErr" />
                <QTextarea
                  v-model="editorYAML"
                  class="contacts-editor-textarea"
                  :rows="26"
                  :disabled="editorSaving"
                  :aria-label="t('contacts_editor_hint')"
                />
                <p class="contacts-editor-note">{{ t("contacts_editor_hint") }}</p>
              </template>
            </div>

            <div v-else class="contacts-detail-body">
              <section class="contacts-detail-section">
                <h4 class="contacts-detail-section-title">{{ t("contacts_section_profile") }}</h4>
                <dl class="contacts-detail-fields">
                  <div class="contacts-detail-field">
                    <dt>{{ t("contacts_field_contact_id") }}</dt>
                    <dd><code>{{ selectedContact.contact_id }}</code></dd>
                  </div>
                  <div class="contacts-detail-field">
                    <dt>{{ t("contacts_field_channel") }}</dt>
                    <dd>{{ channelLabel(t, selectedContact.channel) }}</dd>
                  </div>
                  <div v-if="selectedContact.persona_brief" class="contacts-detail-field is-wide">
                    <dt>{{ t("contacts_field_persona") }}</dt>
                    <dd class="contacts-detail-prose">{{ selectedContact.persona_brief }}</dd>
                  </div>
                </dl>
              </section>

              <section v-if="selectedHandles.length > 0" class="contacts-detail-section">
                <h4 class="contacts-detail-section-title">{{ t("contacts_section_connections") }}</h4>
                <dl class="contacts-detail-fields">
                  <div v-for="handle in selectedHandles" :key="handle.key" class="contacts-detail-field">
                    <dt>{{ handle.channel }}</dt>
                    <dd><code :title="handle.full">{{ handle.full }}</code></dd>
                  </div>
                </dl>
              </section>

              <section v-if="selectedTopics.length > 0" class="contacts-detail-section">
                <h4 class="contacts-detail-section-title">{{ t("contacts_field_topics") }}</h4>
                <ul class="contacts-topic-list">
                  <li v-for="topic in selectedTopics" :key="selectedContact.contact_id + '-' + topic">{{ topic }}</li>
                </ul>
              </section>

              <section class="contacts-detail-section">
                <h4 class="contacts-detail-section-title">{{ t("contacts_section_activity") }}</h4>
                <dl class="contacts-detail-fields">
                  <div class="contacts-detail-field">
                    <dt>{{ t("contacts_field_last_interaction") }}</dt>
                    <dd>{{ timeOrDash(selectedContact.last_interaction_at) }}</dd>
                  </div>
                  <div class="contacts-detail-field">
                    <dt>{{ t("contacts_field_cooldown") }}</dt>
                    <dd>{{ timeOrDash(selectedContact.cooldown_until) }}</dd>
                  </div>
                </dl>
              </section>
            </div>
          </div>
        </QCard>

        <section v-else-if="showEditorPane" class="contacts-placeholder">
          <div class="contacts-placeholder-copy">
            <h3 class="contacts-placeholder-title workspace-document-title">{{ t("contacts_detail_empty_title") }}</h3>
            <p class="contacts-placeholder-note">{{ t("contacts_detail_empty_hint") }}</p>
          </div>
        </section>
      </div>

      <QMessageDialog
        v-model="deleteDialogOpen"
        icon="PhTrash"
        iconColor="red"
        :title="t('action_delete')"
        :text="deleteDialogText"
        :actions="deleteDialogActions"
      />
    </AppPage>
  `,
};

export default ContactsView;
