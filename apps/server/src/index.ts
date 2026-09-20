import fs from "node:fs";
import path from "node:path";
import { createApp } from "./app";
import { createAuth } from "./auth";
import { initializeDatabase, openDatabase } from "./db";
import { submitArticleSlugsToIndexNow } from "./indexnow";
import { NewsletterService } from "./newsletter/service";
import { createUpload } from "./upload";

const port = process.env.PORT || 4001;
const databasePath = path.join(__dirname, "..", "data", "technews.db");
const uploadRoot = path.join(__dirname, "..", "uploads");

fs.mkdirSync(path.dirname(databasePath), { recursive: true });
fs.mkdirSync(uploadRoot, { recursive: true });

const db = openDatabase(databasePath);
initializeDatabase(db, { seedDefaults: true });

const app = createApp({
  db,
  newsletter: new NewsletterService(db),
  auth: createAuth(process.env.JWT_SECRET || "technews-dev-secret-change-in-production"),
  upload: createUpload(uploadRoot),
  uploadRoot,
  fileOperations: { existsSync: fs.existsSync, unlinkSync: fs.unlinkSync },
  newsletterCronSecret: process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || "",
  notifyIndexNow: submitArticleSlugsToIndexNow,
  indexNowLogger: {
    accepted(result) {
      console.log(`IndexNow accepted ${result.submitted} article URL(s) with status ${result.status}.`);
    },
    failed(error) {
      console.error("IndexNow notification failed:", error instanceof Error ? error.message : error);
    },
  },
});

app.listen(port, () => {
  console.log(`Server running on http://localhost:${port}`);
});

export default app;
