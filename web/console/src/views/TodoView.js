import { useToast } from "quail-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import "./TodoView.css";

import AppPage from "../components/AppPage";
import { runtimeApiFetch, translate } from "../core/context";

const REPEAT_KINDS = [
  { id: "daily", labelKey: "todo_repeat_daily" },
  { id: "weekly", labelKey: "todo_repeat_weekly" },
  { id: "monthly", labelKey: "todo_repeat_monthly" },
  { id: "custom", labelKey: "todo_repeat_custom" },
];

const WEEKDAYS = [
  { value: 0, labelKey: "todo_weekday_sun" },
  { value: 1, labelKey: "todo_weekday_mon" },
  { value: 2, labelKey: "todo_weekday_tue" },
  { value: 3, labelKey: "todo_weekday_wed" },
  { value: 4, labelKey: "todo_weekday_thu" },
  { value: 5, labelKey: "todo_weekday_fri" },
  { value: 6, labelKey: "todo_weekday_sat" },
];

const DEFAULT_CRON = "0 9 * * *";
const DEFAULT_REPEAT_TIME = "09:00";
const UTC_TIMEZONE_ITEMS = [
  { value: "UTC-12", label: "UTC-12", city: "Baker Island" },
  { value: "UTC-11", label: "UTC-11", city: "Pago Pago" },
  { value: "UTC-10", label: "UTC-10", city: "Honolulu" },
  { value: "UTC-9", label: "UTC-9", city: "Anchorage" },
  { value: "UTC-8", label: "UTC-8", city: "Los Angeles" },
  { value: "UTC-7", label: "UTC-7", city: "Denver" },
  { value: "UTC-6", label: "UTC-6", city: "Mexico City" },
  { value: "UTC-5", label: "UTC-5", city: "New York" },
  { value: "UTC-4", label: "UTC-4", city: "Santiago" },
  { value: "UTC-3", label: "UTC-3", city: "Buenos Aires" },
  { value: "UTC-2", label: "UTC-2", city: "South Georgia" },
  { value: "UTC-1", label: "UTC-1", city: "Azores" },
  { value: "UTC", label: "UTC+0", city: "London" },
  { value: "UTC+1", label: "UTC+1", city: "Paris" },
  { value: "UTC+2", label: "UTC+2", city: "Cairo" },
  { value: "UTC+3", label: "UTC+3", city: "Moscow" },
  { value: "UTC+4", label: "UTC+4", city: "Dubai" },
  { value: "UTC+5", label: "UTC+5", city: "Karachi" },
  { value: "UTC+6", label: "UTC+6", city: "Dhaka" },
  { value: "UTC+7", label: "UTC+7", city: "Bangkok" },
  { value: "UTC+8", label: "UTC+8", city: "Shanghai" },
  { value: "UTC+9", label: "UTC+9", city: "Tokyo" },
  { value: "UTC+10", label: "UTC+10", city: "Sydney" },
  { value: "UTC+11", label: "UTC+11", city: "Noumea" },
  { value: "UTC+12", label: "UTC+12", city: "Auckland" },
  { value: "UTC+13", label: "UTC+13", city: "Apia" },
  { value: "UTC+14", label: "UTC+14", city: "Kiritimati" },
];

let taskKeySeed = 0;

function nextTaskKey() {
  taskKeySeed += 1;
  return `todo-task-${Date.now()}-${taskKeySeed}`;
}

function trimText(value) {
  return String(value || "").trim();
}

function browserTimezone() {
  try {
    return trimText(Intl.DateTimeFormat().resolvedOptions().timeZone);
  } catch {
    return "";
  }
}

function timezoneOffsetMinutes(timezone) {
  const value = trimText(timezone);
  if (!value) {
    return null;
  }
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: value,
      timeZoneName: "shortOffset",
    }).formatToParts(new Date());
    const raw = trimText(parts.find((part) => part.type === "timeZoneName")?.value);
    if (raw === "GMT" || raw === "UTC") {
      return 0;
    }
    const match = raw.match(/^(?:GMT|UTC)([+-])(\d{1,2})(?::(\d{2}))?$/);
    if (!match) {
      return null;
    }
    const sign = match[1] === "-" ? -1 : 1;
    const hour = Number(match[2]);
    const minute = Number(match[3] || "0");
    if (!Number.isInteger(hour) || !Number.isInteger(minute)) {
      return null;
    }
    return sign * (hour * 60 + minute);
  } catch {
    return null;
  }
}

function formatUTCOffsetValue(minutes) {
  if (!Number.isInteger(minutes) || minutes % 60 !== 0) {
    return "UTC";
  }
  if (minutes === 0) {
    return "UTC";
  }
  const prefix = minutes > 0 ? "+" : "-";
  return `UTC${prefix}${Math.abs(minutes / 60)}`;
}

function browserUTCOffsetValue() {
  return formatUTCOffsetValue(timezoneOffsetMinutes(browserTimezone()));
}

function normalizeScheduleMode(value) {
  return trimText(value).toLowerCase() === "recurring" ? "recurring" : "once";
}

function taskMode(task) {
  const explicit = trimText(task?.mode).toLowerCase();
  if (explicit === "once" || explicit === "recurring") {
    return explicit;
  }
  return trimText(task?.cron) !== "" && trimText(task?.at) === "" ? "recurring" : "once";
}

function normalizeRepeatKind(value) {
  const kind = trimText(value).toLowerCase();
  return REPEAT_KINDS.some((item) => item.id === kind) ? kind : "daily";
}

