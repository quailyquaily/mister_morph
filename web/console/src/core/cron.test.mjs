import assert from "node:assert/strict";
import test from "node:test";

import { isValidCronExpression } from "./cron.js";

test("validates the numeric cron syntax shared by TODO editing and Calendar", () => {
  for (const expression of ["0 9 * * *", "*/15 * * * *", "0 9 1-5 1,6 0,7", "5/2 4 * * 1"]) {
    assert.equal(isValidCronExpression(expression), true, expression);
  }
  for (const expression of ["", "0 9 * *", "0 24 * * *", "0 9 * * MON", "*/0 * * * *", "0 9 5-1 * *"]) {
    assert.equal(isValidCronExpression(expression), false, expression);
  }
});
