import { useToast } from "quail-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import "./TodoView.css";

import AppPage from "../components/AppPage";
import AppMarkdownEditor from "../components/AppMarkdownEditor";
import { currentLocale, runtimeApiFetch, translate } from "../core/context";
import { modelVendorMeta } from "../core/model-vendor";
import { invalidateConsoleSetupReadiness } from "../core/setup";
import { requestSystemNotificationPermission } from "../core/system-notifications";
import channelDiscordLogoURL from "../assets/images/channels/discord.svg";
import channelLarkLogoURL from "../assets/images/channels/lark.svg";
import channelLineLogoURL from "../assets/images/channels/line.svg";
import channelSlackLogoURL from "../assets/images/channels/slack.svg";
import channelTelegramLogoURL from "../assets/images/channels/telegram.svg";

const REPEAT_KINDS = [
  { id: "hourly", labelKey: "todo_repeat_hourly" },
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
const DEFAULT_REPEAT_KIND = "daily";
const DEFAULT_TODO_TITLE = "";
const HEARTBEAT_FILE_NAME = "HEARTBEAT.md";
const HEARTBEAT_ITEM_KEY = "__heartbeat__";
const CONSOLE_NOTIFICATION_CHAT_ID = "console:user";
const CHAT_PLATFORM_LOGOS = {
  discord: channelDiscordLogoURL,
  lark: channelLarkLogoURL,
  line: channelLineLogoURL,
  slack: channelSlackLogoURL,
  telegram: channelTelegramLogoURL,
};
const CHAT_NONE_ICON_URL = `data:image/svg+xml,${encodeURIComponent(
  '<svg width="512" height="512" viewBox="0 0 512 512" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="256" cy="256" r="222" fill="#EEF0F3"/><path d="M170 342L342 170" stroke="#6F7782" stroke-width="48" stroke-linecap="round"/><circle cx="256" cy="256" r="142" stroke="#6F7782" stroke-width="36"/></svg>'
)}`;
const CONTACT_REF_PROTOCOLS = new Set(["tg", "slack", "line", "line_user", "lark", "lark_user", "discord"]);
const UTC_TIMEZONE_ITEMS = [
  { value: "UTC-12", label: "UTC-12", cityKey: "todo_timezone_city_baker_island" },
  { value: "UTC-11", label: "UTC-11", cityKey: "todo_timezone_city_pago_pago" },
  { value: "UTC-10", label: "UTC-10", cityKey: "todo_timezone_city_honolulu" },
  { value: "UTC-9", label: "UTC-9", cityKey: "todo_timezone_city_anchorage" },
  { value: "UTC-8", label: "UTC-8", cityKey: "todo_timezone_city_los_angeles" },
  { value: "UTC-7", label: "UTC-7", cityKey: "todo_timezone_city_denver" },
  { value: "UTC-6", label: "UTC-6", cityKey: "todo_timezone_city_mexico_city" },
  { value: "UTC-5", label: "UTC-5", cityKey: "todo_timezone_city_new_york" },
  { value: "UTC-4", label: "UTC-4", cityKey: "todo_timezone_city_santiago" },
  { value: "UTC-3", label: "UTC-3", cityKey: "todo_timezone_city_buenos_aires" },
  { value: "UTC-2", label: "UTC-2", cityKey: "todo_timezone_city_south_georgia" },
  { value: "UTC-1", label: "UTC-1", cityKey: "todo_timezone_city_azores" },
  { value: "UTC", label: "UTC+0", cityKey: "todo_timezone_city_london" },
  { value: "UTC+1", label: "UTC+1", cityKey: "todo_timezone_city_paris" },
  { value: "UTC+2", label: "UTC+2", cityKey: "todo_timezone_city_cairo" },
  { value: "UTC+3", label: "UTC+3", cityKey: "todo_timezone_city_moscow" },
  { value: "UTC+4", label: "UTC+4", cityKey: "todo_timezone_city_dubai" },
  { value: "UTC+5", label: "UTC+5", cityKey: "todo_timezone_city_karachi" },
  { value: "UTC+6", label: "UTC+6", cityKey: "todo_timezone_city_dhaka" },
  { value: "UTC+7", label: "UTC+7", cityKey: "todo_timezone_city_bangkok" },
  { value: "UTC+8", label: "UTC+8", cityKey: "todo_timezone_city_shanghai" },
  { value: "UTC+9", label: "UTC+9", cityKey: "todo_timezone_city_tokyo" },
  { value: "UTC+10", label: "UTC+10", cityKey: "todo_timezone_city_sydney" },
  { value: "UTC+11", label: "UTC+11", cityKey: "todo_timezone_city_noumea" },
  { value: "UTC+12", label: "UTC+12", cityKey: "todo_timezone_city_auckland" },
  { value: "UTC+13", label: "UTC+13", cityKey: "todo_timezone_city_apia" },
  { value: "UTC+14", label: "UTC+14", cityKey: "todo_timezone_city_kiritimati" },
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
  return REPEAT_KINDS.some((item) => item.id === kind) ? kind : DEFAULT_REPEAT_KIND;
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
  const text = trimText(value);
  if (!/^\d+$/.test(text)) {
    return "1";
  }
  const parsed = Number.parseInt(text, 10);
  if (!Number.isInteger(parsed)) {
    return "1";
  }
  return String(Math.min(31, Math.max(1, parsed)));
}

function normalizeMinuteInput(value) {
  const text = trimText(value);
  if (!/^\d+$/.test(text)) {
    return null;
  }
  const parsed = Number.parseInt(text, 10);
  if (!Number.isInteger(parsed)) {
    return null;
  }
  return Math.min(59, Math.max(0, parsed));
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
  if (minute === null) {
    return {
      repeat_kind: "custom",
      repeat_time: DEFAULT_REPEAT_TIME,
      repeat_weekdays: [1],
      repeat_month_day: "1",
      custom_cron: cron,
    };
  }
  const minuteTime = `00:${pad2(minute)}`;
  if (hourRaw === "*" && domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
    return { repeat_kind: "hourly", repeat_time: minuteTime, repeat_weekdays: [1], repeat_month_day: "1", custom_cron: cron };
  }
  if (hour === null) {
    return {
      repeat_kind: "custom",
      repeat_time: minuteTime,
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
    case "hourly":
      return `${minute} * * * *`;
    case "weekly":
      return `${minute} ${hour} * * ${normalizeWeekdays(task?.repeat_weekdays).join(",")}`;
    case "monthly":
      return `${minute} ${hour} ${normalizeMonthDay(task?.repeat_month_day)} * *`;
    case "daily":
    default:
      return `${minute} ${hour} * * *`;
  }
}

function normalizeBashEnv(refs) {
  if (!Array.isArray(refs)) {
    return [];
  }
  const out = [];
  for (const item of refs) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const name = trimText(item.name);
    const value = String(item.value ?? "");
    if (!name && !trimText(value)) {
      continue;
    }
    out.push({ name, value });
  }
  return out;
}

function compactBashEnv(refs) {
  if (!Array.isArray(refs)) {
    return [];
  }
  return refs
    .map((item) => ({
      name: trimText(item?.name),
      value: String(item?.value ?? ""),
    }))
    .filter((item) => item.name !== "" || trimText(item.value) !== "");
}

function isValidBashEnvName(raw) {
  const key = trimText(raw);
  if (!key) {
    return false;
  }
  for (let i = 0; i < key.length; i += 1) {
    const code = key.charCodeAt(i);
    const isLetter = (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
    if (key[i] === "_" || isLetter) {
      continue;
    }
    if (i > 0 && code >= 48 && code <= 57) {
      continue;
    }
    return false;
  }
  return true;
}

function bashEnvValidationMessage(refs, t) {
  const rows = Array.isArray(refs) ? refs : [];
  const seen = new Set();
  for (const item of rows) {
    const name = trimText(item?.name);
    const value = trimText(item?.value);
    if (!name && !value) {
      continue;
    }
    if (!name && value) {
      return t("todo_validation_bash_env_name_required");
    }
    if (!isValidBashEnvName(name)) {
      return t("todo_validation_bash_env_name_invalid", { name });
    }
    if (seen.has(name)) {
      return t("todo_validation_bash_env_duplicate", { name });
    }
    seen.add(name);
  }
  return "";
}

function normalizeTask(item = {}, fallbackTitle = DEFAULT_TODO_TITLE) {
  const at = trimText(item.at);
  const cron = trimText(item.cron);
  const recurring = inferRecurringState(cron);
  return {
    _key: nextTaskKey(),
    id: trimText(item.id),
    title: trimText(item.title) || fallbackTitle,
    enabled: item.enabled !== false,
    at,
    cron,
    tz: trimText(item.tz),
    content: trimText(item.content),
    chat_id: trimText(item.chat_id),
    llm_profile: trimText(item.llm_profile),
    bash_env: normalizeBashEnv(item.bash_env),
    mode: cron !== "" && at === "" ? "recurring" : "once",
    ...recurring,
  };
}

function normalizeChatOptions(items) {
  const rows = Array.isArray(items) ? items : [];
  const seen = new Set();
  const out = [];
  for (const item of rows) {
    const chatID = trimText(item?.chat_id);
    const name = trimText(item?.name);
    if (!chatID || !name) {
      continue;
    }
    const key = chatID.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push({
      chat_id: chatID,
      platform: trimText(item?.platform),
      type: trimText(item?.type),
      name,
      expires_at: trimText(item?.expires_at),
    });
  }
  return out.sort((left, right) => {
    const leftTitle = `${left.name} ${left.platform} ${left.type}`.toLowerCase();
    const rightTitle = `${right.name} ${right.platform} ${right.type}`.toLowerCase();
    return leftTitle.localeCompare(rightTitle);
  });
}

function normalizeLLMProfiles(items) {
  const rows = Array.isArray(items) ? items : [];
  const seen = new Set();
  const out = [];
  for (const item of rows) {
    const name = trimText(typeof item === "string" ? item : item?.name);
    const key = name.toLowerCase();
    if (!name || seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push({
      name,
      inferenceProvider: trimText(item?.inference_provider || item?.provider),
      modelName: trimText(item?.model),
    });
  }
  return out.sort((left, right) => left.name.localeCompare(right.name));
}

function chatPlatformFromID(chatID) {
  const protocol = trimText(chatID).split(":", 1)[0].toLowerCase();
  if (protocol === "tg") {
    return "telegram";
  }
  if (protocol === "line_user") {
    return "line";
  }
  if (protocol === "lark_user") {
    return "lark";
  }
  if (protocol === "slack" || protocol === "line" || protocol === "lark" || protocol === "discord") {
    return protocol;
  }
  return "";
}

function chatPlatformLogoImage(option) {
  const platform = trimText(option?.platform).toLowerCase() || chatPlatformFromID(option?.chat_id || option?.value);
  return CHAT_PLATFORM_LOGOS[platform] || "";
}

function cloneTaskForDraft(task) {
  if (!task) {
    return null;
  }
  return {
    ...task,
    repeat_weekdays: Array.isArray(task.repeat_weekdays) ? [...task.repeat_weekdays] : [1],
    bash_env: Array.isArray(task.bash_env)
      ? task.bash_env.map((item) => ({
          name: String(item?.name ?? ""),
          value: String(item?.value ?? ""),
        }))
      : [],
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

function serializeTask(task, fallbackTitle = DEFAULT_TODO_TITLE) {
  const mode = taskMode(task);
  const out = {
    id: trimText(task?.id),
    title: trimText(task?.title) || fallbackTitle || DEFAULT_TODO_TITLE,
    content: trimText(task?.content),
  };
  if (task?.enabled === false) {
    out.enabled = false;
  }
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
  const llmProfile = trimText(task?.llm_profile);
  if (llmProfile) {
    out.llm_profile = llmProfile;
  }
  const bashEnv = compactBashEnv(task?.bash_env);
  if (bashEnv.length > 0) {
    out.bash_env = bashEnv;
  }
  return out;
}

function normalizeTaskBeforeSave(task) {
  if (!task) {
    return;
  }
  if (taskMode(task) !== "recurring") {
    return;
  }
  const kind = normalizeRepeatKind(task.repeat_kind);
  task.repeat_kind = kind;
  if (kind === "hourly") {
    const minute = normalizeMinuteInput(cronTimeParts(task.repeat_time).minute);
    task.repeat_time = `00:${pad2(minute ?? 0)}`;
  }
  if (kind === "monthly") {
    task.repeat_month_day = normalizeMonthDay(task.repeat_month_day);
  }
  task.cron = recurringCron(task);
}

const TodoView = {
  components: {
    AppPage,
    AppMarkdownEditor,
  },
  setup() {
    const t = translate;
    const toast = useToast();
    const loading = ref(false);
    const saving = ref(false);
    const tasks = ref([]);
    const chatOptions = ref([]);
    const llmProfiles = ref([]);
    const selectedTaskKey = ref("");
    const selectedTaskDraft = ref(null);
    const draftDirty = ref(false);
    const tasksDirty = ref(false);
    const isMobile = ref(false);
    const mobileEditorVisible = ref(false);
    const deleteDialogOpen = ref(false);
    const deleteTargetKey = ref("");
    const repeatInputRevision = ref(0);
    const chatDropdownRevision = ref(0);
    const heartbeatLoading = ref(false);
    const heartbeatSaving = ref(false);
    const heartbeatContent = ref("");
    const heartbeatMissing = ref(false);
    const heartbeatDirty = ref(false);
    const heartbeatTask = ref(null);
    const heartbeatEnabled = ref(true);
    const runningTaskKey = ref("");

    const heartbeatSelected = computed(() => selectedTaskKey.value === HEARTBEAT_ITEM_KEY);
    const heartbeatDisabled = computed(() => heartbeatEnabled.value === false);
    const selectedStoredTask = computed(() => tasks.value.find((task) => task._key === selectedTaskKey.value) || null);
    const selectedTask = computed(() =>
      heartbeatSelected.value
        ? null
        : selectedTaskDraft.value?._key === selectedTaskKey.value
          ? selectedTaskDraft.value
          : selectedStoredTask.value
    );
    const deleteTarget = computed(() => tasks.value.find((task) => task._key === deleteTargetKey.value) || null);
    const taskHasLocalChanges = computed(() => tasksDirty.value || draftDirty.value);
    const selectedSaveValidationMessage = computed(() => (selectedTask.value ? taskValidationMessage(selectedTask.value) : ""));
    const selectedTaskError = computed(() => (selectedTask.value ? visibleTaskValidationMessage(selectedTask.value) : ""));
    const formValidationMessage = computed(() => selectedTaskError.value);
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
        title: `${item.label} · ${t(item.cityKey)}`,
        value: item.value,
      }))
    );
    const scheduleModeItems = computed(() => [
      { id: "schedule-once", title: t("todo_mode_once"), value: "once" },
      { id: "schedule-recurring", title: t("todo_mode_recurring"), value: "recurring" },
    ]);
    const repeatKindTabs = computed(() => REPEAT_KINDS.map((kind) => ({ id: kind.id, title: t(kind.labelKey) })));
    const chatMenuItems = computed(() => [
      { id: "chat-none", title: t("todo_chat_none"), value: "", image: CHAT_NONE_ICON_URL },
      ...chatOptions.value.map((item) => ({
        id: `chat-${item.chat_id}`,
        title: chatOptionTitle(item),
        subtitle: chatOptionSubtitle(item),
        value: item.chat_id,
        chat_id: item.chat_id,
        name: item.name,
        type: item.type,
        platform: item.platform,
        image: chatPlatformLogoImage(item),
      })),
    ]);
    const llmProfileMenuItems = computed(() => [
      { id: "llm-profile-none", title: t("todo_llm_profile_none"), value: "", icon: "QIconDataflow" },
      ...llmProfiles.value.map((profile) => {
        const vendor = modelVendorMeta(profile.modelName);
        return {
          id: `llm-profile-${profile.name}`,
          title: profile.name,
          subtitle: [profile.inferenceProvider, profile.modelName].filter(Boolean).join("/"),
          value: profile.name,
          image: vendor.icon || undefined,
          icon: vendor.icon ? undefined : "QIconCpuChip",
        };
      }),
    ]);
    const heartbeatIndexMeta = computed(() => {
      if (loading.value || heartbeatLoading.value) {
        return t("todo_heartbeat_loading");
      }
      if (heartbeatDisabled.value) {
        return t("todo_heartbeat_disabled");
      }
      return heartbeatTask.value ? schedulePreviewText(heartbeatTask.value) : t("todo_schedule_missing");
    });
    const heartbeatEditorMeta = computed(() => t("todo_heartbeat_editor_meta"));
    const canSaveTasks = computed(
      () =>
        !loading.value &&
        !saving.value &&
        taskHasLocalChanges.value &&
        !selectedSaveValidationMessage.value
    );
    const canSaveHeartbeat = computed(
      () =>
        heartbeatSelected.value &&
        !heartbeatLoading.value &&
        !heartbeatSaving.value &&
        (heartbeatMissing.value || heartbeatDirty.value)
    );
    const canSave = computed(() => (heartbeatSelected.value ? canSaveHeartbeat.value : canSaveTasks.value));
    const canRunSelectedTask = computed(
      () =>
        Boolean(selectedStoredTask.value) &&
        !heartbeatSelected.value &&
        !loading.value &&
        !saving.value &&
        !runningTaskKey.value &&
        !taskHasLocalChanges.value &&
        !selectedSaveValidationMessage.value &&
        trimText(selectedStoredTask.value?.id) !== ""
    );
    const showIndexPane = computed(() => !isMobile.value || !mobileEditorVisible.value);
    const showEditorPane = computed(() => !isMobile.value || mobileEditorVisible.value);
    const mobileShowBack = computed(() => isMobile.value && mobileEditorVisible.value);
    const mobileBarTitle = computed(() =>
      mobileShowBack.value
        ? heartbeatSelected.value
          ? t("todo_heartbeat_title")
          : taskTitle(selectedTask.value) || t("todo_detail_title")
        : t("todo_title")
    );
    const pageClass = computed(() => (isMobile.value ? "todo-page todo-page-mobile-split" : "todo-page"));
    const selectedIndex = computed(() => tasks.value.findIndex((task) => task._key === selectedTaskKey.value));
    const selectedCanMoveUp = computed(() => selectedIndex.value > 0);
    const selectedCanMoveDown = computed(() => selectedIndex.value >= 0 && selectedIndex.value < tasks.value.length - 1);
    const taskActionMenuItems = computed(() => [
      {
        id: "run-manually",
        title: t("todo_action_run_manually"),
        disabled: !canRunSelectedTask.value,
        action: runSelectedTaskNow,
      },
      {
        id: "move-up",
        title: t("todo_action_move_up"),
        disabled: saving.value || loading.value || !selectedCanMoveUp.value,
        action: () => moveSelectedTask(-1),
      },
      {
        id: "move-down",
        title: t("todo_action_move_down"),
        disabled: saving.value || loading.value || !selectedCanMoveDown.value,
        action: () => moveSelectedTask(1),
      },
      { id: "delete-divider", divider: true },
      {
        id: "delete",
        title: t("action_delete"),
        danger: true,
        disabled: saving.value || loading.value,
        action: confirmDeleteSelectedTask,
      },
    ]);
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
      return trimText(task?.title).replace(/\s+/g, " ") || t("todo_untitled");
    }

    function scheduleLabel(task) {
      return schedulePreviewText(task);
    }

    function taskListDisplayTask(task) {
      if (task?._key === selectedTaskKey.value && selectedTaskDraft.value?._key === task._key) {
        return selectedTaskDraft.value;
      }
      return task;
    }

    function chatOptionTitle(option) {
      if (trimText(option?.platform).toLowerCase() === "console") {
        return t("todo_chat_console_user");
      }
      const name = trimText(option?.name);
      return name || t("todo_chat_unavailable");
    }

    function chatOptionSubtitle(option) {
      if (trimText(option?.platform).toLowerCase() === "console") {
        return t("todo_chat_console_notification");
      }
      return displayChatType(option?.type);
    }

    function displayChatType(value) {
      const type = trimText(value).toLowerCase();
      if (!type) {
        return "";
      }
      const locale = currentLocale();
      const zh = locale === "zh-CN";
      const ja = locale === "ja-JP";
      const labels = {
        private: zh ? "私聊" : ja ? "個人チャット" : "Private chat",
        im: zh ? "私聊" : ja ? "個人チャット" : "Private chat",
        direct: zh ? "私聊" : ja ? "個人チャット" : "Private chat",
        dm: zh ? "私聊" : ja ? "個人チャット" : "Private chat",
        group: zh ? "群聊" : ja ? "グループチャット" : "Group chat",
        supergroup: zh ? "群聊" : ja ? "グループチャット" : "Group chat",
        channel: zh ? "频道" : ja ? "チャンネル" : "Channel",
        room: zh ? "群聊" : ja ? "ルーム" : "Room",
        thread: zh ? "Thread" : ja ? "スレッド" : "Thread",
      };
      return labels[type] || type;
    }

    function chatItem(task) {
      const chatID = trimText(task?.chat_id);
      if (!chatID) {
        return chatMenuItems.value[0];
      }
      return (
        chatMenuItems.value.find((item) => item.value === chatID) || {
          id: `chat-current-${chatID}`,
          title: t("todo_chat_unavailable"),
          value: chatID,
          image: chatPlatformLogoImage({ chat_id: chatID }),
        }
      );
    }

    async function updateChatFromItem(task, item) {
      const chatID = trimText(item?.value);
      if (chatID === CONSOLE_NOTIFICATION_CHAT_ID) {
        const permission = await requestSystemNotificationPermission();
        if (permission !== "granted") {
          const key =
            permission === "denied"
              ? "todo_chat_notification_denied"
              : permission === "unsupported"
                ? "todo_chat_notification_unsupported"
                : "todo_chat_notification_required";
          toast.error(t(key));
          chatDropdownRevision.value += 1;
          return;
        }
      }
      updateTaskField(task, "chat_id", chatID);
    }

    function llmProfileItem(task) {
      const profile = trimText(task?.llm_profile);
      if (!profile) {
        return llmProfileMenuItems.value[0];
      }
      return (
        llmProfileMenuItems.value.find((item) => item.value === profile) || {
          id: `llm-profile-unavailable-${profile}`,
          title: t("todo_llm_profile_unavailable", { profile }),
          value: profile,
          icon: "QIconCpuChip",
        }
      );
    }

    function updateLLMProfileFromItem(task, item) {
      updateTaskField(task, "llm_profile", item?.value || "");
    }

    function safeMentionLabel(raw) {
      return trimText(raw)
        .replace(/[\[\]\r\n]/g, " ")
        .replace(/\s+/g, " ")
        .trim();
    }

    function isContactReferenceID(raw) {
      const value = trimText(raw);
      const protocol = value.split(":", 1)[0].toLowerCase();
      return CONTACT_REF_PROTOCOLS.has(protocol);
    }

    function contactReferencesFromContent(raw) {
      const text = String(raw || "");
      const refs = [];
      const seen = new Set();
      const re = /\[([^\]\r\n]+)\]\(([^)\s]+)\)/g;
      let match = re.exec(text);
      while (match) {
        const label = safeMentionLabel(match[1]);
        const id = trimText(match[2]);
        const key = id.toLowerCase();
        if (label && id && isContactReferenceID(id) && !seen.has(key)) {
          seen.add(key);
          refs.push({ label, id });
        }
        match = re.exec(text);
      }
      return refs;
    }

    function timezoneItem(task) {
      const current = trimText(task?.tz) || browserUTCOffsetValue();
      return (
        timezoneBaseItems.value.find((item) => item.value === current) ||
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

    function taskClass(task) {
      const classes = ["todo-index-item", "workspace-sidebar-item"];
      if (task?._key === selectedTaskKey.value) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function heartbeatClass() {
      const classes = ["todo-index-item", "todo-heartbeat-item", "workspace-sidebar-item"];
      if (heartbeatSelected.value) {
        classes.push("is-active");
      }
      return classes.join(" ");
    }

    function openSelectedTaskDraft() {
      selectedTaskDraft.value = cloneTaskForDraft(selectedStoredTask.value);
      draftDirty.value = false;
    }

    function commitSelectedTaskDraft() {
      const draft = selectedTaskDraft.value;
      if (!draft || !draftDirty.value) {
        return;
      }
      const index = tasks.value.findIndex((task) => task._key === draft._key);
      if (index < 0) {
        selectedTaskDraft.value = null;
        draftDirty.value = false;
        return;
      }
      const nextTasks = [...tasks.value];
      nextTasks[index] = cloneTaskForDraft(draft);
      tasks.value = nextTasks;
      selectedTaskDraft.value = cloneTaskForDraft(nextTasks[index]);
      draftDirty.value = false;
      tasksDirty.value = true;
    }

    function markTaskChanged(task) {
      if (task?._key && task._key === selectedTaskDraft.value?._key) {
        draftDirty.value = true;
        return;
      }
      tasksDirty.value = true;
    }

    function selectTask(task) {
      if (!task || !task._key) {
        return;
      }
      const changingSelection = task._key !== selectedTaskKey.value;
      if (changingSelection) {
        commitSelectedTaskDraft();
        selectedTaskKey.value = task._key;
        openSelectedTaskDraft();
      } else if (!selectedTaskDraft.value) {
        openSelectedTaskDraft();
      }
      if (isMobile.value) {
        mobileEditorVisible.value = true;
      }
    }

    function selectHeartbeat() {
      if (heartbeatSelected.value) {
        if (isMobile.value) {
          mobileEditorVisible.value = true;
        }
        return;
      }
      commitSelectedTaskDraft();
      selectedTaskKey.value = HEARTBEAT_ITEM_KEY;
      selectedTaskDraft.value = null;
      draftDirty.value = false;
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
        title: t("todo_untitled"),
        at: defaultAtValue(),
        cron: "0 9 * * *",
        tz: browserUTCOffsetValue(),
        content: "",
      });
      tasks.value = [...tasks.value, task];
      tasksDirty.value = true;
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
        const data = await runtimeApiFetch("/todo/tasks", {
          method: "PUT",
          body: { tasks: nextTasks.map((task) => serializeTask(task, t("todo_untitled"))) },
        });
        chatOptions.value = normalizeChatOptions(data.chat_options);
        llmProfiles.value = normalizeLLMProfiles(data.llm_profiles);
        tasks.value = nextTasks;
        selectedTaskKey.value = "";
        selectedTaskDraft.value = null;
        draftDirty.value = false;
        tasksDirty.value = false;
        mobileEditorVisible.value = false;
        toast.success(t("msg_delete_success"));
      } catch (e) {
        toast.error(e.message || t("msg_delete_failed"));
      } finally {
        saving.value = false;
        deleteTargetKey.value = "";
      }
    }

    function moveSelectedTask(delta) {
      commitSelectedTaskDraft();
      const index = selectedIndex.value;
      const nextIndex = index + delta;
      if (index < 0 || nextIndex < 0 || nextIndex >= tasks.value.length) {
        return;
      }
      const nextTasks = [...tasks.value];
      const [task] = nextTasks.splice(index, 1);
      nextTasks.splice(nextIndex, 0, task);
      tasks.value = nextTasks;
      tasksDirty.value = true;
      openSelectedTaskDraft();
    }

    function updateTaskField(task, field, value) {
      if (!task || !field) {
        return;
      }
      task[field] = String(value || "");
      markTaskChanged(task);
    }

    function updateTodoTitle(task, value) {
      updateTaskField(task, "title", value);
    }

    function updateTaskEnabled(task, value) {
      if (!task) {
        return;
      }
      task.enabled = value !== false;
      markTaskChanged(task);
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
      markTaskChanged(task);
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
      markTaskChanged(task);
    }

    function updateRepeatKindFromTab(task, detail) {
      if (saving.value || loading.value) {
        return;
      }
      updateRepeatKind(task, detail?.tab?.id);
    }

    function repeatKindTab(task) {
      const kind = repeatKind(task);
      return (
        repeatKindTabs.value.find((item) => item.id === kind) ||
        repeatKindTabs.value.find((item) => item.id === DEFAULT_REPEAT_KIND) ||
        repeatKindTabs.value[0] ||
        null
      );
    }

    function repeatKindInitialIndex(task) {
      const kind = repeatKind(task);
      const index = repeatKindTabs.value.findIndex((item) => item.id === kind);
      if (index >= 0) {
        return index;
      }
      return Math.max(
        0,
        repeatKindTabs.value.findIndex((item) => item.id === DEFAULT_REPEAT_KIND)
      );
    }

    function guardRepeatKindTabsEvent(event) {
      if (!saving.value && !loading.value) {
        return;
      }
      event?.preventDefault?.();
      event?.stopPropagation?.();
    }

    function updateRepeatTime(task, value) {
      if (!task) {
        return;
      }
      task.repeat_time = trimText(value) || DEFAULT_REPEAT_TIME;
      syncRecurringCron(task);
      markTaskChanged(task);
    }

    function refreshRepeatInputs() {
      repeatInputRevision.value += 1;
    }

    function repeatMinuteInputKey(task) {
      return `minute-${task?._key || ""}-${repeatMinuteValue(task)}-${repeatInputRevision.value}`;
    }

    function repeatMonthDayInputKey(task) {
      return `month-day-${task?._key || ""}-${trimText(task?.repeat_month_day)}-${repeatInputRevision.value}`;
    }

    function repeatMinuteValue(task) {
      const [, minute] = normalizeTimeInput(task?.repeat_time).split(":");
      return String(Number(minute));
    }

    function updateRepeatMinute(task, value) {
      if (!task) {
        return;
      }
      const raw = trimText(value);
      const minute = normalizeMinuteInput(value);
      if (minute === null) {
        refreshRepeatInputs();
        return;
      }
      task.repeat_time = `00:${pad2(minute)}`;
      syncRecurringCron(task);
      markTaskChanged(task);
      if (raw !== String(minute)) {
        refreshRepeatInputs();
      }
    }

    function updateRepeatMonthDay(task, value) {
      if (!task) {
        return;
      }
      const raw = trimText(value);
      const day = normalizeMonthDay(value);
      task.repeat_month_day = day;
      syncRecurringCron(task);
      markTaskChanged(task);
      if (raw !== day) {
        refreshRepeatInputs();
      }
    }

    function updateCustomCron(task, value) {
      if (!task) {
        return;
      }
      task.custom_cron = String(value || "");
      syncRecurringCron(task);
      markTaskChanged(task);
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
      markTaskChanged(task);
    }

    function weekdayLabel(value) {
      const item = WEEKDAYS.find((day) => day.value === Number(value));
      return t(item?.labelKey || "todo_weekday_mon");
    }

    function repeatKind(task) {
      return normalizeRepeatKind(task?.repeat_kind);
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

    function schedulePreviewError(task) {
      if (!task) {
        return "";
      }
      if (taskMode(task) === "once") {
        return isValidAtValue(task.at) ? "" : t("todo_validation_at_required");
      }
      return cronPreviewError(task);
    }

    function markedPreviewValue(value) {
      const text = trimText(value);
      return text ? `[[${text}]]` : "";
    }

    function previewLanguage() {
      const locale = currentLocale();
      if (locale.startsWith("zh")) {
        return "zh";
      }
      if (locale.startsWith("ja")) {
        return "ja";
      }
      return "en";
    }

    function parseAtPreviewParts(raw) {
      const match = trimText(raw)
        .replace("T", " ")
        .match(/^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})$/);
      if (!match) {
        return null;
      }
      return {
        month: Number(match[2]),
        day: Number(match[3]),
        hour: Number(match[4]),
        minute: Number(match[5]),
      };
    }

    function previewDateText(parts) {
      if (!parts) {
        return t("todo_schedule_missing");
      }
      switch (previewLanguage()) {
        case "zh":
        case "ja":
          return `${parts.month}${t("todo_preview_month_unit")}${parts.day}${t("todo_preview_day_unit")}`;
        default:
          return new Intl.DateTimeFormat(currentLocale(), {
            month: "short",
            day: "numeric",
            timeZone: "UTC",
          }).format(new Date(Date.UTC(2000, parts.month - 1, parts.day)));
      }
    }

    function previewTimeText(hourRaw, minuteRaw) {
      const hour = Number(hourRaw);
      const minute = Number(minuteRaw);
      if (!Number.isInteger(hour) || !Number.isInteger(minute)) {
        return "";
      }
      switch (previewLanguage()) {
        case "zh":
          return minute === 0 ? `${hour}点` : `${hour}点${minute}分`;
        case "ja":
          return minute === 0 ? `${hour}時` : `${hour}時${minute}分`;
        default: {
          const suffix = hour < 12 ? "AM" : "PM";
          const hour12 = hour % 12 || 12;
          return minute === 0 ? `${hour12} ${suffix}` : `${hour12}:${pad2(minute)} ${suffix}`;
        }
      }
    }

    function previewTimeFromInput(raw) {
      const [hour, minute] = normalizeTimeInput(raw).split(":");
      return previewTimeText(Number(hour), Number(minute));
    }

    function previewWeekdayValues(values) {
      return normalizeWeekdays(values).sort((left, right) => {
        const normalizedLeft = left === 0 ? 7 : left;
        const normalizedRight = right === 0 ? 7 : right;
        return normalizedLeft - normalizedRight;
      });
    }

    function previewWeekdayName(value) {
      const day = Number(value);
      switch (previewLanguage()) {
        case "zh":
          return ["日", "一", "二", "三", "四", "五", "六"][day] || "一";
        case "ja":
          return ["日曜", "月曜", "火曜", "水曜", "木曜", "金曜", "土曜"][day] || "月曜";
        default:
          return ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"][day] || "Monday";
      }
    }

    function joinPreviewList(items) {
      const values = items.map((item) => trimText(item)).filter(Boolean);
      if (values.length <= 1) {
        return values[0] || "";
      }
      const last = values[values.length - 1];
      const head = values.slice(0, -1);
      switch (previewLanguage()) {
        case "zh":
          return `${head.join("、")}和周${last}`;
        case "ja":
          return `${head.join("、")}と${last}`;
        default:
          return values.length === 2 ? `${head[0]} and ${last}` : `${head.join(", ")}, and ${last}`;
      }
    }

    function previewWeekdayText(values) {
      return joinPreviewList(previewWeekdayValues(values).map((day) => previewWeekdayName(day)));
    }

    function previewMonthDayList(days) {
      const values = (Array.isArray(days) ? days : []).map((item) => Number(item)).filter((item) => Number.isInteger(item));
      if (values.length <= 1) {
        return values[0] ? String(values[0]) : "";
      }
      switch (previewLanguage()) {
        case "zh":
          return values.join("、");
        case "ja":
          return joinPreviewList(values.map((item) => `${item}日`));
        default:
          return joinPreviewList(values.map((item) => String(item)));
      }
    }

    function joinGenericPreviewList(items) {
      const values = items.map((item) => trimText(item)).filter(Boolean);
      if (values.length <= 1) {
        return values[0] || "";
      }
      const last = values[values.length - 1];
      const head = values.slice(0, -1);
      switch (previewLanguage()) {
        case "zh":
          return `${head.join("、")}和${last}`;
        case "ja":
          return `${head.join("、")}と${last}`;
        default:
          return values.length === 2 ? `${head[0]} and ${last}` : `${head.join(", ")}, and ${last}`;
      }
    }

    function previewFullWeekdayName(value) {
      const day = Number(value) === 7 ? 0 : Number(value);
      switch (previewLanguage()) {
        case "zh":
          return `周${["日", "一", "二", "三", "四", "五", "六"][day] || "一"}`;
        case "ja":
          return ["日曜", "月曜", "火曜", "水曜", "木曜", "金曜", "土曜"][day] || "月曜";
        default:
          return ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"][day] || "Monday";
      }
    }

    function previewMonthName(value) {
      const month = Number(value);
      if (!Number.isInteger(month) || month < 1 || month > 12) {
        return "";
      }
      switch (previewLanguage()) {
        case "zh":
        case "ja":
          return `${month}${t("todo_preview_month_unit")}`;
        default:
          return new Intl.DateTimeFormat(currentLocale(), {
            month: "long",
            timeZone: "UTC",
          }).format(new Date(Date.UTC(2000, month - 1, 1)));
      }
    }

    function cronFieldValuePreviewText(kind, value) {
      const number = Number(value);
      switch (kind) {
        case "minute":
          switch (previewLanguage()) {
            case "zh":
              return `${number}分`;
            case "ja":
              return `${number}分`;
            default:
              return `minute ${pad2(number)}`;
          }
        case "hour":
          switch (previewLanguage()) {
            case "zh":
              return `${number}点`;
            case "ja":
              return `${number}時`;
            default:
              return `hour ${number}`;
          }
        case "day":
          switch (previewLanguage()) {
            case "zh":
            case "ja":
              return `${number}${t("todo_preview_day_unit")}`;
            default:
              return String(number);
          }
        case "month":
          return previewMonthName(number);
        case "weekday":
          return previewFullWeekdayName(number);
        default:
          return String(number);
      }
    }

    function cronFieldEveryPreviewText(kind) {
      switch (previewLanguage()) {
        case "zh":
          return {
            minute: "每分钟",
            hour: "每小时",
            day: "每日",
            month: "每月",
            weekday: "每天",
          }[kind];
        case "ja":
          return {
            minute: "毎分",
            hour: "毎時",
            day: "毎日",
            month: "毎月",
            weekday: "毎日",
          }[kind];
        default:
          return {
            minute: "every minute",
            hour: "every hour",
            day: "every day of month",
            month: "every month",
            weekday: "every day of week",
          }[kind];
      }
    }

    function cronFieldRangePreviewText(kind, start, end) {
      const left = cronFieldValuePreviewText(kind, start);
      const right = cronFieldValuePreviewText(kind, end);
      switch (previewLanguage()) {
        case "zh":
          return `${left}到${right}`;
        case "ja":
          return `${left}から${right}`;
        default:
          return `${left} through ${right}`;
      }
    }

    function cronStepUnit(kind, count) {
      switch (previewLanguage()) {
        case "zh":
          return {
            minute: "分钟",
            hour: "小时",
            day: "天",
            month: "个月",
            weekday: "天",
          }[kind];
        case "ja":
          return {
            minute: "分",
            hour: "時間",
            day: "日",
            month: "か月",
            weekday: "日",
          }[kind];
        default: {
          const units = {
            minute: "minute",
            hour: "hour",
            day: "day",
            month: "month",
            weekday: "day",
          };
          const unit = units[kind] || "unit";
          return count === 1 ? unit : `${unit}s`;
        }
      }
    }

    function cronFieldEveryStepPreviewText(kind, count) {
      switch (previewLanguage()) {
        case "zh":
          return `每 ${count} ${cronStepUnit(kind, count)}`;
        case "ja":
          return `${count}${cronStepUnit(kind, count)}ごと`;
        default:
          return `every ${count} ${cronStepUnit(kind, count)}`;
      }
    }

    function parseCronPreviewToken(raw, min, max) {
      const [baseRaw, stepRaw, extra] = trimText(raw).split("/");
      if (!baseRaw || extra !== undefined) {
        return null;
      }
      const step = stepRaw === undefined ? null : positiveCronStep(stepRaw);
      if (stepRaw !== undefined && step === null) {
        return null;
      }
      if (baseRaw === "*") {
        return { type: "any", step };
      }
      if (baseRaw.includes("-")) {
        const [startRaw, endRaw, rangeExtra] = baseRaw.split("-");
        if (rangeExtra !== undefined) {
          return null;
        }
        const start = parseSingleCronNumber(startRaw, min, max);
        const end = parseSingleCronNumber(endRaw, min, max);
        if (start === null || end === null || start > end) {
          return null;
        }
        return { type: "range", start, end, step };
      }
      const value = parseSingleCronNumber(baseRaw, min, max);
      return value === null ? null : { type: "single", value, step };
    }

    function cronTokenBasePreviewText(token, kind) {
      if (!token) {
        return "";
      }
      if (token.type === "any") {
        return cronFieldEveryPreviewText(kind);
      }
      if (token.type === "range") {
        return cronFieldRangePreviewText(kind, token.start, token.end);
      }
      return cronFieldValuePreviewText(kind, token.value);
    }

    function cronTokenPreviewText(token, kind) {
      if (!token) {
        return "";
      }
      if (!token.step) {
        return cronTokenBasePreviewText(token, kind);
      }
      if (token.type === "any") {
        return cronFieldEveryStepPreviewText(kind, token.step);
      }
      const base = cronTokenBasePreviewText(token, kind);
      switch (previewLanguage()) {
        case "zh":
          return `${base}中${cronFieldEveryStepPreviewText(kind, token.step)}`;
        case "ja":
          return `${base}のうち${cronFieldEveryStepPreviewText(kind, token.step)}`;
        default:
          return `${cronFieldEveryStepPreviewText(kind, token.step)} from ${base}`;
      }
    }

    function cronFieldPreviewText(raw, kind, min, max) {
      const values = trimText(raw)
        .split(",")
        .map((item) => cronTokenPreviewText(parseCronPreviewToken(item, min, max), kind))
        .filter(Boolean);
      return values.length > 0 ? joinGenericPreviewList(values) : trimText(raw);
    }

    function customCronTimePreviewText(minuteRaw, hourRaw) {
      const minute = parseSingleCronNumber(minuteRaw, 0, 59);
      const hour = parseSingleCronNumber(hourRaw, 0, 23);
      if (minute !== null && hour !== null) {
        const time = previewTimeText(hour, minute);
        return previewLanguage() === "en" ? `At ${time}` : time;
      }
      if (minute !== null && hourRaw === "*") {
        return t("todo_preview_schedule_hourly", { minute: pad2(minute) });
      }
      const minuteText = cronFieldPreviewText(minuteRaw, "minute", 0, 59);
      const hourText = cronFieldPreviewText(hourRaw, "hour", 0, 23);
      switch (previewLanguage()) {
        case "zh":
          return `${hourText}的${minuteText}`;
        case "ja":
          return `${hourText}の${minuteText}`;
        default:
          return `${minuteText} during ${hourText}`;
      }
    }

    function customCronConstraintText(kind, raw) {
      const ranges = {
        day: [1, 31],
        month: [1, 12],
        weekday: [0, 7],
      };
      const [min, max] = ranges[kind] || [0, 0];
      const value = cronFieldPreviewText(raw, kind, min, max);
      switch (previewLanguage()) {
        case "zh":
          return {
            day: `日期为${value}`,
            month: `月份为${value}`,
            weekday: `星期为${value}`,
          }[kind];
        case "ja":
          return {
            day: `日付が${value}`,
            month: `月が${value}`,
            weekday: `曜日が${value}`,
          }[kind];
        default:
          return {
            day: `day of month is ${value}`,
            month: `month is ${value}`,
            weekday: `weekday is ${value}`,
          }[kind];
      }
    }

    function customCronGenericSchedulePreview(parts) {
      const [minuteRaw, hourRaw, domRaw, monthRaw, dowRaw] = parts;
      const time = customCronTimePreviewText(minuteRaw, hourRaw);
      const constraints = [];
      if (domRaw !== "*") {
        constraints.push(customCronConstraintText("day", domRaw));
      }
      if (monthRaw !== "*") {
        constraints.push(customCronConstraintText("month", monthRaw));
      }
      if (dowRaw !== "*") {
        constraints.push(customCronConstraintText("weekday", dowRaw));
      }
      if (constraints.length === 0) {
        return time;
      }
      const constraintText = joinGenericPreviewList(constraints);
      switch (previewLanguage()) {
        case "zh":
          return `${time}，${constraintText}时`;
        case "ja":
          return `${time}、${constraintText}のとき`;
        default:
          return `${time} when ${constraintText}`;
      }
    }

    function previewMonthlySchedule(day, time) {
      return t("todo_preview_schedule_monthly", { day, time });
    }

    function customCronSchedulePreview(task) {
      const cron = recurringCron(task);
      const parts = parseCronParts(cron);
      if (!parts || !isValidCronExpression(cron)) {
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
        return t("todo_preview_schedule_hourly", { minute: pad2(minute) });
      }
      if (minute !== null && hour !== null) {
        const time = previewTimeText(hour, minute);
        if (domRaw === "*" && monthRaw === "*" && dowRaw === "*") {
          return t("todo_preview_schedule_daily", { time });
        }
        if (domRaw === "*" && monthRaw === "*") {
          const days = parseCronNumberSet(dowRaw, 0, 7, true);
          if (days) {
            return t("todo_preview_schedule_weekly", { days: previewWeekdayText(days), time });
          }
        }
        if (monthRaw === "*" && dowRaw === "*") {
          const day = parseSingleCronNumber(domRaw, 1, 31);
          if (day !== null) {
            return previewMonthlySchedule(day, time);
          }
          const days = parseCronNumberSet(domRaw, 1, 31, false);
          if (days) {
            return t("todo_preview_schedule_month_days", { days: previewMonthDayList(days), time });
          }
        }
      }
      return customCronGenericSchedulePreview(parts);
    }

    function naturalSchedulePreview(task) {
      if (taskMode(task) === "once") {
        const parts = parseAtPreviewParts(task?.at);
        return t("todo_preview_schedule_once", {
          date: previewDateText(parts),
          time: parts ? previewTimeText(parts.hour, parts.minute) : "",
        });
      }
      const kind = repeatKind(task);
      if (kind === "custom") {
        return customCronSchedulePreview(task);
      }
      const time = previewTimeFromInput(task?.repeat_time);
      switch (kind) {
        case "hourly": {
          const [, minute] = normalizeTimeInput(task?.repeat_time).split(":");
          return t("todo_preview_schedule_hourly", { minute });
        }
        case "weekly":
          return t("todo_preview_schedule_weekly", { days: previewWeekdayText(task?.repeat_weekdays), time });
        case "monthly":
          return previewMonthlySchedule(normalizeMonthDay(task?.repeat_month_day), time);
        case "daily":
        default:
          return t("todo_preview_schedule_daily", { time });
      }
    }

    function schedulePreviewText(task) {
      return schedulePreviewError(task) || naturalSchedulePreview(task);
    }

    function previewPeopleText(task) {
      const refs = contactReferencesFromContent(task?.content);
      if (refs.length === 0) {
        return "";
      }
      return refs.map((item) => item.label).filter(Boolean).join(t("todo_preview_people_separator"));
    }

    function previewSentence(task) {
      const title = markedPreviewValue(taskTitle(task));
      const error = schedulePreviewError(task);
      if (error) {
        return t("todo_preview_error_summary", {
          title,
          error: markedPreviewValue(error),
        });
      }
      const people = previewPeopleText(task);
      return t(people ? "todo_preview_natural_with_people" : "todo_preview_natural", {
        schedule: markedPreviewValue(schedulePreviewText(task)),
        title,
        people: markedPreviewValue(people),
      });
    }

    function previewSegments(task) {
      const source = previewSentence(task);
      const parts = [];
      const re = /\[\[([\s\S]*?)\]\]/g;
      let offset = 0;
      let match = re.exec(source);
      while (match) {
        if (match.index > offset) {
          parts.push({ type: "text", text: source.slice(offset, match.index) });
        }
        if (match[1]) {
          parts.push({ type: "mark", text: match[1] });
        }
        offset = match.index + match[0].length;
        match = re.exec(source);
      }
      if (offset < source.length) {
        parts.push({ type: "text", text: source.slice(offset) });
      }
      return parts.length > 0 ? parts : [{ type: "text", text: source }];
    }

    function taskPreviewClass(task) {
      return schedulePreviewError(task) ? "todo-task-preview is-error" : "todo-task-preview";
    }

    function taskValidationMessage(task) {
      if (!task) {
        return "";
      }
      if (!trimText(task.content)) {
        return t("todo_validation_content_required");
      }
      if (taskMode(task) === "once") {
        if (!isValidAtValue(task.at)) {
          return t("todo_validation_at_required");
        }
      } else {
        const cronMessage = cronValidationMessage(recurringCron(task));
        if (cronMessage) {
          return cronMessage;
        }
      }
      return bashEnvValidationMessage(task.bash_env, t);
    }

    function ensureBashEnv(task) {
      if (!task) {
        return [];
      }
      if (!Array.isArray(task.bash_env)) {
        task.bash_env = [];
      }
      return task.bash_env;
    }

    function hasBashEnvRows(task) {
      return Array.isArray(task?.bash_env) && task.bash_env.length > 0;
    }

    function addBashEnvRow(task) {
      if (!task) {
        return;
      }
      ensureBashEnv(task).push({ name: "", value: "" });
      markTaskChanged(task);
    }

    function removeBashEnvRow(task, index) {
      if (!task) {
        return;
      }
      const rows = ensureBashEnv(task);
      if (index < 0 || index >= rows.length) {
        return;
      }
      rows.splice(index, 1);
      markTaskChanged(task);
    }

    function updateBashEnvField(task, index, field, value) {
      if (!task || (field !== "name" && field !== "value")) {
        return;
      }
      const rows = ensureBashEnv(task);
      if (index < 0 || index >= rows.length) {
        return;
      }
      rows[index][field] = String(value ?? "");
      markTaskChanged(task);
    }

    function visibleTaskValidationMessage(task) {
      const message = taskValidationMessage(task);
      return message === t("todo_validation_content_required") ? "" : message;
    }

    async function loadHeartbeat() {
      heartbeatLoading.value = true;
      try {
        const data = await runtimeApiFetch(`/state/files/${encodeURIComponent(HEARTBEAT_FILE_NAME)}`);
        heartbeatContent.value = String(data.content || "");
        heartbeatMissing.value = false;
        heartbeatDirty.value = false;
      } catch (e) {
        if (e && e.status === 404) {
          heartbeatContent.value = "";
          heartbeatMissing.value = true;
          heartbeatDirty.value = false;
          return;
        }
        toast.error(e.message || t("msg_read_failed"));
      } finally {
        heartbeatLoading.value = false;
      }
    }

    function onHeartbeatContentChange(value) {
      heartbeatContent.value = String(value || "");
      heartbeatDirty.value = true;
    }

    async function saveHeartbeat() {
      if (!canSaveHeartbeat.value) {
        return;
      }
      heartbeatSaving.value = true;
      try {
        await runtimeApiFetch(`/state/files/${encodeURIComponent(HEARTBEAT_FILE_NAME)}`, {
          method: "PUT",
          body: { content: heartbeatContent.value },
        });
        heartbeatMissing.value = false;
        heartbeatDirty.value = false;
        invalidateConsoleSetupReadiness();
        toast.success(t("msg_save_success"));
      } catch (e) {
        toast.error(e.message || t("msg_save_failed"));
      } finally {
        heartbeatSaving.value = false;
      }
    }

    async function load() {
      loading.value = true;
      try {
        const data = await runtimeApiFetch("/todo/tasks");
        const rows = Array.isArray(data.tasks) ? data.tasks : [];
        const systemRows = Array.isArray(data.system_tasks) ? data.system_tasks : [];
        const heartbeatRow = systemRows.find((item) => trimText(item?.id) === HEARTBEAT_ITEM_KEY);
        chatOptions.value = normalizeChatOptions(data.chat_options);
        llmProfiles.value = normalizeLLMProfiles(data.llm_profiles);
        tasks.value = rows.map((item) => normalizeTask(item, t("todo_untitled")));
        heartbeatEnabled.value = data.heartbeat_enabled !== false;
        heartbeatTask.value = heartbeatRow ? normalizeTask(heartbeatRow, t("todo_heartbeat_title")) : null;
        selectedTaskKey.value = "";
        selectedTaskDraft.value = null;
        draftDirty.value = false;
        tasksDirty.value = false;
        mobileEditorVisible.value = false;
      } catch (e) {
        const message = e.message || t("msg_load_failed");
        toast.error(message);
      } finally {
        loading.value = false;
      }
    }

    async function saveTasks() {
      if (!canSaveTasks.value) {
        return;
      }
      commitSelectedTaskDraft();
      const validationMessages = tasks.value.map((task) => taskValidationMessage(task)).filter(Boolean);
      if (validationMessages.length > 0) {
        toast.error(validationMessages[0]);
        return;
      }
      const nextTasks = tasks.value.map((task) => cloneTaskForDraft(task));
      nextTasks.forEach(normalizeTaskBeforeSave);
      tasks.value = nextTasks;
      refreshRepeatInputs();
      saving.value = true;
      try {
        const data = await runtimeApiFetch("/todo/tasks", {
          method: "PUT",
          body: { tasks: nextTasks.map((task) => serializeTask(task, t("todo_untitled"))) },
        });
        chatOptions.value = normalizeChatOptions(data.chat_options);
        llmProfiles.value = normalizeLLMProfiles(data.llm_profiles);
        tasksDirty.value = false;
        selectedTaskDraft.value = cloneTaskForDraft(selectedStoredTask.value);
        draftDirty.value = false;
        toast.success(t("msg_save_success"));
      } catch (e) {
        const message = e.message || t("msg_save_failed");
        toast.error(message);
      } finally {
        saving.value = false;
      }
    }

    async function runSelectedTaskNow() {
      if (!canRunSelectedTask.value || !selectedStoredTask.value) {
        return;
      }
      const task = selectedStoredTask.value;
      const id = trimText(task.id);
      if (!id) {
        return;
      }
      runningTaskKey.value = task._key;
      try {
        await runtimeApiFetch(`/todo/tasks/${encodeURIComponent(id)}/run`, {
          method: "POST",
        });
        toast.success(t("todo_run_success"));
      } catch (e) {
        toast.error(e.message || t("todo_run_failed"));
      } finally {
        runningTaskKey.value = "";
      }
    }

    async function save() {
      if (heartbeatSelected.value) {
        await saveHeartbeat();
        return;
      }
      await saveTasks();
    }

    onMounted(() => {
      window.addEventListener("resize", refreshMobileMode);
      refreshMobileMode();
      void load();
      void loadHeartbeat();
    });
    onUnmounted(() => {
      window.removeEventListener("resize", refreshMobileMode);
    });

    return {
      t,
      loading,
      saving,
      heartbeatLoading,
      heartbeatSaving,
      heartbeatSelected,
      heartbeatContent,
      heartbeatIndexMeta,
      heartbeatEditorMeta,
      heartbeatDisabled,
      tasks,
      selectedTask,
      selectedTaskKey,
      canSave,
      canRunSelectedTask,
      runningTaskKey,
      showIndexPane,
      showEditorPane,
      mobileShowBack,
      mobileBarTitle,
      pageClass,
      taskActionMenuItems,
      deleteDialogOpen,
      deleteDialogText,
      deleteDialogActions,
      WEEKDAYS,
      addTask,
      confirmDeleteSelectedTask,
      moveSelectedTask,
      updateTaskField,
      updateTodoTitle,
      updateTaskEnabled,
      updateScheduleMode,
      updateAtInput,
      updateRepeatKindFromTab,
      updateRepeatTime,
      updateRepeatMinute,
      updateRepeatMonthDay,
      updateCustomCron,
      updateTimezone,
      updateChatFromItem,
      updateLLMProfileFromItem,
      updateScheduleFromItem,
      toggleRepeatWeekday,
      atInputValue,
      taskTitle,
      scheduleLabel,
      taskClass,
      heartbeatClass,
      taskMode,
      repeatKind,
      repeatKindTabs,
      repeatKindTab,
      repeatKindInitialIndex,
      repeatInputRevision,
      chatDropdownRevision,
      repeatMinuteInputKey,
      repeatMonthDayInputKey,
      repeatMinuteValue,
      taskListDisplayTask,
      guardRepeatKindTabsEvent,
      weekdaySelected,
      weekdayLabel,
      previewSegments,
      taskPreviewClass,
      recurringCron,
      hasBashEnvRows,
      addBashEnvRow,
      removeBashEnvRow,
      updateBashEnvField,
      timezoneBaseItems,
      timezoneItem,
      chatMenuItems,
      chatItem,
      llmProfileMenuItems,
      llmProfileItem,
      scheduleModeItems,
      scheduleModeItem,
      selectHeartbeat,
      onHeartbeatContentChange,
      selectTask,
      showIndexView,
      load,
      runSelectedTaskNow,
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

          <div class="todo-index-body">
            <QProgress v-if="loading || heartbeatLoading" :infinite="true" />

            <section class="todo-index-group todo-heartbeat-group">
              <div class="todo-index-items workspace-sidebar-list">
                <button
                  type="button"
                  :class="heartbeatClass()"
                  :aria-pressed="heartbeatSelected"
                  @click="selectHeartbeat"
                >
                  <span class="workspace-sidebar-item-copy">
                    <span class="todo-index-item-name workspace-sidebar-item-title">{{ t("todo_heartbeat_title") }}</span>
                    <span class="todo-index-item-meta workspace-sidebar-item-meta">
                      <span class="todo-index-schedule">{{ heartbeatIndexMeta }}</span>
                    </span>
                  </span>
                  <span class="todo-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                    <QBadge v-if="heartbeatSelected" dot type="primary" size="sm" />
                  </span>
                </button>
              </div>
            </section>

            <section class="todo-index-group todo-task-group">
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
                    <span class="todo-index-item-name workspace-sidebar-item-title">{{ taskTitle(taskListDisplayTask(task)) }}</span>
                    <span class="todo-index-item-meta workspace-sidebar-item-meta">
                      <span class="todo-index-schedule">{{ scheduleLabel(taskListDisplayTask(task)) }}</span>
                    </span>
                  </span>
                  <span class="todo-index-item-marker workspace-sidebar-item-marker" aria-hidden="true">
                    <QBadge v-if="task._key === selectedTaskKey" dot type="primary" size="sm" />
                  </span>
                </div>
              </div>

              <p v-else-if="!loading" class="todo-empty-list">{{ t("todo_empty_list") }}</p>
            </section>
          </div>
        </aside>

        <QCard v-if="showEditorPane && heartbeatSelected" class="todo-editor-card todo-heartbeat-editor-card" variant="default">
          <div class="todo-editor-shell todo-heartbeat-editor-shell">
            <header class="todo-editor-head">
              <div class="todo-editor-copy">
                <h3 class="todo-editor-document-title workspace-document-title">{{ t("todo_heartbeat_title") }}</h3>
                <p class="todo-editor-meta">{{ heartbeatEditorMeta }}</p>
              </div>
              <div class="todo-editor-actions">
                <QButton class="primary" :disabled="!canSave" :loading="heartbeatSaving" @click="save">
                  {{ t("action_save") }}
                </QButton>
              </div>
            </header>

            <div class="todo-heartbeat-editor-body">
              <div v-if="heartbeatLoading || heartbeatDisabled" class="todo-heartbeat-editor-notices">
                <QProgress v-if="heartbeatLoading" :infinite="true" />
                <QFence
                  v-if="heartbeatDisabled"
                  type="warning"
                  :text="t('todo_heartbeat_disabled_hint')"
                />
              </div>
              <div class="todo-heartbeat-editor-frame">
                <AppMarkdownEditor
                  :modelValue="heartbeatContent"
                  height="100%"
                  :disabled="heartbeatLoading || heartbeatSaving"
                  :placeholder="t('todo_heartbeat_placeholder')"
                  :aria-label="t('todo_heartbeat_title')"
                  @update:modelValue="onHeartbeatContentChange"
                />
              </div>
            </div>
          </div>
        </QCard>

        <QCard v-else-if="showEditorPane && selectedTask" class="todo-editor-card" variant="default">
          <div class="todo-editor-shell">
            <header class="todo-editor-head todo-task-editor-head">
              <div class="todo-editor-toolbar">
                <div class="todo-enabled-control">
                  <QSwitch
                    :modelValue="selectedTask.enabled !== false"
                    :disabled="saving || loading"
                    :title="t('todo_field_enabled')"
                    :aria-label="t('todo_field_enabled')"
                    @update:modelValue="updateTaskEnabled(selectedTask, $event)"
                  />
                </div>
                <div class="todo-editor-actions">
                  <QButton class="primary" :disabled="!canSave" :loading="saving" @click="save">
                    {{ t("action_save") }}
                  </QButton>
                  <QDropdownMenu
                    class="todo-task-actions-menu"
                    :items="taskActionMenuItems"
                    hideSelected
                    hideActionLabel
                    :disabled="saving || loading"
                    :loading="runningTaskKey === selectedTask._key"
                  >
                    <svg
                      class="todo-task-actions-menu-icon"
                      viewBox="0 0 16 16"
                      aria-hidden="true"
                      focusable="false"
                    >
                      <circle cx="3" cy="8" r="1.25" fill="currentColor" />
                      <circle cx="8" cy="8" r="1.25" fill="currentColor" />
                      <circle cx="13" cy="8" r="1.25" fill="currentColor" />
                    </svg>
                    <span class="todo-task-actions-menu-accessible">{{ t("todo_action_more") }}</span>
                  </QDropdownMenu>
                </div>
              </div>
              <label class="todo-editor-title-field">
                <QInput
                  class="todo-title-input"
                  :modelValue="selectedTask.title"
                  :placeholder="t('todo_untitled')"
                  :aria-label="t('todo_field_title')"
                  :disabled="saving || loading"
                  @update:modelValue="updateTodoTitle(selectedTask, $event)"
                />
              </label>
            </header>

            <div class="todo-form">
              <div class="todo-field is-wide todo-content-field">
                <AppMarkdownEditor
                  class="todo-content-markdown-editor"
                  :modelValue="selectedTask.content"
                  height="360px"
                  :placeholder="t('todo_content_placeholder')"
                  :aria-label="t('todo_field_content')"
                  :disabled="saving || loading"
                  @update:modelValue="updateTaskField(selectedTask, 'content', $event)"
                />
              </div>

              <div class="todo-compact-fields">
                <div class="todo-field">
                  <QDropdownMenu
                    :key="'timezone-' + selectedTask._key + '-' + selectedTask.tz"
                    class="todo-dropdown"
                    :items="timezoneBaseItems"
                    :initialItem="timezoneItem(selectedTask)"
                    :placeholder="t('todo_timezone_placeholder')"
                    use-filter
                    scroll-height="400px"
                    use-dialog="always"
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

                <div class="todo-field">
                  <QDropdownMenu
                    :key="'llm-profile-' + selectedTask._key + '-' + selectedTask.llm_profile"
                    class="todo-dropdown todo-dropdown-hide-selected-media"
                    :items="llmProfileMenuItems"
                    :initialItem="llmProfileItem(selectedTask)"
                    :placeholder="t('todo_llm_profile_placeholder')"
                    use-filter
                    use-dialog="always"
                    :disabled="saving || loading"
                    @change="updateLLMProfileFromItem(selectedTask, $event)"
                  >
                    <template #prepend>
                      <span class="todo-control-prepend">{{ t("todo_field_llm_profile") }}</span>
                    </template>
                  </QDropdownMenu>
                </div>

                <div class="todo-field">
                  <QDropdownMenu
                    :key="'chat-' + selectedTask._key + '-' + selectedTask.chat_id + '-' + chatDropdownRevision"
                    class="todo-dropdown todo-dropdown-hide-selected-media"
                    :items="chatMenuItems"
                    :initialItem="chatItem(selectedTask)"
                    :placeholder="t('todo_chat_placeholder')"
                    use-filter
                    use-dialog="always"
                    :disabled="saving || loading"
                    @change="updateChatFromItem(selectedTask, $event)"
                  >
                    <template #prepend>
                      <span class="todo-control-prepend">{{ t("todo_field_chat") }}</span>
                    </template>
                  </QDropdownMenu>
                </div>
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
              </label>

              <div v-else class="todo-field is-wide todo-repeat-field">
                <QTabs
                  :class="saving || loading ? 'todo-repeat-kind-tabs is-disabled' : 'todo-repeat-kind-tabs'"
                  :tabs="repeatKindTabs"
                  :modelValue="repeatKindTab(selectedTask)"
                  :initialIndex="repeatKindInitialIndex(selectedTask)"
                  variant="plain"
                  :aria-label="t('todo_field_repeat')"
                  :aria-disabled="saving || loading"
                  @click.capture="guardRepeatKindTabsEvent"
                  @keydown.enter.capture="guardRepeatKindTabsEvent"
                  @keydown.space.capture="guardRepeatKindTabsEvent"
                  @change="updateRepeatKindFromTab(selectedTask, $event)"
                />

                <div
                  v-if="repeatKind(selectedTask) !== 'custom'"
                  :class="repeatKind(selectedTask) === 'weekly' ? 'todo-repeat-controls is-weekly' : 'todo-repeat-controls'"
                >
                  <label v-if="repeatKind(selectedTask) === 'hourly'" class="todo-field todo-repeat-minute">
                    <QInput
                      :key="repeatMinuteInputKey(selectedTask)"
                      class="todo-minute-input"
                      :modelValue="repeatMinuteValue(selectedTask)"
                      inputType="number"
                      min="0"
                      step="1"
                      :aria-label="t('todo_field_minute')"
                      :disabled="saving || loading"
                      @update:modelValue="updateRepeatMinute(selectedTask, $event)"
                    >
                      <template #prepend>
                        <span class="todo-control-prepend todo-input-prepend">{{ t("todo_field_minute") }}</span>
                      </template>
                    </QInput>
                  </label>

                  <label v-else class="todo-field todo-repeat-time">
                    <QInput
                      :modelValue="selectedTask.repeat_time"
                      inputType="time"
                      :aria-label="t('todo_field_time')"
                      :disabled="saving || loading"
                      @update:modelValue="updateRepeatTime(selectedTask, $event)"
                    >
                      <template #prepend>
                        <span class="todo-control-prepend todo-input-prepend">{{ t("todo_field_time") }}</span>
                      </template>
                    </QInput>
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
                      :key="repeatMonthDayInputKey(selectedTask)"
                      class="todo-month-day-input"
                      :modelValue="selectedTask.repeat_month_day"
                      inputType="number"
                      min="1"
                      step="1"
                      :aria-label="t('todo_field_month_day')"
                      :disabled="saving || loading"
                      @update:modelValue="updateRepeatMonthDay(selectedTask, $event)"
                    >
                      <template #prepend>
                        <span class="todo-control-prepend todo-input-prepend">{{ t("todo_field_day") }}</span>
                      </template>
                    </QInput>
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

              </div>

              <div :class="taskPreviewClass(selectedTask)">
                <span class="todo-task-preview-label">{{ t("todo_repeat_preview") }}</span>
                <span class="todo-task-preview-text">
                  <span
                    v-for="(part, index) in previewSegments(selectedTask)"
                    :key="index"
                    :class="part.type === 'mark' ? 'todo-preview-mark' : 'todo-preview-plain'"
                  >{{ part.text }}</span>
                </span>
              </div>

              <div class="todo-field is-wide todo-bash-env-field">
                <div class="todo-bash-env-head">
                  <span class="todo-task-preview-label">{{ t("todo_field_bash_env") }}</span>
                  <span class="todo-field-note">{{ t("todo_bash_env_hint") }}</span>
                </div>
                <div v-if="hasBashEnvRows(selectedTask)" class="todo-bash-env-rows">
                  <div
                    v-for="(row, index) in selectedTask.bash_env"
                    :key="'bash-env-' + selectedTask._key + '-' + index"
                    class="todo-bash-env-row"
                  >
                    <label class="todo-field todo-bash-env-name">
                      <QInput
                        :modelValue="row.name"
                        :placeholder="t('todo_bash_env_name')"
                        :aria-label="t('todo_bash_env_name')"
                        :disabled="saving || loading"
                        @update:modelValue="updateBashEnvField(selectedTask, index, 'name', $event)"
                      />
                    </label>
                    <label class="todo-field todo-bash-env-value">
                      <QInput
                        :modelValue="row.value"
                        :placeholder="t('todo_bash_env_value')"
                        :aria-label="t('todo_bash_env_value')"
                        :disabled="saving || loading"
                        @update:modelValue="updateBashEnvField(selectedTask, index, 'value', $event)"
                      />
                    </label>
                    <QButton
                      type="button"
                      class="plain icon todo-bash-env-remove"
                      :title="t('action_delete')"
                      :aria-label="t('action_delete')"
                      :disabled="saving || loading"
                      @click="removeBashEnvRow(selectedTask, index)"
                    >
                      <QIconTrash class="icon" />
                    </QButton>
                  </div>
                </div>
                <QButton
                  type="button"
                  class="placeholder sm todo-bash-env-add"
                  :disabled="saving || loading"
                  @click="addBashEnvRow(selectedTask)"
                >
                  <QIconPlus class="icon" />
                  {{ t("todo_bash_env_add") }}
                </QButton>
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
              class="primary todo-placeholder-add"
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