function normalizeTimeInput(value) {
  const match = trimText(value).match(/^(\d{1,2}):(\d{2})$/);
  if (!match) {
    return DEFAULT_REPEAT_TIME;
  }
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (!Number.isInteger(hour) || !Number.isInteger(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
    return DEFAULT_REPEAT_TIME;
  }
  return `${pad2(hour)}:${pad2(minute)}`;
}

function cronTimeParts(value) {
  const time = normalizeTimeInput(value);
  const [hour, minute] = time.split(":");
  return { hour: String(Number(hour)), minute: String(Number(minute)) };
}

function normalizeMonthDay(value) {
  const parsed = Number.parseInt(trimText(value), 10);
  if (!Number.isInteger(parsed)) {
    return "1";
  }
  return String(Math.min(31, Math.max(1, parsed)));
}

function normalizeWeekdays(values) {
  const items = Array.isArray(values) ? values : [];
  const seen = new Set();
  for (const item of items) {
    const value = Number(item);
    if (Number.isInteger(value) && value >= 0 && value <= 6) {
      seen.add(value);
    }
  }
  const out = [...seen].sort((left, right) => left - right);
  return out.length > 0 ? out : [1];
}

function parseCronParts(raw) {
  const parts = trimText(raw).split(/\s+/).filter(Boolean);
  return parts.length === 5 ? parts : null;
}

function parseSingleCronNumber(raw, min, max) {
  if (!/^\d+$/.test(trimText(raw))) {
    return null;
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value < min || value > max) {
    return null;
  }
  return value;
}

function parseCronNumberSet(raw, min, max, sundayAlias = false) {
  const text = trimText(raw);
  if (!text || text === "*") {
    return null;
  }
  const values = new Set();
  for (const token of text.split(",")) {
    const part = trimText(token);
    if (!part) {
      return null;
    }
    if (part.includes("/")) {
      return null;
    }
    if (part.includes("-")) {
      const [startRaw, endRaw, extra] = part.split("-");
      if (extra !== undefined) {
        return null;
      }
      const start = parseSingleCronNumber(startRaw, min, max);
      const end = parseSingleCronNumber(endRaw, min, max);
      if (start === null || end === null || start > end) {
        return null;
      }
      for (let value = start; value <= end; value += 1) {
        values.add(sundayAlias && value === 7 ? 0 : value);
      }
      continue;
    }
    const value = parseSingleCronNumber(part, min, max);
    if (value === null) {
      return null;
    }
    values.add(sundayAlias && value === 7 ? 0 : value);
  }
  return [...values].sort((left, right) => left - right);
}

function inferRecurringState(rawCron) {
  const cron = normalizeCron(rawCron) || DEFAULT_CRON;
  const parts = parseCronParts(cron);
  if (!parts) {
    return {
      repeat_kind: "custom",
      repeat_time: DEFAULT_REPEAT_TIME,
      repeat_weekdays: [1],
      repeat_month_day: "1",
      custom_cron: cron,
    };
  }
  const [minuteRaw, hourRaw, domRaw, monthRaw, dowRaw] = parts;
  const minute = parseSingleCronNumber(minuteRaw, 0, 59);
  const hour = parseSingleCronNumber(hourRaw, 0, 23);
  if (minute === null || hour === null) {
    return {
      repeat_kind: "custom",
      repeat_time: DEFAULT_REPEAT_TIME,
      repeat_weekdays: [1],
      repeat_month_day: "1",
      custom_cron: cron,
    };
  }
  const time = `${pad2(hour)}:${pad2(minute)}`;
  if (domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
    return { repeat_kind: "daily", repeat_time: time, repeat_weekdays: [1], repeat_month_day: "1", custom_cron: cron };
  }
  if (domRaw === "*" && monthRaw === "*") {
    const days = parseCronNumberSet(dowRaw, 0, 7, true);
    if (days) {
      return { repeat_kind: "weekly", repeat_time: time, repeat_weekdays: days, repeat_month_day: "1", custom_cron: cron };
    }
  }
  if (monthRaw === "*" && dowRaw === "*") {
    const day = parseSingleCronNumber(domRaw, 1, 31);
    if (day !== null) {
      return { repeat_kind: "monthly", repeat_time: time, repeat_weekdays: [1], repeat_month_day: String(day), custom_cron: cron };
    }
  }
  return {
    repeat_kind: "custom",
    repeat_time: time,
    repeat_weekdays: [1],
    repeat_month_day: "1",
    custom_cron: cron,
  };
}

function recurringCron(task) {
  const kind = normalizeRepeatKind(task?.repeat_kind);
  if (kind === "custom") {
    return normalizeCron(task?.custom_cron ?? task?.cron);
  }
  const { minute, hour } = cronTimeParts(task?.repeat_time);
  switch (kind) {
    case "weekly":
      return `${minute} ${hour} * * ${normalizeWeekdays(task?.repeat_weekdays).join(",")}`;
    case "monthly":
      return `${minute} ${hour} ${normalizeMonthDay(task?.repeat_month_day)} * *`;
    case "daily":
    default:
      return `${minute} ${hour} * * *`;
  }
}

function normalizeTask(item = {}) {
  const at = trimText(item.at);
  const cron = trimText(item.cron);
  const recurring = inferRecurringState(cron);
  return {
    _key: nextTaskKey(),
    id: trimText(item.id),
    at,
    cron,
    tz: trimText(item.tz),
    content: trimText(item.content),
    chat_id: trimText(item.chat_id),
    mention: trimText(item.mention),
    mode: cron !== "" && at === "" ? "recurring" : "once",
    ...recurring,
  };
}

function pad2(value) {
  return String(value).padStart(2, "0");
}

function defaultAtValue() {
  const next = new Date(Date.now() + 60 * 60 * 1000);
  return `${next.getFullYear()}-${pad2(next.getMonth() + 1)}-${pad2(next.getDate())} ${pad2(next.getHours())}:${pad2(next.getMinutes())}`;
}

function normalizeCron(raw) {
  return trimText(raw).split(/\s+/).filter(Boolean).join(" ");
}

function parseEveryCronStep(raw) {
  const match = trimText(raw).match(/^\*\/(\d+)$/);
  if (!match) {
    return null;
  }
  const value = Number(match[1]);
  return Number.isInteger(value) && value > 0 ? value : null;
}

function cronFieldNumber(raw, min, max) {
  const text = trimText(raw);
  if (!/^\d+$/.test(text)) {
    return null;
  }
  const value = Number(text);
  return Number.isInteger(value) && value >= min && value <= max ? value : null;
}

function positiveCronStep(raw) {
  const text = trimText(raw);
  if (!/^\d+$/.test(text)) {
    return null;
  }
  const value = Number(text);
  return Number.isInteger(value) && value > 0 ? value : null;
}

function isValidCronFieldBase(raw, min, max) {
  const text = trimText(raw);
  if (text === "*") {
    return true;
  }
  if (text.includes("-")) {
    const [startRaw, endRaw, extra] = text.split("-");
    if (extra !== undefined) {
      return false;
    }
    const start = cronFieldNumber(startRaw, min, max);
    const end = cronFieldNumber(endRaw, min, max);
    return start !== null && end !== null && start <= end;
  }
  return cronFieldNumber(text, min, max) !== null;
}

function isValidCronField(raw, min, max) {
  const text = trimText(raw);
  if (!text) {
    return false;
  }
  const tokens = text.split(",");
  for (const tokenRaw of tokens) {
    const token = trimText(tokenRaw);
    if (!token) {
      return false;
    }
    const [baseRaw, stepRaw, extra] = token.split("/");
    if (extra !== undefined) {
      return false;
    }
    if (stepRaw !== undefined) {
      const step = positiveCronStep(stepRaw);
      if (step === null) {
        return false;
      }
    }
    if (!isValidCronFieldBase(baseRaw, min, max)) {
      return false;
    }
  }
  return true;
}

function isValidCronExpression(raw) {
  const parts = parseCronParts(raw);
  if (!parts) {
    return false;
  }
  const [minuteRaw, hourRaw, domRaw, monthRaw, dowRaw] = parts;
  return (
    isValidCronField(minuteRaw, 0, 59) &&
    isValidCronField(hourRaw, 0, 23) &&
    isValidCronField(domRaw, 1, 31) &&
    isValidCronField(monthRaw, 1, 12) &&
    isValidCronField(dowRaw, 0, 7)
  );
}

function isValidAtValue(raw) {
  const match = trimText(raw)
    .replace("T", " ")
    .match(/^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})$/);
  if (!match) {
    return false;
  }
  const [, yearRaw, monthRaw, dayRaw, hourRaw, minuteRaw] = match;
  const year = Number(yearRaw);
  const month = Number(monthRaw);
  const day = Number(dayRaw);
  const hour = Number(hourRaw);
  const minute = Number(minuteRaw);
  if (
    !Number.isInteger(year) ||
    !Number.isInteger(month) ||
    !Number.isInteger(day) ||
    !Number.isInteger(hour) ||
    !Number.isInteger(minute) ||
    year < 1 ||
    month < 1 ||
    month > 12 ||
    day < 1 ||
    day > 31 ||
    hour < 0 ||
    hour > 23 ||
    minute < 0 ||
    minute > 59
  ) {
    return false;
  }
  const date = new Date(Date.UTC(year, month - 1, day, hour, minute));
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day;
}

