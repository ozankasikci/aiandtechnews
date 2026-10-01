import assert from "node:assert/strict";
import test from "node:test";
import { articleViewUrl, sendArticleView } from "./view-beacon";
import { getPublicApiUrl } from "./api";

const browser = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15";

test("the view URL posts to the API's per-article view endpoint", () => {
  assert.equal(articleViewUrl("https://technews.subtunnel.dev/", "a b"), "https://technews.subtunnel.dev/api/articles/a%20b/view");
});

test("sends a text/plain beacon so the cross-origin POST needs no preflight", async () => {
  const sent: { url: string; type: string }[] = [];
  const ok = sendArticleView("https://api.test", "story", {
    userAgent: browser,
    sendBeacon: (url, data) => {
      sent.push({ url, type: (data as Blob).type });
      return true;
    },
  });
  assert.equal(ok, true);
  assert.deepEqual(sent, [{ url: "https://api.test/api/articles/story/view", type: "text/plain" }]);
});

test("falls back to a keepalive no-cors fetch when sendBeacon is refused", () => {
  const calls: RequestInit[] = [];
  const ok = sendArticleView("https://api.test", "story", {
    userAgent: browser,
    sendBeacon: () => false,
    fetch: (async (_url: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init!);
      return new Response(null, { status: 204 });
    }) as typeof fetch,
  });
  assert.equal(ok, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].keepalive, true);
  assert.equal(calls[0].mode, "no-cors");
});

test("automated browsers and crawlers send nothing", () => {
  let sent = 0;
  const count = () => {
    sent++;
    return true;
  };
  assert.equal(sendArticleView("https://api.test", "story", { webdriver: true, userAgent: browser, sendBeacon: count }), false);
  assert.equal(sendArticleView("https://api.test", "story", { userAgent: "Mozilla/5.0 HeadlessChrome/120", sendBeacon: count }), false);
  assert.equal(sendArticleView("https://api.test", "story", { userAgent: "Googlebot/2.1", sendBeacon: count }), false);
  assert.equal(sent, 0);
});

test("the browser-facing API URL never exposes a private API_URL in production", () => {
  assert.equal(getPublicApiUrl({ NODE_ENV: "production", API_URL: "http://10.0.0.5:4001" }), "https://technews.subtunnel.dev");
  assert.equal(getPublicApiUrl({ NODE_ENV: "production", NEXT_PUBLIC_API_URL: "http://localhost:4001" }), "https://technews.subtunnel.dev");
  assert.equal(getPublicApiUrl({ NODE_ENV: "production", NEXT_PUBLIC_API_URL: "https://api.example.com/" }), "https://api.example.com");
  assert.equal(getPublicApiUrl({ NODE_ENV: "development" }), "http://localhost:4001");
  assert.equal(getPublicApiUrl({ NODE_ENV: "development", API_URL: "http://127.0.0.1:4101" }), "http://127.0.0.1:4101");
});
