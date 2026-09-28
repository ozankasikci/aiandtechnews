import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { articleLastModified } from "../sitemap";

const sitemapSource = readFileSync(new URL("../sitemap.ts", import.meta.url), "utf8");
const searchLayoutSource = readFileSync(new URL("../search/layout.tsx", import.meta.url), "utf8");

test("the noindex search page is excluded from the sitemap", () => {
  assert.match(searchLayoutSource, /robots:\s*\{\s*index:\s*false,\s*follow:\s*true\s*\}/);
  assert.doesNotMatch(sitemapSource, /\$\{BASE_URL\}\/search/);
});

test("the sitemap keeps known article URLs during an API outage", () => {
  assert.match(sitemapSource, /function snapshotArticlePages/);
  assert.match(sitemapSource, /article_pages = snapshotArticlePages\(\)/);
});

test("article sitemap dates reflect publication or a later real edit", () => {
  const published = "2026-01-02T12:00:00Z";
  const updated = "2026-01-03T12:00:00Z";

  assert.equal(articleLastModified({ published_at: published, updated_at: updated })?.toISOString(), "2026-01-03T12:00:00.000Z");
  assert.equal(articleLastModified({ published_at: published, updated_at: "2026-01-01T12:00:00Z" })?.toISOString(), "2026-01-02T12:00:00.000Z");
  assert.equal(articleLastModified({ published_at: published, updated_at: "2999-01-01T00:00:00Z" })?.toISOString(), "2026-01-02T12:00:00.000Z");
  assert.equal(articleLastModified({ published_at: "invalid", updated_at: "invalid" }), undefined);
});

test("undated sitemap pages do not claim to change on every request", () => {
  assert.doesNotMatch(sitemapSource, /lastModified:\s*new Date\(\)/);
  assert.doesNotMatch(sitemapSource, /changeFrequency:|priority:/);
});
