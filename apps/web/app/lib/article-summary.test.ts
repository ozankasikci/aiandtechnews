import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const apiSource = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
const typesSource = readFileSync(new URL("../data/articles.ts", import.meta.url), "utf8");
const pageSource = readFileSync(new URL("../article/[slug]/page.tsx", import.meta.url), "utf8");
const summarySource = readFileSync(new URL("../components/ArticleSummary.tsx", import.meta.url), "utf8");

test("articles carry their TL;DR and why-it-matters line from the API", () => {
  assert.match(apiSource, /tldr\?: string\[\];/);
  assert.match(apiSource, /why_it_matters\?: string;/);
  assert.match(apiSource, /tldr: a\.tldr\?\.length \? a\.tldr : undefined,/);
  assert.match(apiSource, /whyItMatters: a\.why_it_matters \|\| undefined,/);
  assert.match(typesSource, /tldr\?: string\[\];/);
  assert.match(typesSource, /whyItMatters\?: string;/);
});

test("the website never reads or shows an article's source", () => {
  for (const source of [apiSource, typesSource, pageSource]) {
    assert.doesNotMatch(source, /source_url|sourceUrl/);
    assert.doesNotMatch(source, /\bsource\?: string/);
  }
});

test("the summary sits between the share buttons and the article body", () => {
  const share = pageSource.indexOf("<ShareButtons");
  const summary = pageSource.indexOf("<ArticleSummary tldr={article.tldr} whyItMatters={article.whyItMatters} />");
  const body = pageSource.indexOf('id="article-body"');
  assert.ok(share >= 0 && summary > share && body > summary, "order: share buttons, summary, body");
});

test("the summary is a numbered brief with a separate why-it-matters section, and nothing when both are missing", () => {
  assert.match(summarySource, /if \(!tldr\?\.length && !whyItMatters\) return null;/);
  assert.match(summarySource, /The short version/);
  assert.match(summarySource, /Why it matters/);
  assert.match(summarySource, /<ol /);
  assert.match(summarySource, /\{i \+ 1\}/);
  assert.doesNotMatch(summarySource, /border-l-4/, "no left-border callout");
});
