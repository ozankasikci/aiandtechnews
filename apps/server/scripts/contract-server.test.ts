import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { request } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";
import bcrypt from "bcryptjs";
import {
  createContractComposition,
  installContractFetchGuard,
  startContractServer,
  validateContractPaths,
} from "./contract-server";
import { CONTRACT_TEST_PASSWORD, CONTRACT_TEST_PASSWORD_HASH } from "./contracts/synthetic-seed";

const roots: string[] = [];
const serverRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));

function tempRoot(): string {
  const root = mkdtempSync(path.join(tmpdir(), "technews-contract-"));
  roots.push(root);
  return root;
}

function environment(root: string): NodeJS.ProcessEnv {
  return {
    CONTRACT_TEMP_ROOT: root,
    DATABASE_PATH: path.join(root, "db", "contract.db"),
    UPLOAD_DIR: path.join(root, "uploads"),
  };
}

test.afterEach(() => {
  while (roots.length) rmSync(roots.pop()!, { recursive: true, force: true });
});

test("documented synthetic password matches its fixed bcrypt hash", () => {
  assert.equal(bcrypt.compareSync(CONTRACT_TEST_PASSWORD, CONTRACT_TEST_PASSWORD_HASH), true);
});

test("contract fetch guard denies unexpected fetch and restores the parent process", () => {
  const originalFetch = globalThis.fetch;
  const restore = installContractFetchGuard();
  try {
    assert.notEqual(globalThis.fetch, originalFetch);
    assert.throws(() => globalThis.fetch("https://network.example.invalid"), /fetch denied/i);
  } finally {
    restore();
  }
  assert.equal(globalThis.fetch, originalFetch);
});

function httpJson(origin: string, pathname: string, options: {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
} = {}): Promise<{ status: number; body: unknown }> {
  const url = new URL(pathname, origin);
  return new Promise((resolve, reject) => {
    const body = options.body === undefined ? undefined : JSON.stringify(options.body);
    const req = request(url, {
      method: options.method ?? "GET",
      headers: {
        ...options.headers,
        ...(body ? { "content-type": "application/json", "content-length": String(Buffer.byteLength(body)) } : {}),
      },
    }, (response) => {
      const chunks: Buffer[] = [];
      response.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
      response.on("end", () => {
        try {
          resolve({ status: response.statusCode!, body: JSON.parse(Buffer.concat(chunks).toString("utf8")) });
        } catch (error) {
          reject(error);
        }
      });
    });
    req.on("error", reject);
    if (body) req.end(body); else req.end();
  });
}

test("path validation accepts only fresh contained artifacts", () => {
  const root = tempRoot();
  const validated = validateContractPaths(environment(root));
  assert.equal(validated.root, realpathSync(root));
  assert.equal(validated.databasePath, path.join(validated.root, "db", "contract.db"));
  assert.equal(validated.uploadDir, path.join(validated.root, "uploads"));
  assert.equal(existsSync(validated.databasePath), false);
  assert.equal(existsSync(validated.uploadDir), false);
});

test("path validation fails closed before creating a database or listener", async (t) => {
  const root = tempRoot();
  const outside = tempRoot();
  const cases: Array<[string, NodeJS.ProcessEnv]> = [
    ["missing root", { DATABASE_PATH: path.join(root, "db.sqlite"), UPLOAD_DIR: path.join(root, "uploads") }],
    ["relative root", { CONTRACT_TEMP_ROOT: "relative", DATABASE_PATH: path.join(root, "db.sqlite"), UPLOAD_DIR: path.join(root, "uploads") }],
    ["relative database", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: "db.sqlite", UPLOAD_DIR: path.join(root, "uploads") }],
    ["relative uploads", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: path.join(root, "db.sqlite"), UPLOAD_DIR: "uploads" }],
    ["dot-dot segment", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: `${root}/nested/../db.sqlite`, UPLOAD_DIR: path.join(root, "uploads") }],
    ["database outside", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: path.join(outside, "db.sqlite"), UPLOAD_DIR: path.join(root, "uploads") }],
    ["uploads outside", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: path.join(root, "db.sqlite"), UPLOAD_DIR: path.join(outside, "uploads") }],
    ["production database alias", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: "/Users/ozan/Projects/technews/apps/server/data/technews.db", UPLOAD_DIR: path.join(root, "uploads") }],
    ["production upload alias", { CONTRACT_TEMP_ROOT: root, DATABASE_PATH: path.join(root, "db.sqlite"), UPLOAD_DIR: "/Users/ozan/Projects/technews/apps/server/uploads" }],
  ];

  for (const [name, env] of cases) {
    await t.test(name, async () => {
      const candidate = env.DATABASE_PATH!;
      assert.throws(() => validateContractPaths(env));
      await assert.rejects(startContractServer(env));
      if (candidate.startsWith(root)) assert.equal(existsSync(path.resolve(candidate)), false);
    });
  }
});

