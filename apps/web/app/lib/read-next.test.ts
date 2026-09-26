import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { pickReadNext, splitAfterParagraph, titleTerms } from "./read-next";

const now = new Date("2026-09-26T18:00:00Z");
const story = (slug: string, headline: string, tag: string, hoursAgo: number) => ({
  slug,
  headline,
  tag,
  publishedAt: new Date(now.getTime() - hoursAgo * 3_600_000).toISOString(),
});

const current = story("openai-pauses", "OpenAI Pauses Top AI Models After Agents Bypass Security and Leak Data", "AI", 5);
const candidates = [
  current,
  story("tesla-optimus", "Tesla Workers Push Back Against Training Replacement Optimus Robots", "Tech", 3),
  story("openai-images", "OpenAI Agents Unintentionally Posted 53 User Images Online", "AI", 14),
  story("gov-hack", "AI Agents Hack Government Agency as Model Releases Continue", "AI", 16),
  story("pagebreak", "Google Deploys PageBreak AI Agent to Hunt Security Flaws", "Tech", 13),
  story("old-openai", "OpenAI Agents Leak Data Again", "AI", 24 * 10),
  story("grads", "New Study Finds No Evidence AI Is Harming Recent Graduate Employment", "AI", 12),
];

test("title terms drop short and common words and keep names", () => {
  assert.deepEqual(titleTerms("OpenAI Pauses Top AI Models After Agents Bypass Security"), [
    "openai", "pauses", "models", "agents", "bypass", "security",
  ]);
});

test("read-next prefers recent stories that share the most title terms", () => {
  const picks = pickReadNext(current, candidates, { now, count: 4 }).map((a) => a.slug);
  assert.equal(picks[0], "openai-images"); // shares openai, agents
  assert.ok(picks.includes("gov-hack")); // shares agents
  assert.ok(picks.includes("pagebreak")); // shares security
  assert.ok(!picks.includes("openai-pauses"), "never the article itself");
  assert.ok(!picks.includes("old-openai"), "never older than a week");
  assert.equal(picks.length, 4);
});

test("read-next fills up with the same section, then anything recent, when few stories match", () => {
  const lonely = story("lonely", "Quantum Widgets Reach Market", "AI", 1);
  const picks = pickReadNext(lonely, candidates, { now, count: 3 }).map((a) => a.slug);
  assert.deepEqual(picks, ["openai-pauses", "grads", "openai-images"]); // newest AI stories first
});

test("the article body splits after the third paragraph only when enough text follows", () => {
  const body = "<p>1</p><p>2</p><h2>H</h2><p>3</p><p>4</p><p>5</p>";
  assert.deepEqual(splitAfterParagraph(body, 3), ["<p>1</p><p>2</p><h2>H</h2><p>3</p>", "<p>4</p><p>5</p>"]);
  assert.deepEqual(splitAfterParagraph("<p>1</p><p>2</p><p>3</p><p>4</p>", 3), ["<p>1</p><p>2</p><p>3</p><p>4</p>", ""]);
});

test("the article page shows a mid-article card and a keep-reading row from the picks", () => {
  const page = readFileSync(new URL("../article/[slug]/page.tsx", import.meta.url), "utf8");
  assert.match(page, /pickReadNext\(article, /);
  assert.match(page, /splitAfterParagraph\(body, 3\)/);
  assert.match(page, /link_position: "mid_article"/);
  assert.match(page, /Keep reading/);
  assert.doesNotMatch(page, /Related Stories/);
});
