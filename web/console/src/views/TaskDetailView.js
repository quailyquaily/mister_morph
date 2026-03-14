import { onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import AppPage from "../components/AppPage";
import { endpointState, runtimeApiFetch, translate } from "../core/context";

const TaskDetailView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const route = useRoute();
    const loading = ref(false);
    const err = ref("");
    const detailJSON = ref("");

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const id = route.params.id || "";
        const data = await runtimeApiFetch(`/tasks/${encodeURIComponent(id)}`);
        detailJSON.value = JSON.stringify(data, null, 2);
      } catch (e) {
        detailJSON.value = "";
        err.value = e.message || t("msg_load_failed");
      } finally {
        loading.value = false;
      }
    }

    function back() {
      router.push("/tasks");
    }

    onMounted(load);
    watch(
      () => [route.params.id, endpointState.selectedRef],
      () => {
        void load();
      }
    );
    return { t, loading, err, detailJSON, load, back };
  },
  template: `
    <AppPage :title="t('task_detail_title')">
      <template #actions>
        <QButton class="outlined xs" @click="back">{{ t("action_back") }}</QButton>
        <QButton class="plain xs" :loading="loading" @click="load">{{ t("action_refresh") }}</QButton>
      </template>
      <QProgress v-if="loading" :infinite="true" />
      <QFence v-if="err" type="danger" icon="QIconCloseCircle" :text="err" />
      <QTextarea :modelValue="detailJSON" :rows="20" :disabled="true" />
    </AppPage>
  `,
};


export default TaskDetailView;
