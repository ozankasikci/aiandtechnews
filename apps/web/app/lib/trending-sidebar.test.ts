import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const apiSource = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
const sidebarSource = readFileSync(new URL("../components/Sidebar.tsx", import.meta.url), "utf8");
const tabsSource = readFileSync(new URL("../components/TrendingTabs.tsx", import.meta.url), "utf8");

test("trending articles can be requested for a time window", () => {
  assert.match(apiSource, /export async function getTrendingArticles\(limit = 5, window\?: TrendingWindow, cache: ListCache = \"list\"\)/);
  assert.match(apiSource, /export type TrendingWindow = "24h" \| "7d"/);
  assert.match(apiSource, /window=\$\{window\}/);
});

test("the sidebar ranks by today's and this week's reads, not all-time views", () => {
  assert.match(sidebarSource, /getTrendingArticles\(5, "24h", cache\)/);
  assert.match(sidebarSource, /getTrendingArticles\(5, "7d", cache\)/);
  assert.match(sidebarSource, /Trending/);
  assert.doesNotMatch(sidebarSource, /Most Popular/);
});

test("the sidebar switches between Today and This week on the client, showing each story's age", () => {
  assert.match(tabsSource, /^"use client";/);
  assert.match(tabsSource, /Today/);
  assert.match(tabsSource, /This week/);
  assert.match(tabsSource, /aria-selected=\{/);
  assert.match(tabsSource, /item\.time/);
});

test("the homepage's colored cards show five of the week's most-read stories not already trending today", () => {
  const home = readFileSync(new URL("../page.tsx", import.meta.url), "utf8");
  assert.match(home, /getTrendingArticles\(20, "7d"\)/);
  assert.match(home, /getTrendingArticles\(5, "24h"\)/);
  assert.match(home, /!trendingToday\.has\(a\.slug\) && Date\.parse\(a\.publishedAt \?\? ""\) < dayAgo/, "skips today's news");
  assert.match(home, /const dayAgo = Date\.now\(\) - 24 \* 3_600_000;/);
  assert.match(home, /Popular last week/);
});
