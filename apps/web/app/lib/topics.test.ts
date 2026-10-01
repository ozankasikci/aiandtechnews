import assert from "node:assert/strict";
import test from "node:test";
import { mapArticle, getTopic, getTopics, type ApiArticle } from "./api";
import {
  firstSentence,
  groupByMonth,
  groupTopicsByKind,
  linkTopicMentions,
  parseTopicDetail,
  parseTopicList,
  parseTopicRefs,
} from "./topics";

const NVIDIA = { slug: "nvidia", name: "Nvidia" };
const link = (slug: string, text: string) => `<a href="/topics/${slug}" class="topic-link" data-topic="${slug}">${text}</a>`;

test("topic refs on articles drop malformed entries and duplicates, and cap at four", () => {
  const refs = parseTopicRefs([
    NVIDIA,
    { slug: "nvidia", name: "Nvidia again" },
    { slug: "Bad Slug", name: "x" },
    { slug: "ok", name: "  " },
    null,
    "str",
    { slug: "openai", name: " OpenAI " },
    { slug: "a" , name: "A" },
    { slug: "b", name: "B" },
    { slug: "c", name: "C" },
  ]);
  assert.deepEqual(refs.map((r) => r.slug), ["nvidia", "openai", "a", "b"]);
  assert.equal(refs[1].name, "OpenAI");
  assert.deepEqual(parseTopicRefs(undefined), []);
  assert.deepEqual(parseTopicRefs("nope"), []);
});

test("mapArticle carries validated topics", () => {
  const api = { id: 1, slug: "s", title: "T", content: "<p>x</p>", published_at: "2026-09-01T00:00:00Z", topics: [NVIDIA, { slug: "", name: "" }] } as unknown as ApiArticle;
  assert.deepEqual(mapArticle(api).topics, [NVIDIA]);
  assert.deepEqual(mapArticle({ ...api, topics: undefined } as ApiArticle).topics, []);
});

test("topic list and detail parsing is defensive", () => {
  assert.equal(parseTopicList(null), null);
  assert.equal(parseTopicList({}), null);
  const list = parseTopicList({
    topics: [
      { slug: "nvidia", name: "Nvidia", kind: "company", articleCount: 12, updatedAt: "2026-09-30T10:00:00Z" },
      { slug: "weird", name: "Weird", kind: "alien", articleCount: 1 },
      { slug: "gpt-6", name: "GPT-6", kind: "product", articleCount: "3" },
    ],
  });
  assert.deepEqual(list?.map((t) => [t.slug, t.articleCount]), [["nvidia", 12], ["gpt-6", 0]]);

  const detail = parseTopicDetail({
    slug: "nvidia", name: "Nvidia", kind: "company", summary: " Chips. ", facts: ["a", 5, " "], articleCount: 4,
    related: [{ slug: "nvidia", name: "Nvidia" }, { slug: "openai", name: "OpenAI" }, { bad: 1 }],
  });
  assert.equal(detail?.summary, "Chips.");
  assert.deepEqual(detail?.facts, ["a"]);
  assert.deepEqual(detail?.related, [{ slug: "openai", name: "OpenAI" }]);
  assert.equal(parseTopicDetail({ slug: "x", name: "X", kind: "theme", summary: "" }), null);
});

test("getTopic tells missing from unavailable, getTopics fails soft", async () => {
  const res = (status: number, body?: unknown) => new Response(body === undefined ? null : JSON.stringify(body), { status });
  const topic = { slug: "nvidia", name: "Nvidia", kind: "company", summary: "S.", facts: [], articleCount: 0, related: [] };
  const found = await getTopic("nvidia", (async () => res(200, { topic, articles: [{ slug: "a" }, null] })) as typeof fetch);
  assert.equal(found.state, "found");
  if (found.state === "found") assert.equal(found.articles.length, 1);

  const calls: string[] = [];
  const missing = await getTopic("x", (async (url: string) => { calls.push(url); return url.endsWith("/api/health") ? res(200, {}) : res(404); }) as typeof fetch);
  assert.equal(missing.state, "missing");
  const down = await getTopic("x", (async (url: string) => (url.endsWith("/api/health") ? res(503) : res(404))) as typeof fetch);
  assert.equal(down.state, "unavailable");
  assert.equal((await getTopic("x", (async () => res(500)) as typeof fetch)).state, "unavailable");
  assert.equal((await getTopic("x", (async () => { throw new Error("net"); }) as typeof fetch)).state, "unavailable");
  assert.equal((await getTopic("x", (async () => res(200, { topic: {}, articles: [] })) as typeof fetch)).state, "unavailable");

  assert.equal(await getTopics((async () => res(404)) as typeof fetch), null);
  assert.equal(await getTopics((async () => { throw new Error("net"); }) as typeof fetch), null);
});

