import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { SHARE_CARD_PATH, shareCardDateLabel, shareCardMoreHeadlines, shareCardUrl } from "./share-card";

const homeSource = readFileSync(new URL("../page.tsx", import.meta.url), "utf8");
const routeSource = readFileSync(new URL("../og/today/route.tsx", import.meta.url), "utf8");

test("the share card URL changes every UTC day", () => {
  assert.equal(shareCardUrl(new Date("2026-09-26T23:59:00Z")), "/og/today?d=2026-09-26");
  assert.equal(shareCardUrl(new Date("2026-09-27T00:01:00Z")), "/og/today?d=2026-09-27");
});

test("the card's date reads as a weekday and date in UTC", () => {
  assert.equal(shareCardDateLabel(new Date("2026-09-26T12:00:00Z")), "Saturday, September 26");
});

test("the card lists up to three headlines after the lead story", () => {
  assert.deepEqual(shareCardMoreHeadlines(["Lead", "Two", "Three", "Four", "Five"]), ["Two", "Three", "Four"]);
  assert.deepEqual(shareCardMoreHeadlines(["Lead", " "]), []);
  assert.deepEqual(shareCardMoreHeadlines([]), []);
});

test("the homepage declares the large share card for Open Graph and X", () => {
  assert.match(homeSource, /export async function generateMetadata/);
  assert.match(homeSource, /card: "summary_large_image"/);
  assert.match(homeSource, /shareCardUrl\(/);
});

test("the share card is rendered as an image at the Open Graph size", () => {
  assert.equal(SHARE_CARD_PATH, "/og/today");
  assert.match(routeSource, /new ImageResponse\(/);
  assert.match(routeSource, /SHARE_CARD_SIZE/);
});