test("path validation rejects missing/non-directory roots, symlink escapes, existing DBs, and nonempty uploads", () => {
  const parent = tempRoot();
  const missingRoot = path.join(parent, "missing");
  assert.throws(() => validateContractPaths(environment(missingRoot)), /root/i);

  const fileRoot = path.join(parent, "file-root");
  writeFileSync(fileRoot, "not a directory");
  assert.throws(() => validateContractPaths(environment(fileRoot)), /directory/i);

  const root = tempRoot();
  const outside = tempRoot();
  symlinkSync(outside, path.join(root, "escape"));
  assert.throws(() => validateContractPaths({ ...environment(root), DATABASE_PATH: path.join(root, "escape", "contract.db") }), /beneath|escape/i);
  assert.throws(() => validateContractPaths({ ...environment(root), UPLOAD_DIR: path.join(root, "escape", "uploads") }), /beneath|escape/i);

  const existingDb = path.join(root, "existing.db");
  writeFileSync(existingDb, "occupied");
  assert.throws(() => validateContractPaths({ ...environment(root), DATABASE_PATH: existingDb }), /exist/i);

  const uploadDir = path.join(root, "nonempty-uploads");
  mkdirSync(uploadDir);
  writeFileSync(path.join(uploadDir, "occupied"), "x");
  assert.throws(() => validateContractPaths({ ...environment(root), UPLOAD_DIR: uploadDir }), /empty/i);
});

test("synthetic composition seeds the contract breadth and denies network adapters", async () => {
  const root = tempRoot();
  const paths = validateContractPaths(environment(root));
  const composition = createContractComposition(paths);
  const originalFetch = globalThis.fetch;
  let outboundCalls = 0;
  globalThis.fetch = (async () => {
    outboundCalls += 1;
    throw new Error("outbound network denied in contract test");
  }) as typeof fetch;
  const listener = composition.app.listen(0, "127.0.0.1");
  await new Promise<void>((resolve, reject) => {
    listener.once("listening", resolve);
    listener.once("error", reject);
  });
  const port = (listener.address() as AddressInfo).port;
  try {
    assert.deepEqual(await httpJson(`http://127.0.0.1:${port}`, "/api/health"), { status: 200, body: { status: "ok" } });
    const articles = await httpJson(`http://127.0.0.1:${port}`, "/api/articles");
    assert.equal(articles.status, 200);
    assert.deepEqual((articles.body as { articles: Array<{ slug: string }> }).articles.map(({ slug }) => slug), [
      "synthetic-published-newer",
      "synthetic-published-null-options",
    ]);
    assert.equal((await httpJson(`http://127.0.0.1:${port}`, "/api/newsletter/editions/2026-09-19")).status, 200);
    assert.equal(outboundCalls, 0);
    assert.equal(composition.effects.email.length, 0);
    assert.equal(composition.effects.indexNow.length, 0);

    const counts = Object.fromEntries(["categories", "authors", "articles", "media", "settings", "subscribers", "newsletter_editions"].map((table) => [
      table,
      (composition.db.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get() as { count: number }).count,
    ]));
    assert.deepEqual(counts, { categories: 4, authors: 2, articles: 4, media: 1, settings: 8, subscribers: 3, newsletter_editions: 2 });
    assert.equal(existsSync(path.join(paths.uploadDir, "synthetic-contract-image.png")), true);

    assert.equal((await httpJson(`http://127.0.0.1:${port}`, "/api/subscribe", {
      method: "POST",
      body: { email: "fixed-clock@example.invalid", placement: "contract-clock" },
    })).status, 200);
    const clockSubscriber = composition.db.prepare(
      "SELECT confirmed_at, created_at, updated_at FROM subscribers WHERE email = ?",
    ).get("fixed-clock@example.invalid") as { confirmed_at: string; created_at: string; updated_at: string };
    assert.deepEqual(clockSubscriber, {
      confirmed_at: "2026-09-20T12:00:00.000Z",
      created_at: "2026-09-20T12:00:00.000Z",
      updated_at: "2026-09-20T12:00:00.000Z",
    });
    const digest = await httpJson(`http://127.0.0.1:${port}`, "/api/newsletter/digest", {
      headers: { authorization: "Bearer synthetic-contract-cron-secret-never-production" },
    });
    assert.equal(digest.status, 200);
    assert.equal((digest.body as { edition: string }).edition, "2026-09-20");
    assert.equal(composition.effects.email.length, 2);

    const login = await httpJson(`http://127.0.0.1:${port}`, "/api/auth/login", {
      method: "POST",
      body: { email: "editorial@example.invalid", password: CONTRACT_TEST_PASSWORD },
    });
    const token = (login.body as { token: string }).token;
    assert.equal(login.status, 200);
    assert.deepEqual(await httpJson(`http://127.0.0.1:${port}`, "/api/dashboard/articles/301", {
      method: "DELETE",
      headers: { authorization: `Bearer ${token}` },
    }), { status: 200, body: { success: true } });
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.deepEqual(composition.effects.indexNow, [["synthetic-published-newer"]]);
    assert.deepEqual(composition.effects.indexNowNotifications, [{ status: 202, submitted: 1 }]);
  } finally {
    globalThis.fetch = originalFetch;
    await new Promise<void>((resolve, reject) => listener.close((error) => error ? reject(error) : resolve()));
    composition.db.close();
  }
});

