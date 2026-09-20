import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import type { AddressInfo } from "node:net";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import bcrypt from "bcryptjs";
import type { Express } from "express";
import { createApp } from "../src/app";
import { createAuth, type AuthService } from "../src/auth";
import { initializeDatabase, openDatabase } from "../src/db";
import { createUpload } from "../src/upload";

const temporaryRoots: string[] = [];
const serverRoot = fileURLToPath(new URL("..", import.meta.url));

test.afterEach(() => {
  while (temporaryRoots.length) rmSync(temporaryRoots.pop()!, { recursive: true, force: true });
});

function temporaryRoot(): string {
  const root = mkdtempSync(path.join(tmpdir(), "technews-app-factory-"));
  temporaryRoots.push(root);
  return root;
}

function importSandbox(): string {
  const root = mkdtempSync(path.join(serverRoot, ".app-factory-import-"));
  temporaryRoots.push(root);
  cpSync(path.join(serverRoot, "src"), path.join(root, "src"), { recursive: true });
  return root;
}

function recursiveSnapshot(root: string): string[] {
  const entries: string[] = [];
  function visit(directory: string): void {
    for (const name of readdirSync(directory).sort()) {
      const absolutePath = path.join(directory, name);
      const relativePath = path.relative(root, absolutePath);
      const stats = statSync(absolutePath);
      if (stats.isDirectory()) {
        entries.push(`directory:${relativePath}`);
        visit(absolutePath);
      } else {
        entries.push(`file:${relativePath}:${readFileSync(absolutePath).toString("base64")}`);
      }
    }
  }
  visit(root);
  return entries;
}

function importInFreshProcess(modulePaths: string[]): ReturnType<typeof spawnSync> {
  const importScript = `
    globalThis.fetch = () => { throw new Error("network effect denied during import"); };
    await Promise.all(${JSON.stringify(modulePaths.map((modulePath) => pathToFileURL(modulePath).href))}.map((url) => import(url)));
  `;
  return spawnSync(process.execPath, ["--import", "tsx", "--input-type=module", "--eval", importScript], {
    cwd: serverRoot,
    encoding: "utf8",
    timeout: 5_000,
  });
}

function assertFreshImportsArePure(root: string, modulePaths: string[]): void {
  const before = recursiveSnapshot(root);
  const result = importInFreshProcess(modulePaths);
  assert.ifError(result.error);
  assert.equal(result.signal, null, `import subprocess was terminated by ${result.signal}`);
  assert.equal(result.status, 0, `import subprocess failed:\n${result.stderr}`);
  assert.deepEqual(recursiveSnapshot(root), before, "uncached imports changed the sandbox filesystem");
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

test("fresh app, database, router, auth, and upload imports have no side effects", () => {
  const root = importSandbox();
  assertFreshImportsArePure(root, [
    "app.ts",
    "db.ts",
    path.join("routes", "public.ts"),
    path.join("routes", "dashboard.ts"),
    "auth.ts",
    "upload.ts",
  ].map((relativePath) => path.join(root, "src", relativePath)));
});

test("fresh-import harness detects a deliberately side-effectful uncached module", () => {
  const root = importSandbox();
  const fixturePath = path.join(root, "src", "import-side-effect-fixture.ts");
  writeFileSync(fixturePath, `import { writeFileSync } from "node:fs";\nwriteFileSync(new URL("../created-during-import", import.meta.url), "effect");\n`);
  assert.throws(
    () => assertFreshImportsArePure(root, [fixturePath]),
    /uncached imports changed the sandbox filesystem/,
  );
});

test("dashboard preserves the receiver of an injected auth service", async () => {
  const root = temporaryRoot();
  const uploadRoot = path.join(root, "uploads");
  mkdirSync(uploadRoot);
  const database = openDatabase(path.join(root, "receiver.db"));
  initializeDatabase(database, { seedDefaults: false });
  database.prepare("INSERT INTO authors (name, email, password_hash, role) VALUES (?, ?, ?, ?)")
    .run("Receiver", "receiver@example.test", bcrypt.hashSync("password", 4), "admin");

  const receiverAuth: AuthService & { token: string } = {
    token: "receiver-token",
    generateToken() {
      assert.equal(this, receiverAuth);
      return this.token;
    },
    requireAuth(req, res, next) {
      assert.equal(this, receiverAuth);
      if (req.headers.authorization !== `Bearer ${this.token}`) {
        res.status(401).json({ error: "Authentication required" });
        return;
      }
      req.user = { id: 1, email: "receiver@example.test", role: "admin" };
      next();
    },
  };
  const app = createApp({
    db: database,
    newsletter: newsletterDenyFake(),
    auth: receiverAuth,
    upload: createUpload(uploadRoot),
    uploadRoot,
    fileOperations: { existsSync, unlinkSync },
    newsletterCronSecret: "test-cron-secret",
    notifyIndexNow: async () => ({ status: 202, submitted: 0 }),
  });

  try {
    await withServer(app, async (origin) => {
      const login = await fetch(`${origin}/api/auth/login`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email: "receiver@example.test", password: "password" }),
      });
      assert.equal(login.status, 200);
      assert.equal((await login.json() as { token: string }).token, receiverAuth.token);

      const me = await fetch(`${origin}/api/auth/me`, {
        headers: { authorization: `Bearer ${receiverAuth.token}` },
      });
      assert.equal(me.status, 200);
      assert.deepEqual(await me.json(), {
        user: { id: 1, email: "receiver@example.test", role: "admin" },
      });
    });
  } finally {
    database.close();
  }
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

