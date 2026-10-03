import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(
  new URL("./[slug]/page.tsx", import.meta.url),
  "utf8",
);

const apiSource = readFileSync(new URL("../lib/api.ts", import.meta.url), "utf8");
const revalidateSource = readFileSync(new URL("../lib/revalidate.ts", import.meta.url), "utf8");

// Article pages used to be force-dynamic because a cached 404 could hide a
// newly published slug. They are now cached for an hour (ISR) to save
// server CPU, and that 404 is cleared on demand: publishing revalidates
// /article/<slug> and its article:<slug> fetch tag via POST /api/revalidate.
test("article routes are cached for a day and render unknown slugs on demand", () => {
  assert.match(pageSource, /export const revalidate\s*=\s*86400\b/);
  assert.doesNotMatch(pageSource, /force-dynamic/);
  // An empty generateStaticParams is what makes Next 15 cache a dynamic
  // segment at all; it prebuilds nothing, and dynamicParams stays true so
  // new slugs render on first request.
  assert.match(pageSource, /export function generateStaticParams\(\)(?::[^\n]*)?\{\s*return \[\];\s*\}/);
  assert.match(pageSource, /export const dynamicParams\s*=\s*true/);
});

test("the article fetch is tagged per slug so a publish can clear a cached 404", () => {
  assert.match(apiSource, /revalidate:\s*ARTICLE_REVALIDATE_SECONDS,\s*tags:\s*\[articleTag\(slug\)\]/);
  assert.match(apiSource, /ARTICLE_REVALIDATE_SECONDS\s*=\s*86400/);
  assert.match(revalidateSource, /path:\s*`\/article\/\$\{slug\}`/);
  assert.match(revalidateSource, /slugs\.map\(articleTag\)/);
});

test("article pages count views from the browser, not from the cached render", () => {
  assert.match(pageSource, /<ArticleViewBeacon apiBase=\{getPublicApiUrl\(\)\} slug=\{article\.slug\} \/>/);
});

test("article routes distinguish missing stories from a temporary API outage", () => {
  assert.match(pageSource, /lookup\.state === ["']missing["']\) notFound\(\)/);
  assert.match(pageSource, /lookup\.state === ["']unavailable["']\) throw new Error/);
});

test("article metadata declares canonical URLs and normalized schema fields", () => {
  assert.match(pageSource, /alternates:\s*\{\s*canonical:/);
  assert.match(pageSource, /datePublished:\s*article\.publishedAt/);
  assert.match(pageSource, /toAbsoluteUrl\(article\.image, BASE_URL\)/);
  assert.doesNotMatch(pageSource, /`https:\/\/www\.aiandtech\.news\$\{article\.image\}`/);
});

test("article pages do not display public source attribution", () => {
  assert.doesNotMatch(pageSource, /Original reporting:/);
  assert.doesNotMatch(pageSource, /eventName=["']source_click["']/);
});
