import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import "./SettingsCreditsView.css";

import AppPage from "../components/AppPage";
import { apiFetch, translate } from "../core/context";
import { openExternalURL as openExternal } from "../core/external-links";

function formatCount(value) {
  const n = Number.isFinite(Number(value)) ? Math.max(0, Math.trunc(Number(value))) : 0;
  return String(n).padStart(2, "0");
}

function sortedByName(items) {
  return [...items].sort((a, b) =>
    String(a?.name || "").localeCompare(String(b?.name || ""), undefined, { sensitivity: "base" })
  );
}

function contributorAvatarURL(item, brokenContributorAvatars) {
  const link = typeof item?.link === "string" ? item.link.trim() : "";
  if (!link || brokenContributorAvatars[item?.id]) {
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

const SettingsCreditsView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const loading = ref(false);
    const err = ref("");
    const openSource = ref([]);
    const contributors = ref([]);
    const brokenContributorAvatars = ref({});

    const introText = computed(() => t("settings_credits_intro"));
    const summaryRows = computed(() => [
      {
        id: "contributors",
        label: t("settings_credits_contributors_title"),
        value: formatCount(contributors.value.length),
      },
      {
        id: "open_source",
        label: t("settings_credits_open_source_title"),
        value: formatCount(openSource.value.length),
      },
    ]);

    async function loadCredits() {
      loading.value = true;
      err.value = "";
      try {
        const data = await apiFetch("/settings/credits");
        openSource.value = sortedByName(Array.isArray(data?.open_source) ? data.open_source : []);
        contributors.value = Array.isArray(data?.contributors) ? data.contributors : [];
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function goBack() {
      router.push("/settings");
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

    function recordClass(kind, index) {
      const classes = ["settings-credits-record", `settings-credits-record--${kind}`];
      if (index % 2 === 1) {
        classes.push("is-alt");
      }
      return classes.join(" ");
    }

    onMounted(() => {
      void loadCredits();
    });

    return {
      t,
      loading,
      err,
      openSource,
      contributors,
      introText,
      summaryRows,
      goBack,
      openExternal,
      contributorAvatar,
      contributorInitials,
      markContributorAvatarBroken,
      recordClass,
      formatCount,
    };
  },
  template: `
    <AppPage :title="t('settings_credits_title')" class="settings-credits-page">
      <template #leading>
        <div class="settings-credits-bar">
          <QButton
            class="outlined xs icon settings-credits-back"
            :title="t('settings_title')"
            :aria-label="t('settings_title')"
            @click="goBack"
          >
            <QIconArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ t("settings_credits_title") }}</h2>
        </div>
      </template>

      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />

      <section class="settings-credits-layout">
        <section class="settings-credits-hero">
          <div class="settings-credits-hero-shell">
            <div class="settings-credits-hero-copy">
              <h3 class="settings-credits-hero-title">{{ t("settings_credits_heading") }}</h3>
              <p class="settings-credits-hero-lead">{{ introText }}</p>
            </div>

            <div class="settings-credits-ledger" aria-label="credits summary">
              <div v-for="row in summaryRows" :key="row.id" class="settings-credits-ledger-row">
                <span class="settings-credits-ledger-label">{{ row.label }}</span>
                <strong class="settings-credits-ledger-value">{{ row.value }}</strong>
              </div>
            </div>
          </div>
        </section>

        <section class="settings-credits-rail settings-credits-rail--contributors">
          <header class="settings-credits-rail-head">
            <p class="settings-credits-kicker">
              <span class="settings-credits-kicker-bracket">[</span>
              Credits
              <span class="settings-credits-kicker-sep">//</span>
              Contributors
              <span class="settings-credits-kicker-bracket">]</span>
            </p>
            <h3 class="settings-credits-rail-title">{{ t("settings_credits_contributors_title") }}</h3>
            <p class="settings-credits-rail-meta">{{ t("settings_credits_contributors_meta") }}</p>
          </header>

          <div class="settings-credits-rail-body">
            <div v-if="contributors.length" class="settings-credits-list settings-credits-list--contributors">
              <a
                v-for="item in contributors"
                :key="item.id"
                class="settings-credits-contributor-card"
                :href="item.link"
                target="_blank"
                rel="noreferrer"
                :title="item.name"
                :aria-label="item.name"
              >
                <div class="settings-credits-contributor-avatar-shell">
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
                </div>
                <h4 class="settings-credits-contributor-name">{{ item.name }}</h4>
              </a>
            </div>

            <div v-else class="settings-credits-empty-block">
              <p class="settings-credits-empty">{{ t("settings_credits_empty_contributors") }}</p>
            </div>
          </div>
        </section>

        <section class="settings-credits-rail settings-credits-rail--open-source">
          <header class="settings-credits-rail-head">
            <p class="settings-credits-kicker">
              <span class="settings-credits-kicker-bracket">[</span>
              Credits
              <span class="settings-credits-kicker-sep">//</span>
              Open Source
              <span class="settings-credits-kicker-bracket">]</span>
            </p>
            <h3 class="settings-credits-rail-title">{{ t("settings_credits_open_source_title") }}</h3>
            <p class="settings-credits-rail-meta">{{ t("settings_credits_open_source_meta") }}</p>
          </header>

          <div class="settings-credits-rail-body">
            <div v-if="openSource.length" class="settings-credits-list">
              <article
                v-for="(item, index) in openSource"
                :key="item.id"
                :class="recordClass('open-source', index)"
              >
                <div class="settings-credits-record-main">
                  <div class="settings-credits-record-head">
                    <h4 class="settings-credits-record-title">{{ item.name }}</h4>
                    <span v-if="item.license" class="settings-credits-record-tag">{{ item.license }}</span>
                  </div>
                  <p class="settings-credits-record-summary">{{ item.summary }}</p>
                </div>

                <div class="settings-credits-record-actions">
                  <QButton
                    class="plain xs icon settings-credits-record-link"
                    :title="t('settings_credits_open_link')"
                    :aria-label="t('settings_credits_open_link')"
                    @click="openExternal(item.link)"
                  >
                    <QIconLinkExternal class="icon settings-credits-link-icon" />
                  </QButton>
                </div>
              </article>
            </div>

            <div v-else class="settings-credits-empty-block">
              <p class="settings-credits-empty">{{ t("settings_credits_empty_open_source") }}</p>
            </div>
          </div>
        </section>
      </section>
    </AppPage>
  `,
};

export default SettingsCreditsView;
