import express, { type Express } from "express";
import cors from "cors";
import type { AuthService } from "./auth";
import type { DatabaseConnection } from "./db";
import type { IndexNowResult } from "./indexnow";
import { createDashboardRouter, type DashboardRouterDependencies } from "./routes/dashboard";
import { createPublicRouter, type PublicRouterDependencies } from "./routes/public";

export interface AppDependencies {
  db: DatabaseConnection;
  newsletter: PublicRouterDependencies["newsletter"];
  auth: AuthService;
  upload: DashboardRouterDependencies["upload"];
  uploadRoot: string;
  newsletterCronSecret: string;
  now?: () => number;
  signupAttempts?: number[];
  fileOperations: DashboardRouterDependencies["fileOperations"];
  notifyIndexNow(slugs: string[]): Promise<IndexNowResult>;
  indexNowLogger?: DashboardRouterDependencies["indexNowLogger"];
}

/** Constructs an isolated Express application without opening resources or reading process state. */
export function createApp(dependencies: AppDependencies): Express {
  const app = express();
  const signupAttempts = dependencies.signupAttempts ?? [];

  app.use(cors());
  app.use(express.json());
  app.use("/uploads", express.static(dependencies.uploadRoot));

  // Keep health and public routes ahead of the dashboard router's auth guard.
  app.get("/api/health", (_req, res) => {
    res.json({ status: "ok" });
  });
  app.use("/api", createPublicRouter({
    db: dependencies.db,
    newsletter: dependencies.newsletter,
    newsletterCronSecret: dependencies.newsletterCronSecret,
    now: dependencies.now ?? Date.now,
    signupAttempts,
  }));
  app.use("/api", createDashboardRouter({
    db: dependencies.db,
    auth: dependencies.auth,
    now: dependencies.now ?? Date.now,
    upload: dependencies.upload,
    uploadRoot: dependencies.uploadRoot,
    fileOperations: dependencies.fileOperations,
    notifyIndexNow: dependencies.notifyIndexNow,
    indexNowLogger: dependencies.indexNowLogger ?? {
      accepted(result) {
        console.log(`IndexNow accepted ${result.submitted} article URL(s) with status ${result.status}.`);
      },
      failed(error) {
        console.error("IndexNow notification failed:", error instanceof Error ? error.message : error);
      },
    },
  }));

  return app;
}
