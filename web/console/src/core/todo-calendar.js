import { cronMatches, parseCronExpression } from "./cron.js";

const MINUTE_MS = 60 * 1000;

function text(value) {
  return String(value || "").trim();
}

function pad2(value) {
  return String(value).padStart(2, "0");
}

function dateKey(parts) {
  return `${String(parts.year).padStart(4, "0")}-${pad2(parts.month)}-${pad2(parts.day)}`;
}

function wallMinuteKey(parts) {
  return `${dateKey(parts)} ${pad2(parts.hour)}:${pad2(parts.minute)}`;
}

function addLocalDays(date, count) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + count);
}

function localDateKey(date) {
  return dateKey({ year: date.getFullYear(), month: date.getMonth() + 1, day: date.getDate() });
}

export function visibleCalendarDays(month) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const start = addLocalDays(first, -first.getDay());
  const today = localDateKey(new Date());
  return Array.from({ length: 42 }, (_, index) => {
    const date = addLocalDays(start, index);
    const key = localDateKey(date);
    return {
      key,
      day: date.getDate(),
      inMonth: date.getMonth() === month.getMonth(),
      isToday: key === today,
    };
  });
}

function fixedUTCOffset(timezone) {
  const match = text(timezone).toUpperCase().match(/^UTC(?:([+-])(\d{1,4})(?::(\d{2}))?)?$/);
  if (!match) {
    return null;
  }
  if (!match[1]) {
    return 0;
  }
  const compact = match[2];
  const hourRaw = match[3] === undefined && compact.length > 2 ? compact.slice(0, -2) : compact;
  const minuteRaw = match[3] === undefined && compact.length > 2 ? compact.slice(-2) : match[3] || "0";
  const hours = Number(hourRaw);
  const minutes = Number(minuteRaw);
  if (!Number.isInteger(hours) || !Number.isInteger(minutes) || minutes > 59) {
    throw new Error("invalid timezone");
  }
  const offset = hours * 60 + minutes;
  if (offset > 14 * 60) {
    throw new Error("invalid timezone");
  }
  return match[1] === "-" ? -offset : offset;
}

function wallClock(timezone) {
  const offset = fixedUTCOffset(timezone);
  if (offset !== null) {
    return (timestamp) => {
      const date = new Date(timestamp + offset * MINUTE_MS);
      return {
        year: date.getUTCFullYear(),
        month: date.getUTCMonth() + 1,
        day: date.getUTCDate(),
        hour: date.getUTCHours(),
        minute: date.getUTCMinutes(),
        weekday: date.getUTCDay(),
      };
    };
  }

  const formatter = new Intl.DateTimeFormat("en-CA-u-hc-h23", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    weekday: "short",
    hourCycle: "h23",
  });
  const weekdays = new Map([
    ["Sun", 0],
    ["Mon", 1],
    ["Tue", 2],
    ["Wed", 3],
    ["Thu", 4],
    ["Fri", 5],
    ["Sat", 6],
  ]);
  return (timestamp) => {
    const parts = Object.fromEntries(formatter.formatToParts(new Date(timestamp)).map((part) => [part.type, part.value]));
    return {
      year: Number(parts.year),
      month: Number(parts.month),
      day: Number(parts.day),
      hour: Number(parts.hour),
      minute: Number(parts.minute),
      weekday: weekdays.get(parts.weekday),
    };
  };
}

function atMinute(raw) {
  const match = text(raw).match(/^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})$/);
  if (!match) {
    throw new Error("invalid at time");
  }
  const [, yearRaw, monthRaw, dayRaw, hourRaw, minuteRaw] = match;
  const year = Number(yearRaw);
  const month = Number(monthRaw);
  const day = Number(dayRaw);
  const hour = Number(hourRaw);
  const minute = Number(minuteRaw);
  const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate();
  if (month < 1 || month > 12 || day < 1 || day > lastDay || hour > 23 || minute > 59) {
    throw new Error("invalid at time");
  }
  return `${yearRaw}-${monthRaw}-${dayRaw} ${hourRaw}:${minuteRaw}`;
}

export function projectTodoCalendar(tasks, from, to, displayTimezone) {
  const fromTime = from instanceof Date ? from.getTime() : NaN;
  const toTime = to instanceof Date ? to.getTime() : NaN;
  if (!Number.isFinite(fromTime) || !Number.isFinite(toTime) || toTime <= fromTime) {
    throw new Error("invalid calendar range");
  }

  const displayClock = wallClock(text(displayTimezone) || "UTC");
  const invalidTaskIDs = [];
  const groups = new Map();
  const taskList = Array.isArray(tasks) ? tasks : [];
  taskList.forEach((task, order) => {
    const id = text(task?.id);
    const at = text(task?.at);
    const cron = text(task?.cron);
    try {
      if (!id || (at === "") === (cron === "")) {
        throw new Error("invalid task schedule");
      }
      const timezone = text(task?.tz) || text(displayTimezone) || "UTC";
      const parsed = {
        id,
        order,
        enabled: task?.enabled !== false,
        at: at ? atMinute(at) : "",
        cron: cron ? parseCronExpression(cron) : null,
      };
      let group = groups.get(timezone);
      if (!group) {
        group = { clock: wallClock(timezone), once: new Map(), recurring: [] };
        groups.set(timezone, group);
      }
      if (parsed.at) {
        const items = group.once.get(parsed.at) || [];
        items.push(parsed);
        group.once.set(parsed.at, items);
      } else {
        group.recurring.push(parsed);
      }
    } catch {
      invalidTaskIDs.push(id);
    }
  });

  const candidates = [];
  const seen = new Set();
  const firstMinute = Math.ceil(fromTime / MINUTE_MS) * MINUTE_MS;
  for (const group of groups.values()) {
    for (let timestamp = firstMinute; timestamp < toTime; timestamp += MINUTE_MS) {
      const wall = group.clock(timestamp);
      const matching = [...(group.once.get(wallMinuteKey(wall)) || [])];
      for (const task of group.recurring) {
        if (cronMatches(task.cron, wall)) {
          matching.push(task);
        }
      }
      if (matching.length === 0) {
        continue;
      }
      const date = dateKey(displayClock(timestamp));
      for (const task of matching) {
        const seenKey = `${task.order}:${date}`;
        if (seen.has(seenKey)) {
          continue;
        }
        seen.add(seenKey);
        candidates.push({
          task_id: task.id,
          date,
          first_at: new Date(timestamp).toISOString(),
          enabled: task.enabled,
          order: task.order,
          timestamp,
        });
      }
    }
  }

  candidates.sort((left, right) => {
    if (left.date !== right.date) {
      return left.date.localeCompare(right.date);
    }
    if (left.enabled !== right.enabled) {
      return left.enabled ? -1 : 1;
    }
    if (left.timestamp !== right.timestamp) {
      return left.timestamp - right.timestamp;
    }
    return left.order - right.order;
  });
  return {
    items: candidates.map(({ task_id, date, first_at }) => ({ task_id, date, first_at })),
    invalidTaskIDs,
  };
}
