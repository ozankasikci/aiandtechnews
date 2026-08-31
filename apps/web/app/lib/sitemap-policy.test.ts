import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

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
