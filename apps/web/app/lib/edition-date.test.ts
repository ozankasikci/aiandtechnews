import assert from "node:assert/strict";
import test from "node:test";
import { formatEditionDate } from "../newsletter/edition-date";

test("edition keys render as full calendar dates", () => {
  assert.equal(formatEditionDate("2026-08-09"), "Sunday, August 9, 2026");
});

test("the date is not shifted by the reader's timezone", () => {
  // A naive `new Date("2026-01-01")` parsed as UTC then rendered locally can
  // slip to December 31 west of Greenwich.
  assert.equal(formatEditionDate("2026-01-01"), "Thursday, January 1, 2026");
});

test("unrecognized keys are passed through unchanged", () => {
  assert.equal(formatEditionDate("not-a-date"), "not-a-date");
  assert.equal(formatEditionDate("2026-13-45"), "2026-13-45");
});
