import assert from "node:assert/strict";
import test from "node:test";
import { parseApiDate, toAbsoluteUrl, toIsoDate } from "./dates";

test("normalizes database timestamps to ISO 8601 UTC values", () => {
  assert.equal(toIsoDate("2026-08-06 04:45:19"), "2026-08-06T04:45:19.000Z");
  assert.equal(toIsoDate("2026-08-06T07:45:19+03:00"), "2026-08-06T04:45:19.000Z");
  assert.equal(toIsoDate("not-a-date"), undefined);
});

test("parses API dates without treating missing time zones as local time", () => {
  assert.equal(parseApiDate("2026-08-06 04:45:19")?.getUTCHours(), 4);
});

test("keeps external image URLs intact and resolves relative images", () => {
  const baseUrl = "https://www.aiandtech.news";

  assert.equal(
    toAbsoluteUrl("https://techcrunch.com/image.jpg", baseUrl),
    "https://techcrunch.com/image.jpg",
  );
  assert.equal(
    toAbsoluteUrl("/images/article.jpg", baseUrl),
    "https://www.aiandtech.news/images/article.jpg",
  );
});
