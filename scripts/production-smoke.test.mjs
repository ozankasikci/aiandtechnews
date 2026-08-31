import assert from "node:assert/strict";
import test from "node:test";
import {
  countSitemapArticleUrls,
  extractFirstArticlePath,
  runProductionSmoke,
} from "./production-smoke.mjs";

test("extracts the first article link from the homepage", () => {
  assert.equal(
    extractFirstArticlePath('<a href="/about">About</a><a href="/article/example-story">Story</a>'),
    "/article/example-story",
  );
});

test("rejects a homepage without an article link", () => {
  assert.throws(() => extractFirstArticlePath('<a href="/about">About</a>'), /does not link/);
});

test("counts canonical article URLs in the sitemap", () => {
  const sitemap = [
    "<loc>https://www.aiandtech.news</loc>",
    "<loc>https://www.aiandtech.news/article/first-story</loc>",
    "<loc>https://www.aiandtech.news/article/second-story</loc>",
  ].join("");
  assert.equal(countSitemapArticleUrls(sitemap), 2);
});

test("checks the backend, homepage article, sitemap, and newsletter archive", async () => {
  const responses = new Map([
    ["https://api.example.com/api/health", new Response(JSON.stringify({ status: "ok" }))],
    ["https://api.example.com/api/articles?limit=1", new Response(JSON.stringify({ articles: [{ slug: "story" }] }))],
    ["https://site.example.com", new Response('<a href="/article/story">Story</a>')],
    ["https://site.example.com/article/story", new Response("<article>Story body</article>")],
    ["https://site.example.com/sitemap.xml", new Response("<loc>https://www.aiandtech.news/article/story</loc>")],
    ["https://api.example.com/api/newsletter/editions?limit=1", new Response(JSON.stringify({ editions: [] }))],
  ]);
  const fakeFetch = async (input) => {
    const response = responses.get(String(input));
    if (!response) return new Response("missing", { status: 404 });
    return response.clone();
  };

  const result = await runProductionSmoke({
    apiOrigin: "https://api.example.com",
    fetchImplementation: fakeFetch,
    siteOrigin: "https://site.example.com",
  });

  assert.equal(result.articlePath, "/article/story");
  assert.equal(result.articleUrlCount, 1);
  assert.equal(result.checks.length, 6);
});

test("fails immediately when the backend health route is unavailable", async () => {
  const fakeFetch = async () => new Response("Not Found", { status: 404 });

  await assert.rejects(
    runProductionSmoke({
      apiOrigin: "https://api.example.com",
      fetchImplementation: fakeFetch,
      siteOrigin: "https://site.example.com",
    }),
    /Backend health check returned HTTP 404: Not Found/,
  );
});
