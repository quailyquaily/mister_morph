import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import "./RepairView.css";

import RawTextEditorDialog from "../components/RawTextEditorDialog";
import { apiFetch, translate } from "../core/context";
import {
  fetchConsoleSetupIntegrity,
  invalidateConsoleSetupReadiness,
  setupStagePath,
} from "../core/setup";

const RepairView = {
  components: {
    RawTextEditorDialog,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const items = ref([]);
    const editorOpen = ref(false);
    const editorItem = ref(null);
    const editorValue = ref("");

    const editorTitle = computed(() => {
      const name = String(editorItem.value?.name || "").trim();
      return name ? `${t("repair_editor_title")} ${name}` : t("repair_editor_title");
    });

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const nextItems = await fetchConsoleSetupIntegrity({ force: true });
        items.value = nextItems;
        if (nextItems.length === 0) {
          await router.replace("/setup");
        }
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function openEditor(item) {
      const key = String(item?.key || "").trim();
      if (!key) {
        return;
      }
      loading.value = true;
      err.value = "";
      try {
        const payload = await apiFetch(`/setup/file?key=${encodeURIComponent(key)}`);
        editorItem.value = {
          key,
          name: typeof payload?.name === "string" ? payload.name : item.name,
          path: typeof payload?.path === "string" ? payload.path : item.path,
        };
        editorValue.value = typeof payload?.content === "string" ? payload.content : "";
        editorOpen.value = true;
      } catch (e) {
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    async function saveEditor() {
      const key = String(editorItem.value?.key || "").trim();
      if (!key) {
        return;
      }
      saving.value = true;
      err.value = "";
      try {
        await apiFetch(`/setup/file?key=${encodeURIComponent(key)}`, {
          method: "PUT",
          body: {
            content: editorValue.value,
          },
        });
        invalidateConsoleSetupReadiness();
        editorOpen.value = false;
        await load();
      } catch (e) {
        err.value = e.message || t("msg_save_failed");
      } finally {
        saving.value = false;
      }
    }

    function closeEditor() {
      if (saving.value) {
        return;
      }
      editorOpen.value = false;
    }

    function goToSetup(item) {
      const stage = setupStagePath(item?.stage);
      const key = String(item?.key || "").trim();
      if (!stage || !key) {
        return;
      }
      void router.push({ path: stage, query: { repair: key } });
    }

    onMounted(() => {
      void load();
    });

    return {
      t,
      loading,
      saving,
      err,
      items,
      editorOpen,
      editorItem,
      editorValue,
      editorTitle,
      openEditor,
      saveEditor,
      closeEditor,
      goToSetup,
    };
  },
  template: `
    <section class="repair-screen">
      <section class="repair-shell stat-item">
        <header class="repair-head">
          <h1 class="repair-title">{{ t("repair_title") }}</h1>
          <p class="repair-copy">{{ t("repair_intro") }}</p>
        </header>

        <QProgress v-if="loading" :infinite="true" />
        <QFence v-if="err" class="repair-error" type="danger" icon="QIconCloseCircle" :text="err" />

        <section v-if="!loading && items.length > 0" class="repair-list">
          <QCard v-for="item in items" :key="item.key" class="repair-item" variant="default">
            <template #header>
              <div class="repair-item-head">
                <div class="repair-item-copy">
                  <strong class="repair-item-title">{{ item.name }}</strong>
                  <code class="repair-item-path">{{ item.path }}</code>
                </div>
              </div>
            </template>
            <p class="repair-item-problem">{{ item.error }}</p>
            <div class="repair-item-actions">
              <QButton class="outlined sm" @click="openEditor(item)">{{ t("repair_action_edit_source") }}</QButton>
              <QButton class="primary sm" @click="goToSetup(item)">{{ t("repair_action_use_setup") }}</QButton>
            </div>
          </QCard>
        </section>

        <p v-if="!loading && !err && items.length === 0" class="repair-empty">{{ t("repair_empty") }}</p>
      </section>

      <RawTextEditorDialog
        :open="editorOpen"
        :title="editorTitle"
        :path="editorItem?.path || ''"
        :modelValue="editorValue"
        :loading="loading"
        :saving="saving"
        @update:modelValue="editorValue = $event"
        @close="closeEditor"
        @save="saveEditor"
      />
    </section>
  `,
};

export default RepairView;
