/**
 * Strict manual importer for one item currently present in an approved RSS feed.
 *
 * Usage:
 *   npx tsx scripts/scrape-single.ts <article-url>
 */
import { runManualImporter } from "./news-importer";

const articleUrl = process.argv[2];
if (!articleUrl) {
  console.error("Usage: npx tsx scripts/scrape-single.ts <article-url>");
  process.exitCode = 1;
} else {
  runManualImporter(articleUrl)
    .then((published) => {
      console.log(`Manual import complete. Published ${published} article${published === 1 ? "" : "s"}.`);
    })
    .catch((error) => {
      console.error("Manual import failed:", error instanceof Error ? error.message : error);
      process.exitCode = 1;
    });
}
