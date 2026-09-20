import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { captureContracts, CONTRACT_OPERATION_IDENTITIES, serializeContracts } from "./capture-contracts";

const serverRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
const fixturePath = path.join(serverRoot, "contracts", "node", "contracts.json");

const expected = [
  ["health.get", "GET", "/api/health"],
  ["articles.list", "GET", "/api/articles"],
  ["articles.trending", "GET", "/api/articles/trending"],
  ["articles.getBySlug", "GET", "/api/articles/:slug"],
  ["articles.getById", "GET", "/api/articles/id/:id"],
  ["categories.list", "GET", "/api/categories"],
  ["authors.list", "GET", "/api/authors"],
  ["newsletter.subscribe", "POST", "/api/subscribe"],
  ["newsletter.confirm", "GET", "/api/newsletter/confirm"],
  ["newsletter.unsubscribeGet", "GET", "/api/newsletter/unsubscribe"],
  ["newsletter.unsubscribePost", "POST", "/api/newsletter/unsubscribe"],
  ["newsletter.editions", "GET", "/api/newsletter/editions"],
  ["newsletter.edition", "GET", "/api/newsletter/editions/:edition"],
  ["newsletter.digestGet", "GET", "/api/newsletter/digest"],
  ["newsletter.digestPost", "POST", "/api/newsletter/digest"],
  ["auth.login", "POST", "/api/auth/login"],
  ["auth.me", "GET", "/api/auth/me"],
  ["auth.logout", "POST", "/api/auth/logout"],
  ["dashboard.articles.list", "GET", "/api/dashboard/articles"],
  ["dashboard.articles.get", "GET", "/api/dashboard/articles/:id"],
  ["dashboard.articles.create", "POST", "/api/dashboard/articles"],
  ["dashboard.articles.update", "PUT", "/api/dashboard/articles/:id"],
  ["dashboard.articles.delete", "DELETE", "/api/dashboard/articles/:id"],
  ["dashboard.categories.list", "GET", "/api/dashboard/categories"],
  ["dashboard.categories.create", "POST", "/api/dashboard/categories"],
  ["dashboard.categories.update", "PUT", "/api/dashboard/categories/:id"],
  ["dashboard.categories.delete", "DELETE", "/api/dashboard/categories/:id"],
  ["dashboard.media.list", "GET", "/api/dashboard/media"],
  ["dashboard.media.upload", "POST", "/api/dashboard/media/upload"],
  ["dashboard.media.delete", "DELETE", "/api/dashboard/media/:id"],
  ["dashboard.settings.get", "GET", "/api/dashboard/settings"],
  ["dashboard.settings.update", "PUT", "/api/dashboard/settings"],
] as const;

test("canonical operation identities cover exactly the 32 retained operations", () => {
  assert.equal(CONTRACT_OPERATION_IDENTITIES.length, 32);
  assert.deepEqual(CONTRACT_OPERATION_IDENTITIES.map(({ operationId, method, path }) => [operationId, method, path]), expected);
  assert.equal(new Set(CONTRACT_OPERATION_IDENTITIES.map(({ operationId }) => operationId)).size, 32);
});

test("reviewed fixture has exact coverage, safe metadata, and representative legacy behavior", () => {
  const fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
  assert.equal(fixture.schemaVersion, 1);
  assert.equal(fixture.source, "node-contract-server");
  assert.equal(fixture.fixedClock, "2026-09-20T12:00:00.000Z");
  assert.deepEqual(fixture.operations.map((entry: any) => [entry.operationId, entry.request.method, entry.request.path]), expected);
  const slug = fixture.operations.find((entry: any) => entry.operationId === "articles.getBySlug");
  assert.equal(slug.response.body.article.view_count, 42, "slug response exposes pre-increment view_count");
  assert.equal(fixture.observations.authLogoutJwtStillValid, true);
  assert.equal(fixture.observations.dashboardUnknownRoute.status, 401);
});

test("fresh capture is byte-identical twice and matches reviewed fixture", { timeout: 30_000 }, async () => {
  const baseline = readFileSync(fixturePath, "utf8");
  const first = serializeContracts(await captureContracts());
  const second = serializeContracts(await captureContracts());
  assert.equal(first, second);
  assert.equal(first, baseline);
});

test("fixtures contain only synthetic, normalized, stable material", () => {
  const text = readFileSync(fixturePath, "utf8");
  assert.equal(text.endsWith("\n"), true);
  assert.doesNotMatch(text, /\/(?:Users|home|tmp|private\/var)\//);
  assert.doesNotMatch(text, /https?:\/\/(?![^\s"/]*(?:example\.invalid|localhost|127\.0\.0\.1))/);
  assert.doesNotMatch(text, /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/);
  assert.doesNotMatch(text, /synthetic-contract-(?:jwt|newsletter|cron)-secret/);
  assert.doesNotMatch(text, /contract-test-password|\$2[aby]\$/);
  assert.doesNotMatch(text, /@(?!example\.invalid)/);
  const wallDate = new Date().toISOString().slice(0, 10);
  if (wallDate !== "2026-09-20") assert.equal(text.includes(wallDate), false, "current wall date must not leak into fixtures");
  for (const volatile of ["date", "etag", "connection", "keep-alive", "transfer-encoding", "content-length", "set-cookie"]) {
    assert.equal(text.includes(`\"${volatile}\"`), false, `volatile header ${volatile} must be excluded`);
  }
  const fixture = JSON.parse(text);
  assert.deepEqual(fixture.normalization.rules.map((rule: any) => rule.placeholder), [
    "$JWT", "$AUTHORIZATION", "$NEWSLETTER_TOKEN", "$CRON_AUTHORIZATION", "$UPLOAD_FILENAME", "$MULTIPART_BOUNDARY",
    "$PASSWORD",
  ]);
});
