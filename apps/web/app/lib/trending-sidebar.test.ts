import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const apiSource = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
const sidebarSource = readFileSync(new URL("../components/Sidebar.tsx", import.meta.url), "utf8");
const tabsSource = readFileSync(new URL("../components/TrendingTabs.tsx", import.meta.url), "utf8");

test("trending articles can be requested for a time window", () => {
  assert.match(apiSource, /export async function getTrendingArticles\(limit = 5, window\?: TrendingWindow\)/);
  assert.match(apiSource, /export type TrendingWindow = "24h" \| "7d"/);
  assert.match(apiSource, /window=\$\{window\}/);
});

test("the sidebar ranks by today's and this week's reads, not all-time views", () => {
  assert.match(sidebarSource, /getTrendingArticles\(5, "24h"\)/);
  assert.match(sidebarSource, /getTrendingArticles\(5, "7d"\)/);
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
