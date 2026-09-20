import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
  captureContracts,
  CONTRACT_OPERATION_IDENTITIES,
  sendContractRequest,
  serializeContracts,
  shutdownChild,
  watchReadyLine,
} from "./capture-contracts";

const serverRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
const fixturePath = path.join(serverRoot, "contracts", "node", "contracts.json");

function jsonPointer(value: any, pointer: string): unknown {
  return pointer.split("/").slice(1).reduce(
    (current, token) => current[token.replaceAll("~1", "/").replaceAll("~0", "~")],
    value,
  );
}

function applyResponseDerivedBindings(value: unknown, bindings: any[], responses: Record<string, unknown>): unknown {
  const replacements = bindings
    .filter((binding) => binding.resolver.type === "responseJsonPointerTransform")
    .map((binding) => {
      const resolver = binding.resolver;
      const selected = jsonPointer(responses[resolver.operationId], resolver.pointer);
      assert.equal(typeof selected, "string");
      assert.equal(resolver.transform, "basename");
      return [path.posix.basename(selected as string), binding.placeholder] as const;
    });
  const replace = (item: unknown): unknown => {
    if (typeof item === "string") {
      return replacements.reduce((result, [runtime, placeholder]) => result.replaceAll(runtime, placeholder), item);
    }
    if (Array.isArray(item)) return item.map(replace);
    if (item && typeof item === "object") return Object.fromEntries(Object.entries(item).map(([key, nested]) => [key, replace(nested)]));
    return item;
  };
  return replace(value);
}

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

const expectedStatuses: Record<string, number> = Object.fromEntries(expected.map(([operationId]) => [operationId, 200]));
Object.assign(expectedStatuses, {
  "dashboard.articles.create": 201,
  "dashboard.categories.create": 201,
  "dashboard.media.upload": 201,
});

test("canonical operation identities cover exactly the 32 retained operations", () => {
  assert.equal(CONTRACT_OPERATION_IDENTITIES.length, 32);
  assert.deepEqual(CONTRACT_OPERATION_IDENTITIES.map(({ operationId, method, path }) => [operationId, method, path]), expected);
  assert.equal(new Set(CONTRACT_OPERATION_IDENTITIES.map(({ operationId }) => operationId)).size, 32);
});

test("reviewed fixture has exact coverage, safe metadata, and representative legacy behavior", () => {
  const fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
  assert.equal(fixture.schemaVersion, 2);
  assert.equal(fixture.source, "node-contract-server");
  assert.equal(fixture.fixedClock, "2026-09-20T12:00:00.000Z");
  assert.deepEqual(fixture.operations.map((entry: any) => [entry.operationId, entry.request.method, entry.request.path]), expected);
  const slug = fixture.operations.find((entry: any) => entry.operationId === "articles.getBySlug");
  assert.equal(slug.response.body.article.view_count, 42, "slug response exposes pre-increment view_count");
  assert.equal(fixture.observations.authLogoutJwtStillValid, true);
  assert.equal(fixture.observations.dashboardUnknownRoute.status, 401);
  assert.deepEqual(
    Object.fromEntries(fixture.operations.map((entry: any) => [entry.operationId, entry.response.status])),
    expectedStatuses,
  );
});

test("every templated request has exact concrete path parameters and reconstructs its captured path", () => {
  const fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
  const templated = fixture.operations.filter((entry: any) => entry.request.path.includes(":"));
  assert.equal(templated.length, 9);
  for (const entry of templated) {
    const names = [...entry.request.path.matchAll(/:([A-Za-z][A-Za-z0-9_]*)/g)].map((match: RegExpMatchArray) => match[1]);
    assert.deepEqual(Object.keys(entry.request.pathParameters ?? {}).sort(), [...names].sort(), entry.operationId);
    for (const value of Object.values(entry.request.pathParameters)) {
      assert.equal(["string", "number", "boolean"].includes(typeof value), true, entry.operationId);
      assert.notEqual(String(value).length, 0, entry.operationId);
    }
    const reconstructed = entry.request.path.replace(/:([A-Za-z][A-Za-z0-9_]*)/g, (_: string, name: string) =>
      encodeURIComponent(String(entry.request.pathParameters[name])));
    assert.equal(reconstructed, entry.request.actualPath, entry.operationId);
  }
});

