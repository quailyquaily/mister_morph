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
    const originalContentByName = ref({});
    const draftContentByName = ref({});
    const missingByName = ref({});

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
    const selectedOriginalContent = computed(() => String(originalContentByName.value[selectedFileName.value] ?? ""));
    const selectedMissing = computed(() => Boolean(missingByName.value[selectedFileName.value]));
    const isDirty = computed(() => content.value !== selectedOriginalContent.value);
    const canSave = computed(() => {
      if (!selectedFileName.value || loading.value || saving.value) {
        return false;
      }
      return selectedMissing.value || isDirty.value;
    });
    const editorMeta = computed(() =>
      t("files_editor_meta", {
        lines: lineCount(content.value),
        chars: content.value.length,
      })
    );
    const editorHint = computed(() => (selectedMissing.value ? t("files_editor_hint_new") : t("files_editor_hint")));
    const statusBadge = computed(() => {
      if (selectedMissing.value && !isDirty.value) {
        return {
          label: t("files_status_new"),
          type: "default",
        };
      }
      if (isDirty.value) {
        return {
          label: t("files_status_dirty"),
          type: "warning",
        };
      }
      return {
        label: t("files_status_saved"),
        type: "success",
      };
    });

    function setDraft(name, value) {
      if (!name) {
        return;
      }
      draftContentByName.value = {
        ...draftContentByName.value,
        [name]: String(value || ""),
      };
    }

    function setOriginalContent(name, value) {
      if (!name) {
        return;
      }
      originalContentByName.value = {
        ...originalContentByName.value,
        [name]: String(value || ""),
      };
    }

    function setMissing(name, missing) {
      if (!name) {
        return;
      }
      missingByName.value = {
        ...missingByName.value,
        [name]: missing === true,
      };
    }

    function hasDirtyDraft(name) {
      const key = String(name || "").trim();
      if (!key) {
        return false;
      }
      if (!(key in draftContentByName.value)) {
        return false;
      }
      return String(draftContentByName.value[key] || "") !== String(originalContentByName.value[key] ?? "");
    }

    function fileNote(item) {
      if (!item?.name) {
        return "";
      }
      if (hasDirtyDraft(item.name)) {
        return t("files_status_dirty");
      }
      if (missingByName.value[item.name]) {
        return t("files_status_new");
      }
      return "";
    }

    function fileClass(item) {
      const classes = ["files-index-item"];
      if (String(item?.name || "") === selectedFileName.value) {
        classes.push("is-active");
      }
      if (hasDirtyDraft(item?.name)) {
        classes.push("is-dirty");
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
        setOriginalContent(fileName, nextContent);
        setMissing(fileName, false);
        content.value = fileName in draftContentByName.value ? String(draftContentByName.value[fileName] || "") : nextContent;
      } catch (e) {
        if (e && e.status === 404) {
          setOriginalContent(fileName, "");
          setMissing(fileName, true);
          content.value = fileName in draftContentByName.value ? String(draftContentByName.value[fileName] || "") : "";
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
        setOriginalContent(fileName, content.value);
        setDraft(fileName, content.value);
        setMissing(fileName, false);
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
      setDraft(selectedFileName.value, nextValue);
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
      editorHint,
      statusBadge,
      canSave,
      fileNote,
      fileClass,
      onContentChange,
      onFileChange,
      save,
    };
  },
  template: `
    <AppPage :title="t('files_title')">
      <div class="files-workbench">
        <aside class="files-index" :aria-label="t('files_nav_title')">
          <div class="files-index-head">
            <p class="ui-kicker">{{ t("files_nav_title") }}</p>
            <p class="files-index-meta">{{ indexMeta }}</p>
          </div>
          <section v-for="group in groupedFileItems" :key="group.key" class="files-index-group">
            <h3 class="files-index-group-title">{{ group.title }}</h3>
            <div class="files-index-items">
              <button
                v-for="item in group.items"
                :key="item.key"
                type="button"
                :class="fileClass(item)"
                @click="onFileChange(item)"
              >
                <span class="files-index-item-name">{{ item.name }}</span>
                <span v-if="fileNote(item)" class="files-index-item-note">{{ fileNote(item) }}</span>
              </button>
            </div>
          </section>
        </aside>

        <QCard class="files-editor-card" variant="default">
          <div class="files-editor-shell">
            <header class="files-editor-head">
              <div class="files-editor-copy">
                <p class="ui-kicker">{{ selectedGroupTitle }}</p>
                <h3 class="files-editor-title">{{ selectedFileName || t("files_title") }}</h3>
                <p class="files-editor-meta">{{ editorMeta }}</p>
              </div>
              <div class="files-editor-actions">
                <QBadge :type="statusBadge.type" size="sm">{{ statusBadge.label }}</QBadge>
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
              :hint="editorHint"
              height="clamp(420px, 72vh, 820px)"
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
