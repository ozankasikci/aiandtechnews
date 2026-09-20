import fs from "node:fs";
import path from "node:path";
import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";
import { createApp } from "../src/app";
import { createAuth } from "../src/auth";
import { initializeDatabase, openDatabase, type DatabaseConnection } from "../src/db";
import type { NewsletterEmail } from "../src/newsletter/email";
import { NewsletterService } from "../src/newsletter/service";
import { createUpload } from "../src/upload";
import { seedSyntheticContractData } from "./contracts/synthetic-seed";

const HOST = "127.0.0.1";
const FORBIDDEN_PORTS = new Set([3001, 3002, 4001]);
const FIXED_NOW = Date.parse("2026-09-20T12:00:00.000Z");
const FIXED_AUTH_SECRET = "synthetic-contract-jwt-secret-never-production";
const FIXED_CRON_SECRET = "synthetic-contract-cron-secret-never-production";
const FIXED_NEWSLETTER_SECRET = "synthetic-contract-newsletter-secret-000000000000";
const serverRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));

export interface ContractPaths {
  root: string;
  databasePath: string;
  uploadDir: string;
}

export interface ContractEffects {
  email: Array<{ to: string; subject: string; idempotencyKey: string }>;
  indexNow: string[][];
  indexNowNotifications: Array<{ status: number; submitted: number } | { error: unknown }>;
}

export interface ContractComposition {
  app: ReturnType<typeof createApp>;
  db: DatabaseConnection;
  effects: ContractEffects;
}

export interface RunningContractServer extends ContractComposition {
  listener: Server;
  host: typeof HOST;
  port: number;
  close(): Promise<void>;
}

const KNOWN_PRODUCTION_ALIASES = [
  "/Users/ozan/Projects/technews",
  path.join(serverRoot, "data", "technews.db"),
  path.join(serverRoot, "uploads"),
].map((candidate) => path.resolve(candidate));

function requireAbsolute(environment: NodeJS.ProcessEnv, name: "CONTRACT_TEMP_ROOT" | "DATABASE_PATH" | "UPLOAD_DIR"): string {
  const value = environment[name];
  if (!value) throw new Error(`${name} is required`);
  if (!path.isAbsolute(value)) throw new Error(`${name} must be absolute`);
  if (value.split(/[\\/]+/).includes("..")) throw new Error(`${name} must not contain '..' segments`);
  return path.normalize(value);
}

function assertNotKnownProductionAlias(candidate: string, name: string): void {
  for (const forbidden of KNOWN_PRODUCTION_ALIASES) {
    const relative = path.relative(forbidden, candidate);
    if (relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative))) {
      throw new Error(`${name} is a known production alias`);
    }
  }
}

/** Resolve symlinks in the deepest existing ancestor without requiring the leaf to exist. */
function canonicalProspectivePath(candidate: string): string {
  let existing = candidate;
  const missing: string[] = [];
  for (;;) {
    try {
      fs.lstatSync(existing);
      break;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
      const parent = path.dirname(existing);
      if (parent === existing) throw error;
      missing.unshift(path.basename(existing));
      existing = parent;
    }
  }
  return path.join(fs.realpathSync(existing), ...missing);
}

function assertStrictlyBeneath(root: string, candidate: string, name: string): void {
  const relative = path.relative(root, candidate);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
    throw new Error(`${name} must resolve strictly beneath CONTRACT_TEMP_ROOT (symlink escape denied)`);
  }
}

/** Purely validates caller-owned locations. It never creates the root or artifacts. */
export function validateContractPaths(environment: NodeJS.ProcessEnv): ContractPaths {
  const rootInput = requireAbsolute(environment, "CONTRACT_TEMP_ROOT");
  const databaseInput = requireAbsolute(environment, "DATABASE_PATH");
  const uploadInput = requireAbsolute(environment, "UPLOAD_DIR");

  // Reject dangerous names before any filesystem lookup of those candidates.
  assertNotKnownProductionAlias(rootInput, "CONTRACT_TEMP_ROOT");
  assertNotKnownProductionAlias(databaseInput, "DATABASE_PATH");
  assertNotKnownProductionAlias(uploadInput, "UPLOAD_DIR");

  let rootStats: fs.Stats;
  try {
    rootStats = fs.lstatSync(rootInput);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") throw new Error("CONTRACT_TEMP_ROOT must already exist");
    throw error;
  }
  if (!rootStats.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error("CONTRACT_TEMP_ROOT must be a real directory, not a file or symlink");
  }
  const root = fs.realpathSync(rootInput);
  const databasePath = canonicalProspectivePath(databaseInput);
  const uploadDir = canonicalProspectivePath(uploadInput);
  assertStrictlyBeneath(root, databasePath, "DATABASE_PATH");
  assertStrictlyBeneath(root, uploadDir, "UPLOAD_DIR");

  if (fs.existsSync(databaseInput)) throw new Error("DATABASE_PATH must not exist at startup");
  if (fs.existsSync(uploadInput)) {
    const stats = fs.lstatSync(uploadInput);
    if (!stats.isDirectory() || stats.isSymbolicLink()) throw new Error("UPLOAD_DIR must be an empty real directory or absent");
    if (fs.readdirSync(uploadInput).length !== 0) throw new Error("UPLOAD_DIR must be empty at startup");
  }

  return { root, databasePath, uploadDir };
}