test("created-resource dependencies are explicit and agree with response IDs", () => {
  const fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
  for (const [kind, createId, dependentIds] of [
    ["article", "dashboard.articles.create", ["dashboard.articles.update", "dashboard.articles.delete"]],
    ["category", "dashboard.categories.create", ["dashboard.categories.update", "dashboard.categories.delete"]],
    ["media", "dashboard.media.upload", ["dashboard.media.delete"]],
  ] as const) {
    const created = fixture.operations.find((entry: any) => entry.operationId === createId);
    const id = created.response.body[kind].id;
    for (const dependentId of dependentIds) {
      const dependent = fixture.operations.find((entry: any) => entry.operationId === dependentId);
      assert.deepEqual(dependent.dependencies, [{
        operationId: createId,
        responsePointer: `/${kind}/id`,
        requestTarget: "/pathParameters/id",
      }]);
      assert.equal(dependent.request.pathParameters.id, id);
    }
  }
});

test("replay bindings contain safe compatibility vectors and reproducible multipart bytes", () => {
  const fixture = JSON.parse(readFileSync(fixturePath, "utf8"));
  const bindings = Object.fromEntries(fixture.replay.bindings.map((binding: any) => [binding.placeholder, binding]));
  assert.deepEqual(Object.keys(bindings).sort(), [
    "$AUTHORIZATION", "$CRON_AUTHORIZATION", "$JWT", "$MULTIPART_BOUNDARY", "$NEWSLETTER_CONFIRM_TOKEN",
    "$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN", "$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN", "$PASSWORD", "$UPLOAD_FILENAME",
  ].sort());
  assert.deepEqual(bindings.$JWT.vector.claims, {
    id: 201,
    email: "editorial@example.invalid",
    role: "admin",
    iat: 1789905600,
    exp: 1790510400,
  });
  assert.equal(bindings.$JWT.vector.alg, "HS256");
  assert.equal(bindings.$JWT.vector.claims.exp - bindings.$JWT.vector.claims.iat, 604800);
  assert.match(bindings.$JWT.vector.sha256, /^[a-f0-9]{64}$/);
  for (const [placeholder, id, purpose] of [
    ["$NEWSLETTER_CONFIRM_TOKEN", 501, "confirm"],
    ["$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN", 502, "unsubscribe"],
    ["$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN", 503, "unsubscribe"],
  ] as const) {
    assert.match(bindings[placeholder].vector.sha256, /^[a-f0-9]{64}$/);
    assert.deepEqual(bindings[placeholder].vector.payload, { v: 1, id, purpose, exp: 1789992000 });
    assert.equal(bindings[placeholder].resolver.expiresAt, "2026-09-21T12:00:00.000Z");
  }
  const upload = fixture.operations.find((entry: any) => entry.operationId === "dashboard.media.upload");
  assert.equal(Buffer.from(upload.request.multipart.contentBase64, "base64").length, upload.request.multipart.size);
  assert.equal(Buffer.from(upload.request.multipart.contentBase64, "base64").toString("utf8"), "synthetic image bytes");

  const runtimeBasename = "0123456789abcdef0123456789abcdef.png";
  const runtimeBody = structuredClone(upload.response.body);
  runtimeBody.media.url = `/uploads/${runtimeBasename}`;
  assert.equal(bindings.$UPLOAD_FILENAME.resolver.type, "responseJsonPointerTransform");
  assert.equal(bindings.$UPLOAD_FILENAME.resolver.pointer, "/media/url");
  assert.equal(bindings.$UPLOAD_FILENAME.resolver.transform, "basename");
  assert.deepEqual(
    applyResponseDerivedBindings(runtimeBody, fixture.replay.bindings, { "dashboard.media.upload": runtimeBody }),
    upload.response.body,
  );
  assert.equal(
    path.posix.basename(jsonPointer(runtimeBody, bindings.$UPLOAD_FILENAME.resolver.pointer) as string),
    runtimeBasename,
  );
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
  assert.doesNotMatch(text, /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/);
  assert.doesNotMatch(text, /synthetic-contract-(?:jwt|newsletter|cron)-secret/);
  assert.doesNotMatch(text, /contract-test-password|\$2[aby]\$/);
  const wallDate = new Date().toISOString().slice(0, 10);
  if (wallDate !== "2026-09-20") assert.equal(text.includes(wallDate), false, "current wall date must not leak into fixtures");
  for (const volatile of ["date", "etag", "connection", "keep-alive", "transfer-encoding", "content-length", "set-cookie"]) {
    assert.equal(text.includes(`\"${volatile}\"`), false, `volatile header ${volatile} must be excluded`);
  }
  const fixture = JSON.parse(text);
  const visit = (value: unknown): void => {
    if (typeof value === "string") {
      for (const match of value.matchAll(/https?:\/\/[^\s"<>]+/g)) {
        const hostname = new URL(match[0]).hostname;
        assert.equal(hostname === "127.0.0.1" || hostname === "localhost" || hostname === "example.invalid" || hostname.endsWith(".example.invalid"), true, match[0]);
      }
      for (const match of value.matchAll(/[A-Z0-9._%+-]+@([A-Z0-9.-]+\.[A-Z]{2,})/gi)) assert.equal(match[1], "example.invalid", match[0]);
    } else if (Array.isArray(value)) value.forEach(visit);
    else if (value && typeof value === "object") Object.values(value).forEach(visit);
  };
  visit(fixture);
});

test("HTTP helper rejects malformed JSON, aborted responses, and absolute-deadline overruns", async (t) => {
  const server = createServer((request, response) => {
    if (request.url === "/malformed") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end("{");
    } else if (request.url === "/aborted") {
      response.writeHead(200, { "content-type": "application/json" });
      response.write("{");
      response.destroy();
    } else if (request.url === "/drip") {
      response.writeHead(200, { "content-type": "text/plain" });
      const interval = setInterval(() => response.write("."), 10);
      response.once("close", () => clearInterval(interval));
      setTimeout(() => response.end("done"), 150);
    }
  });
  await new Promise<void>((resolve, reject) => {
    server.listen(0, "127.0.0.1", resolve);
    server.once("error", reject);
  });
  t.after(() => new Promise<void>((resolve) => server.close(() => resolve())));
  const address = server.address();
  assert(address && typeof address !== "string");
  const origin = `http://127.0.0.1:${address.port}`;
  await assert.rejects(sendContractRequest(origin, "GET", "/malformed", {}, 100), /JSON/i);
  await assert.rejects(sendContractRequest(origin, "GET", "/aborted", {}, 100), /aborted|socket hang up/i);
  await assert.rejects(sendContractRequest(origin, "GET", "/timeout", {}, 25), /timeout/i);
  const started = performance.now();
  await assert.rejects(sendContractRequest(origin, "GET", "/drip", {}, 25), /timeout/i);
  assert.ok(performance.now() - started < 100, "absolute deadline must not be extended by response activity");
});

test("ready stdout is one complete line for the whole child lifecycle", async () => {
  const child = spawn(process.execPath, ["-e", [
    "process.stdout.write('{\\\"event\\\":\\\"contract-server-ready\\\",\\\"host\\\":\\\"127.0.0.1\\\",');",
    "setTimeout(() => process.stdout.write('\\\"port\\\":1234}\\n'), 5);",
    "setTimeout(() => process.stdout.write('delayed-extra'), 15);",
    "setTimeout(() => process.exit(0), 25);",
  ].join("")], { stdio: ["ignore", "pipe", "pipe"] });
  const watched = watchReadyLine(child, () => "", 500);
  assert.deepEqual(await watched.ready, { event: "contract-server-ready", host: "127.0.0.1", port: 1234 });
  await new Promise<void>((resolve) => child.once("close", () => resolve()));
  assert.throws(() => watched.validateComplete(), /exactly one|extra/i);
});

test("ready metadata rejects out-of-range ports and child spawn errors", async () => {
  for (const port of [0, 65536]) {
    const child = spawn(process.execPath, ["-e", `process.stdout.write(JSON.stringify({event:'contract-server-ready',host:'127.0.0.1',port:${port}})+'\\n');setTimeout(()=>{},1000)`], { stdio: ["ignore", "pipe", "pipe"] });
    const watched = watchReadyLine(child, () => "", 500);
    await assert.rejects(watched.ready, /unsafe ready metadata/i);
    const exited = new Promise<void>((resolve) => child.once("exit", () => resolve()));
    child.kill("SIGKILL");
    await exited;
  }
  const missing = spawn(path.join(serverRoot, "definitely-not-an-executable"), [], { stdio: ["ignore", "pipe", "pipe"] });
  await assert.rejects(watchReadyLine(missing, () => "", 500).ready, /ENOENT|spawn/i);
});

test("shutdown escalates to SIGKILL within bounded waits", async () => {
  const child = spawn(process.execPath, ["-e", "process.on('SIGTERM',()=>{});process.stdout.write('started\\n');setInterval(()=>{},1000)"], { stdio: ["ignore", "pipe", "pipe"] });
  await new Promise<void>((resolve) => child.stdout.once("data", () => resolve()));
  const result = await shutdownChild(child, 25, 500);
  assert.equal(result.signal, "SIGKILL");
});
