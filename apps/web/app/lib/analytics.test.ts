import assert from "node:assert/strict";
import test from "node:test";
import { sanitizePagePath, sanitizeSearchTerm } from "./analytics";

test("normalizes search terms before analytics collection", () => {
  assert.equal(sanitizeSearchTerm("  open   source AI  "), "open source AI");
});

test("redacts search terms that may contain personal contact information", () => {
  assert.equal(sanitizeSearchTerm("person@example.com"), "[redacted]");
  assert.equal(sanitizeSearchTerm("https://example.com/private"), "[redacted]");
});

test("sanitizes query values before they reach page_view events", () => {
  assert.equal(
    sanitizePagePath("/search", new URLSearchParams("q=person%40example.com")),
    "/search?q=%5Bredacted%5D",
  );
  assert.equal(
    sanitizePagePath("/search", new URLSearchParams("q=open+source+AI")),
    "/search?q=open+source+AI",
  );
});

test("drops empty query values and keeps plain paths unchanged", () => {
  assert.equal(sanitizePagePath("/search", new URLSearchParams("q=+")), "/search");
  assert.equal(sanitizePagePath("/about", new URLSearchParams()), "/about");
});
