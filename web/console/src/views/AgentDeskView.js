import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";

import defaultEndpointAvatarURL from "../assets/images/app_logo_current.svg";
import AgentChatPane from "../components/AgentChatPane";
import AppDialogShell from "../components/AppDialogShell";
import {
  adjacentDeskPaneID,
  closeDeskPane,
  deskPanes,
  resizeDeskSplit,
  splitDeskPane,
  updateDeskPaneEndpoint,
  updateDeskPaneTopic,
} from "../core/agent-desk-layout";
import { resolveDeskShortcut } from "../core/agent-desk-shortcuts";
import { createEmptyDeskTab, normalizeDeskTabs } from "../core/agent-desk-tabs";
import { rememberLastTopicID } from "../core/chat-topic-memory";
import { endpointDisplayItem, visibleEndpoints } from "../core/endpoints";
import { endpointRoutePath } from "../core/endpoint-routes";
import { endpointState, ensureEndpointsLoaded, translate } from "../core/context";
import "./AgentDeskView.css";

const STORAGE_KEY = "mistermorph_console_agent_desk_v3";
const LEGACY_STORAGE_KEY = "mistermorph_console_agent_desk_v2";
const KEYBOARD_PREFIX_TIMEOUT_MS = 2200;
const TAB_EMOJIS = ["🌱", "🧭", "🪶", "🪐", "🍵", "🎐", "🧩", "🛠️", "🔭", "📡", "🫧", "🌙"];

function cleanText(value) {
  return String(value || "").trim();
}

function endpointSubmitRef(endpoint) {
  const mapped = cleanText(endpoint?.submit_endpoint_ref);
  if (mapped) {
    return mapped;
  }
  return endpoint?.can_submit === true ? cleanText(endpoint?.endpoint_ref) : "";
}

