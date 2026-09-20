import { spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createNewsletterToken } from "../src/newsletter/tokens";
import { CONTRACT_TEST_PASSWORD } from "./contracts/synthetic-seed";

const FIXED_CLOCK = "2026-09-20T12:00:00.000Z";
const NEWSLETTER_SECRET = "synthetic-contract-newsletter-secret-000000000000";
const CRON_SECRET = "synthetic-contract-cron-secret-never-production";
const CLIENT_ORIGIN = "https://client.example.invalid";
const serverRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
const fixturePath = path.join(serverRoot, "contracts", "node", "contracts.json");

type Method = "GET" | "POST" | "PUT" | "DELETE";
export interface OperationIdentity { operationId: string; method: Method; path: string }
export const CONTRACT_OPERATION_IDENTITIES: readonly OperationIdentity[] = [
  { operationId: "health.get", method: "GET", path: "/api/health" },
  { operationId: "articles.list", method: "GET", path: "/api/articles" },
  { operationId: "articles.trending", method: "GET", path: "/api/articles/trending" },
  { operationId: "articles.getBySlug", method: "GET", path: "/api/articles/:slug" },
  { operationId: "articles.getById", method: "GET", path: "/api/articles/id/:id" },
  { operationId: "categories.list", method: "GET", path: "/api/categories" },
  { operationId: "authors.list", method: "GET", path: "/api/authors" },
  { operationId: "newsletter.subscribe", method: "POST", path: "/api/subscribe" },
  { operationId: "newsletter.confirm", method: "GET", path: "/api/newsletter/confirm" },
  { operationId: "newsletter.unsubscribeGet", method: "GET", path: "/api/newsletter/unsubscribe" },
  { operationId: "newsletter.unsubscribePost", method: "POST", path: "/api/newsletter/unsubscribe" },
  { operationId: "newsletter.editions", method: "GET", path: "/api/newsletter/editions" },
  { operationId: "newsletter.edition", method: "GET", path: "/api/newsletter/editions/:edition" },
  { operationId: "newsletter.digestGet", method: "GET", path: "/api/newsletter/digest" },
  { operationId: "newsletter.digestPost", method: "POST", path: "/api/newsletter/digest" },
  { operationId: "auth.login", method: "POST", path: "/api/auth/login" },
  { operationId: "auth.me", method: "GET", path: "/api/auth/me" },
  { operationId: "auth.logout", method: "POST", path: "/api/auth/logout" },
  { operationId: "dashboard.articles.list", method: "GET", path: "/api/dashboard/articles" },
  { operationId: "dashboard.articles.get", method: "GET", path: "/api/dashboard/articles/:id" },
  { operationId: "dashboard.articles.create", method: "POST", path: "/api/dashboard/articles" },
  { operationId: "dashboard.articles.update", method: "PUT", path: "/api/dashboard/articles/:id" },
  { operationId: "dashboard.articles.delete", method: "DELETE", path: "/api/dashboard/articles/:id" },
  { operationId: "dashboard.categories.list", method: "GET", path: "/api/dashboard/categories" },
  { operationId: "dashboard.categories.create", method: "POST", path: "/api/dashboard/categories" },
  { operationId: "dashboard.categories.update", method: "PUT", path: "/api/dashboard/categories/:id" },
  { operationId: "dashboard.categories.delete", method: "DELETE", path: "/api/dashboard/categories/:id" },
  { operationId: "dashboard.media.list", method: "GET", path: "/api/dashboard/media" },
  { operationId: "dashboard.media.upload", method: "POST", path: "/api/dashboard/media/upload" },
  { operationId: "dashboard.media.delete", method: "DELETE", path: "/api/dashboard/media/:id" },
  { operationId: "dashboard.settings.get", method: "GET", path: "/api/dashboard/settings" },
  { operationId: "dashboard.settings.update", method: "PUT", path: "/api/dashboard/settings" },
];

interface RawResponse { status: number; headers: Record<string, string>; body: unknown }
interface SendOptions {
  actualPath?: string;
  body?: unknown;
  runtimeAuthorization?: string;
  fixtureAuthorization?: string;
  fixtureBody?: unknown;
  multipart?: { filename: string; mimeType: string; content: string };
  query?: Record<string, string>;
}

function selectedResponseHeaders(headers: import("node:http").IncomingHttpHeaders): Record<string, string> {
  const selected: Record<string, string> = {};
  for (const name of ["access-control-allow-origin", "content-type"]) {
    const value = headers[name];
    if (typeof value === "string") selected[name] = value;
  }
  return selected;
}