function atInputValue(task) {
  return trimText(task?.at).replace(" ", "T");
}

function serializeTask(task) {
  const mode = taskMode(task);
  const out = {
    id: trimText(task?.id),
    content: trimText(task?.content),
  };
  if (mode === "recurring") {
    out.cron = normalizeCron(task?.cron);
  } else {
    out.at = trimText(task?.at).replace("T", " ");
  }
  const tz = trimText(task?.tz);
  if (tz) {
    out.tz = tz;
  }
  const chatID = trimText(task?.chat_id);
  if (chatID) {
    out.chat_id = chatID;
  }
  const mention = trimText(task?.mention);
  if (mention) {
    out.mention = mention;
  }
  return out;
}

function snapshotTasks(tasks) {
  return JSON.stringify((Array.isArray(tasks) ? tasks : []).map((task) => serializeTask(task)));
}

const TodoView = {
  components: {
    AppPage,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const loading = ref(false);
    const saving = ref(false);
    const tasks = ref([]);
    const selectedTaskKey = ref("");
    const loadedSnapshot = ref("[]");
    const isMobile = ref(false);
    const mobileEditorVisible = ref(false);
    const contactsLoading = ref(false);
    const contactsErr = ref("");
    const contacts = ref([]);
    const deleteDialogOpen = ref(false);
    const deleteTargetKey = ref("");

    const selectedTask = computed(() => tasks.value.find((task) => task._key === selectedTaskKey.value) || null);
    const deleteTarget = computed(() => tasks.value.find((task) => task._key === deleteTargetKey.value) || null);
    const validationErrors = computed(() => tasks.value.map((task) => taskValidationMessage(task)).filter(Boolean));
    const visibleValidationErrors = computed(() => tasks.value.map((task) => visibleTaskValidationMessage(task)).filter(Boolean));
    const selectedTaskError = computed(() => (selectedTask.value ? visibleTaskValidationMessage(selectedTask.value) : ""));
    const formValidationMessage = computed(() => {
      if (!visibleValidationErrors.value.length) {
        return "";
      }
      return selectedTaskError.value || t("todo_validation_tasks_invalid");
    });
    let lastValidationToast = "";
    watch(formValidationMessage, (message) => {
      const text = trimText(message);
      if (!text) {
        lastValidationToast = "";
        return;
      }
      if (text === lastValidationToast) {
        return;
      }
      lastValidationToast = text;
      toast.error(text);
    });
    const timezoneBaseItems = computed(() =>
      UTC_TIMEZONE_ITEMS.map((item) => ({
        id: `tz-${item.value}`,
        title: `${item.label} · ${item.city}`,
        value: item.value,
      }))
    );
    const scheduleModeItems = computed(() => [
      { id: "schedule-once", title: t("todo_mode_once"), value: "once" },
      { id: "schedule-recurring", title: t("todo_mode_recurring"), value: "recurring" },
    ]);
    const mentionItems = computed(() => {
      const out = [{ id: "mention-none", title: t("todo_mention_none"), value: "", contactID: "" }];
      const rows = Array.isArray(contacts.value) ? contacts.value : [];
      for (const contact of rows) {
        if (contactStatus(contact) === "inactive") {
          continue;
        }
        const value = mentionReferenceForContact(contact);
        if (!value) {
          continue;
        }
        out.push({
          id: `mention-${trimText(contact?.contact_id)}`,
          title: contactOptionTitle(contact),
          value,
          contactID: trimText(contact?.contact_id),
        });
      }
      return out;
    });
    const taskCountMeta = computed(() => t("todo_nav_meta", { count: tasks.value.length }));
    const canSave = computed(
      () =>
        !loading.value &&
        !saving.value &&
        validationErrors.value.length === 0 &&
        snapshotTasks(tasks.value) !== loadedSnapshot.value
    );
    const showIndexPane = computed(() => !isMobile.value || !mobileEditorVisible.value);
    const showEditorPane = computed(() => !isMobile.value || mobileEditorVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobileEditorVisible.value);
    const mobileBarTitle = computed(() =>
      mobileShowBack.value ? taskTitle(selectedTask.value) || t("todo_detail_title") : t("todo_title")
    );
    const pageClass = computed(() => (isMobile.value ? "todo-page todo-page-mobile-split" : "todo-page"));
    const selectedIndex = computed(() => tasks.value.findIndex((task) => task._key === selectedTaskKey.value));
    const selectedCanMoveUp = computed(() => selectedIndex.value > 0);
    const selectedCanMoveDown = computed(() => selectedIndex.value >= 0 && selectedIndex.value < tasks.value.length - 1);
    const deleteDialogText = computed(() =>
      t("todo_delete_confirm", { title: taskTitle(deleteTarget.value || selectedTask.value || null) })
    );
    const deleteDialogActions = computed(() => [
      {
        name: "cancel",
        label: t("action_cancel"),
        class: "outlined",
        action: closeDeleteDialog,
      },
      {
        name: "delete",
        label: t("action_delete"),
        class: "danger",
        action: deleteSelectedTask,
      },
    ]);

    function refreshMobileMode() {
      isMobile.value = typeof window !== "undefined" && window.innerWidth <= 920;
      if (!isMobile.value) {
        mobileEditorVisible.value = false;
      }
    }

    function showIndexView() {
      mobileEditorVisible.value = false;
    }

    function taskTitle(task) {
      const content = trimText(task?.content).replace(/\s+/g, " ");
      if (content) {
        return content.length > 82 ? `${content.slice(0, 79)}...` : content;
      }
      return trimText(task?.id) || t("todo_untitled");
    }

    function scheduleLabel(task) {
      if (taskMode(task) === "recurring") {
        return recurringPreviewText(task);
      }
      return trimText(task?.at) || t("todo_schedule_missing");
    }

    function contactStatus(contact) {
      return trimText(contact?.status).toLowerCase();
    }

    function contactDisplayName(contact) {
      return trimText(contact?.nickname) || trimText(contact?.contact_id) || t("contacts_unnamed");
    }

    function contactOptionTitle(contact) {
      const name = contactDisplayName(contact);
      const channel = trimText(contact?.channel || contact?.kind);
      return channel ? `${name} · ${channel}` : name;
    }

    function safeMentionLabel(raw) {
      return trimText(raw)
        .replace(/[\[\]\r\n]/g, " ")
        .replace(/\s+/g, " ")
        .trim();
    }

    function mentionReferenceForContact(contact) {
      const contactID = trimText(contact?.contact_id);
      if (!contactID) {
        return "";
      }
      return `[${safeMentionLabel(contactDisplayName(contact)) || t("contacts_unnamed")}](${contactID})`;
    }

    function mentionID(raw) {
      const value = trimText(raw);
      const match = value.match(/\]\(([^)]+)\)\s*$/);
      return match ? trimText(match[1]) : value;
    }

    function mentionTitle(raw) {
      const value = trimText(raw);
      const match = value.match(/^\[([^\]]+)\]\([^)]+\)\s*$/);
      return match ? trimText(match[1]) : value;
    }

    function timezoneItems(task) {
      const current = trimText(task?.tz);
      if (!current || timezoneBaseItems.value.some((item) => item.value === current)) {
        return timezoneBaseItems.value;
      }
      return timezoneBaseItems.value;
    }

    function timezoneItem(task) {
      const current = trimText(task?.tz) || browserUTCOffsetValue();
      return (
        timezoneItems(task).find((item) => item.value === current) ||
        (trimText(task?.tz) ? { id: `tz-current-${current}`, title: current, value: current } : null) ||
        timezoneBaseItems.value.find((item) => item.value === "UTC") ||
        timezoneBaseItems.value[0]
      );
    }

    function updateTimezone(task, item) {
      updateTaskField(task, "tz", item?.value || "");
    }

    function scheduleModeItem(task) {
      const mode = taskMode(task);
      return scheduleModeItems.value.find((item) => item.value === mode) || scheduleModeItems.value[0];
    }

    function updateScheduleFromItem(task, item) {
      updateScheduleMode(task, item?.value || "once");
    }

    function mentionItem(task) {
      const current = trimText(task?.mention);
      if (!current) {
        return mentionItems.value[0];
      }
      const currentID = mentionID(current);
      const found = mentionItems.value.find((item) => item.value === current || (item.contactID && item.contactID === currentID));
      return found || { id: `mention-${currentID || current}`, title: mentionTitle(current), value: current, contactID: currentID };
    }

    function updateMention(task, item) {
      updateTaskField(task, "mention", item?.value || "");
    }

    function modeLabel(task) {
      return taskMode(task) === "recurring" ? t("todo_mode_recurring") : t("todo_mode_once");
    }

    function taskClass(task) {
      const classes = ["todo-index-item", "workspace-sidebar-item"];
      if (task?._key === selectedTaskKey.value) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function selectTask(task) {
      if (!task || !task._key) {
        return;
      }
      selectedTaskKey.value = task._key;
      if (isMobile.value) {
        mobileEditorVisible.value = true;
      }
    }

    function nextID() {
      const used = new Set(tasks.value.map((task) => trimText(task.id)).filter(Boolean));
      let index = tasks.value.length + 1;
      let id = `task-${index}`;
      while (used.has(id)) {
        index += 1;
        id = `task-${index}`;
      }
      return id;
    }

    function addTask() {
      const task = normalizeTask({
        id: nextID(),
        at: defaultAtValue(),
        cron: "0 9 * * *",
        tz: browserUTCOffsetValue(),
        content: "",
      });
      tasks.value = [...tasks.value, task];
      selectTask(task);
    }

    function confirmDeleteSelectedTask() {
      const key = selectedTaskKey.value;
      if (!key) {
        return;
      }
      deleteTargetKey.value = key;
      deleteDialogOpen.value = true;
    }

    function closeDeleteDialog() {
      deleteDialogOpen.value = false;
      deleteTargetKey.value = "";
    }

    async function deleteSelectedTask() {
      if (saving.value) {
        return;
      }
      const key = deleteTargetKey.value;
      if (!key) {
        closeDeleteDialog();
        return;
      }
      if (!tasks.value.some((task) => task._key === key)) {
        closeDeleteDialog();
        return;
      }
      const nextTasks = tasks.value.filter((task) => task._key !== key);
      saving.value = true;
      deleteDialogOpen.value = false;
      try {
        await runtimeApiFetch("/todo/tasks", {
          method: "PUT",
          body: { tasks: nextTasks.map((task) => serializeTask(task)) },
        });
        tasks.value = nextTasks;
        selectedTaskKey.value = "";
        mobileEditorVisible.value = false;
        loadedSnapshot.value = snapshotTasks(nextTasks);
        toast.success(t("msg_delete_success"));
      } catch (e) {
        toast.error(e.message || t("msg_delete_failed"));
      } finally {
        saving.value = false;
        deleteTargetKey.value = "";
      }
    }

    function moveSelectedTask(delta) {
      const index = selectedIndex.value;
      const nextIndex = index + delta;
      if (index < 0 || nextIndex < 0 || nextIndex >= tasks.value.length) {
        return;
      }
      const nextTasks = [...tasks.value];
      const [task] = nextTasks.splice(index, 1);
      nextTasks.splice(nextIndex, 0, task);
      tasks.value = nextTasks;
    }

    function updateTaskField(task, field, value) {
      if (!task || !field) {
        return;
      }
      task[field] = String(value || "");
    }

    function updateScheduleMode(task, mode) {
      if (!task) {
        return;
      }
      task.mode = normalizeScheduleMode(mode);
      if (task.mode === "recurring") {
        if (!trimText(task.cron)) {
          task.cron = DEFAULT_CRON;
        }
        const recurring = inferRecurringState(task.cron);
        task.repeat_kind = recurring.repeat_kind;
        task.repeat_time = recurring.repeat_time;
        task.repeat_weekdays = recurring.repeat_weekdays;
        task.repeat_month_day = recurring.repeat_month_day;
        task.custom_cron = recurring.custom_cron;
        task.cron = recurringCron(task);
      }
      if (task.mode === "once" && !trimText(task.at)) {
        task.at = defaultAtValue();
      }
    }

    function updateAtInput(task, value) {
      const text = trimText(value).replace("T", " ");
      const match = text.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2})(?::\d{2})?$/);
      updateTaskField(task, "at", match ? match[1] : text);
    }

    function syncRecurringCron(task) {
      if (!task) {
        return;
      }
      task.cron = recurringCron(task);
    }

    function updateRepeatKind(task, kind) {
      if (!task) {
        return;
      }
      task.repeat_kind = normalizeRepeatKind(kind);
      if (!trimText(task.repeat_time)) {
        task.repeat_time = DEFAULT_REPEAT_TIME;
      }
      if (!Array.isArray(task.repeat_weekdays) || task.repeat_weekdays.length === 0) {
        task.repeat_weekdays = [1];
      }
      if (!trimText(task.repeat_month_day)) {
        task.repeat_month_day = "1";
      }
      if (!trimText(task.custom_cron)) {
        task.custom_cron = trimText(task.cron) || DEFAULT_CRON;
      }
      syncRecurringCron(task);
    }

    function updateRepeatTime(task, value) {
      if (!task) {
        return;
      }
      task.repeat_time = trimText(value) || DEFAULT_REPEAT_TIME;
      syncRecurringCron(task);
    }

    function updateRepeatMonthDay(task, value) {
      if (!task) {
        return;
      }
      task.repeat_month_day = normalizeMonthDay(value);
      syncRecurringCron(task);
    }

    function updateCustomCron(task, value) {
      if (!task) {
        return;
      }
      task.custom_cron = String(value || "");
      syncRecurringCron(task);
    }

    function weekdaySelected(task, weekday) {
      return normalizeWeekdays(task?.repeat_weekdays).includes(Number(weekday));
    }

    function toggleRepeatWeekday(task, weekday) {
      if (!task) {
        return;
      }
      const value = Number(weekday);
      const days = normalizeWeekdays(task.repeat_weekdays);
      if (days.includes(value)) {
        if (days.length === 1) {
          return;
        }
        task.repeat_weekdays = days.filter((day) => day !== value);
      } else {
        task.repeat_weekdays = normalizeWeekdays([...days, value]);
      }
      syncRecurringCron(task);
    }

    function weekdayLabel(value) {
      const item = WEEKDAYS.find((day) => day.value === Number(value));
      return t(item?.labelKey || "todo_weekday_mon");
    }

    function selectedWeekdayText(task) {
      return normalizeWeekdays(task?.repeat_weekdays).map((day) => weekdayLabel(day)).join(", ");
    }

    function repeatKind(task) {
      return normalizeRepeatKind(task?.repeat_kind);
    }

    function customCronPreview(task) {
      const cron = recurringCron(task);
      const parts = parseCronParts(cron);
      if (!parts) {
        return t("todo_preview_custom_invalid");
      }
      const [minuteRaw, hourRaw, domRaw, monthRaw, dowRaw] = parts;
      if (minuteRaw === "*" && hourRaw === "*" && domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
        return t("todo_preview_custom_every_minute");
      }
      const minuteStep = parseEveryCronStep(minuteRaw);
      if (minuteStep !== null && hourRaw === "*" && domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
        return t("todo_preview_custom_every_minutes", { count: minuteStep });
      }
      const minute = parseSingleCronNumber(minuteRaw, 0, 59);
      const hour = parseSingleCronNumber(hourRaw, 0, 23);
      if (minute !== null && hourRaw === "*" && domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
        return t("todo_preview_custom_hourly", { minute: pad2(minute) });
      }
      if (minute === null || hour === null) {
        return t("todo_preview_custom_fallback");
      }
      const time = `${pad2(hour)}:${pad2(minute)}`;
      if (domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
        return t("todo_preview_daily", { time });
      }
      if (domRaw === "*" && monthRaw === "*") {
        const days = parseCronNumberSet(dowRaw, 0, 7, true);
        if (days) {
          return t("todo_preview_weekly", { days: days.map((day) => weekdayLabel(day)).join(", "), time });
        }
      }
      if (monthRaw === "*" && dowRaw === "*") {
        const day = parseSingleCronNumber(domRaw, 1, 31);
        if (day !== null) {
          return t("todo_preview_monthly", { day, time });
        }
        const days = parseCronNumberSet(domRaw, 1, 31, false);
        if (days) {
          return t("todo_preview_custom_month_days", { days: days.join(", "), time });
        }
      }
      return t("todo_preview_custom_fallback");
    }

    function recurringPreview(task) {
      const kind = repeatKind(task);
      const time = normalizeTimeInput(task?.repeat_time);
      switch (kind) {
        case "weekly":
          return t("todo_preview_weekly", { days: selectedWeekdayText(task), time });
        case "monthly":
          return t("todo_preview_monthly", { day: normalizeMonthDay(task?.repeat_month_day), time });
        case "custom":
          return customCronPreview(task);
        case "daily":
        default:
          return t("todo_preview_daily", { time });
      }
    }

    function cronValidationMessage(raw) {
      return isValidCronExpression(raw) ? "" : t("todo_validation_cron_invalid");
    }

    function cronPreviewError(task) {
      if (!task || taskMode(task) !== "recurring") {
        return "";
      }
      return cronValidationMessage(recurringCron(task));
    }

    function recurringPreviewText(task) {
      return cronPreviewError(task) || recurringPreview(task);
    }

    function recurringPreviewClass(task) {
      return cronPreviewError(task) ? "todo-repeat-preview is-error" : "todo-repeat-preview";
    }

    function taskValidationMessage(task) {
      if (!task) {
        return "";
      }
      if (!trimText(task.content)) {
        return t("todo_validation_content_required");
      }
      if (taskMode(task) === "once") {
        return isValidAtValue(task.at) ? "" : t("todo_validation_at_required");
      }
      return cronValidationMessage(recurringCron(task));
    }

    function visibleTaskValidationMessage(task) {
      const message = taskValidationMessage(task);
      return message === t("todo_validation_content_required") ? "" : message;
    }

    async function load() {
      loading.value = true;
      try {
        const data = await runtimeApiFetch("/todo/tasks");
        const rows = Array.isArray(data.tasks) ? data.tasks : [];
        tasks.value = rows.map((item) => normalizeTask(item));
        selectedTaskKey.value = "";
        mobileEditorVisible.value = false;
        loadedSnapshot.value = snapshotTasks(tasks.value);
      } catch (e) {
        const message = e.message || t("msg_load_failed");
        toast.error(message);
      } finally {
        loading.value = false;
      }
    }

    async function loadContacts() {
      contactsLoading.value = true;
      contactsErr.value = "";
      try {
        const data = await runtimeApiFetch("/contacts/list");
        contacts.value = Array.isArray(data.items) ? data.items : [];
      } catch (e) {
        const message = e.message || t("todo_mention_load_failed");
        contactsErr.value = message;
        toast.warning(message);
      } finally {
        contactsLoading.value = false;
      }
    }

    async function save() {
      if (!canSave.value) {
        return;
      }
      saving.value = true;
      try {
        await runtimeApiFetch("/todo/tasks", {
          method: "PUT",
          body: { tasks: tasks.value.map((task) => serializeTask(task)) },
        });
        loadedSnapshot.value = snapshotTasks(tasks.value);
        toast.success(t("msg_save_success"));
      } catch (e) {
        const message = e.message || t("msg_save_failed");
        toast.error(message);
      } finally {
        saving.value = false;
      }
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      void load();
      void loadContacts();
    });
    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
    });

    return {
      t,
      loading,
      saving,
      tasks,
      selectedTask,
      selectedTaskKey,
      taskCountMeta,
      canSave,
      contactsLoading,
      contactsErr,
      showIndexPane,
      showEditorPane,
      mobileShowBack,
      mobileBarTitle,
      pageClass,
      selectedCanMoveUp,
      selectedCanMoveDown,
      deleteDialogOpen,
      deleteDialogText,
      deleteDialogActions,
      REPEAT_KINDS,
      WEEKDAYS,
      addTask,
      confirmDeleteSelectedTask,
      moveSelectedTask,
      updateTaskField,
      updateScheduleMode,
      updateAtInput,
      updateRepeatKind,
      updateRepeatTime,
      updateRepeatMonthDay,
      updateCustomCron,
      updateTimezone,
      updateScheduleFromItem,
      updateMention,
      toggleRepeatWeekday,
      atInputValue,
      taskTitle,
      scheduleLabel,
      modeLabel,
      taskClass,
      taskMode,
      repeatKind,
      weekdaySelected,
      weekdayLabel,
      recurringPreview,
      recurringPreviewText,
      recurringPreviewClass,
      recurringCron,
      timezoneItems,
      timezoneItem,
      scheduleModeItems,
      scheduleModeItem,
      mentionItems,
      mentionItem,
      selectTask,
      showIndexView,
      load,
      save,
    };
  },
  template: `
    <AppPage :title="t('todo_title')" :class="pageClass" :hideDesktopBar="true" :showMobileNavTrigger="!mobileShowBack">
      <template #leading>
        <div class="todo-page-bar">
          <QButton
            v-if="mobileShowBack"
            class="outlined xs icon todo-page-bar-back"
            :title="t('todo_nav_title')"
            :aria-label="t('todo_nav_title')"
            @click="showIndexView"
          >
            <QIconArrowLeft class="icon" />
          </QButton>
          <h2 class="page-title page-bar-title workspace-section-title">{{ mobileBarTitle }}</h2>
        </div>
      </template>

      <div class="todo-workbench">
        <aside v-if="showIndexPane" class="todo-index workspace-sidebar-section" :aria-label="t('todo_nav_title')">
          <div class="todo-index-head workspace-sidebar-head">
            <div class="todo-index-copy">
              <h3 class="todo-index-title workspace-section-title">{{ t("todo_nav_title") }}</h3>
              <p class="todo-index-meta">{{ taskCountMeta }}</p>
            </div>
            <QButton
              class="plain sm icon todo-index-new"
              :title="t('todo_action_add')"
              :aria-label="t('todo_action_add')"
              @click="addTask"
            >
              <QIconPlus class="icon" />
            </QButton>
          </div>

          <QProgress v-if="loading" :infinite="true" />

          <div v-if="tasks.length > 0" class="todo-index-items workspace-sidebar-list" role="listbox">
            <div
              v-for="task in tasks"
              :key="task._key"
              :class="taskClass(task)"
              role="option"
              tabindex="0"
              :aria-selected="task._key === selectedTaskKey"
              @click="selectTask(task)"
              @keydown.enter.prevent="selectTask(task)"
              @keydown.space.prevent="selectTask(task)"
            >
              <span class="workspace-sidebar-item-copy">
                <span class="todo-index-item-name workspace-sidebar-item-title">{{ taskTitle(task) }}</span>
                <span class="todo-index-item-meta workspace-sidebar-item-meta">
                  <span class="todo-index-schedule">{{ scheduleLabel(task) }}</span>
                  <span class="todo-index-kind">{{ modeLabel(task) }}</span>
                </span>
              </span>
              <span class="todo-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                <QBadge v-if="task._key === selectedTaskKey" dot type="primary" size="sm" />
              </span>
            </div>
          </div>

          <p v-else-if="!loading" class="todo-empty-list">{{ t("todo_empty_list") }}</p>
        </aside>

        <QCard v-if="showEditorPane && selectedTask" class="todo-editor-card" variant="default">
          <div class="todo-editor-shell">
            <header class="todo-editor-head">
              <div class="todo-editor-copy">
                <p class="ui-kicker">{{ modeLabel(selectedTask) }}</p>
                <h3 class="todo-editor-title workspace-document-title">
                  {{ taskTitle(selectedTask) }}
                </h3>
                <p class="todo-editor-meta">
                  {{ scheduleLabel(selectedTask) }}
                </p>
              </div>
              <div class="todo-editor-actions">
                <QButton
                  class="outlined icon"
                  :title="t('todo_action_move_up')"
                  :aria-label="t('todo_action_move_up')"
                  :disabled="!selectedCanMoveUp || saving || loading"
                  @click="moveSelectedTask(-1)"
                >
                  <QIconChevronUp class="icon" />
                </QButton>
                <QButton
                  class="outlined icon"
                  :title="t('todo_action_move_down')"
                  :aria-label="t('todo_action_move_down')"
                  :disabled="!selectedCanMoveDown || saving || loading"
                  @click="moveSelectedTask(1)"
                >
                  <QIconChevronDown class="icon" />
                </QButton>
                <QButton
                  class="danger icon"
                  :title="t('action_delete')"
                  :aria-label="t('action_delete')"
                  :disabled="saving || loading"
                  @click="confirmDeleteSelectedTask"
                >
                  <QIconTrash class="icon" />
                </QButton>
                <QButton class="primary" :disabled="!canSave" :loading="saving" @click="save">
                  {{ t("action_save") }}
                </QButton>
              </div>
            </header>

            <div class="todo-form">
              <label class="todo-field is-wide">
                <QTextarea
                  :modelValue="selectedTask.content"
                  :rows="8"
                  :placeholder="t('todo_content_placeholder')"
                  :disabled="saving || loading"
                  @update:modelValue="updateTaskField(selectedTask, 'content', $event)"
                />
              </label>

              <div class="todo-field is-wide">
                <QDropdownMenu
                  :key="'mention-' + selectedTask._key + '-' + selectedTask.mention"
                  class="todo-dropdown todo-mention-dropdown"
                  :items="mentionItems"
                  :initialItem="mentionItem(selectedTask)"
                  :placeholder="t('todo_mention_placeholder')"
                  :useFilter="true"
                  useDialog="always"
                  :emptyHit="contactsErr ? t('todo_mention_load_failed') : t('todo_mention_empty')"
                  :disabled="saving || loading"
                  @change="updateMention(selectedTask, $event)"
                >
                  <template #prepend>
                    <span class="todo-control-prepend">{{ t("todo_field_mention") }}</span>
                  </template>
                </QDropdownMenu>
              </div>

              <div class="todo-field">
                <QDropdownMenu
                  :key="'timezone-' + selectedTask._key + '-' + selectedTask.tz"
                  class="todo-dropdown"
                  :items="timezoneItems(selectedTask)"
                  :initialItem="timezoneItem(selectedTask)"
                  :placeholder="t('todo_timezone_placeholder')"
                  :useFilter="true"
                  useDialog="always"
                  :disabled="saving || loading"
                  @change="updateTimezone(selectedTask, $event)"
                >
                  <template #prepend>
                    <span class="todo-control-prepend">{{ t("todo_field_timezone") }}</span>
                  </template>
                </QDropdownMenu>
              </div>

              <div class="todo-field">
                <QDropdownMenu
                  :key="'schedule-' + selectedTask._key + '-' + taskMode(selectedTask)"
                  class="todo-dropdown"
                  :items="scheduleModeItems"
                  :initialItem="scheduleModeItem(selectedTask)"
                  :placeholder="t('todo_field_schedule')"
                  :disabled="saving || loading"
                  @change="updateScheduleFromItem(selectedTask, $event)"
                >
                  <template #prepend>
                    <span class="todo-control-prepend">{{ t("todo_field_schedule") }}</span>
                  </template>
                </QDropdownMenu>
              </div>

              <label v-if="taskMode(selectedTask) === 'once'" class="todo-field is-wide">
                <QDatetimePicker
                  class="todo-datetime-picker"
                  :modelValue="atInputValue(selectedTask)"
                  accept="datetime"
                  :disabled="saving || loading"
                  @update:modelValue="updateAtInput(selectedTask, $event)"
                >
                  <template #prepend>
                    <span class="todo-control-prepend todo-datetime-prepend">{{ t("todo_field_at") }}</span>
                  </template>
                </QDatetimePicker>
                <span class="todo-field-note">{{ t("todo_at_note") }}</span>
              </label>

              <div v-else class="todo-field is-wide todo-repeat-field">
                <div class="todo-repeat-kind-grid" role="group" :aria-label="t('todo_field_repeat')">
                  <QButton
                    v-for="kind in REPEAT_KINDS"
                    :key="kind.id"
                    type="button"
                    :class="repeatKind(selectedTask) === kind.id ? 'todo-repeat-kind is-active' : 'todo-repeat-kind'"
                    :aria-pressed="repeatKind(selectedTask) === kind.id"
                    :disabled="saving || loading"
                    @click="updateRepeatKind(selectedTask, kind.id)"
                  >
                    {{ t(kind.labelKey) }}
                  </QButton>
                </div>

                <div v-if="repeatKind(selectedTask) !== 'custom'" class="todo-repeat-controls">
                  <label class="todo-field todo-repeat-time">
                    <QInput
                      :modelValue="selectedTask.repeat_time"
                      inputType="time"
                      :aria-label="t('todo_field_time')"
                      :disabled="saving || loading"
                      @update:modelValue="updateRepeatTime(selectedTask, $event)"
                    />
                  </label>

                  <div v-if="repeatKind(selectedTask) === 'weekly'" class="todo-field todo-weekday-field">
                    <div class="todo-weekday-picker" role="group" :aria-label="t('todo_field_weekdays')">
                      <QButton
                        v-for="day in WEEKDAYS"
                        :key="day.value"
                        type="button"
                        :class="weekdaySelected(selectedTask, day.value) ? 'todo-weekday is-active' : 'todo-weekday'"
                        :aria-pressed="weekdaySelected(selectedTask, day.value)"
                        :disabled="saving || loading"
                        @click="toggleRepeatWeekday(selectedTask, day.value)"
                      >
                        {{ weekdayLabel(day.value) }}
                      </QButton>
                    </div>
                  </div>

                  <label v-if="repeatKind(selectedTask) === 'monthly'" class="todo-field todo-month-day-field">
                    <QInput
                      class="todo-month-day-input"
                      :modelValue="selectedTask.repeat_month_day"
                      inputType="number"
                      :aria-label="t('todo_field_month_day')"
                      :disabled="saving || loading"
                      @update:modelValue="updateRepeatMonthDay(selectedTask, $event)"
                    />
                  </label>
                </div>

                <label v-else class="todo-field is-wide todo-custom-cron">
                  <QInput
                    :modelValue="selectedTask.custom_cron"
                    :placeholder="t('todo_custom_cron_placeholder')"
                    :aria-label="t('todo_field_cron')"
                    :disabled="saving || loading"
                    @update:modelValue="updateCustomCron(selectedTask, $event)"
                  />
                  <span class="todo-field-note">{{ t("todo_custom_cron_note") }}</span>
                </label>

                <div :class="recurringPreviewClass(selectedTask)">
                  <span class="todo-repeat-preview-label">{{ t("todo_repeat_preview") }}</span>
                  <strong class="todo-repeat-preview-text">{{ recurringPreviewText(selectedTask) }}</strong>
                  <code class="todo-repeat-preview-cron">{{ recurringCron(selectedTask) }}</code>
                </div>
              </div>
            </div>

          </div>
        </QCard>
        <section v-else-if="showEditorPane" class="todo-placeholder">
          <div class="todo-placeholder-copy">
            <h3 class="todo-placeholder-title workspace-document-title">{{ t("todo_detail_empty_title") }}</h3>
            <p class="todo-placeholder-note">{{ t("todo_detail_empty_hint") }}</p>
          </div>
          <div class="todo-placeholder-actions">
            <QButton
              class="primary sm todo-placeholder-add"
              :disabled="loading || saving"
              @click="addTask"
            >
              <QIconPlus class="icon" />
              {{ t("todo_action_add") }}
            </QButton>
          </div>
        </section>
      </div>
      <QMessageDialog
        v-model="deleteDialogOpen"
        icon="QIconTrash"
        iconColor="red"
        :title="t('action_delete')"
        :text="deleteDialogText"
        :actions="deleteDialogActions"
      />
    </AppPage>
  `,
};

export default TodoView;