test("topics group by kind, most covered first, skipping empty kinds", () => {
  const groups = groupTopicsByKind([
    { slug: "a", name: "A", kind: "theme", articleCount: 3 },
    { slug: "b", name: "B", kind: "company", articleCount: 5 },
    { slug: "c", name: "C", kind: "company", articleCount: 9 },
  ]);
  assert.deepEqual(groups.map((g) => g.kind), ["company", "theme"]);
  assert.deepEqual(groups[0].topics.map((t) => t.slug), ["c", "b"]);
});

test("first sentence for meta descriptions", () => {
  assert.equal(firstSentence("Nvidia makes chips. It is big."), "Nvidia makes chips.");
  assert.equal(firstSentence("No end here"), "No end here");
  assert.equal(firstSentence("Version 1.5 shipped. Next."), "Version 1.5 shipped.");
  assert.ok(firstSentence("word ".repeat(100), 50).length <= 50);
});

test("articles group by UTC month in the given order", () => {
  const items = [
    { id: 1, d: "2026-09-30T23:30:00Z" },
    { id: 2, d: "2026-09-02T10:00:00Z" },
    { id: 3, d: "2026-08-15T10:00:00Z" },
    { id: 4, d: "2025-12-31T23:59:59Z" },
    { id: 5, d: undefined },
  ];
  const groups = groupByMonth(items, (i) => i.d);
  assert.deepEqual(groups.map((g) => [g.key, g.label, g.items.map((i) => i.id)]), [
    ["2026-09", "September 2026", [1, 2]],
    ["2026-08", "August 2026", [3]],
    ["2025-12", "December 2025", [4]],
    ["unknown", "Earlier", [5]],
  ]);
});

test("links the first plain mention of each topic, keeping the article's wording", () => {
  const { html, slugs } = linkTopicMentions("<p>NVIDIA shipped it. Nvidia again.</p>", [NVIDIA]);
  assert.equal(html, `<p>${link("nvidia", "NVIDIA")} shipped it. Nvidia again.</p>`);
  assert.deepEqual(slugs, ["nvidia"]);
});

test("never links inside existing links, headings, or non-paragraph markup", () => {
  const glossary = `<a href="/glossary/x" class="glossary-term">Nvidia</a>`;
  const input = `<h2>Nvidia news</h2><p>${glossary} said so.</p><blockquote>Nvidia</blockquote><p>Then Nvidia.</p>`;
  const { html, slugs } = linkTopicMentions(input, [NVIDIA]);
  assert.equal(html, `<h2>Nvidia news</h2><p>${glossary} said so.</p><blockquote>Nvidia</blockquote><p>Then ${link("nvidia", "Nvidia")}.</p>`);
  assert.deepEqual(slugs, ["nvidia"]);
});

test("caps links at three, prefers longer names, respects word edges and acronyms", () => {
  const topics = [
    { slug: "openai", name: "OpenAI" },
    { slug: "gpt-6", name: "GPT-6" },
    { slug: "gpt", name: "GPT" },
    { slug: "nvidia", name: "Nvidia" },
    { slug: "meta", name: "Meta" },
  ];
  const input = "<p>Not Metadata, but gpt-6 and GPT-6 and gpt. OpenAI's Nvidia deal, then Meta.</p>";
  const { html, slugs } = linkTopicMentions(input, topics);
  assert.deepEqual(slugs, ["gpt-6", "openai", "nvidia"]);
  assert.ok(html.includes("Not Metadata"));
  assert.ok(html.includes(`and ${link("gpt-6", "GPT-6")} and gpt.`));
  assert.ok(html.includes(`${link("openai", "OpenAI")}'s`));
  assert.ok(!html.includes('data-topic="meta"'));
});

test("no topics or no match leaves the html untouched", () => {
  const input = "<p>Nothing here.</p>";
  assert.deepEqual(linkTopicMentions(input, []), { html: input, slugs: [] });
  assert.deepEqual(linkTopicMentions(input, [NVIDIA]), { html: input, slugs: [] });
});
