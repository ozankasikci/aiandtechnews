/**
 * Strict manual importer for one item currently present in an approved RSS feed,
 * or a still-fresh item that the user explicitly approved before it aged out.
 *
 * Usage:
 *   npx tsx scripts/scrape-single.ts <article-url>
 *   npx tsx scripts/scrape-single.ts <article-url> --approved-aged-out <published-at> "<approved-title>"
 */
import { parseManualImporterArgs, runManualImporter } from "./news-importer";

try {
  const { articleUrl, approval } = parseManualImporterArgs(process.argv.slice(2));
  runManualImporter(articleUrl, approval)
    .then((published) => {
      console.log(`Manual import complete. Published ${published} article${published === 1 ? "" : "s"}.`);
    })
    .catch((error) => {
      console.error("Manual import failed:", error instanceof Error ? error.message : error);
      process.exitCode = 1;
    });
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
