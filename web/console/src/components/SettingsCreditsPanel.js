import { onMounted, ref } from "vue";
import "./SettingsCreditsPanel.css";

import { apiFetch, translate } from "../core/context";
import { openExternalURL as openExternal } from "../core/external-links";

function sortCreditsByName(items) {
  return [...items].sort((a, b) =>
    String(a?.name || "").localeCompare(String(b?.name || ""), undefined, { sensitivity: "base" })
  );
}

function contributorAvatarURL(item, brokenAvatars) {
  const link = typeof item?.link === "string" ? item.link.trim() : "";
  if (!link || brokenAvatars[item?.id]) {
    return "";
  }
  try {
    const url = new URL(link);
    if (url.hostname !== "github.com" && url.hostname !== "www.github.com") {
      return "";
    }
    const handle = url.pathname.split("/").filter(Boolean)[0] || "";
    return handle ? `https://github.com/${handle}.png?size=160` : "";
  } catch {
    return "";
  }
}

function contributorInitials(name) {
  const parts = String(name || "").trim().split(/\s+/).filter(Boolean);
  if (!parts.length) {
    return "?";
  }
  if (parts.length === 1) {
    return Array.from(parts[0]).slice(0, 2).join("").toUpperCase();
  }
  return parts.slice(0, 2).map((part) => Array.from(part)[0] || "").join("").toUpperCase();
}

const SettingsCreditsPanel = {
  setup() {
    const t = translate;
    const loading = ref(false);
    const error = ref("");
    const openSource = ref([]);
    const contributors = ref([]);
    const brokenContributorAvatars = ref({});

    async function loadCredits() {
      loading.value = true;
      error.value = "";
      try {
        const data = await apiFetch("/settings/credits");
        openSource.value = sortCreditsByName(Array.isArray(data?.open_source) ? data.open_source : []);
        contributors.value = Array.isArray(data?.contributors) ? data.contributors : [];
      } catch (err) {
        error.value = err?.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function contributorAvatar(item) {
      return contributorAvatarURL(item, brokenContributorAvatars.value);
    }

    function markContributorAvatarBroken(id) {
      if (!id || brokenContributorAvatars.value[id]) {
        return;
      }
      brokenContributorAvatars.value = {
        ...brokenContributorAvatars.value,
        [id]: true,
      };
    }

    onMounted(() => {
      void loadCredits();
    });

    return {
      t,
      loading,
      error,
      openSource,
      contributors,
      openExternal,
      contributorAvatar,
      contributorInitials,
      markContributorAvatarBroken,
    };
  },
  template: `
    <div class="settings-panel-body settings-panel-body-plain settings-credits-panel">
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="error" type="danger" icon="PhXCircle" :text="error" />

      <QCard variant="default">
        <div class="settings-panel-shell">
          <header class="settings-panel-head">
            <div class="settings-panel-copy">
              <h3 class="settings-panel-title workspace-document-title">{{ t("settings_credits_contributors_title") }}</h3>
              <p class="settings-panel-meta">{{ t("settings_credits_contributors_meta") }}</p>
            </div>
          </header>

          <div class="settings-panel-body">
            <div v-if="contributors.length" class="settings-credits-contributor-grid">
              <a
                v-for="item in contributors"
                :key="item.id"
                class="settings-credits-contributor-card"
                :href="item.link"
                target="_blank"
                rel="noopener noreferrer"
                :aria-label="t('settings_credits_open_profile') + ': ' + item.name"
              >
                <span class="settings-credits-contributor-avatar-shell">
                  <img
                    v-if="contributorAvatar(item)"
                    class="settings-credits-contributor-avatar"
                    :src="contributorAvatar(item)"
                    :alt="item.name"
                    loading="lazy"
                    decoding="async"
                    @error="markContributorAvatarBroken(item.id)"
                  />
                  <span v-else class="settings-credits-contributor-avatar-fallback">
                    {{ contributorInitials(item.name) }}
                  </span>
                </span>
                <strong class="settings-credits-contributor-name">{{ item.name }}</strong>
                <PhArrowSquareOut class="settings-credits-external-icon icon" aria-hidden="true" />
              </a>
            </div>
            <p v-else class="settings-credits-empty">{{ t("settings_credits_empty_contributors") }}</p>
          </div>
        </div>
      </QCard>

      <QCard variant="default">
        <div class="settings-panel-shell">
          <header class="settings-panel-head">
            <div class="settings-panel-copy">
              <h3 class="settings-panel-title workspace-document-title">{{ t("settings_credits_open_source_title") }}</h3>
              <p class="settings-panel-meta">{{ t("settings_credits_open_source_meta") }}</p>
            </div>
          </header>

          <div class="settings-panel-body">
            <div v-if="openSource.length" class="settings-credits-project-list">
              <article v-for="item in openSource" :key="item.id" class="settings-credits-project-row">
                <div class="settings-credits-project-copy">
                  <div class="settings-credits-project-head">
                    <strong class="settings-credits-project-title">{{ item.name }}</strong>
                    <span
                      v-if="item.license"
                      class="settings-credits-project-license"
                      :title="t('settings_credits_license_label')"
                    >
                      {{ item.license }}
                    </span>
                  </div>
                  <p class="settings-credits-project-summary">{{ item.summary }}</p>
                </div>
                <QButton
                  v-if="item.link"
                  class="plain xs icon settings-credits-project-link"
                  :title="t('settings_credits_open_link')"
                  :aria-label="t('settings_credits_open_link')"
                  @click="openExternal(item.link)"
                >
                  <PhArrowSquareOut class="settings-credits-external-icon icon" />
                </QButton>
              </article>
            </div>
            <p v-else class="settings-credits-empty">{{ t("settings_credits_empty_open_source") }}</p>
          </div>
        </div>
      </QCard>
    </div>
  `,
};

export default SettingsCreditsPanel;
