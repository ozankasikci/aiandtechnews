import assert from "node:assert/strict";
import test from "node:test";
import { classFromHex, fallbackArticles, fallbackArticlesByCategory, snapshotToArticle } from "./fallback";

const now = new Date("2026-08-24T12:00:00Z");

test("the snapshot is not empty, so listings survive an API outage", () => {
  const articles = fallbackArticles(undefined, now);
  assert.ok(articles.length > 0, "expected the bundled snapshot to contain articles");
  assert.ok(articles.every((a) => a.slug && a.headline));
});

test("limit caps the number of fallback articles", () => {
  assert.equal(fallbackArticles(3, now).length, 3);
});

test("category filtering matches on slug or name, case-insensitively", () => {
  const all = fallbackArticles(undefined, now);
  const someTag = all[0].tag;
  const matches = fallbackArticlesByCategory(someTag, undefined, now);
  assert.ok(matches.length > 0);
  assert.ok(matches.every((a) => a.tag.toLowerCase() === someTag.toLowerCase()));
  assert.equal(fallbackArticlesByCategory("no-such-category", undefined, now).length, 0);
});

test("mapping fills the fields listings render", () => {
  const article = snapshotToArticle(
    {
      id: 7,
      slug: "a-story",
      title: "A story",
      excerpt: "Something happened",
      featured_image: "/img.png",
      published_at: "2026-08-24T09:00:00Z",
      category: { name: "AI", slug: "ai", color: "#8B5CF6" },
    },
    now,
  );
  assert.equal(article.tag, "AI");
  assert.equal(article.tagColor, "bg-accent-purple");
  assert.equal(article.time, "3h ago");
  assert.equal(article.publishedAt, "2026-08-24T09:00:00.000Z");
  assert.equal(article.author, "TechNews Editorial");
});

test("hex colors map to accent classes regardless of case", () => {
  assert.equal(classFromHex("#F97316"), "bg-accent-orange");
  assert.equal(classFromHex(undefined), "bg-accent-purple");
});
