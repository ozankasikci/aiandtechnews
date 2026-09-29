import assert from "node:assert/strict";
import test from "node:test";
import { MAX_REVALIDATE_SLUGS, handleRevalidateRequest, parseRevalidateBody, revalidateTargets } from "./revalidate";

function post(body: unknown, authorization?: string, raw?: string) {
  const headers: Record<string, string> = { "content-type": "application/json" };
  if (authorization) headers.authorization = authorization;
  return new Request("https://www.aiandtech.news/api/revalidate", {
    method: "POST",
    headers,
    body: raw ?? JSON.stringify(body),
  });
}

function recorder(secret: string | undefined = "cron-secret") {
  const tags: string[] = [];
  const paths: string[] = [];
  return {
    tags,
    paths,
    deps: {
      secret,
      revalidateTag: (tag: string) => void tags.push(tag),
      revalidatePath: (path: string, type?: string) => void paths.push(type ? `${path} (${type})` : path),
    },
  };
}

test("rejects requests without the cron secret and revalidates nothing", async () => {
  for (const [authorization, secret] of [
    [undefined, "cron-secret"],
    ["Bearer wrong", "cron-secret"],
    ["cron-secret", "cron-secret"],
    ["Bearer undefined", undefined],
    ["Bearer ", ""],
  ] as const) {
    const calls = recorder(secret);
    const response = await handleRevalidateRequest(post({ slugs: ["a"] }, authorization), calls.deps);
    assert.equal(response.status, 401);
    assert.deepEqual([calls.tags, calls.paths], [[], []]);
  }
});

test("revalidates each article page and tag plus every list page", async () => {
  const calls = recorder();
  const response = await handleRevalidateRequest(
    post({ slugs: ["new-story", "old-story", "new-story"] }, "Bearer cron-secret"),
    calls.deps,
  );
  assert.equal(response.status, 200);
  const json = await response.json();
  assert.equal(json.revalidated, true);
  assert.deepEqual(json.slugs, ["new-story", "old-story"]);
  assert.deepEqual(calls.tags, ["article:new-story", "article:old-story", "articles"]);
  assert.deepEqual(calls.paths, [
    "/article/new-story",
    "/article/old-story",
    "/",
    "/archive",
    "/[category] (page)",
    "/sitemap.xml",
    "/news-sitemap.xml",
    "/rss.xml",
    "/og/today",
  ]);
});

test("an empty slug list still refreshes the list pages", async () => {
  const calls = recorder();
  const response = await handleRevalidateRequest(post({ slugs: [] }, "Bearer cron-secret"), calls.deps);
  assert.equal(response.status, 200);
  assert.deepEqual(calls.tags, ["articles"]);
  assert.ok(calls.paths.includes("/"));
});

test("rejects malformed bodies and slugs without revalidating", async () => {
  const tooMany = Array.from({ length: MAX_REVALIDATE_SLUGS + 1 }, (_, i) => `slug-${i}`);
  const bodies: unknown[] = [
    null,
    [],
    "slugs",
    {},
    { slugs: "a" },
    { slugs: tooMany },
    { slugs: ["ok", 5] },
    { slugs: ["../etc"] },
    { slugs: ["has space"] },
    { slugs: [".."] },
    { slugs: ["a/b"] },
    { slugs: ["-lead"] },
    { slugs: ["q?x=1"] },
    { slugs: ["a".repeat(201)] },
    { slugs: [""] },
  ];
  for (const body of bodies) {
    const calls = recorder();
    const response = await handleRevalidateRequest(post(body, "Bearer cron-secret"), calls.deps);
    assert.equal(response.status, 400, JSON.stringify(body)?.slice(0, 80));
    assert.deepEqual([calls.tags, calls.paths], [[], []]);
  }
  const calls = recorder();
  const response = await handleRevalidateRequest(post(undefined, "Bearer cron-secret", "{not json"), calls.deps);
  assert.equal(response.status, 400);
  assert.deepEqual(calls.tags, []);
});

test("accepts the slugs the API generates", () => {
  assert.deepEqual(parseRevalidateBody({ slugs: ["openai-ships-gpt-6-to-everyone", "a1"] }), {
    ok: true,
    slugs: ["openai-ships-gpt-6-to-everyone", "a1"],
  });
  assert.equal(revalidateTargets(["x"]).tags[0], "article:x");
});
