import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function source(path) {
  return readFile(new URL(path, import.meta.url), "utf8");
}

test("calendar selects the whole date cell and reports that day's tasks", async () => {
  const component = await source("../components/TodoCalendar.js");

  assert.match(component, /emits:\s*\[[^\]]*"select-date"/u);
  assert.match(component, /emit\("select-date",\s*\{\s*date:/u);
  assert.match(component, /task_ids:/u);
  assert.match(component, /<article[^>]*@click="selectDate\(day\)"/us);
  assert.match(component, /:aria-selected="day\.key === selectedDate"/u);
  assert.match(component, /@click\.stop="selectEntry\(entry\)"/u);
  assert.match(component, /const today = dateKey\(new Date\(\)\);/u);
  assert.match(component, /\{ immediate: true \}/u);
});

test("TODO view shows a date agenda until a concrete task is selected", async () => {
  const view = await source("../views/TodoView.js");

  assert.match(view, /const selectedCalendarDate = ref\(""\);/u);
  assert.match(view, /const selectedCalendarTasks = computed/u);
  assert.match(view, /function selectCalendarDate\(selection\)/u);
  assert.match(view, /@select-date="selectCalendarDate"/u);
  assert.match(view, /class="todo-calendar-day-detail-card/u);
  assert.match(view, /v-for="task in selectedCalendarTasks"/u);
  assert.match(view, /@click="selectTask\(task\)"/u);
});
