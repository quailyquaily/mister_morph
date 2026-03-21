import { computed, onMounted, ref } from "vue";
import "./StateFilesView.css";

import AppPage from "../components/AppPage";
import MarkdownEditor from "../components/MarkdownEditor";
import { runtimeApiFetch, translate } from "../core/context";

const DEFAULT_FILES = [
  { name: "TODO.md", group: "todo" },
  { name: "TODO.DONE.md", group: "todo" },
  { name: "IDENTITY.md", group: "persona" },
  { name: "SOUL.md", group: "persona" },
  { name: "HEARTBEAT.md", group: "heartbeat" },
];

const GROUP_ORDER = ["todo", "persona", "heartbeat", "other"];

function normalizeGroup(value) {
  return String(value || "").trim().toLowerCase();
}

function groupTitle(t, group) {
  switch (normalizeGroup(group)) {
    case "todo":
      return t("files_group_todo");
    case "contacts":
      return t("files_group_contacts");
    case "persona":
      return t("files_group_persona");
    case "heartbeat":
      return t("files_group_heartbeat");
    default:
      return t("files_group_other");
  }
}

function groupRank(group) {
  const index = GROUP_ORDER.indexOf(normalizeGroup(group));
  return index >= 0 ? index : GROUP_ORDER.length;
}

function compareFileItems(left, right) {
  const rankDiff = groupRank(left.group) - groupRank(right.group);
  if (rankDiff !== 0) {
    return rankDiff;
  }
  return left.name.localeCompare(right.name);
}

function toFileItem(t, item) {
  const name = String(item?.name || "").trim();
  const group = normalizeGroup(item?.group);
  return {
    key: `${group}:${name}`,
    name,
    group,
  };
}

function lineCount(value) {
  const text = String(value || "");
  if (!text) {
    return 0;
  }
  return text.split(/\r?\n/).length;
}

