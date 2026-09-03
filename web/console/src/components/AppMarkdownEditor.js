import { storeToRefs } from "pinia";
import { computed, onBeforeUnmount, ref, watch } from "vue";

import channelDiscordLogoURL from "../assets/images/channels/discord.svg";
import channelLarkLogoURL from "../assets/images/channels/lark.svg";
import channelLineLogoURL from "../assets/images/channels/line.svg";
import channelMixinLogoURL from "../assets/images/channels/mixin.svg";
import channelSlackLogoURL from "../assets/images/channels/slack.svg";
import channelTelegramLogoURL from "../assets/images/channels/telegram.svg";
import {
  endpointState,
  runtimeApiDownloadForEndpoint,
  runtimeApiFetchForEndpoint,
  translate,
} from "../core/context";
import { useContactsStore } from "../stores/contactsStore";
import MarkdownEditor from "./MarkdownEditor";
import "./AppMarkdownEditor.css";

const CHANNEL_LOGOS = {
  discord: channelDiscordLogoURL,
  lark: channelLarkLogoURL,
  line: channelLineLogoURL,
  mixin: channelMixinLogoURL,
  slack: channelSlackLogoURL,
  telegram: channelTelegramLogoURL,
};

function trimText(value) {
  return String(value || "").trim();
}

function platformFromReferenceID(referenceID) {
  const protocol = trimText(referenceID).split(":", 1)[0].toLowerCase();
  if (protocol === "tg") {
    return "telegram";
  }
  if (protocol === "line_user") {
    return "line";
  }
  if (protocol === "lark_user") {
    return "lark";
  }
  return protocol;
}

function conversationPlatform(conversation) {
  return trimText(conversation?.platform).toLowerCase() || platformFromReferenceID(conversation?.chat_id);
}

