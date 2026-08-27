import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import "./TodoCalendar.css";

import { currentLocale, translate } from "../core/context";
import { projectTodoCalendar, visibleCalendarDays } from "../core/todo-calendar";

const MONTH_PATTERN = /^(\d{4})-(\d{2})$/;
const DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/;

function pad2(value) {
  return String(value).padStart(2, "0");
}

function dateKey(date) {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`;
}

function monthKey(date) {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}`;
}

function monthDate(value) {
  const match = String(value || "").match(MONTH_PATTERN);
  if (!match) {
    return null;
  }
  const year = Number(match[1]);
  const month = Number(match[2]);
  if (year < 1 || month < 1 || month > 12) {
    return null;
  }
  const date = new Date(year, month - 1, 1);
  return date.getFullYear() === year && date.getMonth() === month - 1 ? date : null;
}

function dateFromKey(value) {
  const match = String(value || "").match(DATE_PATTERN);
  if (!match) {
    return null;
  }
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
}

function addDays(date, count) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + count);
}

function browserTimezone() {
  try {
    return String(Intl.DateTimeFormat().resolvedOptions().timeZone || "").trim() || "UTC";
  } catch {
    return "UTC";
  }
}

const TodoCalendar = {
  props: {
    tasks: { type: Array, default: () => [] },
    tasksLoading: { type: Boolean, default: false },
    taskTitle: { type: Function, required: true },
    scheduleLabel: { type: Function, required: true },
  },
  emits: ["add-task", "select-date", "select-task", "show-list"],
  setup(props, { emit }) {
    const t = translate;
    const route = useRoute();
    const router = useRouter();
    const timezone = browserTimezone();
    const selectedDate = ref("");
    const expandedDate = ref("");

    const activeMonth = computed(() => monthDate(route.query.month) || new Date(new Date().getFullYear(), new Date().getMonth(), 1));
    const activeMonthKey = computed(() => monthKey(activeMonth.value));
    const days = computed(() => visibleCalendarDays(activeMonth.value));
    const projection = computed(() => {
      const from = dateFromKey(days.value[0]?.key) || activeMonth.value;
      return projectTodoCalendar(props.tasks, from, addDays(from, 42), timezone);
    });
    const monthTitle = computed(() =>
      new Intl.DateTimeFormat(currentLocale(), { year: "numeric", month: "long" }).format(activeMonth.value)
    );
    const weekdayLabels = computed(() => [
      t("todo_weekday_sun"),
      t("todo_weekday_mon"),
      t("todo_weekday_tue"),
      t("todo_weekday_wed"),
      t("todo_weekday_thu"),
      t("todo_weekday_fri"),
      t("todo_weekday_sat"),
    ]);
    const viewTabs = computed(() => [
      { id: "list", title: t("todo_view_list") },
      { id: "calendar", title: t("todo_view_calendar") },
    ]);
    const selectedViewTab = computed(() => viewTabs.value[1]);
    const taskByID = computed(() => {
      const result = new Map();
      props.tasks.forEach((task) => {
        const id = String(task?.id || "").trim();
        if (id && !result.has(id)) {
          result.set(id, task);
        }
      });
      return result;
    });
    const entriesByDate = computed(() => {
      const grouped = new Map();
      projection.value.items.forEach((item) => {
        const task = taskByID.value.get(String(item?.task_id || "").trim());
        const date = String(item?.date || "").trim();
        if (!task || !DATE_PATTERN.test(date)) {
          return;
        }
        const entry = { ...item, task };
        if (!grouped.has(date)) {
          grouped.set(date, []);
        }
        grouped.get(date).push(entry);
      });
      return grouped;
    });
    const selectedDateEntries = computed(() => entriesByDate.value.get(selectedDate.value) || []);
    const selectedDateTitle = computed(() => {
      const date = dateFromKey(selectedDate.value);
      return date
        ? new Intl.DateTimeFormat(currentLocale(), { year: "numeric", month: "long", day: "numeric" }).format(date)
        : selectedDate.value;
    });
    const isEmpty = computed(() => !props.tasksLoading && projection.value.items.length === 0);

    function entriesFor(day) {
      return entriesByDate.value.get(day.key) || [];
    }

    function visibleEntries(day) {
      return entriesFor(day).slice(0, 3);
    }

    function taskIsRecurring(task) {
      return !String(task?.at || "").trim();
    }

    function entryTime(entry) {
      const date = new Date(entry?.first_at);
      if (Number.isNaN(date.getTime())) {
        return "";
      }
      return new Intl.DateTimeFormat(currentLocale(), {
        hour: "2-digit",
        minute: "2-digit",
        timeZone: timezone,
      }).format(date);
    }

    function entryMeta(entry) {
      return taskIsRecurring(entry?.task) ? props.scheduleLabel(entry.task) : entryTime(entry);
    }

    function entryClass(entry) {
      return ["todo-calendar-entry", { "is-disabled": entry?.task?.enabled === false }];
    }

    function entryAccessibleName(entry) {
      const status = entry?.task?.enabled === false ? t("todo_calendar_disabled") : t("todo_calendar_enabled");
      const kind = taskIsRecurring(entry?.task) ? t("todo_calendar_recurring") : t("todo_calendar_once");
      return `${entry?.date || ""}, ${entryMeta(entry)}, ${props.taskTitle(entry?.task)}, ${kind}, ${status}`;
    }

    function emitDateSelection(activate) {
      emit("select-date", {
        date: selectedDate.value,
        title: selectedDateTitle.value,
        task_ids: selectedDateEntries.value.map((entry) => entry.task_id),
        activate,
      });
    }

    function selectDate(day) {
      selectedDate.value = day.key;
      expandedDate.value = "";
      emitDateSelection(true);
    }

    function selectEntry(entry) {
      selectedDate.value = entry.date;
      expandedDate.value = "";
      emitDateSelection(false);
      emit("select-task", entry.task);
    }

    function toggleOverflow(day) {
      selectedDate.value = day.key;
      expandedDate.value = expandedDate.value === day.key ? "" : day.key;
      emitDateSelection(true);
    }

    function setMonth(delta) {
      const next = new Date(activeMonth.value.getFullYear(), activeMonth.value.getMonth() + delta, 1);
      expandedDate.value = "";
      void router.push({
        query: { ...route.query, view: "calendar", month: monthKey(next) },
      });
    }

    function showToday() {
      const today = new Date();
      selectedDate.value = dateKey(today);
      expandedDate.value = "";
      emitDateSelection(true);
      void router.push({
        query: { ...route.query, view: "calendar", month: monthKey(today) },
      });
    }

    function onViewChange(detail) {
      if (detail?.tab?.id === "list") {
        emit("show-list");
      }
    }

    watch(
      activeMonthKey,
      () => {
        const today = dateKey(new Date());
        selectedDate.value = days.value.some((day) => day.key === today)
          ? today
          : `${activeMonthKey.value}-01`;
        expandedDate.value = "";
        emitDateSelection(true);
      },
      { immediate: true }
    );

    watch(selectedDateEntries, () => emitDateSelection(false));

    return {
      t,
      days,
      monthTitle,
      weekdayLabels,
      viewTabs,
      selectedViewTab,
      selectedDate,
      selectedDateTitle,
      selectedDateEntries,
      expandedDate,
      isEmpty,
      entriesFor,
      visibleEntries,
      taskIsRecurring,
      entryTime,
      entryMeta,
      entryClass,
      entryAccessibleName,
      selectDate,
      selectEntry,
      toggleOverflow,
      setMonth,
      showToday,
      onViewChange,
    };
  },
  template: `
    <section class="todo-calendar" :aria-label="t('todo_view_calendar')">
      <header class="todo-calendar-toolbar">
        <div class="todo-calendar-toolbar-primary">
          <QTabs
            class="todo-view-tabs todo-calendar-view-tabs"
            :tabs="viewTabs"
            :modelValue="selectedViewTab"
            :initialIndex="1"
            variant="plain"
            @change="onViewChange"
          />
          <QButton
            class="plain sm icon todo-calendar-add"
            :title="t('todo_action_add')"
            :aria-label="t('todo_action_add')"
            @click="$emit('add-task')"
          >
            <QIconPlus class="icon" />
          </QButton>
        </div>

        <div class="todo-calendar-month-nav">
          <QButton class="plain sm icon" :title="t('todo_calendar_previous')" :aria-label="t('todo_calendar_previous')" @click="setMonth(-1)">
            <QIconArrowLeft class="icon" />
          </QButton>
          <h3 class="todo-calendar-month-title">{{ monthTitle }}</h3>
          <QButton class="plain sm icon" :title="t('todo_calendar_next')" :aria-label="t('todo_calendar_next')" @click="setMonth(1)">
            <QIconArrowRight class="icon" />
          </QButton>
          <QButton class="plain sm todo-calendar-today" @click="showToday">{{ t("todo_calendar_today") }}</QButton>
        </div>
      </header>

      <div class="todo-calendar-weekdays" role="row">
        <span v-for="label in weekdayLabels" :key="label" role="columnheader">{{ label }}</span>
      </div>

      <div v-if="tasksLoading" class="todo-calendar-loading" aria-hidden="true">
        <QSkeleton variant="card" height="118px" :count="3" />
      </div>

      <div v-else class="todo-calendar-grid" role="grid">
        <article
          v-for="day in days"
          :key="day.key"
          :class="['todo-calendar-day', { 'is-outside': !day.inMonth, 'is-today': day.isToday, 'is-selected': day.key === selectedDate }]"
          role="gridcell"
          :aria-label="day.key"
          :aria-selected="day.key === selectedDate"
          @click="selectDate(day)"
        >
          <button type="button" class="todo-calendar-date" :aria-pressed="day.key === selectedDate" @click.stop="selectDate(day)">
            <span>{{ day.day }}</span>
          </button>

          <div class="todo-calendar-day-items">
            <button
              v-for="entry in visibleEntries(day)"
              :key="entry.task_id + ':' + entry.date"
              type="button"
              :class="entryClass(entry)"
              :aria-label="entryAccessibleName(entry)"
              @click.stop="selectEntry(entry)"
            >
              <span class="todo-calendar-status" aria-hidden="true"></span>
              <span class="todo-calendar-entry-title">{{ taskTitle(entry.task) }}</span>
              <span class="todo-calendar-entry-meta">{{ entryMeta(entry) }}</span>
            </button>

            <button
              v-if="entriesFor(day).length > 3"
              type="button"
              class="todo-calendar-more"
              :aria-expanded="expandedDate === day.key"
              @click.stop="toggleOverflow(day)"
            >
              {{ t("todo_calendar_more", { count: entriesFor(day).length - 3 }) }}
            </button>

            <span v-if="entriesFor(day).length > 0" class="todo-calendar-mobile-count" aria-hidden="true">
              <span
                v-for="entry in entriesFor(day).slice(0, 3)"
                :key="'dot:' + entry.task_id + ':' + entry.date"
                :class="['todo-calendar-mobile-dot', { 'is-disabled': entry.task.enabled === false }]"
              ></span>
              <span class="todo-calendar-mobile-total">{{ entriesFor(day).length }}</span>
            </span>
          </div>

          <div v-if="expandedDate === day.key" class="todo-calendar-overflow" role="menu">
            <strong class="todo-calendar-overflow-date">{{ day.key }}</strong>
            <button
              v-for="entry in entriesFor(day)"
              :key="'overflow:' + entry.task_id + ':' + entry.date"
              type="button"
              :class="entryClass(entry)"
              role="menuitem"
              @click.stop="selectEntry(entry)"
            >
              <span class="todo-calendar-status" aria-hidden="true"></span>
              <span class="todo-calendar-entry-title">{{ taskTitle(entry.task) }}</span>
              <span class="todo-calendar-entry-meta">{{ entryMeta(entry) }}</span>
            </button>
          </div>
        </article>

        <div v-if="isEmpty" class="todo-calendar-empty">
          <strong>{{ t("todo_calendar_empty") }}</strong>
          <span>{{ t("todo_calendar_empty_hint") }}</span>
        </div>
      </div>

      <section class="todo-calendar-agenda" :aria-label="t('todo_calendar_agenda')">
        <header class="todo-calendar-agenda-head">
          <strong>{{ selectedDateTitle }}</strong>
          <span>{{ t("todo_calendar_item_count", { count: selectedDateEntries.length }) }}</span>
        </header>
        <div v-if="selectedDateEntries.length > 0" class="todo-calendar-agenda-items">
          <button
            v-for="entry in selectedDateEntries"
            :key="'agenda:' + entry.task_id + ':' + entry.date"
            type="button"
            :class="entryClass(entry)"
            @click="selectEntry(entry)"
          >
            <span class="todo-calendar-status" aria-hidden="true"></span>
            <span class="todo-calendar-entry-title">{{ taskTitle(entry.task) }}</span>
            <span class="todo-calendar-entry-meta">{{ entryMeta(entry) }}</span>
          </button>
        </div>
        <p v-else>{{ t("todo_calendar_day_empty") }}</p>
      </section>
    </section>
  `,
};

export default TodoCalendar;
