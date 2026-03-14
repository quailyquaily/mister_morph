import { onMounted, ref } from "vue";

import AppPage from "../components/AppPage";
import { runtimeApiFetch, translate } from "../core/context";

const DEFAULT_FILES = [
  { name: "TODO.md", group: "todo" },
  { name: "TODO.DONE.md", group: "todo" },
  { name: "IDENTITY.md", group: "persona" },
  { name: "SOUL.md", group: "persona" },
  { name: "HEARTBEAT.md", group: "heartbeat" },
];

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

function toFileItem(t, item) {
  const name = String(item?.name || "").trim();
  const group = normalizeGroup(item?.group);
  return {
    title: `${groupTitle(t, group)} / ${name}`,
    name,
    group,
  };
}

const StateFilesView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const ok = ref("");

    const fileItems = ref(DEFAULT_FILES.map((item) => toFileItem(t, item)));
    const selectedFile = ref(fileItems.value[0]);
    const content = ref("");

    async function loadFiles() {
      const data = await runtimeApiFetch("/state/files");
      const items = Array.isArray(data.items) ? data.items : [];
      if (items.length === 0) {
        return;
      }
      fileItems.value = items
        .map((item) => toFileItem(t, item))
        .filter((item) => item.name !== "")
        .filter((item) => item.group !== "contacts");
      if (fileItems.value.length === 0) {
        return;
      }
      if (!fileItems.value.find((item) => item.name === selectedFile.value?.name)) {
        selectedFile.value = fileItems.value[0];
      }
    }

    async function loadContent(name) {
      loading.value = true;
      err.value = "";
      ok.value = "";
      try {
        const data = await runtimeApiFetch(`/state/files/${encodeURIComponent(name)}`);
        content.value = data.content || "";
      } catch (e) {
        if (e && e.status === 404) {
          content.value = "";
          ok.value = t("msg_file_missing_create");
          return;
        }
        err.value = e.message || t("msg_read_failed");
      } finally {
        loading.value = false;
      }
    }

    async function save() {
      saving.value = true;
      err.value = "";
      ok.value = "";
      try {
        await runtimeApiFetch(`/state/files/${encodeURIComponent(selectedFile.value.name)}`, {
          method: "PUT",
          body: { content: content.value },
        });
        ok.value = t("msg_save_success");
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    async function onFileChange(item) {
      if (!item || typeof item !== "object" || !item.name) {
        return;
      }
      selectedFile.value = item;
      await loadContent(item.name);
    }

    async function init() {
      await loadFiles();
      await loadContent(selectedFile.value.name);
    }

    onMounted(init);
    return { t, loading, saving, err, ok, fileItems, selectedFile, content, onFileChange, save };
  },
  template: `
    <AppPage :title="t('files_title')">
      <div class="toolbar wrap">
        <div class="tool-item">
          <QDropdownMenu
            :items="fileItems"
            :initialItem="selectedFile"
            :placeholder="t('placeholder_select_file')"
            @change="onFileChange"
          />
        </div>
        <QButton class="primary" :loading="saving" @click="save">{{ t("action_save") }}</QButton>
      </div>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <QFence v-if="ok" type="success" icon="QIconCheckCircle" :text="ok" />
      <QTextarea v-model="content" :rows="22" />
    </AppPage>
  `,
};

export default StateFilesView;
