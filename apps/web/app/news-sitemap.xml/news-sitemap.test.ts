import assert from "node:assert/strict";
import test from "node:test";
import { buildNewsSitemap } from "./news-sitemap";

test("news sitemap includes only articles published in the previous two days", () => {
  const xml = buildNewsSitemap(
    [
      { slug: "new-story", title: "AI & chips", published_at: "2026-08-06 08:00:00" },
      { slug: "old-story", title: "Old story", published_at: "2026-08-03 08:00:00" },
      { slug: "future-story", title: "Future story", published_at: "2026-08-06 13:00:00" },
      { slug: "invalid-story", title: "Invalid story", published_at: "unknown" },
    ],
    new Date("2026-08-06T12:00:00.000Z"),
  );

  assert.match(xml, /new-story/);
  assert.match(xml, /2026-08-06T08:00:00\.000Z/);
  assert.match(xml, /AI &amp; chips/);
  assert.doesNotMatch(xml, /old-story|future-story|invalid-story/);
});

test("news sitemap emits the Google News namespace and publication metadata", () => {
  const xml = buildNewsSitemap([], new Date("2026-08-06T12:00:00.000Z"));

  assert.match(xml, /xmlns:news="http:\/\/www\.google\.com\/schemas\/sitemap-news\/0\.9"/);
  assert.match(xml, /<urlset/);
  assert.match(xml, /<\/urlset>/);
});
