import assert from "node:assert/strict";
import test from "node:test";

import { projectTodoCalendar, visibleCalendarDays } from "./todo-calendar.js";

function itemKeys(projection) {
  return projection.items.map((item) => `${item.task_id}@${item.first_at}`);
}

test("starts the visible month grid on Sunday", () => {
  const days = visibleCalendarDays(new Date(2026, 7, 1));

  assert.equal(days.length, 42);
  assert.equal(days[0].key, "2026-07-26");
  assert.equal(days[6].key, "2026-08-01");
  assert.equal(days[41].key, "2026-09-05");
});

test("projects once and recurring tasks and collapses frequent runs by day", () => {
  const projection = projectTodoCalendar(
    [
      { id: "once", at: "2026-08-04 09:30", tz: "Asia/Tokyo", content: "One-off" },
      { id: "daily", cron: "0 8 * * *", tz: "Asia/Tokyo", content: "Daily" },
      { id: "many", enabled: false, cron: "*/15 * * * *", tz: "Asia/Tokyo", content: "Often" },
    ],
    new Date("2026-08-02T15:00:00Z"),
    new Date("2026-08-09T15:00:00Z"),
    "Asia/Tokyo"
  );

  assert.deepEqual(projection.invalidTaskIDs, []);
  assert.deepEqual(itemKeys(projection), [
    "daily@2026-08-02T23:00:00.000Z",
    "many@2026-08-02T15:00:00.000Z",
    "daily@2026-08-03T23:00:00.000Z",
    "once@2026-08-04T00:30:00.000Z",
    "many@2026-08-03T15:00:00.000Z",
    "daily@2026-08-04T23:00:00.000Z",
    "many@2026-08-04T15:00:00.000Z",
    "daily@2026-08-05T23:00:00.000Z",
    "many@2026-08-05T15:00:00.000Z",
    "daily@2026-08-06T23:00:00.000Z",
    "many@2026-08-06T15:00:00.000Z",
    "daily@2026-08-07T23:00:00.000Z",
    "many@2026-08-07T15:00:00.000Z",
    "daily@2026-08-08T23:00:00.000Z",
    "many@2026-08-08T15:00:00.000Z",
  ]);
});

test("converts task-local schedules to the display timezone", () => {
  const projection = projectTodoCalendar(
    [
      { id: "tokyo-once", at: "2026-08-03 08:00", tz: "Asia/Tokyo" },
      { id: "tokyo-daily", cron: "0 8 * * *", tz: "Asia/Tokyo" },
    ],
    new Date("2026-08-02T07:00:00Z"),
    new Date("2026-08-04T07:00:00Z"),
    "America/Los_Angeles"
  );

  assert.deepEqual(itemKeys(projection), [
    "tokyo-once@2026-08-02T23:00:00.000Z",
    "tokyo-daily@2026-08-02T23:00:00.000Z",
    "tokyo-daily@2026-08-03T23:00:00.000Z",
  ]);
  assert.deepEqual(projection.items.map((item) => item.date), ["2026-08-02", "2026-08-02", "2026-08-03"]);
});

test("uses runtime cron OR semantics and ignores invalid tasks", () => {
  const projection = projectTodoCalendar(
    [
      { id: "weekly", cron: "0 9 * * 1", tz: "UTC" },
      { id: "monthly", cron: "0 10 5 * *", tz: "UTC" },
      { id: "or-rule", cron: "0 11 2 * 1", tz: "UTC" },
      { id: "bad", cron: "invalid", tz: "UTC" },
    ],
    new Date("2026-08-01T00:00:00Z"),
    new Date("2026-08-10T00:00:00Z"),
    "UTC"
  );

  assert.deepEqual(projection.invalidTaskIDs, ["bad"]);
  assert.deepEqual(itemKeys(projection), [
    "or-rule@2026-08-02T11:00:00.000Z",
    "weekly@2026-08-03T09:00:00.000Z",
    "or-rule@2026-08-03T11:00:00.000Z",
    "monthly@2026-08-05T10:00:00.000Z",
  ]);
});

test("uses real timezone transitions for missing and repeated DST minutes", () => {
  const spring = projectTodoCalendar(
    [{ id: "nightly", cron: "30 2 * * *", tz: "America/New_York" }],
    new Date("2026-03-07T05:00:00Z"),
    new Date("2026-03-10T04:00:00Z"),
    "America/New_York"
  );
  assert.deepEqual(itemKeys(spring), [
    "nightly@2026-03-07T07:30:00.000Z",
    "nightly@2026-03-09T06:30:00.000Z",
  ]);

  const fall = projectTodoCalendar(
    [{ id: "repeated", cron: "30 1 * * *", tz: "America/New_York" }],
    new Date("2026-11-01T04:00:00Z"),
    new Date("2026-11-02T05:00:00Z"),
    "America/New_York"
  );
  assert.deepEqual(itemKeys(fall), ["repeated@2026-11-01T05:30:00.000Z"]);
});

test("supports the runtime UTC offset timezone syntax", () => {
  const projection = projectTodoCalendar(
    [{ id: "offset", cron: "0 9 * * *", tz: "UTC+5:30" }],
    new Date("2026-08-01T00:00:00Z"),
    new Date("2026-08-02T00:00:00Z"),
    "UTC"
  );

  assert.deepEqual(itemKeys(projection), ["offset@2026-08-01T03:30:00.000Z"]);
});
