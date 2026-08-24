import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const feedSource = readFileSync(
  new URL("../components/ArticleFeed.tsx", import.meta.url),
  "utf8",
);
const newsletterSource = readFileSync(
  new URL("../components/Newsletter.tsx", import.meta.url),
  "utf8",
);

test("the landing-page feed shows the newsletter card after three stories", () => {
  assert.match(feedSource, /index === 2 && <NewsletterBanner placement=\{newsletterPlacement\} \/>/);
  assert.match(feedSource, /newsletterPlacement = "homepage_feed"/);
});

test("the landing-page signup form stacks at the mobile breakpoint", () => {
  assert.match(newsletterSource, /className="flex flex-col sm:flex-row gap-2"/);
  assert.match(newsletterSource, /className="w-full sm:w-auto bg-accent-purple/);
});
