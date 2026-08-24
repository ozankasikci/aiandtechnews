import assert from "node:assert/strict";
import test from "node:test";
import { buildRssFeed } from "./rss";

const now = new Date("2026-08-24T12:00:00Z");

test("feed includes channel metadata and a self link", () => {
  const xml = buildRssFeed([], now);
  assert.match(xml, /<title>AI and Tech News<\/title>/);
  assert.match(xml, /<atom:link href="https:\/\/www\.aiandtech\.news\/rss\.xml" rel="self"/);
  assert.match(xml, /<lastBuildDate>Mon, 24 Aug 2026 12:00:00 GMT<\/lastBuildDate>/);
});

test("items carry permalink guids, RFC 822 dates, and escaped text", () => {
  const xml = buildRssFeed(
    [
      {
        slug: "ai-chips",
        title: "AI & Chips <2026>",
        excerpt: 'The "next" wave',
        published_at: "2026-08-23 10:30:00",
        category: { name: "AI" },
      },
    ],
    now,
  );
  assert.match(xml, /<guid isPermaLink="true">https:\/\/www\.aiandtech\.news\/article\/ai-chips<\/guid>/);
  assert.match(xml, /<pubDate>Sun, 23 Aug 2026 10:30:00 GMT<\/pubDate>/);
  assert.match(xml, /<title>AI &amp; Chips &lt;2026&gt;<\/title>/);
  assert.match(xml, /<description>The &quot;next&quot; wave<\/description>/);
  assert.match(xml, /<category>AI<\/category>/);
});

test("items are sorted newest first and unparseable dates are dropped", () => {
  const xml = buildRssFeed(
    [
      { slug: "older", title: "Older", excerpt: "", published_at: "2026-08-20 08:00:00" },
      { slug: "broken", title: "Broken", excerpt: "", published_at: "not-a-date" },
      { slug: "newer", title: "Newer", excerpt: "", published_at: "2026-08-23 08:00:00" },
    ],
    now,
  );
  assert.doesNotMatch(xml, /Broken/);
  assert.ok(xml.indexOf("Newer") < xml.indexOf("Older"));
});