export function createContractComposition(paths: ContractPaths): ContractComposition {
  fs.mkdirSync(path.dirname(paths.databasePath), { recursive: true });
  fs.mkdirSync(paths.uploadDir, { recursive: true });
  const db = openDatabase(paths.databasePath);
  const effects: ContractEffects = { email: [], indexNow: [], indexNowNotifications: [] };
  try {
    initializeDatabase(db, { seedDefaults: false });
    seedSyntheticContractData(db, paths.uploadDir);
    const emailRecorder = async (email: NewsletterEmail, idempotencyKey: string) => {
      effects.email.push({ to: email.to, subject: email.subject, idempotencyKey });
      return { id: `synthetic-email-${effects.email.length}` };
    };
    const newsletter = new NewsletterService(db, {
      NEWSLETTER_SITE_URL: "https://site.example.invalid",
      NEWSLETTER_TOKEN_SECRET: FIXED_NEWSLETTER_SECRET,
      NEWSLETTER_FROM: "Synthetic Contract <newsletter@example.invalid>",
      NEWSLETTER_REPLY_TO: "reply@example.invalid",
    }, emailRecorder);
    const app = createApp({
      db,
      newsletter,
      auth: createAuth(FIXED_AUTH_SECRET),
      upload: createUpload(paths.uploadDir),
      uploadRoot: paths.uploadDir,
      fileOperations: { existsSync: fs.existsSync, unlinkSync: fs.unlinkSync },
      newsletterCronSecret: FIXED_CRON_SECRET,
      now: () => FIXED_NOW,
      signupAttempts: [],
      notifyIndexNow: async (slugs) => {
        effects.indexNow.push([...slugs]);
        return { status: 202, submitted: slugs.length };
      },
      indexNowLogger: {
        accepted: (result) => { effects.indexNowNotifications.push({ ...result }); },
        failed: (error) => { effects.indexNowNotifications.push({ error }); },
      },
    });
    return { app, db, effects };
  } catch (error) {
    db.close();
    throw error;
  }
}

function listen(app: ContractComposition["app"]): Promise<Server> {
  return new Promise((resolve, reject) => {
    const listener = app.listen(0, HOST);
    const onError = (error: Error) => reject(error);
    listener.once("error", onError);
    listener.once("listening", () => {
      listener.off("error", onError);
      resolve(listener);
    });
  });
}

export async function startContractServer(environment: NodeJS.ProcessEnv = process.env): Promise<RunningContractServer> {
  const paths = validateContractPaths(environment);
  const composition = createContractComposition(paths);
  let listener: Server | undefined;
  try {
    listener = await listen(composition.app);
    const address = listener.address();
    if (!address || typeof address === "string") throw new Error("contract listener did not receive an IP address");
    const { address: assignedHost, port } = address as AddressInfo;
    if (assignedHost !== HOST || FORBIDDEN_PORTS.has(port)) {
      throw new Error(`unsafe contract listener assignment: ${assignedHost}:${port}`);
    }
    let closed = false;
    return {
      ...composition,
      listener,
      host: HOST,
      port,
      async close(): Promise<void> {
        if (closed) return;
        closed = true;
        try {
          await new Promise<void>((resolve, reject) => listener!.close((error) => error ? reject(error) : resolve()));
        } finally {
          composition.db.close();
        }
      },
    };
  } catch (error) {
    if (listener?.listening) await new Promise<void>((resolve) => listener!.close(() => resolve()));
    composition.db.close();
    throw error;
  }
}

export function installContractFetchGuard(): () => void {
  const originalFetch = globalThis.fetch;
  const guardedFetch = (() => {
    throw new Error("global fetch denied by isolated contract harness");
  }) as typeof fetch;
  globalThis.fetch = guardedFetch;
  let restored = false;
  return () => {
    if (restored) return;
    restored = true;
    globalThis.fetch = originalFetch;
  };
}

export async function runContractServer(environment: NodeJS.ProcessEnv = process.env): Promise<void> {
  const restoreFetch = installContractFetchGuard();
  let running: RunningContractServer | undefined;
  try {
    running = await startContractServer(environment);
    const paths = validateReadyMetadata(environment, running);
    process.stdout.write(`${JSON.stringify(paths)}\n`);

    await new Promise<void>((resolve) => {
      const shutdown = () => {
        process.off("SIGINT", shutdown);
        process.off("SIGTERM", shutdown);
        resolve();
      };
      process.once("SIGINT", shutdown);
      process.once("SIGTERM", shutdown);
    });
  } finally {
    try {
      await running?.close();
    } finally {
      restoreFetch();
    }
  }
}

function validateReadyMetadata(environment: NodeJS.ProcessEnv, running: RunningContractServer) {
  const root = path.resolve(environment.CONTRACT_TEMP_ROOT!);
  const database = path.relative(root, path.resolve(environment.DATABASE_PATH!));
  const uploads = path.relative(root, path.resolve(environment.UPLOAD_DIR!));
  return { event: "contract-server-ready", host: running.host, port: running.port, database, uploads };
}

const isMain = process.argv[1] !== undefined && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  runContractServer().catch((error) => {
    process.stderr.write(`contract server startup failed: ${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
