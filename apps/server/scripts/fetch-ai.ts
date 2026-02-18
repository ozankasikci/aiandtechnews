import { execSync } from "child_process";
import db, { initializeDatabase } from "../src/db";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

initializeDatabase();

const AI_URLS = [
  "https://techcrunch.com/2026/02/17/anthropic-releases-sonnet-4-6/",
  "https://techcrunch.com/2026/02/17/metas-new-deal-with-nvidia-buys-up-millions-of-ai-chips/",
  "https://arstechnica.com/ai/2026/02/googles-ai-search-results-will-make-links-more-obvious/",
  "https://techcrunch.com/2026/02/17/mistral-ai-buys-koyeb-in-first-acquisition-to-back-its-cloud-platform/",
  "https://arstechnica.com/ai/2026/02/password-managers-promise-that-they-cant-see-your-vaults-is-often-misleading/",
];

function stripHtml(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/&[#\w]+;/g, " ").replace(/\s+/g, " ").trim();
}

const urlExists = db.prepare("SELECT 1 FROM articles WHERE source_url = ?");
const aiCatId = (db.prepare("SELECT id FROM categories WHERE slug = 'ai'").get() as any).id;
const getAuthor = db.prepare("SELECT id FROM authors ORDER BY RANDOM() LIMIT 1");

(async () => {
  let imported = 0;
  for (const url of AI_URLS) {
    if (urlExists.get(url)) { console.log(`Skip (exists): ${url}`); continue; }

    console.log(`Fetching: ${url}`);
    const res = await fetch(url);
    const html = await res.text();

    const ogMatch = html.match(/<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']/i)
      || html.match(/<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']/i);
    const ogImage = ogMatch ? ogMatch[1] : null;

    const titleMatch = html.match(/<title[^>]*>([^<]+)<\/title>/i);
    const origTitle = titleMatch ? titleMatch[1].replace(/ \| .*$/, "").replace(/ - .*$/, "").trim() : "AI Article";

    // Extract content
    const paragraphs = html.match(/<p[^>]*>[\s\S]*?<\/p>/gi) || [];
    const content = paragraphs
      .map(p => p.replace(/<(?!\/?(p|b|i|strong|em|a|br)\b)[^>]+>/gi, ""))
      .filter(p => stripHtml(p).length > 40)
      .slice(0, 15)
      .join("\n");

    if (!content || content.length < 100) { console.log(`Skip (no content): ${url}`); continue; }

    // Determine source
    const source = url.includes("techcrunch") ? "TechCrunch" : url.includes("theverge") ? "The Verge" : "Ars Technica";

    // Rewrite with Claude
    const prompt = `Rewrite this tech news article in your own words. Return JSON only:
{"title": "catchy headline", "excerpt": "1-2 sentence summary (max 200 chars)", "content": "Full rewritten article in HTML using <p>, <h2> tags. 3-6 paragraphs."}

Original title: ${origTitle}
Source: ${source}
Content: ${stripHtml(content).slice(0, 3000)}`;

    const tmpFile = path.join(os.tmpdir(), `technews-ai-${Date.now()}.txt`);
    fs.writeFileSync(tmpFile, prompt);
    console.log(`  Rewriting: ${origTitle.slice(0, 60)}...`);

    let result: string;
    try {
      result = execSync(`cat "${tmpFile}" | claude -p - --output-format json 2>/dev/null`, {
        timeout: 60000, maxBuffer: 1024 * 1024, encoding: "utf-8"
      });
    } catch { console.log("  Claude failed, skipping"); continue; }
    finally { try { fs.unlinkSync(tmpFile); } catch {} }

    let text = result.trim();
    try { const o = JSON.parse(text); if (o.result) text = o.result; } catch {}
    const jsonMatch = text.match(/\{[\s\S]*"title"[\s\S]*"content"[\s\S]*\}/);
    if (!jsonMatch) { console.log("  Parse failed"); continue; }

    const parsed = JSON.parse(jsonMatch[0]);
    const slug = parsed.title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 120);
    const authorId = (getAuthor.get() as any).id;

    db.prepare(`INSERT INTO articles (title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at, view_count, created_at, updated_at, source, source_url)
      VALUES (?, ?, ?, ?, ?, ?, ?, 'published', datetime('now'), 0, datetime('now'), datetime('now'), ?, ?)`)
      .run(parsed.title, slug, parsed.excerpt, parsed.content, ogImage, aiCatId, authorId, source, url);

    console.log(`  ✓ Imported: ${parsed.title}`);
    imported++;
    if (imported >= 5) break;
  }
  console.log(`\nDone! Imported ${imported} AI articles.`);
})();