test("the injected clock reaches every newsletter operation", async () => {
  const root = temporaryRoot();
  const uploadRoot = path.join(root, "uploads");
  mkdirSync(uploadRoot);
  const database = openDatabase(path.join(root, "newsletter-clock.db"));
  initializeDatabase(database, { seedDefaults: false });
  const fixedNow = Date.parse("2026-09-20T12:00:00.000Z");
  const calls: Array<[string, Date | undefined]> = [];
  const newsletter = {
    async requestSubscription(_email: string, _placement: string, now?: Date) {
      calls.push(["request", now]);
      return { state: "subscribed" as const };
    },
    async confirmSubscription(_token: string, now?: Date) {
      calls.push(["confirm", now]);
      return { state: "confirmed" as const, welcomeSent: false };
    },
    unsubscribe(_token: string, now?: Date) {
      calls.push(["unsubscribe", now]);
      return "unsubscribed" as const;
    },
    listEditions() { return []; },
    getEdition() { return null; },
    async sendDailyDigest(now?: Date) {
      calls.push(["digest", now]);
      return { edition: "2026-09-20", recipients: 0, sent: 0, failed: 0 };
    },
  };
  const app = createApp({
    db: database,
    newsletter,
    auth: createAuth("test-secret"),
    upload: createUpload(uploadRoot),
    uploadRoot,
    fileOperations: { existsSync, unlinkSync },
    newsletterCronSecret: "test-cron-secret",
    now: () => fixedNow,
    notifyIndexNow: async () => ({ status: 202, submitted: 0 }),
  });

  try {
    await withServer(app, async (origin) => {
      assert.equal((await fetch(`${origin}/api/subscribe`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email: "clock@example.test", placement: "clock-test" }),
      })).status, 200);
      assert.equal((await fetch(`${origin}/api/newsletter/confirm?token=clock-token`)).status, 200);
      assert.equal((await fetch(`${origin}/api/newsletter/unsubscribe?token=clock-token`)).status, 200);
      assert.equal((await fetch(`${origin}/api/newsletter/digest`, {
        headers: { authorization: "Bearer test-cron-secret" },
      })).status, 200);
    });
    assert.deepEqual(calls.map(([operation, now]) => [operation, now?.toISOString()]), [
      ["request", "2026-09-20T12:00:00.000Z"],
      ["confirm", "2026-09-20T12:00:00.000Z"],
      ["unsubscribe", "2026-09-20T12:00:00.000Z"],
      ["digest", "2026-09-20T12:00:00.000Z"],
    ]);
  } finally {
    database.close();
  }
});
