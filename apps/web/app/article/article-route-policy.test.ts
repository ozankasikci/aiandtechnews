import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(
  new URL("./[slug]/page.tsx", import.meta.url),
  "utf8",
);

test("article routes render dynamically so newly published slugs cannot cache a 404", () => {
  assert.match(pageSource, /export const dynamic\s*=\s*["']force-dynamic["']/);
  assert.doesNotMatch(pageSource, /generateStaticParams/);
});
