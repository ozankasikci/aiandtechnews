import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  INDEXNOW_CATCH_UP_WINDOW_MS,
  INDEXNOW_KEY,
  isAuthorizedCronRequest,
  recentlyChangedArticleUrls,
  submitIndexNowUrls,
} from "./indexnow";

test("the web catch-up job uses the hosted IndexNow key", () => {
  const keyFile = readFileSync(new URL(`../../public/${INDEXNOW_KEY}.txt`, import.meta.url), "utf8");
  assert.equal(keyFile.trim(), INDEXNOW_KEY);
});

test("Vercel schedules the IndexNow catch-up once per day", () => {
  const vercelConfig = JSON.parse(
    readFileSync(new URL("../../vercel.json", import.meta.url), "utf8"),
  ) as { crons: Array<{ path: string; schedule: string }> };

  assert.deepEqual(
    vercelConfig.crons.find((cron) => cron.path === "/api/indexnow"),
    { path: "/api/indexnow", schedule: "0 6 * * *" },
  );
});

test("recently changed articles include new and edited stories only", () => {
  const now = Date.UTC(2026, 7, 30, 12);
  const urls = recentlyChangedArticleUrls([
    {
      slug: "new-story",
      published_at: new Date(now - 60 * 60 * 1000).toISOString(),
      updated_at: new Date(now - 60 * 60 * 1000).toISOString(),
    },
    {
      slug: "edited-story",
      published_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
      updated_at: new Date(now - 2 * 60 * 60 * 1000).toISOString(),
    },
    {
      slug: "unchanged-story",
      published_at: new Date(now - INDEXNOW_CATCH_UP_WINDOW_MS - 1).toISOString(),
      updated_at: new Date(now - INDEXNOW_CATCH_UP_WINDOW_MS - 1).toISOString(),
    },
  ], now);

  assert.deepEqual(urls, [
    "https://www.aiandtech.news/article/new-story",
    "https://www.aiandtech.news/article/edited-story",
  ]);
});

test("IndexNow submits unique canonical URLs with the hosted key", async () => {
  let requestUrl = "";
  let requestInit: RequestInit | undefined;
  const fakeFetch: typeof fetch = async (input, init) => {
    requestUrl = String(input);
    requestInit = init;
    return new Response(null, { status: 202 });
  };

  const result = await submitIndexNowUrls([
    "https://www.aiandtech.news/article/first-story",
    "https://www.aiandtech.news/article/first-story",
  ], { fetchImplementation: fakeFetch });

  assert.deepEqual(result, { status: 202, submitted: 1 });
  assert.equal(requestUrl, "https://api.indexnow.org/indexnow");
  assert.deepEqual(JSON.parse(String(requestInit?.body)), {
    host: "www.aiandtech.news",
    key: INDEXNOW_KEY,
    keyLocation: `https://www.aiandtech.news/${INDEXNOW_KEY}.txt`,
    urlList: ["https://www.aiandtech.news/article/first-story"],
  });
});

test("cron authorization requires the configured bearer secret", () => {
  assert.equal(isAuthorizedCronRequest("Bearer secret-value", "secret-value"), true);
  assert.equal(isAuthorizedCronRequest("Bearer wrong-value", "secret-value"), false);
  assert.equal(isAuthorizedCronRequest("Bearer undefined", undefined), false);
});

test("IndexNow rejects foreign URLs before sending a request", async () => {
  let called = false;
  const fakeFetch: typeof fetch = async () => {
    called = true;
    return new Response(null, { status: 200 });
  };

  await assert.rejects(
    submitIndexNowUrls(["https://example.com/article/story"], { fetchImplementation: fakeFetch }),
    /does not belong/,
  );
  assert.equal(called, false);
});
