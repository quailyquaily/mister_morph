import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useToast } from "quail-ui";
import "./RepairView.css";

import RawTextEditorDialog from "../components/RawTextEditorDialog";
import AppDialogShell from "../components/AppDialogShell";
import { endpointApiFetch, translate } from "../core/context";
import {
  endpointRefFromRouteParam,
  endpointRoutePath,
} from "../core/endpoint-routes";
import {
  fetchConsoleSetupIntegrity,
  invalidateConsoleSetupReadiness,
  setupStagePath,
} from "../core/setup";

const RepairView = {
  components: {
	AppDialogShell,
    RawTextEditorDialog,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const route = useRoute();
    const router = useRouter();
    const setupEndpointRef = computed(() =>
      endpointRefFromRouteParam(route.params.endpoint_ref),
    );
    const loading = ref(false);
    const saving = ref(false);
    const err = ref("");
    const items = ref([]);
    const editorOpen = ref(false);
    const editorItem = ref(null);
    const editorValue = ref("");
	const secretValue = ref("");
	const secretSaving = ref(false);
	const replaceDialogOpen = ref(false);
	const replaceTarget = ref(null);
	const removeDialogOpen = ref(false);
	const removeTarget = ref(null);

	const issueTitleKeys = {
		file_unreadable: "repair_issue_file_unreadable",
		config_empty: "repair_issue_config_empty",
		config_invalid: "repair_issue_config_invalid",
		invalid_secret_ref: "repair_issue_invalid_secret_ref",
		os_secret_not_found: "repair_issue_os_secret_not_found",
		os_secret_store_unavailable: "repair_issue_os_secret_store_unavailable",
		identity_invalid: "repair_issue_identity_invalid",
		soul_invalid: "repair_issue_soul_invalid",
	};

    const editorTitle = computed(() => {
      const name = String(editorItem.value?.name || "").trim();
      return name ? `${t("repair_editor_title")} ${name}` : t("repair_editor_title");
    });

	const removeDialogText = computed(() =>
		t("repair_remove_secret_confirm", {
			field: fieldLabel(removeTarget.value),
		}),
	);
	const replaceDialogTitle = computed(() => t("repair_action_replace_secret"));
	const removeDialogActions = computed(() => [
		{
			name: "cancel",
			label: t("action_cancel"),
			class: "outlined",
			action: closeRemoveDialog,
		},
		{
			name: "remove",
			label: t("repair_action_remove_reference"),
			class: "danger",
			action: removeSecretReference,
		},
	]);

	function issueID(item) {
		return [item?.key, item?.code, ...(Array.isArray(item?.field_path) ? item.field_path : [])]
			.map((part) => String(part || "").trim())
			.filter(Boolean)
			.join(":");
	}

	function issueTitle(item) {
		return t(issueTitleKeys[item?.code] || "repair_issue_unknown");
	}

	function fieldLabel(item) {
		return Array.isArray(item?.field_path) ? item.field_path.join(".") : "";
	}

	function hasRepairableSecretRef(item) {
		return item?.code === "os_secret_not_found" && fieldLabel(item) !== "";
	}

	function canRetry(item) {
		return item?.code === "file_unreadable" || item?.code === "os_secret_store_unavailable";
	}

	function canEditSource(item) {
		return item?.code !== "file_unreadable";
	}

	function canUseSetup(item) {
		return ["file_unreadable", "config_empty", "config_invalid", "identity_invalid", "soul_invalid"].includes(
			item?.code,
		);
	}

	function openReplaceDialog(item) {
		replaceTarget.value = item;
		secretValue.value = "";
		replaceDialogOpen.value = true;
	}

	function closeReplaceDialog() {
		if (secretSaving.value) {
			return;
		}
		replaceDialogOpen.value = false;
		replaceTarget.value = null;
		secretValue.value = "";
	}

	async function replaceSecret() {
		const fieldPath = Array.isArray(replaceTarget.value?.field_path)
			? replaceTarget.value.field_path
			: [];
		if (fieldPath.length === 0 || secretValue.value === "") {
			return;
		}
		secretSaving.value = true;
		try {
			await endpointApiFetch(setupEndpointRef.value, "/setup/secret", {
				method: "PUT",
				body: {
					field_path: fieldPath,
					value: secretValue.value,
				},
			});
			replaceDialogOpen.value = false;
			replaceTarget.value = null;
			secretValue.value = "";
			invalidateConsoleSetupReadiness();
			await load();
		} catch (e) {
			toast.error(e.message || t("msg_save_failed"));
		} finally {
			secretSaving.value = false;
		}
	}

	function openRemoveDialog(item) {
		removeTarget.value = item;
		removeDialogOpen.value = true;
	}

	function closeRemoveDialog() {
		removeDialogOpen.value = false;
		removeTarget.value = null;
	}

	async function removeSecretReference() {
		const fieldPath = Array.isArray(removeTarget.value?.field_path)
			? removeTarget.value.field_path
			: [];
		if (fieldPath.length === 0) {
			return;
		}
		secretSaving.value = true;
		try {
			await endpointApiFetch(setupEndpointRef.value, "/setup/secret", {
				method: "DELETE",
				body: { field_path: fieldPath },
			});
			closeRemoveDialog();
			invalidateConsoleSetupReadiness();
			await load();
		} catch (e) {
			toast.error(e.message || t("msg_save_failed"));
		} finally {
			secretSaving.value = false;
		}
	}

    async function load() {
      loading.value = true;
      err.value = "";
      try {
        const nextItems = await fetchConsoleSetupIntegrity({
          force: true,
          endpointRef: setupEndpointRef.value,
        });
        items.value = nextItems;
        if (nextItems.length === 0) {
          await router.replace(endpointRoutePath(setupEndpointRef.value, "/setup"));
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
        const payload = await endpointApiFetch(
          setupEndpointRef.value,
          `/setup/file?key=${encodeURIComponent(key)}`,
        );
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
        await endpointApiFetch(setupEndpointRef.value, `/setup/file?key=${encodeURIComponent(key)}`, {
          method: "PUT",
          body: {
            content: editorValue.value,
          },
        });
        invalidateConsoleSetupReadiness();
        editorOpen.value = false;
        await load();
      } catch (e) {
        toast.error(e.message || t("msg_save_failed"));
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
      const stage = setupStagePath(item?.stage, setupEndpointRef.value);
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
		secretValue,
		secretSaving,
		replaceDialogOpen,
		replaceTarget,
		replaceDialogTitle,
		removeDialogOpen,
		removeDialogText,
		removeDialogActions,
		issueID,
		issueTitle,
		fieldLabel,
		hasRepairableSecretRef,
		canRetry,
		canEditSource,
		canUseSetup,
		openReplaceDialog,
		closeReplaceDialog,
		replaceSecret,
		openRemoveDialog,
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
		  <p class="repair-kicker">{{ t("repair_kicker") }}</p>
		  <h1 class="repair-title">{{ t("repair_title") }}</h1>
		  <p class="repair-copy">{{ t("repair_intro") }}</p>
        </header>

        <QProgress v-if="loading" :infinite="true" />
        <QFence v-if="err" class="repair-error" type="danger" icon="PhXCircle" :text="err" />

        <section v-if="!loading && items.length > 0" class="repair-list">
		  <QCard v-for="item in items" :key="issueID(item)" class="repair-item" variant="default">
            <template #header>
              <div class="repair-item-head">
                <div class="repair-item-copy">
				  <strong class="repair-item-title">{{ issueTitle(item) }}</strong>
				  <span class="repair-item-resource">{{ item.name }}</span>
				  <code v-if="fieldLabel(item)" class="repair-item-field">{{ fieldLabel(item) }}</code>
				  <code class="repair-item-path">{{ item.path }}</code>
                </div>
              </div>
            </template>
			<p class="repair-item-problem">{{ item.error }}</p>
            <div class="repair-item-actions">
			  <QButton v-if="canRetry(item)" class="outlined sm" @click="load">{{ t("repair_action_retry") }}</QButton>
			  <QButton
				v-if="hasRepairableSecretRef(item)"
				class="primary sm"
				@click="openReplaceDialog(item)"
			  >
				{{ t("repair_action_replace_secret") }}
			  </QButton>
			  <QButton v-if="hasRepairableSecretRef(item)" class="danger sm" @click="openRemoveDialog(item)">
				{{ t("repair_action_remove_reference") }}
			  </QButton>
			  <QButton v-if="canEditSource(item)" class="outlined sm" @click="openEditor(item)">{{ t("repair_action_edit_source") }}</QButton>
			  <QButton v-if="canUseSetup(item)" class="primary sm" @click="goToSetup(item)">{{ t("repair_action_use_setup") }}</QButton>
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
	  <AppDialogShell
		:modelValue="replaceDialogOpen"
		:title="replaceDialogTitle"
		width="520px"
		:closeDisabled="secretSaving"
		@update:modelValue="replaceDialogOpen = $event"
		@close="closeReplaceDialog"
	  >
		<form class="repair-secret-dialog" @submit.prevent="replaceSecret">
		  <div class="repair-secret-dialog-copy">
			<span class="repair-secret-dialog-label">{{ t("repair_secret_field_label") }}</span>
			<code>{{ fieldLabel(replaceTarget) }}</code>
		  </div>
		  <QInput
			:modelValue="secretValue"
			inputType="password"
			:placeholder="t('repair_secret_placeholder')"
			:disabled="secretSaving"
			@update:modelValue="secretValue = $event"
		  />
		  <footer class="repair-secret-dialog-actions">
			<QButton class="outlined" type="button" :disabled="secretSaving" @click="closeReplaceDialog">
			  {{ t("action_cancel") }}
			</QButton>
			<QButton class="primary" type="submit" :disabled="secretSaving || secretValue === ''">
			  {{ t("action_save") }}
			</QButton>
		  </footer>
		</form>
	  </AppDialogShell>
	  <QMessageDialog
		v-model="removeDialogOpen"
		icon="PhTrash"
		iconColor="red"
		:title="t('repair_remove_secret_title')"
		:text="removeDialogText"
		:actions="removeDialogActions"
	  />
    </section>
  `,
};

export default RepairView;
