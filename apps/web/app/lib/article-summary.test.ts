import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parsePrimarySource } from "./api";

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

test("an original report links to the primary document it was written from, and only that", () => {
  assert.match(apiSource, /primarySource: parsePrimarySource\(a\.primary_source\),/);
  assert.match(typesSource, /primarySource\?: \{ name: string; url: string \};/);
  const body = pageSource.indexOf('id="article-body"');
  const link = pageSource.indexOf("{article.primarySource && (");
  assert.ok(body >= 0 && link > body, "the source link comes after the body");
  assert.match(pageSource, /href=\{article\.primarySource\.url\}/);
  assert.match(pageSource, /isBasedOn: article\.primarySource\?\.url,/);
});

test("a primary source needs a name and an https link", () => {
  assert.deepEqual(parsePrimarySource({ name: " Anthropic ", url: "https://www.anthropic.com/news/x" }), {
    name: "Anthropic",
    url: "https://www.anthropic.com/news/x",
  });
  for (const bad of [undefined, null, "x", {}, { name: "A" }, { name: "", url: "https://a.com" }, { name: "A", url: "http://a.com" }, { name: "A", url: "javascript:alert(1)" }]) {
    assert.equal(parsePrimarySource(bad), undefined);
  }
});

test("the summary sits between the share buttons and the article body", () => {
  const share = pageSource.indexOf("<ShareButtons");
  const summary = pageSource.indexOf("<ArticleSummary tldr={article.tldr} whyItMatters={article.whyItMatters} />");
  const body = pageSource.indexOf('id="article-body"');
  assert.ok(share >= 0 && summary > share && body > summary, "order: share buttons, summary, body");
});

test("the summary leads with why it matters as a pull quote, then the facts, and shows nothing when both are missing", () => {
  assert.match(summarySource, /if \(!tldr\?\.length && !whyItMatters\) return null;/);
  const why = summarySource.indexOf("Why it matters");
  const facts = summarySource.indexOf("The facts");
  assert.ok(why > 0 && facts > why, "why it matters comes before the facts");
  assert.match(summarySource, /<li key=\{point\}/);
  assert.doesNotMatch(summarySource, /border-l-4/, "no left-border callout");
});

test("the article page shows one ad between the summary and the body, hidden until it is filled", () => {
  const adSource = readFileSync(new URL("../components/AdSlot.tsx", import.meta.url), "utf8");
  const css = readFileSync(new URL("../globals.css", import.meta.url), "utf8");
  const layout = readFileSync(new URL("../layout.tsx", import.meta.url), "utf8");
  const adsTxt = readFileSync(new URL("../../public/ads.txt", import.meta.url), "utf8");
  const summary = pageSource.indexOf("<ArticleSummary tldr={article.tldr} whyItMatters={article.whyItMatters} />");
  const ad = pageSource.indexOf("<AdSlot slot={AD_SLOT_ARTICLE_TOP} />");
  const body = pageSource.indexOf('id="article-body"');
  assert.ok(summary >= 0 && ad > summary && body > ad, "order: summary, ad, body");
  assert.equal(pageSource.split("<AdSlot").length - 1, 1, "one ad on the article page");
  assert.match(adSource, /className="ad-slot"/);
  assert.match(css, /\.ad-slot \{\s*display: none;/);
  assert.match(css, /\.ad-slot:has\(ins\.adsbygoogle\[data-ad-status="filled"\]\)/);
  assert.match(layout, /<script async src=\{ADSENSE_SCRIPT_URL\} crossOrigin="anonymous" \/>/);
  assert.equal(adsTxt.trim(), "google.com, pub-4199226001427614, DIRECT, f08c47fec0942fa0");
});