function initialAvatarDataURL(value) {
  const initial = Array.from(trimText(value))[0]?.toUpperCase() || "?";
  const escaped = initial.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28"><rect width="28" height="28" rx="14" fill="#fff"/><text x="14" y="14" dy=".35em" text-anchor="middle" fill="#426f9e" font-family="system-ui,sans-serif" font-size="12" font-weight="650">${escaped}</text></svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

function safeMentionLabel(value) {
  return trimText(value)
    .replace(/[\[\]\r\n]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function contactDisplayName(contact, t) {
  return trimText(contact?.nickname) || trimText(contact?.contact_id) || t("contacts_unnamed");
}

function mentionItemsForContacts(contacts, avatarSources, t) {
  const items = [];
  for (const contact of Array.isArray(contacts) ? contacts : []) {
    const contactID = trimText(contact?.contact_id);
    const channel = trimText(contact?.channel).toLowerCase();
    const status = trimText(contact?.status).toLowerCase();
    if (!contactID || channel === "console" || status === "inactive") {
      continue;
    }
    const title = contactDisplayName(contact, t);
    const label = safeMentionLabel(title) || t("contacts_unnamed");
    items.push({
      id: `mention-${contactID}`,
      title,
      subtitle: channel.toUpperCase(),
      value: `[${label}](${contactID})`,
      image: trimText(avatarSources?.[contactID]) || initialAvatarDataURL(title),
    });
  }
  return items;
}

function conversationItemsForProfiles(profiles) {
  const items = [];
  const seen = new Set();
  for (const profile of Array.isArray(profiles) ? profiles : []) {
    const chatID = trimText(profile?.chat_id);
    if (!chatID) {
      continue;
    }
    const key = chatID.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    const title = trimText(profile?.name) || chatID;
    const label = safeMentionLabel(title) || chatID;
    const platform = conversationPlatform(profile);
    const type = trimText(profile?.type);
    items.push({
      id: `conversation-${chatID}`,
      title,
      subtitle: [platform, type].filter(Boolean).map((value) => value.toUpperCase()).join(" · "),
      value: `[${label}](${chatID})`,
      image: CHANNEL_LOGOS[platform] || initialAvatarDataURL(title),
    });
  }
  return items.sort((left, right) => left.title.localeCompare(right.title));
}

const AppMarkdownEditor = {
  components: {
    MarkdownEditor,
  },
  props: {
    modelValue: {
      type: String,
      default: "",
    },
    placeholder: {
      type: String,
      default: "",
    },
    hint: {
      type: String,
      default: "",
    },
    height: {
      type: String,
      default: "",
    },
    disabled: {
      type: Boolean,
      default: false,
    },
    readOnly: {
      type: Boolean,
      default: false,
    },
    ariaLabel: {
      type: String,
      default: "",
    },
  },
  emits: ["update:modelValue"],
  setup(props) {
    const t = translate;
    const editorRef = ref(null);
    const contactsStore = useContactsStore();
    const { items: contacts, loading: contactsLoading, error: contactsError } = storeToRefs(contactsStore);
    const contactAvatarSources = ref({});
    const mentionItems = computed(() => mentionItemsForContacts(contacts.value, contactAvatarSources.value, t));
    const conversationItems = ref([]);
    const conversationsError = ref("");
    const conversationsLoading = ref(false);
    let conversationsRequestSeq = 0;
    let contactAvatarsRequestSeq = 0;
    let contactAvatarsLoading = false;
    const attemptedContactAvatars = new Set();
    const contactAvatarObjectURLs = new Set();

    function loadContacts() {
      void contactsStore.load({ perfSource: "markdown-editor-toolbar" }).catch(() => {});
    }

    function clearContactAvatars() {
      contactAvatarsRequestSeq += 1;
      contactAvatarsLoading = false;
      attemptedContactAvatars.clear();
      contactAvatarSources.value = {};
      for (const objectURL of contactAvatarObjectURLs) {
        URL.revokeObjectURL(objectURL);
      }
      contactAvatarObjectURLs.clear();
    }

    async function loadContactAvatars() {
      if (contactAvatarsLoading) {
        return;
      }
      const endpointRef = trimText(endpointState.selectedRef);
      if (!endpointRef) {
        return;
      }
      const candidates = contacts.value.filter((contact) => {
        const contactID = trimText(contact?.contact_id);
        const avatarPath = trimText(contact?.avatar_url);
        const requestKey = `${contactID}\n${avatarPath}`;
        if (!contactID || !avatarPath || contactAvatarSources.value[contactID] || attemptedContactAvatars.has(requestKey)) {
          return false;
        }
        attemptedContactAvatars.add(requestKey);
        return true;
      });
      if (candidates.length === 0) {
        return;
      }

      contactAvatarsLoading = true;
      const requestSeq = ++contactAvatarsRequestSeq;
      try {
        await Promise.all(
          candidates.map(async (contact) => {
            const contactID = trimText(contact.contact_id);
            try {
              const blob = await runtimeApiDownloadForEndpoint(endpointRef, trimText(contact.avatar_url), {
                cache: "force-cache",
              });
              if (requestSeq !== contactAvatarsRequestSeq) {
                return;
              }
              const objectURL = URL.createObjectURL(blob);
              contactAvatarObjectURLs.add(objectURL);
              contactAvatarSources.value = { ...contactAvatarSources.value, [contactID]: objectURL };
            } catch {
              // Keep the initial fallback when an avatar cannot be loaded.
            }
          })
        );
      } finally {
        if (requestSeq === contactAvatarsRequestSeq) {
          contactAvatarsLoading = false;
        }
      }
    }

    async function loadConversations() {
      if (conversationsLoading.value) {
        return;
      }
      const endpointRef = trimText(endpointState.selectedRef);
      const requestSeq = conversationsRequestSeq + 1;
      conversationsRequestSeq = requestSeq;
      conversationsError.value = "";
      if (!endpointRef) {
        conversationItems.value = [];
        conversationsLoading.value = false;
        return;
      }
      conversationsLoading.value = true;
      try {
        const data = await runtimeApiFetchForEndpoint(endpointRef, "/contacts/chat-profile", {
          perfSource: "markdown-editor-toolbar",
        });
        if (requestSeq !== conversationsRequestSeq) {
          return;
        }
        conversationItems.value = conversationItemsForProfiles(data?.items);
      } catch (error) {
        if (requestSeq !== conversationsRequestSeq) {
          return;
        }
        conversationItems.value = [];
        conversationsError.value = error?.message || "failed to load conversations";
      } finally {
        if (requestSeq === conversationsRequestSeq) {
          conversationsLoading.value = false;
        }
      }
    }

    function insertReference(item) {
      const reference = trimText(item?.value);
      if (!reference) {
        return;
      }
      editorRef.value?.insertAtCursor(reference, { spacing: true });
    }

    function endpointChanged() {
      clearContactAvatars();
      loadContacts();
      conversationsRequestSeq += 1;
      conversationItems.value = [];
      conversationsError.value = "";
      conversationsLoading.value = false;
    }

    watch(() => endpointState.selectedRef, endpointChanged, { immediate: true });
    onBeforeUnmount(clearContactAvatars);

    return {
      contactsError,
      contactsLoading,
      conversationItems,
      conversationsError,
      editorRef,
      insertReference,
      loadContactAvatars,
      loadConversations,
      mentionItems,
      t,
    };
  },
  template: `
    <MarkdownEditor
      ref="editorRef"
      :modelValue="modelValue"
      :placeholder="placeholder"
      :hint="hint"
      :height="height"
      :disabled="disabled"
      :readOnly="readOnly"
      :ariaLabel="ariaLabel"
      @update:modelValue="$emit('update:modelValue', $event)"
    >
      <template #toolbar>
        <QDropdownMenu
          class="markdown-editor-toolbar-picker markdown-editor-mention-picker"
          :items="mentionItems"
          :placeholder="t('markdown_editor_mention_placeholder')"
          :useFilter="true"
          useDialog="always"
          hideSelected
          hideActionLabel
          variant="plain"
          :title="t('markdown_editor_mention')"
          :aria-label="t('markdown_editor_mention')"
          :emptyHit="contactsError ? t('markdown_editor_mention_load_failed') : t('markdown_editor_mention_empty')"
          :disabled="disabled || readOnly"
          :loading="contactsLoading"
          @click.capture="loadContactAvatars"
          @change="insertReference"
        >
          <span class="markdown-editor-toolbar-glyph" aria-hidden="true">
              <PhUserCircle class="markdown-editor-toolbar-icon" />
          </span>
        </QDropdownMenu>
        <QDropdownMenu
          class="markdown-editor-toolbar-picker markdown-editor-conversation-picker"
          :items="conversationItems"
          :placeholder="t('markdown_editor_conversation_placeholder')"
          :useFilter="true"
          useDialog="always"
          hideSelected
          hideActionLabel
          variant="plain"
          :title="t('markdown_editor_conversation')"
          :aria-label="t('markdown_editor_conversation')"
          :emptyHit="conversationsError ? t('markdown_editor_conversation_load_failed') : t('markdown_editor_conversation_empty')"
          :disabled="disabled || readOnly"
          @click.capture="loadConversations"
          @keydown.down.capture="loadConversations"
          @keydown.enter.capture="loadConversations"
          @change="insertReference"
        >
          <span class="markdown-editor-toolbar-glyph" aria-hidden="true">
            <PhChat class="markdown-editor-toolbar-icon" />
          </span>
        </QDropdownMenu>
        <slot name="toolbar"></slot>
      </template>
    </MarkdownEditor>
  `,
};

export default AppMarkdownEditor;
