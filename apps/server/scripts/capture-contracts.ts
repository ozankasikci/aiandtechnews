import { spawn, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
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
const MULTIPART_BOUNDARY = "technews-contract-boundary";
const REQUEST_TIMEOUT_MS = 5_000;
const SHUTDOWN_GRACE_MS = 2_000;
const SHUTDOWN_KILL_MS = 2_000;
const TOKEN_EXPIRY = new Date("2026-09-21T12:00:00.000Z");
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
interface Dependency { operationId: string; responsePointer: string; requestTarget: string }
export interface SendOptions {
  actualPath?: string;
  pathParameters?: Record<string, string | number | boolean>;
  dependencies?: Dependency[];
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

export function sendContractRequest(
  origin: string,
  method: Method,
  target: string,
  options: SendOptions = {},
  timeoutMs = REQUEST_TIMEOUT_MS,
): Promise<RawResponse> {
  const url = new URL(target, origin);
  let payload: Buffer | undefined;
  const headers: Record<string, string> = { origin: CLIENT_ORIGIN };
  if (options.runtimeAuthorization) headers.authorization = options.runtimeAuthorization;
  if (options.multipart) {
    const boundary = MULTIPART_BOUNDARY;
    payload = Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${options.multipart.filename}"\r\nContent-Type: ${options.multipart.mimeType}\r\n\r\n${options.multipart.content}\r\n--${boundary}--\r\n`);
    headers["content-type"] = `multipart/form-data; boundary=${boundary}`;
  } else if (options.body !== undefined) {
    payload = Buffer.from(JSON.stringify(options.body));
    headers["content-type"] = "application/json";
  }
  if (payload) headers["content-length"] = String(payload.length);
  return new Promise((resolve, reject) => {
    let settled = false;
    const settle = (action: () => void) => {
      if (settled) return;
      settled = true;
      action();
    };
    const req = httpRequest(url, { method, headers }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
      res.on("error", (error) => settle(() => reject(error)));
      res.on("aborted", () => settle(() => reject(new Error(`HTTP response aborted for ${method} ${target}`))));
      res.on("end", () => {
        try {
          const text = Buffer.concat(chunks).toString("utf8");
          let body: unknown = text;
          if ((res.headers["content-type"] ?? "").includes("application/json")) body = JSON.parse(text);
          settle(() => resolve({ status: res.statusCode!, headers: selectedResponseHeaders(res.headers), body }));
        } catch (error) {
          settle(() => reject(new Error(`invalid JSON response for ${method} ${target}`, { cause: error })));
        }
      });
    });
    req.setTimeout(timeoutMs, () => req.destroy(new Error(`HTTP request timeout after ${timeoutMs}ms for ${method} ${target}`)));
    req.on("error", (error) => settle(() => reject(error)));
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

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function decodeJwtVector(token: string) {
  const [encodedHeader, encodedClaims] = token.split(".");
  const header = JSON.parse(Buffer.from(encodedHeader, "base64url").toString("utf8")) as { alg: string };
  const decoded = JSON.parse(Buffer.from(encodedClaims, "base64url").toString("utf8")) as Record<string, unknown>;
  const { id, email, role, iat, exp } = decoded;
  return { alg: header.alg, claims: { id, email, role, iat, exp }, sha256: sha256(token) };
}

function newsletterVector(token: string, subscriberId: number, purpose: "confirm" | "unsubscribe") {
  const [encodedPayload] = token.split(".");
  const payload = JSON.parse(Buffer.from(encodedPayload, "base64url").toString("utf8")) as Record<string, unknown>;
  return {
    payload,
    sha256: sha256(token),
  };
}

interface ReadyMetadata { event: "contract-server-ready"; host: "127.0.0.1"; port: number }
interface ChildExit { code: number | null; signal: NodeJS.Signals | null }

export function watchReadyLine(
  child: ChildProcess,
  stderr: () => string,
  timeoutMs = 8_000,
): { ready: Promise<ReadyMetadata>; validateComplete(): void } {
  let stdout = "";
  let parsedLine: string | undefined;
  let lifecycleError: Error | undefined;
  const ready = new Promise<ReadyMetadata>((resolve, reject) => {
    let settled = false;
    const finish = (action: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      action();
    };
    const timeout = setTimeout(() => finish(() => reject(new Error(`contract server ready timeout: ${stderr()}`))), timeoutMs);
    child.once("error", (error) => {
      lifecycleError = error;
      finish(() => reject(error));
    });
    child.once("exit", (code, signal) => {
      if (!parsedLine) finish(() => reject(new Error(`contract server exited before ready (code=${code}, signal=${signal}): ${stderr()}`)));
    });
    if (!child.stdout) {
      finish(() => reject(new Error("contract server stdout is not piped")));
      return;
    }
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
      if (parsedLine !== undefined) return;
      const newline = stdout.indexOf("\n");
      if (newline < 0) return;
      parsedLine = stdout.slice(0, newline);
      try {
        const value = JSON.parse(parsedLine) as Partial<ReadyMetadata>;
        if (
          value.event !== "contract-server-ready"
          || value.host !== "127.0.0.1"
          || !Number.isInteger(value.port)
          || value.port! < 1
          || value.port! > 65_535
        ) throw new Error("unsafe ready metadata");
        finish(() => resolve({ event: "contract-server-ready", host: "127.0.0.1", port: value.port! }));
      } catch (error) {
        finish(() => reject(error));
      }
    });
  });
  return {
    ready,
    validateComplete(): void {
      if (lifecycleError) throw lifecycleError;
      if (parsedLine === undefined || stdout !== `${parsedLine}\n`) {
        throw new Error("contract server stdout must contain exactly one complete ready line and no extra bytes");
      }
    },
  };
}

function childExit(child: ChildProcess): Promise<ChildExit> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return Promise.resolve({ code: child.exitCode, signal: child.signalCode });
  }
  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
}

