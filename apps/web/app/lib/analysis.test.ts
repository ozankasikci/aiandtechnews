import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { mapArticle, type ApiArticle } from "./api";

const article = { id: 1, slug: "story", title: "Story", content: "<p>Reporting.</p>", published_at: "2026-10-22T10:00:00Z" } as ApiArticle;

test("mapping carries optional analysis and internal link titles", () => {
  const analysis = { kind: "context" as const, text: "Checked analysis.", links: [{ slug: "earlier", title: "Earlier coverage" }] };
  assert.deepEqual(mapArticle({ ...article, analysis }).analysis, analysis);
  assert.equal(mapArticle(article).analysis, undefined);
  assert.deepEqual(mapArticle({ ...article, analysis: { ...analysis, links: [] } }).analysis?.links, []);
});

test("analysis renders after the full body and before end-of-article components", () => {
  const page = readFileSync(new URL("../article/[slug]/page.tsx", import.meta.url), "utf8");
  const component = readFileSync(new URL("../components/ArticleAnalysis.tsx", import.meta.url), "utf8");
  const block = page.indexOf("<ArticleAnalysis analysis={article.analysis} />");
  assert.ok(block > page.indexOf("{bodyRest &&"));
  assert.ok(block < page.indexOf("{article.primarySource &&"));
  assert.ok(block < page.indexOf("<NewsletterBanner"));
  assert.match(component, /if \(!analysis\) return null;/);
  assert.match(component, /uppercase tracking-widest[^>]*>What this means/);
  assert.match(component, /\{analysis.text\}/);
  assert.match(component, /analysis.links.length > 0/);
  assert.match(component, /Related coverage/);
  assert.match(component, /href=\{`\/article\/\$\{slug\}`\}/);
  assert.match(component, /\{title\}/);
  assert.doesNotMatch(component, /dangerouslySetInnerHTML/);
});
