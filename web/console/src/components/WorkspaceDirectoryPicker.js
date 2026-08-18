import { computed, nextTick, ref, watch } from "vue";

import { runtimeApiFetchForEndpoint, translate } from "../core/context";
import { workspaceTreeIcon } from "../core/workspace-icons";
import AppDialogShell from "./AppDialogShell";

const RECENT_STORAGE_KEY = "mistermorph_console_recent_workspaces_v1";
const RECENT_LIMIT = 32;
const SOURCE_RECENT = "recent";
const SOURCE_HOME = "home";
const SOURCE_SYSTEM = "system";
const SOURCE_STATE = "state_dir";
const SOURCE_CACHE = "cache_dir";

function text(value) {
  return String(value || "").trim();
}

function normalizeRecentPaths(raw) {
  if (!Array.isArray(raw)) {
    return [];
  }
  const seen = new Set();
  const paths = [];
  for (const value of raw) {
    const path = text(value);
    if (!path || seen.has(path)) {
      continue;
    }
    seen.add(path);
    paths.push(path);
    if (paths.length >= RECENT_LIMIT) {
      break;
    }
  }
  return paths;
}

function loadRecentPaths() {
  if (typeof localStorage === "undefined") {
    return [];
  }
  try {
    return normalizeRecentPaths(JSON.parse(localStorage.getItem(RECENT_STORAGE_KEY) || "[]"));
  } catch {
    return [];
  }
}

function saveRecentPath(paths, path) {
  const next = normalizeRecentPaths([text(path), ...(Array.isArray(paths) ? paths : [])]);
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify(next));
  }
  return next;
}

function normalizeItems(raw) {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .map((item) => ({
      name: text(item?.name),
      path: text(item?.path),
      is_dir: item?.is_dir === true,
      has_children: item?.has_children === true,
    }))
    .filter((item) => item.name && item.path);
}

function hasPath(itemsByPath, path) {
  return Object.prototype.hasOwnProperty.call(itemsByPath || {}, path);
}

function treeRows(itemsByPath, expandedByPath, parentPath, depth = 0) {
  const items = Array.isArray(itemsByPath?.[parentPath]) ? itemsByPath[parentPath] : [];
  const rows = [];
  for (const entry of items) {
    const loaded = hasPath(itemsByPath, entry.path);
    const hasLoadedChildren = loaded && itemsByPath[entry.path].length > 0;
    const expandable = entry.is_dir && (entry.has_children || hasLoadedChildren);
    const expanded = expandable && expandedByPath?.[entry.path] === true;
    rows.push({
      key: `${parentPath}:${entry.path}`,
      depth,
      entry,
      expandable,
      expanded,
      source: "tree",
    });
    if (expanded && loaded) {
      rows.push(...treeRows(itemsByPath, expandedByPath, entry.path, depth + 1));
    }
  }
  return rows;
}

function pathLabel(path) {
  const normalized = text(path).replace(/[\\/]+$/u, "");
  if (!normalized) {
    return text(path);
  }
  const parts = normalized.split(/[\\/]/u).filter(Boolean);
  return parts.at(-1) || text(path);
}