function send(origin: string, method: Method, target: string, options: SendOptions = {}): Promise<RawResponse> {
  const url = new URL(target, origin);
  let payload: Buffer | undefined;
  const headers: Record<string, string> = { origin: CLIENT_ORIGIN };
  if (options.runtimeAuthorization) headers.authorization = options.runtimeAuthorization;
  if (options.multipart) {
    const boundary = "technews-contract-boundary";
    payload = Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${options.multipart.filename}"\r\nContent-Type: ${options.multipart.mimeType}\r\n\r\n${options.multipart.content}\r\n--${boundary}--\r\n`);
    headers["content-type"] = `multipart/form-data; boundary=${boundary}`;
  } else if (options.body !== undefined) {
    payload = Buffer.from(JSON.stringify(options.body));
    headers["content-type"] = "application/json";
  }
  if (payload) headers["content-length"] = String(payload.length);
  return new Promise((resolve, reject) => {
    const req = httpRequest(url, { method, headers }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
      res.on("end", () => {
        const text = Buffer.concat(chunks).toString("utf8");
        let body: unknown = text;
        if ((res.headers["content-type"] ?? "").includes("application/json")) body = JSON.parse(text);
        resolve({ status: res.statusCode!, headers: selectedResponseHeaders(res.headers), body });
      });
    });
    req.on("error", reject);
    req.end(payload);
  });
}

function normalizeUploads(value: unknown): unknown {
  if (typeof value === "string") {
    if (/^\/uploads\/[a-f0-9]{32}\.png$/.test(value)) return "/uploads/$UPLOAD_FILENAME";
    if (/^[a-f0-9]{32}\.png$/.test(value)) return "$UPLOAD_FILENAME";
    return value;
  }
  if (Array.isArray(value)) return value.map(normalizeUploads);
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, normalizeUploads(item)]));
  return value;
}

async function readyLine(child: ReturnType<typeof spawn>, stderr: () => string): Promise<{ host: string; port: number }> {
  return new Promise((resolve, reject) => {
    let stdout = "";
    const timeout = setTimeout(() => reject(new Error(`contract server ready timeout: ${stderr()}`)), 8_000);
    const fail = (code: number | null, signal: NodeJS.Signals | null) => {
      clearTimeout(timeout);
      reject(new Error(`contract server exited before ready (code=${code}, signal=${signal}): ${stderr()}`));
    };
    child.once("exit", fail);
    child.stdout!.setEncoding("utf8");
    child.stdout!.on("data", (chunk: string) => {
      stdout += chunk;
      const lines = stdout.split("\n");
      if (lines.length < 2) return;
      clearTimeout(timeout);
      child.off("exit", fail);
      if (lines.slice(1).some((line) => line.trim())) return reject(new Error("contract server emitted more than one ready line"));
      try {
        const ready = JSON.parse(lines[0]);
        if (ready.event !== "contract-server-ready" || ready.host !== "127.0.0.1" || !Number.isInteger(ready.port)) throw new Error("unsafe ready metadata");
        resolve(ready);
      } catch (error) { reject(error); }
    });
  });
}

