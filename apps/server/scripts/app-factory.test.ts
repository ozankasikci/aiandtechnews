import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readdirSync, rmSync, unlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import type { AddressInfo } from "node:net";
import test from "node:test";
import bcrypt from "bcryptjs";
import type { Express } from "express";
import { createApp } from "../src/app";
import { createAuth } from "../src/auth";
import { initializeDatabase, openDatabase } from "../src/db";
import { createUpload } from "../src/upload";

const temporaryRoots: string[] = [];

test.afterEach(() => {
  while (temporaryRoots.length) rmSync(temporaryRoots.pop()!, { recursive: true, force: true });
});

function temporaryRoot(): string {
  const root = mkdtempSync(path.join(tmpdir(), "technews-app-factory-"));
  temporaryRoots.push(root);
  return root;
}

function newsletterDenyFake() {
  let calls = 0;
  return {
    get calls() { return calls; },
    async requestSubscription() { calls += 1; return { state: "subscribed" as const }; },
    async confirmSubscription() { calls += 1; throw new Error("newsletter effect denied"); },
    unsubscribe() { calls += 1; throw new Error("newsletter effect denied"); },
    listEditions() { calls += 1; throw new Error("newsletter effect denied"); },
    getEdition() { calls += 1; throw new Error("newsletter effect denied"); },
    async sendDailyDigest() { calls += 1; throw new Error("newsletter effect denied"); },
  };
}

function makeApp(root: string, now = () => 1_000_000) {
  const uploadRoot = path.join(root, "uploads");
  mkdirSync(uploadRoot);
  const database = openDatabase(path.join(root, "test.db"));
  initializeDatabase(database, { seedDefaults: false });
  const newsletter = newsletterDenyFake();
  let indexNowCalls = 0;
  const app = createApp({
    db: database,
    newsletter,
    auth: createAuth("test-secret"),
    upload: createUpload(uploadRoot),
    uploadRoot,
    fileOperations: { existsSync, unlinkSync },
    newsletterCronSecret: "test-cron-secret",
    now,
    notifyIndexNow: async () => { indexNowCalls += 1; return { status: 202, submitted: 0 }; },
  });
  return { app, database, newsletter, getIndexNowCalls: () => indexNowCalls };
}

async function withServer<T>(app: Express, run: (origin: string) => Promise<T>): Promise<T> {
  const server = app.listen(0, "127.0.0.1");
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
  const address = server.address() as AddressInfo;
  try {
    return await run(`http://127.0.0.1:${address.port}`);
  } finally {
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
}

test("app, database, router, auth, and upload modules are import-side-effect free", async () => {
  const root = temporaryRoot();
  const before = readdirSync(root);
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (() => { throw new Error("network effect denied during import"); }) as typeof fetch;
  try {
    await Promise.all([
      import("../src/app"),
      import("../src/db"),
      import("../src/routes/public"),
      import("../src/routes/dashboard"),
      import("../src/auth"),
      import("../src/upload"),
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.deepEqual(readdirSync(root), before);
});

test("schema initialization without defaults creates empty production tables", () => {
  const root = temporaryRoot();
  const database = openDatabase(path.join(root, "empty.db"));
  try {
    initializeDatabase(database, { seedDefaults: false });
    for (const table of ["categories", "authors", "settings"]) {
      assert.equal((database.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get() as { count: number }).count, 0);
    }
  } finally {
    database.close();
  }
});

test("production seed behavior remains explicitly opt-in", () => {
  const root = temporaryRoot();
  const database = openDatabase(path.join(root, "seeded.db"));
  try {
    initializeDatabase(database, { seedDefaults: true });
    assert.equal((database.prepare("SELECT COUNT(*) AS count FROM categories").get() as { count: number }).count, 3);
    assert.equal((database.prepare("SELECT COUNT(*) AS count FROM authors").get() as { count: number }).count, 2);
    assert.equal((database.prepare("SELECT COUNT(*) AS count FROM settings").get() as { count: number }).count, 8);
    const author = database.prepare("SELECT password_hash FROM authors WHERE email = ?").get("admin@technews.com") as { password_hash: string };
    assert.equal(bcrypt.compareSync("technews2026", author.password_hash), true);
  } finally {
    database.close();
  }
});

test("factory serves health and public reads without external effects", async () => {
  const root = temporaryRoot();
  const fixture = makeApp(root);
  try {
    fixture.database.prepare("INSERT INTO categories (name, slug) VALUES (?, ?)").run("AI", "ai");
    fixture.database.prepare("INSERT INTO authors (name, email, password_hash) VALUES (?, ?, ?)").run("Editor", "editor@example.test", "unused");
    fixture.database.prepare(`INSERT INTO articles
      (title, slug, excerpt, content, category_id, author_id, status, published_at)
      VALUES (?, ?, ?, ?, 1, 1, 'published', ?)`)
      .run("Fixture", "fixture", "Excerpt", "<p>Body</p>", "2026-01-01T00:00:00.000Z");

    await withServer(fixture.app, async (origin) => {
      const health = await fetch(`${origin}/api/health`);
      assert.equal(health.status, 200);
      assert.deepEqual(await health.json(), { status: "ok" });
      const response = await fetch(`${origin}/api/articles`);
      assert.equal(response.status, 200);
      const body = await response.json() as { articles: Array<{ slug: string }> };
      assert.deepEqual(body.articles.map((article) => article.slug), ["fixture"]);
    });
    assert.equal(fixture.newsletter.calls, 0);
    assert.equal(fixture.getIndexNowCalls(), 0);
  } finally {
    fixture.database.close();
  }
});

test("app instances isolate database and signup limiter state", async () => {
  const first = makeApp(temporaryRoot());
  const second = makeApp(temporaryRoot());
  try {
    first.database.prepare("INSERT INTO categories (name, slug) VALUES (?, ?)").run("First", "first");
    second.database.prepare("INSERT INTO categories (name, slug) VALUES (?, ?)").run("Second", "second");

    await withServer(first.app, async (firstOrigin) => {
      const categories = await (await fetch(`${firstOrigin}/api/categories`)).json() as { categories: Array<{ slug: string }> };
      assert.deepEqual(categories.categories.map(({ slug }) => slug), ["first"]);
      for (let attempt = 0; attempt < 30; attempt += 1) {
        await fetch(`${firstOrigin}/api/subscribe`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ email: "reader@example.test" }),
        });
      }
      assert.equal((await fetch(`${firstOrigin}/api/subscribe`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email: "reader@example.test" }),
      })).status, 429);
    });

    await withServer(second.app, async (secondOrigin) => {
      const categories = await (await fetch(`${secondOrigin}/api/categories`)).json() as { categories: Array<{ slug: string }> };
      assert.deepEqual(categories.categories.map(({ slug }) => slug), ["second"]);
      assert.notEqual((await fetch(`${secondOrigin}/api/subscribe`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email: "reader@example.test" }),
      })).status, 429);
    });
  } finally {
    first.database.close();
    second.database.close();
  }
});
