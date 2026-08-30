import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  articleUrl,
  INDEXNOW_KEY,
  submitArticleSlugsToIndexNow,
  submitIndexNowUrls,
} from "../src/indexnow";

test("the public verification file contains the configured IndexNow key", () => {
  const keyFile = readFileSync(
    new URL(`../../web/public/${INDEXNOW_KEY}.txt`, import.meta.url),
    "utf8",
  );
  assert.equal(keyFile.trim(), INDEXNOW_KEY);
});

test("article URLs use the canonical host and encode the slug", () => {
  assert.equal(
    articleUrl("new model/preview"),
    "https://www.aiandtech.news/article/new%20model%2Fpreview",
  );
});

test("IndexNow submits unique canonical URLs with the hosted key", async () => {
  let requestUrl = "";
  let requestInit: RequestInit | undefined;
  const fakeFetch: typeof fetch = async (input, init) => {
    requestUrl = String(input);
    requestInit = init;
    return new Response(null, { status: 202 });
  };

  const result = await submitArticleSlugsToIndexNow(
    ["first-story", "first-story", "second-story"],
    { fetchImplementation: fakeFetch },
  );

  assert.deepEqual(result, { status: 202, submitted: 2 });
  assert.equal(requestUrl, "https://api.indexnow.org/indexnow");
  assert.equal(requestInit?.method, "POST");
  assert.equal(new Headers(requestInit?.headers).get("content-type"), "application/json; charset=utf-8");
  assert.deepEqual(JSON.parse(String(requestInit?.body)), {
    host: "www.aiandtech.news",
    key: INDEXNOW_KEY,
    keyLocation: `https://www.aiandtech.news/${INDEXNOW_KEY}.txt`,
    urlList: [
      "https://www.aiandtech.news/article/first-story",
      "https://www.aiandtech.news/article/second-story",
    ],
  });
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

test("IndexNow surfaces unsuccessful responses without blocking callers from handling them", async () => {
  const fakeFetch: typeof fetch = async () => new Response("invalid key", { status: 403 });

  await assert.rejects(
    submitArticleSlugsToIndexNow(["story"], { fetchImplementation: fakeFetch }),
    /403: invalid key/,
  );
});