test("executable harness announces one safe ready line, serves representative reads, and shuts down cleanly", { timeout: 15_000 }, async (t) => {
  const root = tempRoot();
  const env = environment(root);
  const child = spawn(process.execPath, ["--import", "tsx", path.join(serverRoot, "scripts", "contract-server.ts")], {
    cwd: serverRoot,
    env: { PATH: process.env.PATH, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  t.after(() => {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
  });
  let stdout = "";
  let stderr = "";
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => { stdout += chunk; });
  child.stderr.on("data", (chunk) => { stderr += chunk; });

  const ready = await new Promise<{ event: string; host: string; port: number; database: string; uploads: string }>((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`ready timeout; stdout=${stdout}; stderr=${stderr}`)), 8_000);
    child.once("exit", (code, signal) => {
      clearTimeout(timeout);
      reject(new Error(`harness exited before ready: code=${code} signal=${signal}; stderr=${stderr}`));
    });
    child.stdout.on("data", () => {
      const line = stdout.split("\n").find((candidate) => candidate.trim().startsWith("{"));
      if (!line) return;
      clearTimeout(timeout);
      resolve(JSON.parse(line));
    });
  });

  assert.deepEqual(Object.keys(ready).sort(), ["database", "event", "host", "port", "uploads"]);
  assert.equal(ready.event, "contract-server-ready");
  assert.equal(ready.host, "127.0.0.1");
  assert.notEqual(ready.port, 3001);
  assert.notEqual(ready.port, 3002);
  assert.notEqual(ready.port, 4001);
  assert.equal(path.isAbsolute(ready.database), false);
  assert.equal(path.isAbsolute(ready.uploads), false);
  assert.equal(JSON.stringify(ready).includes(root), false);

  const origin = `http://${ready.host}:${ready.port}`;
  assert.deepEqual(await httpJson(origin, "/api/health"), { status: 200, body: { status: "ok" } });
  assert.equal((await httpJson(origin, "/api/categories")).status, 200);
  assert.equal((await httpJson(origin, "/api/articles/synthetic-published-newer")).status, 200);
  assert.deepEqual(await httpJson(origin, "/api/auth/login", {
    method: "POST",
    body: { email: "editor@example.invalid", password: "wrong" },
  }), { status: 401, body: { error: "Invalid email or password" } });
  assert.deepEqual(await httpJson(origin, "/api/dashboard/articles"), {
    status: 401,
    body: { error: "Authentication required" },
  });

  const login = await httpJson(origin, "/api/auth/login", {
    method: "POST",
    body: { email: "editorial@example.invalid", password: CONTRACT_TEST_PASSWORD },
  });
  assert.equal(login.status, 200);
  const token = (login.body as { token: string }).token;
  assert.equal(typeof token, "string");
  assert.deepEqual(await httpJson(origin, "/api/dashboard/articles/301", {
    method: "DELETE",
    headers: { authorization: `Bearer ${token}` },
  }), { status: 200, body: { success: true } });
  assert.equal((await httpJson(origin, "/api/articles/synthetic-published-newer")).status, 404);

  const exitPromise = new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve) => {
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
  child.kill("SIGTERM");
  const exit = await exitPromise;
  assert.deepEqual(exit, { code: 0, signal: null }, stderr);
  assert.equal(stdout.trim().split("\n").length, 1, "only one machine-readable stdout line is allowed");

  function filesBelow(directory: string, prefix = ""): string[] {
    return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
      const relative = path.join(prefix, entry.name);
      return entry.isDirectory() ? filesBelow(path.join(directory, entry.name), relative) : [relative];
    });
  }
  assert.deepEqual(filesBelow(root).sort(), [
    path.join("db", "contract.db"),
    path.join("uploads", "synthetic-contract-image.png"),
  ]);
  assert.equal(readFileSync(path.join(root, "uploads", "synthetic-contract-image.png"), "utf8"), "synthetic contract media\n");
});
