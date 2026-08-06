import assert from "node:assert/strict";
import test from "node:test";
import { sanitizeSearchTerm } from "./analytics";

test("normalizes search terms before analytics collection", () => {
  assert.equal(sanitizeSearchTerm("  open   source AI  "), "open source AI");
});

test("redacts search terms that may contain personal contact information", () => {
  assert.equal(sanitizeSearchTerm("person@example.com"), "[redacted]");
  assert.equal(sanitizeSearchTerm("https://example.com/private"), "[redacted]");
});
