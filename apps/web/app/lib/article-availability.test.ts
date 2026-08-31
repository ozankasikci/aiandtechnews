import assert from "node:assert/strict";
import test from "node:test";
import { getArticleLookup } from "./api";

const article = {
  id: 1,
  title: "Example",
  slug: "example",
  excerpt: "Example excerpt",
  content: "<p>Example body</p>",
  featured_image: "/example.jpg",
  category_id: 1,
  author_id: 1,
  status: "published",
  published_at: "2026-08-31T08:00:00Z",
  view_count: 0,
  created_at: "2026-08-31T08:00:00Z",
  updated_at: "2026-08-31T08:00:00Z",
  category: { id: 1, name: "AI", slug: "ai", description: "", color: "#8b5cf6" },
  author: { id: 1, name: "TechNews Editorial", email: "", avatar: "", bio: "", role: "editor" },
};

test("returns a published article when the API responds successfully", async () => {
  const fakeFetch: typeof fetch = async () => Response.json({ article });
  assert.deepEqual(await getArticleLookup("example", fakeFetch), { state: "found", article });
});

test("treats a missing slug as a real 404 only when the API is healthy", async () => {
  const fakeFetch: typeof fetch = async (input) =>
    String(input).endsWith("/api/health")
      ? Response.json({ status: "ok" })
      : new Response("Not found", { status: 404 });

  assert.deepEqual(await getArticleLookup("missing", fakeFetch), { state: "missing" });
});

test("treats a uniform API 404 as a temporary backend outage", async () => {
  const fakeFetch: typeof fetch = async () => new Response("Not Found", { status: 404 });
  assert.deepEqual(await getArticleLookup("example", fakeFetch), { state: "unavailable" });
});

test("treats network failures as temporary backend outages", async () => {
  const fakeFetch: typeof fetch = async () => {
    throw new Error("network unavailable");
  };
  assert.deepEqual(await getArticleLookup("example", fakeFetch), { state: "unavailable" });
});
