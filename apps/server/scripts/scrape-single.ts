/**
 * Scrape a single URL through the Claude rewrite pipeline.
 * Usage: npx tsx scripts/scrape-single.ts <url> <source>
 */
import { execSync } from "child_process";
import db, { initializeDatabase } from "../src/db";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

initializeDatabase();

const articleUrl = process.argv[2];
const source = process.argv[3] || "TechCrunch";

if (!articleUrl) {
  console.error("Usage: npx tsx scripts/scrape-single.ts <url> [source]");
  process.exit(1);
}

function stripHtml(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/&[#\w]+;/g, " ").replace(/\s+/g, " ").trim();
}

(async () => {
  console.log(`Fetching: ${articleUrl}`);
  const res = await fetch(articleUrl);
  const html = await res.text();

  // Extract og:image for hotlinking
  const ogMatch = html.match(/<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']/i)
    || html.match(/<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']/i);
  const ogImage = ogMatch ? ogMatch[1] : null;

  // Extract paragraphs
  const pRegex = /<p[^>]*>([\s\S]*?)<\/p>/gi;
  const paragraphs: string[] = [];
  let m;
  while ((m = pRegex.exec(html)) !== null) {
    const text = m[1].replace(/<[^>]+>/g, "").replace(/&[#\w]+;/g, " ").trim();
    if (text.length > 60 && !text.toLowerCase().includes("cookie") && !text.includes("newsletter") && !text.includes("PST")) {
      paragraphs.push(text);
    }
  }
  const originalContent = paragraphs.slice(0, 15).join("\n\n");
  console.log(`Extracted ${paragraphs.length} paragraphs`);

  // Get original title from og:title
  const ogTitle = html.match(/<meta[^>]*property=["']og:title["'][^>]*content=["'](.*?)["']/)?.[1]
    || html.match(/<title>(.*?)<\/title>/)?.[1]
    || "Untitled";
  const cleanedTitle = ogTitle.replace(/&[#\w]+;/g, " ").trim();

  // Claude rewrite
  const prompt = `You are a tech news editor for TechNews, a modern tech publication. Rewrite this article in your own words with a fresh editorial voice. Keep all facts accurate but make it original content — not a paraphrase.

Original source: ${source}
Original title: ${cleanedTitle}

Original content:
${originalContent.slice(0, 4000)}

Respond in EXACTLY this JSON format, nothing else:
{
  "title": "Your rewritten headline (catchy, concise)",
  "excerpt": "1-2 sentence summary for the article card (max 200 chars)",
  "content": "Full rewritten article in HTML using <p>, <h2>, <h3> tags. 3-6 paragraphs. Original reporting voice."
}`;

  const tmpFile = path.join(os.tmpdir(), `technews-prompt-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, prompt);

  console.log("Rewriting with Claude...");
  const result = execSync(
    `cat "${tmpFile}" | claude -p - --output-format json 2>/dev/null`,
    { timeout: 60000, maxBuffer: 1024 * 1024, encoding: "utf-8" }
  );
  try { fs.unlinkSync(tmpFile); } catch {}

  let text = result.trim();
  try { const o = JSON.parse(text); if (o.result) text = o.result; } catch {}

  const jsonMatch = text.match(/\{[\s\S]*"title"[\s\S]*"content"[\s\S]*\}/);
  if (!jsonMatch) { console.error("Claude JSON parse failed"); process.exit(1); }
  const parsed = JSON.parse(jsonMatch[0]);
  console.log(`Rewritten: "${parsed.title}"`);

  const slug = parsed.title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 120);

  // Categorize
  const titleLower = parsed.title.toLowerCase();
  let category = "tech";
  const catMap: Record<string, string[]> = {
    ai: ["ai", "artificial intelligence", "llm", "openai", "anthropic", "gemini"],
    science: ["science", "space", "nasa", "physics", "climate", "quantum"],
    entertainment: ["game", "gaming", "movie", "netflix", "playstation", "xbox", "nintendo"],
  };
  for (const [cat, kws] of Object.entries(catMap)) {
    if (kws.some(kw => titleLower.includes(kw))) { category = cat; break; }
  }

  const cat = db.prepare("SELECT id FROM categories WHERE slug = ?").get(category) as any;
  const author = db.prepare("SELECT id FROM authors ORDER BY RANDOM() LIMIT 1").get() as any;

  // Check duplicate
  const exists = db.prepare("SELECT 1 FROM articles WHERE slug = ?").get(slug);
  if (exists) { console.log("Article already exists with this slug!"); process.exit(0); }

  db.prepare(`INSERT INTO articles (title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at, view_count, created_at, updated_at, source)
    VALUES (?, ?, ?, ?, ?, ?, ?, 'published', datetime('now'), 0, datetime('now'), datetime('now'), ?)`)
    .run(parsed.title, slug, parsed.excerpt, parsed.content, ogImage, cat.id, author.id, source);

  console.log(`✓ Inserted: "${parsed.title}" → ${category} (slug: ${slug})`);
})();