const WorkspaceDirectoryPicker = {
  name: "WorkspaceDirectoryPicker",
  components: { AppDialogShell },
  props: {
    modelValue: {
      type: Boolean,
      default: false,
    },
    endpointRef: {
      type: String,
      default: "",
    },
    initialPath: {
      type: String,
      default: "",
    },
  },
  emits: ["update:modelValue", "select"],
  setup(props, { emit }) {
    const t = translate;
    const sourceID = ref(SOURCE_HOME);
    const itemsByPath = ref({});
    const expandedByPath = ref({});
    const loading = ref(false);
    const error = ref("");
    const stateDir = ref("");
    const cacheDir = ref("");
    const selection = ref("");
    const showHidden = ref(false);
    const recentPaths = ref(loadRecentPaths());
    const createOpen = ref(false);
    const createName = ref("");
    const creating = ref(false);
    const createField = ref(null);
    let requestVersion = 0;

    const source = computed(() => {
      switch (sourceID.value) {
        case SOURCE_RECENT:
          return { kind: "recent", path: "" };
        case SOURCE_SYSTEM:
          return { kind: "system", path: "" };
        case SOURCE_STATE:
          return { kind: "place", path: text(stateDir.value) };
        case SOURCE_CACHE:
          return { kind: "place", path: text(cacheDir.value) };
        default:
          return { kind: "home", path: "~" };
      }
    });
    const placeSources = computed(() =>
      [
        { id: SOURCE_STATE, title: t("chat_workspace_dialog_state_dir"), path: stateDir.value },
        { id: SOURCE_CACHE, title: t("chat_workspace_dialog_cache_dir"), path: cacheDir.value },
      ].filter((item) => text(item.path))
    );
    const rows = computed(() => {
      if (source.value.kind === "recent") {
        return recentPaths.value.map((path) => ({
          key: `recent:${path}`,
          depth: 0,
          source: "recent",
          entry: {
            name: pathLabel(path),
            path,
            is_dir: true,
            has_children: false,
          },
          expandable: false,
          expanded: false,
        }));
      }
      return treeRows(itemsByPath.value, expandedByPath.value, source.value.path);
    });
    const createParent = computed(() => text(selection.value) || text(source.value.path));
    const createDisabled = computed(
      () => loading.value || creating.value || !text(props.endpointRef) || !createParent.value
    );
    const createSubmitDisabled = computed(
      () => createDisabled.value || !text(createName.value)
    );
    const confirmDisabled = computed(
      () => creating.value || !text(props.endpointRef) || !text(selection.value)
    );
    const emptyText = computed(() =>
      source.value.kind === "recent"
        ? t("chat_workspace_dialog_recent_empty")
        : t("chat_workspace_dialog_empty")
    );

    function resetTree() {
      requestVersion += 1;
      itemsByPath.value = {};
      expandedByPath.value = {};
      loading.value = false;
      error.value = "";
    }

    function sourceItemClass(value) {
      return [
        "workspace-sidebar-item",
        "chat-workspace-dialog-sidebar-item",
        sourceID.value === value ? "is-active" : "",
      ].filter(Boolean).join(" ");
    }

    function rowClass(row) {
      return [
        "chat-workspace-tree-entry",
        "is-actionable",
        "is-selectable",
        row?.entry?.is_dir ? "is-dir" : "",
        row?.source === "recent" ? "is-recent" : "",
        text(selection.value) === text(row?.entry?.path) ? "is-selected" : "",
      ].filter(Boolean).join(" ");
    }

    async function loadPath(rawPath) {
      const endpointRef = text(props.endpointRef);
      const path = text(rawPath);
      if (!endpointRef) {
        resetTree();
        return false;
      }
      const version = requestVersion + 1;
      requestVersion = version;
      loading.value = true;
      try {
        const query = new URLSearchParams();
        if (path) {
          query.set("path", path);
        }
        if (showHidden.value) {
          query.set("show_hidden", "true");
        }
        const payload = await runtimeApiFetchForEndpoint(
          endpointRef,
          query.toString() ? `/workspace/browse?${query.toString()}` : "/workspace/browse"
        );
        if (version !== requestVersion) {
          return false;
        }
        stateDir.value = text(payload?.state_dir);
        cacheDir.value = text(payload?.cache_dir);
        itemsByPath.value = {
          ...itemsByPath.value,
          [path]: normalizeItems(payload?.items),
        };
        if (path && path !== source.value.path) {
          expandedByPath.value = { ...expandedByPath.value, [path]: true };
        }
        error.value = "";
        return true;
      } catch (cause) {
        if (version === requestVersion) {
          error.value = cause?.message || t("msg_load_failed");
        }
        return false;
      } finally {
        if (version === requestVersion) {
          loading.value = false;
        }
      }
    }

    async function activateSource(value) {
      sourceID.value = text(value) || SOURCE_HOME;
      resetTree();
      if (source.value.kind === "recent") {
        return;
      }
      const ok = await loadPath(source.value.path);
      if (ok && source.value.kind === "place") {
        selection.value = source.value.path;
      }
    }

    async function toggleNode(row) {
      const path = text(row?.entry?.path);
      if (!row?.entry?.is_dir || !path || !row?.expandable) {
        return;
      }
      if (expandedByPath.value[path]) {
        const next = { ...expandedByPath.value };
        delete next[path];
        expandedByPath.value = next;
        return;
      }
      if (!hasPath(itemsByPath.value, path) && !(await loadPath(path))) {
        return;
      }
      expandedByPath.value = { ...expandedByPath.value, [path]: true };
    }

    async function selectRow(row) {
      if (!row?.entry?.is_dir) {
        return;
      }
      selection.value = text(row.entry.path);
      await toggleNode(row);
    }

    async function updateShowHidden(value) {
      showHidden.value = Boolean(value);
      if (source.value.kind === "recent") {
        return;
      }
      resetTree();
      await loadPath(source.value.path);
    }

    function openCreate() {
      if (createDisabled.value) {
        return;
      }
      createOpen.value = true;
      createName.value = "";
      void nextTick(() => createField.value?.querySelector("input")?.focus());
    }

    function cancelCreate() {
      if (creating.value) {
        return;
      }
      createOpen.value = false;
      createName.value = "";
    }

    async function createDirectory() {
      const endpointRef = text(props.endpointRef);
      const parentPath = createParent.value;
      const name = text(createName.value);
      if (!endpointRef || !parentPath || !name || creating.value) {
        return;
      }
      creating.value = true;
      error.value = "";
      try {
        const payload = await runtimeApiFetchForEndpoint(endpointRef, "/workspace/directory", {
          method: "POST",
          body: { parent_path: parentPath, name },
        });
        const createdPath = text(payload?.path);
        if (!createdPath) {
          throw new Error(t("msg_save_failed"));
        }
        if (source.value.kind !== "recent") {
          await loadPath(parentPath);
          expandedByPath.value = { ...expandedByPath.value, [parentPath]: true };
        }
        selection.value = createdPath;
        createOpen.value = false;
        createName.value = "";
      } catch (cause) {
        error.value = cause?.message || t("msg_save_failed");
      } finally {
        creating.value = false;
      }
    }

    function close() {
      if (!creating.value) {
        emit("update:modelValue", false);
      }
    }

    function confirm() {
      const path = text(selection.value);
      if (!path || confirmDisabled.value) {
        return;
      }
      recentPaths.value = saveRecentPath(recentPaths.value, path);
      emit("select", path);
      emit("update:modelValue", false);
    }

    async function open() {
      recentPaths.value = loadRecentPaths();
      selection.value = text(props.initialPath);
      showHidden.value = false;
      createOpen.value = false;
      createName.value = "";
      await activateSource(SOURCE_HOME);
      if (text(props.initialPath)) {
        selection.value = text(props.initialPath);
      }
    }

    watch(
      () => props.modelValue,
      (value) => {
        if (value) {
          void open();
        } else {
          resetTree();
        }
      },
      { immediate: true }
    );

    return {
      t,
      activateSource,
      cancelCreate,
      close,
      confirm,
      confirmDisabled,
      createDirectory,
      createDisabled,
      createField,
      createName,
      createOpen,
      createParent,
      createSubmitDisabled,
      creating,
      emptyText,
      error,
      loading,
      openCreate,
      placeSources,
      rowClass,
      rows,
      selectRow,
      selection,
      showHidden,
      sourceID,
      sourceItemClass,
      updateShowHidden,
      workspaceTreeIcon,
    };
  },
  template: `
    <AppDialogShell
      v-if="modelValue"
      :modelValue="modelValue"
      :title="t('chat_workspace_dialog_title')"
      width="720px"
      :closeDisabled="creating"
      @close="close"
    >
      <section class="chat-workspace-dialog">
        <QFence
          v-if="error"
          class="chat-workspace-pane-fence"
          type="danger"
          icon="QIconCloseCircle"
          :text="error"
        />
        <div class="chat-workspace-dialog-shell">
          <aside class="chat-workspace-dialog-sidebar workspace-sidebar-section">
            <section class="chat-workspace-dialog-sidebar-group">
              <p class="chat-workspace-dialog-sidebar-title ui-kicker">{{ t('chat_workspace_dialog_places') }}</p>
              <div class="chat-workspace-dialog-sidebar-list workspace-sidebar-list">
                <button type="button" :class="sourceItemClass('recent')" :disabled="creating" @click="activateSource('recent')">
                  <span class="workspace-sidebar-item-copy"><span class="workspace-sidebar-item-title">{{ t('chat_workspace_dialog_recent') }}</span></span>
                </button>
                <button type="button" :class="sourceItemClass('home')" :disabled="creating" @click="activateSource('home')">
                  <span class="workspace-sidebar-item-copy"><span class="workspace-sidebar-item-title">{{ t('chat_workspace_dialog_home') }}</span></span>
                </button>
                <button type="button" :class="sourceItemClass('system')" :disabled="creating" @click="activateSource('system')">
                  <span class="workspace-sidebar-item-copy"><span class="workspace-sidebar-item-title">{{ t('chat_workspace_dialog_system') }}</span></span>
                </button>
                <button
                  v-for="item in placeSources"
                  :key="item.id"
                  type="button"
                  :class="sourceItemClass(item.id)"
                  :title="item.path"
                  :disabled="creating"
                  @click="activateSource(item.id)"
                >
                  <span class="workspace-sidebar-item-copy"><span class="workspace-sidebar-item-title">{{ item.title }}</span></span>
                </button>
              </div>
            </section>
          </aside>

          <div class="chat-workspace-dialog-main">
            <div class="chat-workspace-browser-toolbar">
              <span class="chat-workspace-browser-parent">
                <span class="chat-workspace-browser-parent-label ui-kicker">{{ t('chat_workspace_dialog_create_in') }}</span>
                <code class="chat-workspace-browser-parent-path" :title="createParent">{{ createParent || t('chat_workspace_dialog_selection_empty') }}</code>
              </span>
              <QButton class="plain xs chat-workspace-browser-create-button" :disabled="createDisabled || createOpen" @click="openCreate">
                <QIconPlus class="icon" />
                <span>{{ t('chat_workspace_dialog_new_directory') }}</span>
              </QButton>
            </div>

            <div v-if="createOpen" class="chat-workspace-browser-create">
              <div ref="createField" class="chat-workspace-browser-create-field">
                <QInput
                  v-model="createName"
                  :placeholder="t('chat_workspace_dialog_directory_name')"
                  :aria-label="t('chat_workspace_dialog_directory_name')"
                  :disabled="creating"
                  @keydown.enter.prevent="createDirectory"
                />
              </div>
              <div class="chat-workspace-browser-create-actions">
                <QButton class="plain sm" :disabled="creating" @click="cancelCreate">{{ t('action_cancel') }}</QButton>
                <QButton class="primary sm" :loading="creating" :disabled="createSubmitDisabled" @click="createDirectory">
                  {{ t('chat_workspace_dialog_create_directory') }}
                </QButton>
              </div>
            </div>

            <div class="chat-workspace-browser-shell">
              <p v-if="loading && rows.length === 0" class="chat-workspace-tree-status">{{ t('chat_workspace_dialog_loading') }}</p>
              <div v-else-if="rows.length" class="chat-workspace-tree-list is-browser">
                <div
                  v-for="row in rows"
                  :key="row.key"
                  class="chat-workspace-tree-row"
                  :style="{ '--tree-depth': row.depth }"
                >
                  <button
                    type="button"
                    :class="rowClass(row)"
                    :disabled="!row.entry.is_dir || creating"
                    :title="row.entry.path"
                    @click="selectRow(row)"
                  >
                    <span class="chat-workspace-tree-kind" aria-hidden="true">
                      <img class="chat-workspace-tree-icon" :src="workspaceTreeIcon(row.entry, row.expanded)" alt="" />
                    </span>
                    <span v-if="row.source === 'recent'" class="chat-workspace-recent-item">
                      <span class="chat-workspace-recent-item-name">{{ row.entry.name }}</span>
                      <span class="chat-workspace-recent-item-path">{{ row.entry.path }}</span>
                    </span>
                    <span v-else class="chat-workspace-tree-name">{{ row.entry.name }}</span>
                  </button>
                </div>
              </div>
              <p v-else class="chat-workspace-tree-status">{{ emptyText }}</p>
            </div>
          </div>

          <div class="chat-workspace-dialog-actions">
            <div class="chat-workspace-dialog-options">
              <QSwitch
                :modelValue="showHidden"
                :disabled="loading || creating"
                :aria-label="t('chat_workspace_dialog_show_hidden')"
                @update:modelValue="updateShowHidden"
              />
              <span class="chat-workspace-dialog-option-label">{{ t('chat_workspace_dialog_show_hidden') }}</span>
            </div>
            <div class="chat-workspace-dialog-action-buttons">
              <QButton class="plain sm" :disabled="creating" @click="close">{{ t('action_cancel') }}</QButton>
              <QButton class="primary sm" :disabled="confirmDisabled" @click="confirm">{{ t('chat_workspace_action_attach') }}</QButton>
            </div>
          </div>
        </div>
      </section>
    </AppDialogShell>
  `,
};

export default WorkspaceDirectoryPicker;