function newNodeID(kind) {
  const suffix = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${kind}-${suffix}`;
}

function readStoredDesk() {
  for (const key of [STORAGE_KEY, LEGACY_STORAGE_KEY]) {
    try {
      const parsed = JSON.parse(window.localStorage?.getItem(key) || "null");
      if (parsed && typeof parsed === "object") {
        return parsed;
      }
    } catch {
      // Try the previous storage version when the current value is invalid.
    }
  }
  return null;
}

function writeStoredDesk(tabs, activeTabID) {
  try {
    window.localStorage?.setItem(STORAGE_KEY, JSON.stringify({ tabs, activeTabID }));
  } catch {
    // The desk remains usable when browser storage is unavailable.
  }
}

const AgentDeskNode = {
  name: "AgentDeskNode",
  components: {
    AgentChatPane,
  },
  props: {
    node: {
      type: Object,
      required: true,
    },
    endpointMap: {
      type: Object,
      required: true,
    },
    endpointOptions: {
      type: Array,
      default: () => [],
    },
    activePaneID: {
      type: String,
      default: "",
    },
    dividerEdges: {
      type: Array,
      default: () => [],
    },
  },
  emits: [
    "activate",
    "close",
    "endpoint-change",
    "resize",
    "split",
    "topic-change",
    "topic-missing",
  ],
  setup(props, { emit }) {
    const t = translate;
    const splitRoot = ref(null);
    const endpoint = computed(
      () =>
        props.endpointMap.get(cleanText(props.node?.endpointRef)) || {
          endpoint_ref: cleanText(props.node?.endpointRef),
          name: cleanText(props.node?.endpointRef),
          connected: false,
          can_submit: false,
        }
    );
    const splitStyle = computed(() => {
      if (props.node?.type !== "split") {
        return {};
      }
      const ratio = Number(props.node.ratio) || 0.5;
      const first = `minmax(0, ${ratio}fr)`;
      const second = `minmax(0, ${1 - ratio}fr)`;
      return props.node.direction === "column"
        ? { gridTemplateRows: `${first} 5px ${second}` }
        : { gridTemplateColumns: `${first} 5px ${second}` };
    });
    function resizeFromPointer(event) {
      const root = splitRoot.value;
      if (!root || props.node?.type !== "split") {
        return;
      }
      const bounds = root.getBoundingClientRect();
      const ratio = props.node.direction === "column"
        ? (event.clientY - bounds.top) / bounds.height
        : (event.clientX - bounds.left) / bounds.width;
      emit("resize", { splitID: props.node.id, ratio });
    }

    function positionDividerHandle(event) {
      const divider = event.currentTarget;
      const bounds = divider.getBoundingClientRect();
      const position = props.node?.direction === "column"
        ? event.clientX - bounds.left
        : event.clientY - bounds.top;
      const length = props.node?.direction === "column" ? bounds.width : bounds.height;
      divider.style.setProperty(
        "--agent-desk-divider-handle-position",
        `${Math.max(0, Math.min(length, position))}px`
      );
    }

    function startResize(event) {
      if (event.button !== 0) {
        return;
      }
      positionDividerHandle(event);
      event.currentTarget.setPointerCapture(event.pointerId);
      resizeFromPointer(event);
    }

    function moveResize(event) {
      positionDividerHandle(event);
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        resizeFromPointer(event);
      }
    }

    function stopResize(event) {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
    }

    function resizeWithKeyboard(event) {
      if (props.node?.type !== "split") {
        return;
      }
      const decreasing = props.node.direction === "column" ? event.key === "ArrowUp" : event.key === "ArrowLeft";
      const increasing = props.node.direction === "column" ? event.key === "ArrowDown" : event.key === "ArrowRight";
      if (!decreasing && !increasing) {
        return;
      }
      event.preventDefault();
      emit("resize", {
        splitID: props.node.id,
        ratio: Number(props.node.ratio) + (increasing ? 0.05 : -0.05),
      });
    }

    function childDividerEdges(position) {
      const edges = new Set(props.dividerEdges);
      if (props.node?.direction === "row") {
        edges.add(position === "first" ? "right" : "left");
      } else {
        edges.add(position === "first" ? "bottom" : "top");
      }
      return [...edges];
    }

    return {
      childDividerEdges,
      t,
      endpoint,
      moveResize,
      positionDividerHandle,
      resizeWithKeyboard,
      splitRoot,
      splitStyle,
      startResize,
      stopResize,
    };
  },
  template: `
    <div
      v-if="node.type === 'split'"
      ref="splitRoot"
      class="agent-desk-split"
      :class="'is-' + node.direction"
      :style="splitStyle"
    >
      <div class="agent-desk-branch">
        <AgentDeskNode
          :node="node.first"
          :endpointMap="endpointMap"
          :endpointOptions="endpointOptions"
          :activePaneID="activePaneID"
          :dividerEdges="childDividerEdges('first')"
          @activate="$emit('activate', $event)"
          @close="$emit('close', $event)"
          @endpoint-change="$emit('endpoint-change', $event)"
          @resize="$emit('resize', $event)"
          @split="$emit('split', $event)"
          @topic-change="$emit('topic-change', $event)"
          @topic-missing="$emit('topic-missing', $event)"
        />
      </div>
      <button
        type="button"
        class="agent-desk-divider"
        :class="'is-' + node.direction"
        role="separator"
        :aria-label="t('agent_desk_resize_panes')"
        :aria-orientation="node.direction === 'row' ? 'vertical' : 'horizontal'"
        aria-valuemin="20"
        aria-valuemax="80"
        :aria-valuenow="Math.round(node.ratio * 100)"
        @pointerenter="positionDividerHandle"
        @pointerdown.prevent="startResize"
        @pointermove="moveResize"
        @pointerup="stopResize"
        @pointercancel="stopResize"
        @keydown="resizeWithKeyboard"
      ></button>
      <div class="agent-desk-branch">
        <AgentDeskNode
          :node="node.second"
          :endpointMap="endpointMap"
          :endpointOptions="endpointOptions"
          :activePaneID="activePaneID"
          :dividerEdges="childDividerEdges('second')"
          @activate="$emit('activate', $event)"
          @close="$emit('close', $event)"
          @endpoint-change="$emit('endpoint-change', $event)"
          @resize="$emit('resize', $event)"
          @split="$emit('split', $event)"
          @topic-change="$emit('topic-change', $event)"
          @topic-missing="$emit('topic-missing', $event)"
        />
      </div>
    </div>
    <div
      v-else
      class="agent-desk-pane-slot"
      :class="{ 'is-active': activePaneID === node.id }"
      :data-divider-edges="dividerEdges.join(' ')"
    >
      <AgentChatPane
        :paneId="node.id"
        :endpoint="endpoint"
        :endpointOptions="endpointOptions"
        :initialTopicId="node.topicID || ''"
        :canClose="true"
        @activate="$emit('activate', $event)"
        @close="$emit('close', $event)"
        @endpoint-change="$emit('endpoint-change', $event)"
        @split="$emit('split', $event)"
        @topic-change="$emit('topic-change', $event)"
        @topic-missing="$emit('topic-missing', $event)"
      />
      <div v-if="activePaneID === node.id" class="agent-desk-pane-active" aria-hidden="true"></div>
    </div>
  `,
};

const AgentDeskView = {
  name: "AgentDeskView",
  components: {
    AgentDeskNode,
    AppDialogShell,
  },
  setup() {
    const t = translate;
    const router = useRouter();
    const tabs = ref([]);
    const activeTabID = ref("");
    const loading = ref(true);
    const loadError = ref("");
    const keyboardPrefixActive = ref(false);
    const keyboardHelpOpen = ref(false);
    let initialized = false;
    let persistTimerID = 0;
    let keyboardPrefixTimerID = 0;

    const endpoints = computed(() => visibleEndpoints(endpointState.items));
    const endpointMap = computed(
      () => new Map(endpoints.value.map((endpoint) => [cleanText(endpoint?.endpoint_ref), endpoint]))
    );
    const endpointOptions = computed(() =>
      endpoints.value
        .map((endpoint) => {
          const display = endpointDisplayItem(endpoint, t);
          const endpointRef = cleanText(endpoint?.endpoint_ref);
          const connected = endpoint?.connected === true;
          return {
            id: endpointRef,
            value: endpointRef,
            title: cleanText(endpoint?.agent_name) || display.title,
            image: cleanText(endpoint?.avatar_url) || defaultEndpointAvatarURL,
            connected,
            disabled: !connected || !endpointSubmitRef(endpoint),
          };
        })
        .filter((item) => item.value)
    );
    const activeTab = computed(
      () => tabs.value.find((tab) => tab.id === activeTabID.value) || null
    );
    const layout = computed({
      get: () => activeTab.value?.layout || null,
      set: (value) => {
        if (activeTab.value) {
          activeTab.value.layout = value;
        }
      },
    });
    const activePaneID = computed({
      get: () => cleanText(activeTab.value?.activePaneID),
      set: (value) => {
        if (activeTab.value) {
          activeTab.value.activePaneID = cleanText(value);
        }
      },
    });
    const panes = computed(() => deskPanes(layout.value));
    const endpointSignature = computed(() =>
      endpoints.value.map((endpoint) => cleanText(endpoint?.endpoint_ref)).join("|")
    );

    function defaultEndpointRef() {
      const selectedRef = cleanText(endpointState.selectedRef);
      if (endpointMap.value.has(selectedRef)) {
        return selectedRef;
      }
      return endpointOptions.value.find((item) => !item.disabled)?.value || endpointOptions.value[0]?.value || "";
    }

    function ensureActiveTab(preferredTabID = "") {
      const preferred = cleanText(preferredTabID);
      if (tabs.value.some((tab) => tab.id === preferred)) {
        activeTabID.value = preferred;
      } else if (!tabs.value.some((tab) => tab.id === activeTabID.value)) {
        activeTabID.value = tabs.value[0]?.id || "";
      }
    }

    function addTab() {
      const tab = createEmptyDeskTab(newNodeID("tab"), tabs.value, TAB_EMOJIS);
      if (!tab) {
        return;
      }
      tabs.value.push(tab);
      activeTabID.value = tab.id;
    }

    function activateTab(tabID) {
      ensureActiveTab(tabID);
      ensureActivePane(activeTab.value?.activePaneID);
    }

    function ensureActivePane(preferredPaneID = "") {
      const availablePaneIDs = panes.value.map((pane) => pane.id);
      const preferred = cleanText(preferredPaneID);
      if (availablePaneIDs.includes(preferred)) {
        activePaneID.value = preferred;
      } else if (!availablePaneIDs.includes(activePaneID.value)) {
        activePaneID.value = availablePaneIDs[0] || "";
      }
    }

    function createInitialLayout() {
      if (!activeTab.value) {
        addTab();
      }
      const endpointRef = defaultEndpointRef();
      if (!endpointRef) {
        layout.value = null;
        activePaneID.value = "";
        return;
      }
      const pane = {
        type: "pane",
        id: newNodeID("pane"),
        endpointRef,
        topicID: "",
      };
      layout.value = pane;
      activePaneID.value = pane.id;
      focusPane(pane.id, true);
    }

    function reconcileTabs() {
      if (endpointMap.value.size === 0) {
        ensureActiveTab(activeTabID.value);
        ensureActivePane(activePaneID.value);
        return;
      }
      const normalized = normalizeDeskTabs(
        { tabs: tabs.value, activeTabID: activeTabID.value },
        [...endpointMap.value.keys()],
        defaultEndpointRef(),
        TAB_EMOJIS
      );
      tabs.value = normalized.tabs;
      activeTabID.value = normalized.activeTabID;
    }

    function schedulePersist() {
      if (!initialized) {
        return;
      }
      if (persistTimerID) {
        window.clearTimeout(persistTimerID);
      }
      persistTimerID = window.setTimeout(() => {
        persistTimerID = 0;
        writeStoredDesk(tabs.value, activeTabID.value);
      }, 120);
    }

    function paneElement(paneID) {
      const targetID = cleanText(paneID);
      return [...document.querySelectorAll(".agent-chat-pane")].find(
        (element) => cleanText(element?.dataset?.paneId) === targetID
      ) || null;
    }

    function focusPane(paneID, composer = false) {
      void nextTick(() => {
        const pane = paneElement(paneID);
        const target = composer
          ? pane?.querySelector(
              ".chat-composer textarea, .chat-composer input, .chat-composer [contenteditable='true']"
            )
          : pane;
        target?.focus({ preventScroll: true });
      });
    }

    function activatePane(paneID, focusTarget = "") {
      const targetID = cleanText(paneID);
      ensureActivePane(targetID);
      if (activePaneID.value !== targetID) {
        return;
      }
      if (focusTarget === "composer") {
        focusPane(targetID, true);
      } else if (focusTarget === "pane") {
        focusPane(targetID);
      }
    }

    function changePaneEndpoint(payload) {
      const paneID = cleanText(payload?.paneID);
      const endpointRef = cleanText(payload?.item?.value);
      if (!paneID || !endpointMap.value.has(endpointRef) || payload?.item?.disabled === true) {
        return;
      }
      layout.value = updateDeskPaneEndpoint(layout.value, paneID, endpointRef);
      activePaneID.value = paneID;
    }

    function changePaneTopic(payload) {
      const paneID = cleanText(payload?.paneID);
      if (!panes.value.some((pane) => pane.id === paneID)) {
        return;
      }
      layout.value = updateDeskPaneTopic(layout.value, paneID, payload?.topicID);
    }

    function splitPane(payload) {
      const paneID = cleanText(payload?.paneID);
      const sourcePane = panes.value.find((pane) => pane.id === paneID);
      if (!sourcePane) {
        return;
      }
      const usedRefs = new Set(panes.value.map((pane) => pane.endpointRef));
      const nextEndpointRef =
        endpointOptions.value.find((item) => !item.disabled && !usedRefs.has(item.value))?.value ||
        sourcePane.endpointRef;
      const pane = {
        type: "pane",
        id: newNodeID("pane"),
        endpointRef: nextEndpointRef,
        topicID: "",
      };
      layout.value = splitDeskPane(layout.value, paneID, {
        splitID: newNodeID("split"),
        direction: payload?.direction === "column" ? "column" : "row",
        pane,
      });
      activePaneID.value = pane.id;
      focusPane(pane.id, true);
    }

    function closePane(paneID) {
      const currentPanes = panes.value;
      if (currentPanes.length === 0) {
        return;
      }
      const targetID = cleanText(paneID);
      const targetIndex = currentPanes.findIndex((pane) => pane.id === targetID);
      if (targetIndex < 0) {
        return;
      }
      layout.value = closeDeskPane(layout.value, targetID);
      const remaining = deskPanes(layout.value);
      activePaneID.value = remaining[Math.min(targetIndex, remaining.length - 1)]?.id || "";
      if (activePaneID.value) {
        focusPane(activePaneID.value, true);
      }
    }

    function resizeSplit(payload) {
      layout.value = resizeDeskSplit(layout.value, payload?.splitID, payload?.ratio);
    }

    function openFullChat(payload) {
      const paneID = cleanText(payload?.paneID);
      const pane = panes.value.find((item) => item.id === paneID);
      const endpointRef = cleanText(pane?.endpointRef);
      const topicID = cleanText(payload?.topicID || pane?.topicID);
      const endpoint = endpointMap.value.get(endpointRef);
      if (!endpoint) {
        return;
      }
      const submitRef = endpointSubmitRef(endpoint);
      if (submitRef && topicID) {
        rememberLastTopicID(submitRef, topicID);
      }
      const chatPagePath = topicID ? `/chat/${encodeURIComponent(topicID)}` : "/chat";
      router.push(endpointRoutePath(endpointRef, chatPagePath));
    }

    function exitDesk() {
      const pane = panes.value.find((item) => item.id === activePaneID.value);
      if (pane && endpointMap.value.has(pane.endpointRef)) {
        openFullChat({ paneID: pane.id, topicID: pane.topicID });
        return;
      }
      const path = endpointRoutePath(endpointState.selectedRef, "/chat");
      router.push(path || "/overview");
    }

    function setKeyboardPrefix(active) {
      keyboardPrefixActive.value = Boolean(active);
      if (keyboardPrefixTimerID) {
        window.clearTimeout(keyboardPrefixTimerID);
        keyboardPrefixTimerID = 0;
      }
      if (keyboardPrefixActive.value) {
        keyboardPrefixTimerID = window.setTimeout(() => {
          keyboardPrefixActive.value = false;
          keyboardPrefixTimerID = 0;
        }, KEYBOARD_PREFIX_TIMEOUT_MS);
      }
    }

    function focusPaneByOffset(offset) {
      if (panes.value.length === 0) {
        return;
      }
      const currentIndex = Math.max(0, panes.value.findIndex((pane) => pane.id === activePaneID.value));
      const nextIndex = (currentIndex + offset + panes.value.length) % panes.value.length;
      activatePane(panes.value[nextIndex].id, "composer");
    }

    function runKeyboardAction(action, index) {
      if (action.startsWith("focus-") && ["left", "right", "up", "down"].includes(action.slice(6))) {
        const paneID = adjacentDeskPaneID(layout.value, activePaneID.value, action.slice(6));
        if (paneID) {
          activatePane(paneID, "composer");
        }
        return;
      }
      switch (action) {
        case "focus-next":
          focusPaneByOffset(1);
          break;
        case "focus-previous":
          focusPaneByOffset(-1);
          break;
        case "focus-index":
          if (panes.value[index]) {
            activatePane(panes.value[index].id, "composer");
          }
          break;
        case "split-row":
          splitPane({ paneID: activePaneID.value, direction: "row" });
          break;
        case "split-column":
          splitPane({ paneID: activePaneID.value, direction: "column" });
          break;
        case "close-pane":
          closePane(activePaneID.value);
          break;
        case "focus-composer":
          focusPane(activePaneID.value, true);
          break;
        case "exit-desk":
          exitDesk();
          break;
        case "show-help":
          keyboardHelpOpen.value = true;
          break;
        default:
          break;
      }
    }

    function handleKeyboardShortcut(event) {
      const result = resolveDeskShortcut(event, keyboardPrefixActive.value);
      if (!result.handled) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      setKeyboardPrefix(result.prefixActive);
      if (result.action !== "prefix" && result.action !== "cancel") {
        runKeyboardAction(result.action, result.index);
      }
    }

    function shortcutRows() {
      return [
        { keys: "Ctrl+B → ← ↓ ↑ → / H J K L", label: t("agent_desk_shortcut_focus_direction") },
        { keys: "Ctrl+B → 1…9", label: t("agent_desk_shortcut_focus_index") },
        { keys: "Ctrl+B → N / P", label: t("agent_desk_shortcut_focus_cycle") },
        { keys: "Ctrl+B → % / V", label: t("agent_desk_shortcut_split_right") },
        { keys: 'Ctrl+B → " / S', label: t("agent_desk_shortcut_split_down") },
        { keys: "Ctrl+B → X", label: t("agent_desk_shortcut_close") },
        { keys: "Ctrl+B → Enter", label: t("agent_desk_shortcut_composer") },
        { keys: "Ctrl+B → E", label: t("agent_desk_shortcut_exit") },
        { keys: "Ctrl+B → ?", label: t("agent_desk_shortcut_help") },
        { keys: "Tab → divider → Arrow", label: t("agent_desk_shortcut_resize") },
      ];
    }

    function paneAvailable(pane) {
      const endpoint = endpointMap.value.get(cleanText(pane?.endpointRef));
      return endpoint?.connected === true && Boolean(endpointSubmitRef(endpoint));
    }

    function tabHasOffline(tab) {
      return deskPanes(tab?.layout).some((pane) => !paneAvailable(pane));
    }

    function tabLabel(tab, index) {
      return t("agent_desk_tab_label", {
        index: index + 1,
        count: deskPanes(tab?.layout).length,
        status: tabHasOffline(tab) ? t("agent_desk_tab_offline") : "",
      });
    }

    watch(endpointSignature, () => {
      if (initialized) {
        reconcileTabs();
      }
    });
    watch([tabs, activeTabID], schedulePersist, { deep: true });

    onMounted(async () => {
      loadError.value = "";
      try {
        await ensureEndpointsLoaded();
      } catch (cause) {
        loadError.value = cause?.message || t("msg_load_failed");
      }
      const stored = readStoredDesk();
      const storedLayouts = Array.isArray(stored?.tabs)
        ? stored.tabs.map((tab) => tab?.layout)
        : [stored?.layout];
      const validRefs = endpointMap.value.size > 0
        ? [...endpointMap.value.keys()]
        : storedLayouts.flatMap((storedLayout) => deskPanes(storedLayout).map((pane) => pane.endpointRef));
      const normalized = normalizeDeskTabs(
        stored,
        validRefs,
        defaultEndpointRef(),
        TAB_EMOJIS
      );
      tabs.value = normalized.tabs;
      activeTabID.value = normalized.activeTabID;
      if (tabs.value.length === 0) {
        addTab();
      }
      ensureActiveTab(activeTabID.value);
      ensureActivePane(activeTab.value?.activePaneID);
      initialized = true;
      writeStoredDesk(tabs.value, activeTabID.value);
      document.addEventListener("keydown", handleKeyboardShortcut, true);
      loading.value = false;
    });

    onBeforeUnmount(() => {
      document.removeEventListener("keydown", handleKeyboardShortcut, true);
      if (persistTimerID) {
        window.clearTimeout(persistTimerID);
      }
      if (keyboardPrefixTimerID) {
        window.clearTimeout(keyboardPrefixTimerID);
      }
      if (initialized) {
        writeStoredDesk(tabs.value, activeTabID.value);
      }
    });

    return {
      t,
      activePaneID,
      activeTabID,
      activatePane,
      activateTab,
      addTab,
      changePaneEndpoint,
      changePaneTopic,
      closePane,
      createInitialLayout,
      endpointMap,
      endpointOptions,
      exitDesk,
      keyboardHelpOpen,
      keyboardPrefixActive,
      layout,
      loadError,
      loading,
      openFullChat,
      panes,
      resizeSplit,
      shortcutRows,
      splitPane,
      tabHasOffline,
      tabLabel,
      tabs,
    };
  },
  template: `
    <section class="agent-desk-page" :aria-label="t('agent_desk_title')">
      <div v-if="loading" class="agent-desk-loading" aria-hidden="true">
        <QSkeleton width="100%" height="100%" />
      </div>
      <div v-else class="agent-desk-shell">
        <aside class="agent-desk-view-tabs">
          <nav class="agent-desk-view-tab-list" :aria-label="t('agent_desk_tabs')">
            <button
              v-for="(tab, index) in tabs"
              :key="tab.id"
              type="button"
              class="agent-desk-view-tab"
              :class="{
                'is-active': activeTabID === tab.id,
                'is-empty': !tab.layout,
                'is-offline': tabHasOffline(tab),
              }"
              :aria-label="tabLabel(tab, index)"
              :title="tabLabel(tab, index)"
              :aria-current="activeTabID === tab.id ? 'true' : undefined"
              @click="activateTab(tab.id)"
            >
              <span class="agent-desk-view-tab-emoji" aria-hidden="true">{{ tab.emoji || '💬' }}</span>
              <span class="agent-desk-view-tab-status" aria-hidden="true"></span>
            </button>
          </nav>
          <div class="agent-desk-view-tab-tools">
            <button
              type="button"
              class="agent-desk-view-tab"
              :aria-label="t('agent_desk_add_tab')"
              :title="t('agent_desk_add_tab')"
              @click="addTab"
            >
              <QIconPlus class="icon" />
            </button>
            <button
              type="button"
              class="agent-desk-view-tab"
              :class="{ 'is-prefix-active': keyboardPrefixActive }"
              :aria-label="t('agent_desk_keyboard_help')"
              :title="t('agent_desk_keyboard_help')"
              @click="keyboardHelpOpen = true"
            >
              <QIconKeyboard class="icon" />
            </button>
            <button
              type="button"
              class="agent-desk-view-tab agent-desk-exit-tab"
              :aria-label="t('agent_desk_exit')"
              :title="t('agent_desk_exit')"
              @click="exitDesk"
            >
              <QIconLogout class="icon" />
            </button>
          </div>
        </aside>

        <main class="agent-desk-stage">
          <div v-if="!layout" class="agent-desk-empty">
            <span class="agent-desk-empty-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>
            <h1>{{ t('agent_desk_empty_title') }}</h1>
            <p>{{ loadError || t('agent_desk_empty_hint') }}</p>
            <QButton class="plain" :disabled="endpointOptions.length === 0" @click="createInitialLayout">
              {{ t('agent_desk_add_pane') }}
            </QButton>
          </div>
          <AgentDeskNode
            v-else
            :node="layout"
            :endpointMap="endpointMap"
            :endpointOptions="endpointOptions"
            :activePaneID="activePaneID"
            @activate="activatePane"
            @close="closePane"
            @endpoint-change="changePaneEndpoint"
            @resize="resizeSplit"
            @split="splitPane"
            @topic-change="changePaneTopic"
            @topic-missing="closePane($event.paneID)"
          />
        </main>
      </div>

      <AppDialogShell
        :modelValue="keyboardHelpOpen"
        :title="t('agent_desk_keyboard_help')"
        width="560px"
        @update:modelValue="keyboardHelpOpen = $event"
        @close="keyboardHelpOpen = false"
      >
        <div class="agent-desk-shortcut-help">
          <p>{{ t('agent_desk_keyboard_intro') }}</p>
          <dl>
            <div v-for="item in shortcutRows()" :key="item.keys" class="agent-desk-shortcut-row">
              <dt><kbd>{{ item.keys }}</kbd></dt>
              <dd>{{ item.label }}</dd>
            </div>
          </dl>
        </div>
      </AppDialogShell>
    </section>
  `,
};

export default AgentDeskView;