async function boundedExit(exit: Promise<ChildExit>, timeoutMs: number): Promise<ChildExit | undefined> {
  return Promise.race([
    exit,
    new Promise<undefined>((resolve) => setTimeout(() => resolve(undefined), timeoutMs)),
  ]);
}

export async function shutdownChild(
  child: ChildProcess,
  gracefulMs = SHUTDOWN_GRACE_MS,
  finalMs = SHUTDOWN_KILL_MS,
): Promise<ChildExit> {
  if (child.exitCode !== null || child.signalCode !== null) return { code: child.exitCode, signal: child.signalCode };
  const exit = childExit(child);
  child.kill("SIGTERM");
  const graceful = await boundedExit(exit, gracefulMs);
  if (graceful) return graceful;
  child.kill("SIGKILL");
  const killed = await boundedExit(exit, finalMs);
  if (killed) return killed;
  throw new Error(`contract server did not exit within ${gracefulMs + finalMs}ms after SIGTERM and SIGKILL`);
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
  const readyOutput = watchReadyLine(child, () => stderr);
  const operations: any[] = [];
  let result: any;
  let captureError: unknown;
  try {
    const ready = await readyOutput.ready;
    const origin = `http://${ready.host}:${ready.port}`;
    const record = async (identity: OperationIdentity, options: SendOptions = {}) => {
      const response = await sendContractRequest(origin, identity.method, options.actualPath ?? identity.path, options);
      const requestHeaders: Record<string, string> = { origin: CLIENT_ORIGIN };
      if (options.body !== undefined) requestHeaders["content-type"] = "application/json";
      if (options.multipart) requestHeaders["content-type"] = "multipart/form-data; boundary=$MULTIPART_BOUNDARY";
      if (options.fixtureAuthorization) requestHeaders.authorization = options.fixtureAuthorization;
      operations.push({
        operationId: identity.operationId,
        request: {
          method: identity.method,
          path: identity.path,
          ...(identity.path.includes(":") ? {
            actualPath: (options.actualPath ?? identity.path).split("?", 1)[0],
            pathParameters: options.pathParameters,
          } : {}),
          ...(options.query ? { query: options.query } : {}),
          headers: requestHeaders,
          ...(options.multipart ? { multipart: {
            field: "file",
            filename: options.multipart.filename,
            mimeType: options.multipart.mimeType,
            size: Buffer.byteLength(options.multipart.content),
            contentBase64: Buffer.from(options.multipart.content).toString("base64"),
          } } : {}),
          ...(options.body !== undefined ? { body: options.fixtureBody ?? options.body } : {}),
        },
        ...(options.dependencies ? { dependencies: options.dependencies } : {}),
        response: { status: response.status, headers: response.headers, body: normalizeUploads(response.body) },
      });
      return response;
    };
    const op = Object.fromEntries(CONTRACT_OPERATION_IDENTITIES.map((item) => [item.operationId, item]));
    await record(op["health.get"]);
    await record(op["articles.list"], { actualPath: "/api/articles?page=1&limit=2", query: { page: "1", limit: "2" } });
    await record(op["articles.trending"], { actualPath: "/api/articles/trending?limit=2", query: { limit: "2" } });
    await record(op["articles.getBySlug"], { actualPath: "/api/articles/synthetic-published-newer", pathParameters: { slug: "synthetic-published-newer" } });
    await record(op["articles.getById"], { actualPath: "/api/articles/id/301", pathParameters: { id: 301 } });
    await record(op["categories.list"]);
    await record(op["authors.list"]);
    await record(op["newsletter.subscribe"], { body: { email: "capture@example.invalid", placement: "contract-capture" } });
    const confirmToken = createNewsletterToken(501, "confirm", NEWSLETTER_SECRET, TOKEN_EXPIRY);
    const unsubscribeActive = createNewsletterToken(502, "unsubscribe", NEWSLETTER_SECRET, TOKEN_EXPIRY);
    const unsubscribeOld = createNewsletterToken(503, "unsubscribe", NEWSLETTER_SECRET, TOKEN_EXPIRY);
    await record(op["newsletter.confirm"], { actualPath: `/api/newsletter/confirm?token=${encodeURIComponent(confirmToken)}`, query: { token: "$NEWSLETTER_CONFIRM_TOKEN" } });
    await record(op["newsletter.unsubscribeGet"], { actualPath: `/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeActive)}`, query: { token: "$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN" } });
    await record(op["newsletter.unsubscribePost"], { body: { token: unsubscribeOld }, fixtureBody: { token: "$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN" } });
    await record(op["newsletter.editions"], { actualPath: "/api/newsletter/editions?limit=2", query: { limit: "2" } });
    await record(op["newsletter.edition"], { actualPath: "/api/newsletter/editions/2026-09-19", pathParameters: { edition: "2026-09-19" } });
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
    const postLogoutMe = await sendContractRequest(origin, "GET", "/api/auth/me", auth);
    await record(op["dashboard.articles.list"], { ...auth, actualPath: "/api/dashboard/articles?page=1&limit=5", query: { page: "1", limit: "5" } });
    await record(op["dashboard.articles.get"], { ...auth, actualPath: "/api/dashboard/articles/303", pathParameters: { id: 303 } });
    const articleBody = { title: "Captured Draft", slug: "captured-draft", excerpt: "Synthetic captured excerpt", content: "Synthetic captured body.", category_id: 101, status: "draft" };
    const createdArticle = await record(op["dashboard.articles.create"], { ...auth, body: articleBody });
    const articleId = (createdArticle.body as any).article.id;
    const articleDependency = [{ operationId: "dashboard.articles.create", responsePointer: "/article/id", requestTarget: "/pathParameters/id" }];
    await record(op["dashboard.articles.update"], { ...auth, actualPath: `/api/dashboard/articles/${articleId}`, pathParameters: { id: articleId }, dependencies: articleDependency, body: { title: "Captured Draft Updated", meta_description: null } });
    await record(op["dashboard.articles.delete"], { ...auth, actualPath: `/api/dashboard/articles/${articleId}`, pathParameters: { id: articleId }, dependencies: articleDependency });
    await record(op["dashboard.categories.list"], auth);
    const createdCategory = await record(op["dashboard.categories.create"], { ...auth, body: { name: "Captured Category", slug: "captured-category", description: "Synthetic captured category", color: "#abcdef" } });
    const categoryId = (createdCategory.body as any).category.id;
    const categoryDependency = [{ operationId: "dashboard.categories.create", responsePointer: "/category/id", requestTarget: "/pathParameters/id" }];
    await record(op["dashboard.categories.update"], { ...auth, actualPath: `/api/dashboard/categories/${categoryId}`, pathParameters: { id: categoryId }, dependencies: categoryDependency, body: { description: "Updated synthetic category", color: "#fedcba" } });
    await record(op["dashboard.categories.delete"], { ...auth, actualPath: `/api/dashboard/categories/${categoryId}`, pathParameters: { id: categoryId }, dependencies: categoryDependency });
    await record(op["dashboard.media.list"], auth);
    const uploaded = await record(op["dashboard.media.upload"], { ...auth, multipart: { filename: "capture.png", mimeType: "image/png", content: "synthetic image bytes" } });
    const mediaId = (uploaded.body as any).media.id;
    await record(op["dashboard.media.delete"], { ...auth, actualPath: `/api/dashboard/media/${mediaId}`, pathParameters: { id: mediaId }, dependencies: [{ operationId: "dashboard.media.upload", responsePointer: "/media/id", requestTarget: "/pathParameters/id" }] });
    await record(op["dashboard.settings.get"], auth);
    await record(op["dashboard.settings.update"], { ...auth, body: { site_name: "Captured Synthetic TechNews", newsletter_enabled: false, ignored_key: "preserved-in-request-only" } });
    const unknown = await sendContractRequest(origin, "GET", "/api/dashboard/not-a-route");
    if (operations.length !== 32) throw new Error(`captured ${operations.length} operations, expected 32`);
    result = {
      schemaVersion: 2,
      source: "node-contract-server",
      fixedClock: FIXED_CLOCK,
      replay: {
        bindings: [
          {
            placeholder: "$PASSWORD",
            resolver: { type: "secretRef", name: "CONTRACT_TEST_PASSWORD" },
            sensitive: true,
          },
          {
            placeholder: "$JWT",
            resolver: { type: "responseJsonPointer", operationId: "auth.login", pointer: "/token" },
            sensitive: true,
            vector: decodeJwtVector(jwt),
          },
          {
            placeholder: "$AUTHORIZATION",
            resolver: { type: "template", value: "Bearer ${$JWT}" },
            dependsOn: ["$JWT"],
            sensitive: true,
          },
          {
            placeholder: "$NEWSLETTER_CONFIRM_TOKEN",
            resolver: { type: "newsletterToken", secretRef: "NEWSLETTER_TOKEN_SECRET", subscriberId: 501, purpose: "confirm", expiresAt: TOKEN_EXPIRY.toISOString() },
            sensitive: true,
            vector: newsletterVector(confirmToken, 501, "confirm"),
          },
          {
            placeholder: "$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN",
            resolver: { type: "newsletterToken", secretRef: "NEWSLETTER_TOKEN_SECRET", subscriberId: 502, purpose: "unsubscribe", expiresAt: TOKEN_EXPIRY.toISOString() },
            sensitive: true,
            vector: newsletterVector(unsubscribeActive, 502, "unsubscribe"),
          },
          {
            placeholder: "$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN",
            resolver: { type: "newsletterToken", secretRef: "NEWSLETTER_TOKEN_SECRET", subscriberId: 503, purpose: "unsubscribe", expiresAt: TOKEN_EXPIRY.toISOString() },
            sensitive: true,
            vector: newsletterVector(unsubscribeOld, 503, "unsubscribe"),
          },
          {
            placeholder: "$CRON_AUTHORIZATION",
            resolver: { type: "secretRefTemplate", secretRef: "CRON_SECRET", value: "Bearer ${secret}" },
            sensitive: true,
          },
          {
            placeholder: "$UPLOAD_FILENAME",
            resolver: { type: "responseJsonPointer", operationId: "dashboard.media.upload", pointer: "/media/filename" },
          },
          {
            placeholder: "$MULTIPART_BOUNDARY",
            resolver: { type: "literal", value: MULTIPART_BOUNDARY },
          },
        ],
      },
      normalization: {
        strategy: "Only nondeterministic or sensitive values are replaced; hashes and decoded metadata provide safe compatibility vectors.",
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
  } catch (error) {
    captureError = error;
  }

  const cleanupErrors: unknown[] = [];
  try {
    const exit = await shutdownChild(child);
    if (exit.code !== 0) cleanupErrors.push(new Error(`contract server shutdown failed (code=${exit.code}, signal=${exit.signal}): ${stderr}`));
    readyOutput.validateComplete();
  } catch (error) {
    cleanupErrors.push(error);
  }
  try {
    rmSync(root, { recursive: true, force: true });
  } catch (error) {
    cleanupErrors.push(error);
  }
  const errors = [...(captureError === undefined ? [] : [captureError]), ...cleanupErrors];
  if (errors.length === 1) throw errors[0];
  if (errors.length > 1) throw new AggregateError(errors, "contract capture and/or cleanup failed");
  return result;
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