export async function captureContracts(): Promise<any> {
  const root = mkdtempSync(path.join(tmpdir(), "technews-contract-capture-"));
  let stderr = "";
  const child = spawn(process.execPath, ["--import", "tsx", path.join(serverRoot, "scripts", "contract-server.ts")], {
    cwd: serverRoot,
    env: {
      PATH: process.env.PATH ?? "",
      CONTRACT_TEMP_ROOT: root,
      DATABASE_PATH: path.join(root, "database", "contract.db"),
      UPLOAD_DIR: path.join(root, "uploads"),
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stderr!.setEncoding("utf8");
  child.stderr!.on("data", (chunk: string) => { stderr += chunk; });
  const exited = new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve) => child.once("exit", (code, signal) => resolve({ code, signal })));
  const operations: any[] = [];
  try {
    const ready = await readyLine(child, () => stderr);
    const origin = `http://${ready.host}:${ready.port}`;
    const record = async (identity: OperationIdentity, options: SendOptions = {}) => {
      const response = await send(origin, identity.method, options.actualPath ?? identity.path, options);
      const requestHeaders: Record<string, string> = { origin: CLIENT_ORIGIN };
      if (options.body !== undefined) requestHeaders["content-type"] = "application/json";
      if (options.multipart) requestHeaders["content-type"] = "multipart/form-data; boundary=$MULTIPART_BOUNDARY";
      if (options.fixtureAuthorization) requestHeaders.authorization = options.fixtureAuthorization;
      operations.push({
        operationId: identity.operationId,
        request: {
          method: identity.method,
          path: identity.path,
          ...(options.query ? { query: options.query } : {}),
          headers: requestHeaders,
          ...(options.multipart ? { multipart: { field: "file", filename: options.multipart.filename, mimeType: options.multipart.mimeType, size: Buffer.byteLength(options.multipart.content) } } : {}),
          ...(options.body !== undefined ? { body: options.fixtureBody ?? options.body } : {}),
        },
        response: { status: response.status, headers: response.headers, body: normalizeUploads(response.body) },
      });
      return response;
    };
    const op = Object.fromEntries(CONTRACT_OPERATION_IDENTITIES.map((item) => [item.operationId, item]));
    await record(op["health.get"]);
    await record(op["articles.list"], { actualPath: "/api/articles?page=1&limit=2", query: { page: "1", limit: "2" } });
    await record(op["articles.trending"], { actualPath: "/api/articles/trending?limit=2", query: { limit: "2" } });
    await record(op["articles.getBySlug"], { actualPath: "/api/articles/synthetic-published-newer" });
    await record(op["articles.getById"], { actualPath: "/api/articles/id/301" });
    await record(op["categories.list"]);
    await record(op["authors.list"]);
    await record(op["newsletter.subscribe"], { body: { email: "capture@example.invalid", placement: "contract-capture" } });
    const confirmToken = createNewsletterToken(501, "confirm", NEWSLETTER_SECRET, new Date("2026-09-21T12:00:00.000Z"));
    const unsubscribeActive = createNewsletterToken(502, "unsubscribe", NEWSLETTER_SECRET);
    const unsubscribeOld = createNewsletterToken(503, "unsubscribe", NEWSLETTER_SECRET);
    await record(op["newsletter.confirm"], { actualPath: `/api/newsletter/confirm?token=${encodeURIComponent(confirmToken)}`, query: { token: "$NEWSLETTER_TOKEN" } });
    await record(op["newsletter.unsubscribeGet"], { actualPath: `/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeActive)}`, query: { token: "$NEWSLETTER_TOKEN" } });
    await record(op["newsletter.unsubscribePost"], { body: { token: unsubscribeOld }, fixtureBody: { token: "$NEWSLETTER_TOKEN" } });
    await record(op["newsletter.editions"], { actualPath: "/api/newsletter/editions?limit=2", query: { limit: "2" } });
    await record(op["newsletter.edition"], { actualPath: "/api/newsletter/editions/2026-09-19" });
    await record(op["newsletter.digestGet"], { runtimeAuthorization: `Bearer ${CRON_SECRET}`, fixtureAuthorization: "$CRON_AUTHORIZATION" });
    await record(op["newsletter.digestPost"], { runtimeAuthorization: `Bearer ${CRON_SECRET}`, fixtureAuthorization: "$CRON_AUTHORIZATION" });
    const login = await record(op["auth.login"], {
      body: { email: "editorial@example.invalid", password: CONTRACT_TEST_PASSWORD },
      fixtureBody: { email: "editorial@example.invalid", password: "$PASSWORD" },
    });
    const jwt = (login.body as { token: string }).token;
    operations.at(-1).response.body.token = "$JWT";
    const auth = { runtimeAuthorization: `Bearer ${jwt}`, fixtureAuthorization: "$AUTHORIZATION" };
    await record(op["auth.me"], auth);
    await record(op["auth.logout"], auth);
    const postLogoutMe = await send(origin, "GET", "/api/auth/me", auth);
    await record(op["dashboard.articles.list"], { ...auth, actualPath: "/api/dashboard/articles?page=1&limit=5", query: { page: "1", limit: "5" } });
    await record(op["dashboard.articles.get"], { ...auth, actualPath: "/api/dashboard/articles/303" });
    const articleBody = { title: "Captured Draft", slug: "captured-draft", excerpt: "Synthetic captured excerpt", content: "Synthetic captured body.", category_id: 101, status: "draft" };
    const createdArticle = await record(op["dashboard.articles.create"], { ...auth, body: articleBody });
    const articleId = (createdArticle.body as any).article.id;
    await record(op["dashboard.articles.update"], { ...auth, actualPath: `/api/dashboard/articles/${articleId}`, body: { title: "Captured Draft Updated", meta_description: null } });
    await record(op["dashboard.articles.delete"], { ...auth, actualPath: `/api/dashboard/articles/${articleId}` });
    await record(op["dashboard.categories.list"], auth);
    const createdCategory = await record(op["dashboard.categories.create"], { ...auth, body: { name: "Captured Category", slug: "captured-category", description: "Synthetic captured category", color: "#abcdef" } });
    const categoryId = (createdCategory.body as any).category.id;
    await record(op["dashboard.categories.update"], { ...auth, actualPath: `/api/dashboard/categories/${categoryId}`, body: { description: "Updated synthetic category", color: "#fedcba" } });
    await record(op["dashboard.categories.delete"], { ...auth, actualPath: `/api/dashboard/categories/${categoryId}` });
    await record(op["dashboard.media.list"], auth);
    const uploaded = await record(op["dashboard.media.upload"], { ...auth, multipart: { filename: "capture.png", mimeType: "image/png", content: "synthetic image bytes" } });
    const mediaId = (uploaded.body as any).media.id;
    await record(op["dashboard.media.delete"], { ...auth, actualPath: `/api/dashboard/media/${mediaId}` });
    await record(op["dashboard.settings.get"], auth);
    await record(op["dashboard.settings.update"], { ...auth, body: { site_name: "Captured Synthetic TechNews", newsletter_enabled: false, ignored_key: "preserved-in-request-only" } });
    const unknown = await send(origin, "GET", "/api/dashboard/not-a-route");
    if (operations.length !== 32) throw new Error(`captured ${operations.length} operations, expected 32`);
    return {
      schemaVersion: 1,
      source: "node-contract-server",
      fixedClock: FIXED_CLOCK,
      normalization: {
        rules: [
          { placeholder: "$JWT", appliesTo: "auth.login response token", runtime: "fixed-clock signed JWT" },
          { placeholder: "$AUTHORIZATION", appliesTo: "authenticated request authorization headers", runtime: "Bearer plus raw JWT" },
          { placeholder: "$NEWSLETTER_TOKEN", appliesTo: "newsletter query/body tokens", runtime: "signed with synthetic secret" },
          { placeholder: "$CRON_AUTHORIZATION", appliesTo: "digest authorization headers", runtime: "Bearer plus synthetic cron secret" },
          { placeholder: "$UPLOAD_FILENAME", appliesTo: "Multer-generated response URL/filename", runtime: "cryptographically random basename" },
          { placeholder: "$MULTIPART_BOUNDARY", appliesTo: "multipart content-type boundary", runtime: "fixed harness boundary omitted from fixture" },
          { placeholder: "$PASSWORD", appliesTo: "auth.login request password", runtime: "documented synthetic test password" },
        ],
        fixedValuesRemainLiteral: ["database IDs", "2026-09-20T12:00:00.000Z and derived timestamps", "semantic body ordering/nulls/errors/statuses"],
      },
      operations,
      observations: {
        excludedFromOperationCoverage: true,
        authLogoutJwtStillValid: postLogoutMe.status === 200,
        dashboardUnknownRoute: { request: { method: "GET", path: "/api/dashboard/not-a-route", headers: { origin: CLIENT_ORIGIN } }, status: unknown.status, body: unknown.body },
        deferredErrors: ["Multer invalid type and size-limit scenarios"],
      },
    };
  } finally {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGTERM");
    const exit = await exited;
    rmSync(root, { recursive: true, force: true });
    if (exit.code !== 0) throw new Error(`contract server shutdown failed (code=${exit.code}, signal=${exit.signal}): ${stderr}`);
  }
}

export function serializeContracts(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

async function main(): Promise<void> {
  const accept = process.argv.slice(2).includes("--accept");
  const unknown = process.argv.slice(2).filter((arg) => arg !== "--accept");
  if (unknown.length) throw new Error(`unknown argument(s): ${unknown.join(", ")}`);
  const fresh = serializeContracts(await captureContracts());
  if (accept) {
    mkdirSync(path.dirname(fixturePath), { recursive: true });
    const temporary = `${fixturePath}.tmp-${process.pid}`;
    try {
      writeFileSync(temporary, fresh, { flag: "wx" });
      renameSync(temporary, fixturePath);
    } finally { rmSync(temporary, { force: true }); }
    process.stdout.write(`accepted 32 Node HTTP contracts: ${path.relative(serverRoot, fixturePath)}\n`);
    return;
  }
  let baseline: string;
  try { baseline = readFileSync(fixturePath, "utf8"); }
  catch { throw new Error(`contract baseline missing; review and run contract:capture to create ${path.relative(serverRoot, fixturePath)}`); }
  if (fresh !== baseline) throw new Error(`contract mismatch: ${path.relative(serverRoot, fixturePath)} differs from a fresh capture; inspect the diff, then run contract:capture only to accept reviewed changes`);
  process.stdout.write("32 Node HTTP contracts match reviewed baseline\n");
}

const isMain = process.argv[1] !== undefined && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) main().catch((error) => { process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`); process.exitCode = 1; });