const StateFilesView = {
  components: {
    AppPage,
    MarkdownEditor,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const ok = ref("");

    const fileItems = ref(DEFAULT_FILES.map((item) => toFileItem(t, item)).sort(compareFileItems));
    const selectedFile = ref(fileItems.value[0] || null);
    const content = ref("");
    const missing = ref(false);
    const isDirty = ref(false);

    const selectedFileName = computed(() => String(selectedFile.value?.name || "").trim());
    const selectedGroupTitle = computed(() => {
      if (!selectedFileName.value) {
        return t("files_nav_title");
      }
      return groupTitle(t, selectedFile.value?.group);
    });
    const groupedFileItems = computed(() => {
      const groups = [];
      const buckets = new Map();
      for (const item of [...fileItems.value].sort(compareFileItems)) {
        if (!item?.name) {
          continue;
        }
        const key = normalizeGroup(item.group);
        if (!buckets.has(key)) {
          buckets.set(key, {
            key,
            title: groupTitle(t, key),
            items: [],
          });
          groups.push(buckets.get(key));
        }
        buckets.get(key).items.push(item);
      }
      return groups;
    });
    const indexMeta = computed(() => t("files_nav_meta", { count: fileItems.value.length }));
    const canSave = computed(() => {
      if (!selectedFileName.value || loading.value || saving.value) {
        return false;
      }
      return missing.value || isDirty.value;
    });
    const editorMeta = computed(() =>
      t("files_editor_meta", {
        lines: lineCount(content.value),
        chars: content.value.length,
      })
    );
    function isSelectedItem(item) {
      return String(item?.name || "") === selectedFileName.value;
    }

    function fileClass(item) {
      const classes = ["files-index-item", "workspace-sidebar-item"];
      if (isSelectedItem(item)) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    async function loadFiles() {
      const data = await runtimeApiFetch("/state/files");
      const items = Array.isArray(data.items) ? data.items : [];
      if (items.length === 0) {
        return;
      }
      fileItems.value = items
        .map((item) => toFileItem(t, item))
        .filter((item) => item.name !== "")
        .filter((item) => item.group !== "contacts")
        .sort(compareFileItems);
      if (fileItems.value.length === 0) {
        return;
      }
      if (!fileItems.value.find((item) => item.name === selectedFile.value?.name)) {
        selectedFile.value = fileItems.value[0];
      }
    }

    async function loadContent(name) {
      const fileName = String(name || "").trim();
      if (!fileName) {
        content.value = "";
        return;
      }
      loading.value = true;
      err.value = "";
      ok.value = "";
      try {
        const data = await runtimeApiFetch(`/state/files/${encodeURIComponent(fileName)}`);
        const nextContent = String(data.content || "");
        content.value = nextContent;
        missing.value = false;
        isDirty.value = false;
      } catch (e) {
        if (e && e.status === 404) {
          content.value = "";
          missing.value = true;
          isDirty.value = false;
          return;
        }
        err.value = e.message || t("msg_read_failed");
      } finally {
        loading.value = false;
      }
    }

    async function save() {
      const fileName = selectedFileName.value;
      if (!fileName) {
        return;
      }
      saving.value = true;
      err.value = "";
      ok.value = "";
      try {
        await runtimeApiFetch(`/state/files/${encodeURIComponent(fileName)}`, {
          method: "PUT",
          body: { content: content.value },
        });
        missing.value = false;
        isDirty.value = false;
        ok.value = t("msg_save_success");
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    function onContentChange(value) {
      const nextValue = String(value || "");
      content.value = nextValue;
      ok.value = "";
      isDirty.value = true;
    }

    async function onFileChange(item) {
      if (!item || typeof item !== "object" || !item.name) {
        return;
      }
      if (String(item.name) === selectedFileName.value) {
        return;
      }
      selectedFile.value = item;
      await loadContent(item.name);
    }

    async function init() {
      await loadFiles();
      await loadContent(selectedFile.value?.name);
    }

    onMounted(init);
    return {
      t,
      loading,
      saving,
      err,
      ok,
      fileItems,
      selectedFile,
      content,
      groupedFileItems,
      indexMeta,
      selectedFileName,
      selectedGroupTitle,
      editorMeta,
      canSave,
      isSelectedItem,
      fileClass,
      onContentChange,
      onFileChange,
      save,
    };
  },
  template: `
    <AppPage :title="t('files_title')" class="files-page" :hideDesktopBar="true">
      <div class="files-workbench">
        <aside class="files-index workspace-sidebar-section" :aria-label="t('files_nav_title')">
          <div class="files-index-head workspace-sidebar-head">
            <p class="ui-kicker">{{ t("files_title") }}</p>
            <h3 class="files-index-title workspace-section-title">{{ t("files_nav_title") }}</h3>
            <p class="files-index-meta">{{ indexMeta }}</p>
          </div>
          <section v-for="group in groupedFileItems" :key="group.key" class="files-index-group">
            <h3 class="files-index-group-title">{{ group.title }}</h3>
            <div class="files-index-items workspace-sidebar-list">
              <button
                v-for="item in group.items"
                :key="item.key"
                type="button"
                :class="fileClass(item)"
                @click="onFileChange(item)"
              >
                <span class="files-index-item-name workspace-sidebar-item-title">{{ item.name }}</span>
                <span class="files-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                  <QBadge v-if="isSelectedItem(item)" dot type="primary" size="sm" />
                </span>
              </button>
            </div>
          </section>
        </aside>

        <QCard class="files-editor-card" variant="default">
          <div class="files-editor-shell">
            <header class="files-editor-head">
              <div class="files-editor-copy">
                <p class="ui-kicker">{{ selectedGroupTitle }}</p>
                <h3 class="files-editor-title workspace-document-title">{{ selectedFileName || t("files_title") }}</h3>
                <p class="files-editor-meta">{{ editorMeta }}</p>
              </div>
              <div class="files-editor-actions">
                <QButton class="primary" :disabled="!canSave" :loading="saving" @click="save">
                  {{ t("action_save") }}
                </QButton>
              </div>
            </header>

            <div class="files-editor-notices">
              <QProgress v-if="loading" :infinite="true" />
              <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
              <QFence v-else-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
            </div>

            <MarkdownEditor
              :modelValue="content"
              height="100%"
              :disabled="loading"
              :placeholder="selectedFileName"
              :aria-label="selectedFileName || t('files_title')"
              @update:modelValue="onContentChange"
            />
          </div>
        </QCard>
      </div>
    </AppPage>
  `,
};

export default StateFilesView;
