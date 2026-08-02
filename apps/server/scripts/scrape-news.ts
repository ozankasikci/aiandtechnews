/**
 * Strict daily news importer.
 *
 * Usage:
 *   IMPORT_ONLY=1 npx tsx scripts/scrape-news.ts
 */
import { runDailyImporter } from "./news-importer";

runDailyImporter()
  .then((published) => {
    console.log(`Daily import complete. Published ${published} article${published === 1 ? "" : "s"}.`);
  })
  .catch((error) => {
    console.error("Daily import failed:", error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
