import assert from "node:assert/strict";
import test from "node:test";

import {
  claimConsoleNotification,
  normalizeConsoleNotification,
  resolveBrowserNotificationPermission,
} from "./system-notification-utils.js";

test("normalizeConsoleNotification trims and validates a cron event", () => {
  assert.deepEqual(
    normalizeConsoleNotification({
      id: " run-1 ",
      task_id: " task-1 ",
      title: " Daily review ",
      body: " Review complete. ",
      status: "done",
      created_at: "2026-07-19T09:00:00Z",
    }),
    {
      id: "run-1",
      title: "Daily review",
      body: "Review complete.",
    }
  );
  assert.equal(normalizeConsoleNotification({ title: "Missing id" }), null);
});

test("claimConsoleNotification rejects a duplicate and bounds stored ids", () => {
  const values = new Map();
  const storage = {
    getItem(key) {
      return values.get(key) || null;
    },
    setItem(key, value) {
      values.set(key, value);
    },
  };

  assert.equal(claimConsoleNotification(storage, "run-1", 2), true);
  assert.equal(claimConsoleNotification(storage, "run-1", 2), false);
  assert.equal(claimConsoleNotification(storage, "run-2", 2), true);
  assert.equal(claimConsoleNotification(storage, "run-3", 2), true);
  assert.equal(claimConsoleNotification(storage, "run-1", 2), true);
});

test("resolveBrowserNotificationPermission requires a secure notification context", () => {
  function GrantedNotification() {}
  GrantedNotification.permission = "granted";
  function DeniedNotification() {}
  DeniedNotification.permission = "denied";
  assert.equal(resolveBrowserNotificationPermission({ isSecureContext: false, Notification: GrantedNotification }), "unsupported");
  assert.equal(
    resolveBrowserNotificationPermission({ isSecureContext: true, Notification: GrantedNotification }),
    "granted"
  );
  assert.equal(
    resolveBrowserNotificationPermission({ isSecureContext: true, Notification: DeniedNotification }),
    "denied"
  );
  assert.equal(resolveBrowserNotificationPermission({ isSecureContext: true }), "unsupported");
});
