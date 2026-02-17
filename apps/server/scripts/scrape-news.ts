/**
 * TechNews Scraper — fetches real tech/AI news from RSS feeds (TechCrunch, The Verge, Ars Technica),
 * then inserts them directly into the SQLite database.
 *
 * Usage: cd apps/server && npx tsx scripts/scrape-news.ts
 */

import { execSync } from "child_process";
import db, { initializeDatabase } from "../src/db";

// ── Ensure DB is ready ──────────────────────────────────────────────
initializeDatabase();

// ── Category mapping ────────────────────────────────────────────────
const CATEGORY_KEYWORDS: Record<string, string[]> = {
  ai: ["ai", "artificial intelligence", "machine learning", "llm", "gpt", "openai", "chatgpt", "deep learning", "neural", "transformer", "anthropic", "gemini", "copilot", "diffusion", "generative"],
  science: ["science", "space", "nasa", "physics", "biology", "climate", "quantum", "research", "study", "mars", "cern"],
  reviews: ["review", "hands-on", "benchmark", "vs", "comparison", "tested", "unboxing"],
  entertainment: ["game", "gaming", "movie", "film", "netflix", "streaming", "playstation", "xbox", "nintendo", "spotify", "disney"],
  creators: ["creator", "youtube", "tiktok", "influencer", "podcast", "content creator", "twitch"],
  tech: [], // default fallback
};

function categorize(title: string, content: string): string {
  const titleLower = title.toLowerCase();
  const contentLower = content.toLowerCase().slice(0, 500);
  // Check title first (stronger signal), then content
  for (const [cat, keywords] of Object.entries(CATEGORY_KEYWORDS)) {
    if (cat === "tech") continue;
    if (keywords.some((kw) => titleLower.includes(kw))) return cat;
  }
  for (const [cat, keywords] of Object.entries(CATEGORY_KEYWORDS)) {
    if (cat === "tech") continue;
    // Require at least 2 keyword matches in content for non-title categorization
    const matches = keywords.filter((kw) => contentLower.includes(kw)).length;
    if (matches >= 2) return cat;
  }
  return "tech";
}

function cleanTitle(title: string): string {
  return title
    .replace(/<!\[CDATA\[(.*?)\]\]>/g, "$1")
    .replace(/&#8217;/g, "'")
    .replace(/&#8216;/g, "'")
    .replace(/&#8220;/g, '"')
    .replace(/&#8221;/g, '"')
    .replace(/&#8211;/g, "–")
    .replace(/&#8212;/g, "—")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .trim();
}

function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 120);
}

function stripHtml(html: string): string {
  return html
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/<style[\s\S]*?<\/style>/gi, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, " ")
    .trim();
}

// ── Article content extraction ──────────────────────────────────────
const ALLOWED_TAGS = new Set(["p", "h2", "h3", "blockquote", "ul", "ol", "li"]);
const JUNK_TAGS = /(<nav[\s\S]*?<\/nav>|<header[\s\S]*?<\/header>|<footer[\s\S]*?<\/footer>|<aside[\s\S]*?<\/aside>|<script[\s\S]*?<\/script>|<style[\s\S]*?<\/style>|<noscript[\s\S]*?<\/noscript>|<svg[\s\S]*?<\/svg>|<form[\s\S]*?<\/form>|<iframe[\s\S]*?<\/iframe>|<button[\s\S]*?<\/button>|<figcaption[\s\S]*?<\/figcaption>|<label[\s\S]*?<\/label>|<input[^>]*\/?>)/gi;

function stripJunk(html: string): string {
  let prev = "";
  let result = html;
  // Remove HTML comments
  result = result.replace(/<!--[\s\S]*?-->/g, "");
  // Remove elements with junk classes/ids
  result = result.replace(/<[^>]*(?:class|id)="[^"]*(?:sidebar|social-share|related-posts|comment|newsletter|promo|cookie|consent|modal|popup|banner|menu|toolbar|settings)[^"]*"[^>]*>[\s\S]*?<\/[^>]+>/gi, "");
  while (result !== prev) {
    prev = result;
    result = result.replace(JUNK_TAGS, "");
  }
  return result;
}

function sanitizeHtml(html: string): string {
  // Keep only allowed tags, strip all others but preserve text content
  return html
    .replace(/<\/?([a-z][a-z0-9]*)\b[^>]*\/?>/gi, (match, tag) => {
      const t = tag.toLowerCase();
      if (ALLOWED_TAGS.has(t)) {
        // Return clean open/close tag
        const isClose = match.startsWith("</");
        return isClose ? `</${t}>` : `<${t}>`;
      }
      return " ";
    })
    .replace(/&nbsp;/g, " ")
    .replace(/\s+/g, " ")
    .replace(/\s*(<\/?(?:p|h2|h3|blockquote|ul|ol|li)>)\s*/g, "$1")
    .trim();
}

function extractArticleContent(html: string): string {
  // Step 1: Strip junk elements
  const cleaned = stripJunk(html);

  // Step 2: Try article-specific selectors
  const selectors = [
    /<article[^>]*>([\s\S]*?)<\/article>/i,
    /<div[^>]*class="[^"]*(?:post-content|article-body|article-content|entry-content|story-body|post-body)[^"]*"[^>]*>([\s\S]*?)<\/div>/i,
    /<div[^>]*role="main"[^>]*>([\s\S]*?)<\/div>/i,
    /<main[^>]*>([\s\S]*?)<\/main>/i,
  ];

  let articleHtml = "";
  for (const sel of selectors) {
    const m = cleaned.match(sel);
    if (m?.[1] && stripHtml(m[1]).length > 200) {
      articleHtml = m[1];
      break;
    }
  }

  // Step 3: Fallback — find the largest cluster of <p> tags
  if (!articleHtml) {
    // Collect all <p>...</p> blocks
    const pBlocks = cleaned.match(/<p[^>]*>[\s\S]*?<\/p>/gi) || [];
    if (pBlocks.length > 0) {
      articleHtml = pBlocks.join("\n");
    }
  }

  if (!articleHtml) return "";

  // Step 4: Strip junk again from the extracted section, sanitize
  const sanitized = sanitizeHtml(stripJunk(articleHtml));

  // Step 5: Remove any remaining comments, decode entities, limit length
  const decoded = sanitized
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'");

  // Remove known site-specific junk text
  let final = decoded
    .replace(/^.*?(?:Story text|Text settings|Minimize to nav)\s*/i, "")
    .replace(/<li>\s*<\/li>/g, "")
    .replace(/<ul>\s*<\/ul>/g, "")
    .replace(/<ol>\s*<\/ol>/g, "")
    .replace(/^(\s*<ul>(?:<li>[^<]{0,30}<\/li>)*<\/ul>\s*)+/i, "")
    .trim();
  // Remove any text not wrapped in allowed tags at the start
  final = final.replace(/^[^<]+(?=<p>|<h2>|<h3>|<blockquote>|<ul>|<ol>)/i, "").trim();
  if (!final) final = decoded;

  // Limit to ~3000 chars
  if (final.length <= 3000) return final;
  // Cut at a closing tag boundary
  const cut = final.slice(0, 3000);
  const lastClose = cut.lastIndexOf("</p>");
  return lastClose > 0 ? cut.slice(0, lastClose + 4) : cut;
}

function extractExcerpt(rssDescription: string, articleContent: string): string {
  // Prefer RSS description if it's clean (not too short, not navigation junk)
  const cleanDesc = stripHtml(rssDescription || "");
  const junkPatterns = /^(skip to|menu|navigation|search|sign in|log in|subscribe)/i;
  if (cleanDesc.length > 50 && cleanDesc.length < 500 && !junkPatterns.test(cleanDesc)) {
    return cleanDesc.slice(0, 300);
  }
  // Fall back to first 200 chars of article text
  const plainContent = stripHtml(articleContent);
  return plainContent.slice(0, 200);
}

// ── Newsworthiness filter ───────────────────────────────────────────
function isNewsworthy(title: string, _source: string): boolean {
  const t = title.trim();
  if (/^Show HN:/i.test(t)) return false;
  if (/\[pdf\]/i.test(t)) return false;
  if (/\[video\]/i.test(t)) return false;
  if (/Abstract:|Study:|arXiv/i.test(t)) return false;
  if (/^State of Show HN/i.test(t)) return false;
  if (/\(\d{4}\)$/.test(t)) return false; // old dated posts like "Suicide Linux (2009)"
  return true;
}

// ── Smart excerpt generation ────────────────────────────────────────
function decodeHtmlEntities(text: string): string {
  return text
    .replace(/&#8211;/g, "–").replace(/&#8212;/g, "—")
    .replace(/&#8217;/g, "'").replace(/&#8216;/g, "'")
    .replace(/&#8220;/g, '"').replace(/&#8221;/g, '"')
    .replace(/&#39;/g, "'").replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<").replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"').replace(/&nbsp;/g, " ")
    .replace(/&#\d+;/g, "");
}

function generateExcerpt(title: string, content: string, rssExcerpt: string): string {
  const cleanRss = decodeHtmlEntities(stripHtml(rssExcerpt || "")).trim();
  const junkPatterns = /^(skip to|menu|navigation|search|sign in|log in|subscribe|advertisement)/i;
  const titleNorm = title.toLowerCase().trim();

  // Check if RSS excerpt is usable
  if (
    cleanRss.length > 50 &&
    cleanRss.toLowerCase().trim() !== titleNorm &&
    !junkPatterns.test(cleanRss) &&
    !cleanRss.startsWith("&#")
  ) {
    return truncateAtSentence(cleanRss, 200);
  }

  // Extract from content
  const plainContent = decodeHtmlEntities(stripHtml(content || "")).trim();
  if (plainContent.length > 50 && plainContent.toLowerCase() !== titleNorm) {
    // Get first 1-2 sentences
    const sentences = plainContent.match(/[^.!?]+[.!?]+/g);
    if (sentences && sentences.length > 0) {
      let excerpt = sentences[0].trim();
      if (sentences.length > 1 && excerpt.length < 100) {
        excerpt += " " + sentences[1].trim();
      }
      // Don't return if it's just the title
      if (excerpt.toLowerCase().trim() !== titleNorm) {
        return truncateAtSentence(excerpt, 200);
      }
    }
    // Fallback: first 200 chars
    if (plainContent.toLowerCase().slice(0, title.length) !== titleNorm) {
      return truncateAtSentence(plainContent, 200);
    }
  }

  return "";
}

function truncateAtSentence(text: string, max: number): string {
  if (text.length <= max) return text;
  const cut = text.slice(0, max);
  // Try to end at sentence boundary
  const lastPeriod = Math.max(cut.lastIndexOf(". "), cut.lastIndexOf("! "), cut.lastIndexOf("? "));
  if (lastPeriod > max * 0.5) return cut.slice(0, lastPeriod + 1);
  // End at word boundary
  const lastSpace = cut.lastIndexOf(" ");
  return lastSpace > max * 0.5 ? cut.slice(0, lastSpace) + "…" : cut + "…";
}

function extractOgImage(html: string): string | null {
  const match = html.match(/<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']/i)
    || html.match(/<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']/i);
  return match?.[1] || null;
}

function formatContent(rawText: string): string {
  const text = rawText.slice(0, 2000).trim();
  // Split into paragraphs roughly every 2-3 sentences
  const sentences = text.split(/(?<=[.!?])\s+/);
  const paragraphs: string[] = [];
  let buf: string[] = [];
  for (const s of sentences) {
    buf.push(s);
    if (buf.length >= 3) {
      paragraphs.push(buf.join(" "));
      buf = [];
    }
  }
  if (buf.length) paragraphs.push(buf.join(" "));
  return paragraphs.join("\n\n");
}

async function safeFetch(url: string, timeoutMs = 10000): Promise<string | null> {
  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    const res = await fetch(url, {
      signal: controller.signal,
      headers: { "User-Agent": "TechNews-Scraper/1.0" },
    });
    clearTimeout(timer);
    if (!res.ok) return null;
    return await res.text();
  } catch {
    return null;
  }
}

// ── Article type ────────────────────────────────────────────────────
interface RawArticle {
  title: string;
  url: string;
  excerpt: string;
  source: string;
}

// ── RSS parsing ─────────────────────────────────────────────────────
function parseRssItems(xml: string, source: string, max = 10): RawArticle[] {
  const items: RawArticle[] = [];
  // Support both <item> (RSS) and <entry> (Atom) formats
  const itemBlocks = xml.split(/<(?:item|entry)[\s>]/i).slice(1);
  for (const block of itemBlocks.slice(0, max)) {
    const title = block.match(/<title><!\[CDATA\[(.*?)\]\]>|<title[^>]*>(.*?)<\/title>/s);
    const link = block.match(/<link><!\[CDATA\[(.*?)\]\]>|<link[^>]*href="([^"]+)"|<link>(.*?)<\/link>/s);
    const desc = block.match(/<(?:description|summary|content)><!\[CDATA\[(.*?)\]\]>|<(?:description|summary|content)[^>]*>(.*?)<\/(?:description|summary|content)>/s);
    const t = (title?.[1] || title?.[2] || "").trim();
    const l = (link?.[1] || link?.[2] || link?.[3] || "").trim();
    const d = stripHtml(desc?.[1] || desc?.[2] || "").slice(0, 300);
    if (t && l) items.push({ title: cleanTitle(t), url: l, excerpt: cleanTitle(d), source });
  }
  return items;
}

// ── Feed fetchers ───────────────────────────────────────────────────
async function fetchRssFeed(url: string, source: string): Promise<RawArticle[]> {
  console.log(`Fetching ${source}...`);
  const xml = await safeFetch(url);
  if (!xml) { console.error(`  ✗ Failed to fetch ${source}`); return []; }
  const items = parseRssItems(xml, source);
  console.log(`  ✓ ${items.length} items from ${source}`);
  return items;
}

// ── Ensure categories exist ─────────────────────────────────────────
function ensureCategories() {
  const insert = db.prepare(
    "INSERT OR IGNORE INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)"
  );
  const colors: Record<string, string> = {
    tech: "#3b82f6", reviews: "#f59e0b", science: "#10b981",
    entertainment: "#ec4899", ai: "#8b5cf6", creators: "#f97316",
  };
  const needed = ["tech", "reviews", "science", "entertainment", "ai", "creators"];
  for (const slug of needed) {
    insert.run(slug.charAt(0).toUpperCase() + slug.slice(1), slug, `${slug} news`, colors[slug] || "#6366f1");
  }
}

// ── AI Image Generation ─────────────────────────────────────────────
const IMAGE_SCRIPT = "/opt/homebrew/lib/node_modules/openclaw/skills/nano-banana-pro/scripts/generate_image.py";
const IMAGE_OUTPUT_DIR = `${process.env.HOME}/Projects/technews/apps/web/public/images/generated`;

const CATEGORY_STYLE: Record<string, string> = {
  ai: "with subtle AI or digital elements in the scene, tech company or data center atmosphere",
  tech: "featuring relevant technology or devices in a modern setting",
  science: "in a scientific or research environment, lab or space setting",
  entertainment: "in an entertainment or media setting, screens or stage atmosphere",
  reviews: "featuring the product or device in clean product photography style",
  creators: "in a creative studio or content production environment",
};

function buildImagePrompt(title: string, category: string): string {
  const stopWords = new Set(["the", "a", "an", "is", "are", "was", "were", "in", "on", "at", "to", "for", "of", "with", "by", "from", "its", "it", "and", "or", "but", "not", "no", "has", "have", "had", "will", "can", "could", "would", "should", "may", "might", "new", "how", "why", "what", "when", "where", "who", "that", "this", "over", "after", "before", "about", "into", "up", "out", "just", "now", "more", "most", "says", "said", "report", "reports", "according"]);
  const words = title.toLowerCase().replace(/[^a-z0-9\s]/g, "").split(/\s+/).filter(w => w.length > 2 && !stopWords.has(w));
  const topicWords = words.slice(0, 3).join(", ");

  const style = CATEGORY_STYLE[category] || CATEGORY_STYLE.tech;
  return `Photorealistic editorial magazine photo about ${topicWords}, ${style}, dramatic lighting, high quality, 16:9 composition, no text no words no logos no watermarks`.slice(0, 200);
}

async function buildSmartImagePrompt(title: string, excerpt: string): Promise<string> {
  const apiKey = getGeminiApiKey();
  if (!apiKey) return "";
  try {
    const prompt = `You are an art director for a tech news magazine. Given this article headline and excerpt, write a single image generation prompt (max 180 chars) for a featured photo/illustration. Match the tone: business stories get professional/corporate imagery, space stories get cosmic visuals, legal stories get courthouse/gavel imagery, consumer tech gets product shots, etc. Be specific and visual. NO text, NO logos, NO watermarks. Just the prompt, nothing else.

Title: ${title}
Excerpt: ${excerpt}`;

    const resp = await fetch(
      `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          contents: [{ parts: [{ text: prompt }] }],
          generationConfig: { temperature: 0.8, maxOutputTokens: 256 }
        })
      }
    );
    if (!resp.ok) return "";
    const data = await resp.json() as any;
    let text = (data?.candidates?.[0]?.content?.parts?.[0]?.text || "").trim();
    text = text.replace(/^["'`]+|["'`]+$/g, "").trim();
    if (text.length > 20 && text.length < 250) {
      console.log(`  🎯 Smart prompt: ${text.slice(0, 100)}...`);
      return text + ", no text no words no logos no watermarks";
    }
  } catch (err) {
    console.error("  ✗ Smart prompt failed, using fallback");
  }
  return "";
}

async function generateArticleImage(title: string, category: string, slug: string, excerpt?: string): Promise<string | null> {
  if (process.env.SKIP_IMAGES) { console.log(`  ⏭️  Skipping image gen (SKIP_IMAGES): ${title.slice(0, 50)}`); return null; }
  const outputPath = `${IMAGE_OUTPUT_DIR}/${slug}.png`;
  const smartPrompt = excerpt ? await buildSmartImagePrompt(title, excerpt) : "";
  const prompt = smartPrompt || buildImagePrompt(title, category);

  console.log(`  🎨 Generating image for: ${title}`);
  console.log(`     Prompt: ${prompt}`);

  try {
    // Ensure GEMINI_API_KEY is available
    getGeminiApiKey();
    execSync(
      `uv run "${IMAGE_SCRIPT}" --prompt "${prompt.replace(/"/g, '\\"')}" --filename "${outputPath}" --resolution 2K`,
      { timeout: 120000, stdio: "pipe", env: { ...process.env } }
    );
    return `/images/generated/${slug}.png`;
  } catch (err) {
    console.error(`  ✗ Image generation failed for "${title}":`, (err as Error).message?.slice(0, 200));
    return null;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// ── Gemini API Key (module-level) ───────────────────────────────────
function getGeminiApiKey(): string | null {
  if (process.env.GEMINI_API_KEY) return process.env.GEMINI_API_KEY;
  try {
    const fs = require("fs");
    const cfg = JSON.parse(fs.readFileSync(`${process.env.HOME}/.openclaw/openclaw.json`, "utf8"));
    const key = cfg.skills?.entries?.["nano-banana-pro"]?.apiKey;
    if (key) { process.env.GEMINI_API_KEY = key; return key; }
  } catch {}
  return null;
}

// ── AI Article Content Generation ───────────────────────────────────
async function generateArticleContent(title: string, excerpt: string, category: string): Promise<string> {
  const apiKey = getGeminiApiKey();
  if (!apiKey) return `<p>${excerpt}</p>`;

  const prompt = `Write a professional tech news article about: ${title}

Context: ${excerpt}
Category: ${category}

Guidelines:
- Write 4-5 paragraphs in a professional editorial tone like The Verge or TechCrunch
- Start with a strong lede paragraph summarizing the key news
- Include relevant context and background in paragraph 2-3
- Add analysis or industry implications in paragraph 4
- End with what to watch for / what comes next
- Be factual and balanced, don't make up quotes or specific numbers not in the context
- Don't use clickbait language
- Output clean HTML with <p> tags for paragraphs, <h2> for section headers if needed
- Do NOT include the title in the output
- Keep it under 500 words`;

  console.log(`  📝 Enhancing content for: ${title}...`);

  try {
    const response = await fetch(
      `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          contents: [{ parts: [{ text: prompt }] }],
          generationConfig: { temperature: 0.7, maxOutputTokens: 1024 }
        })
      }
    );
    const data = await response.json();
    const text = data.candidates?.[0]?.content?.parts?.[0]?.text;
    if (text && text.length > 100) {
      // Clean up: remove markdown code fences if present
      const cleaned = text.replace(/^```html?\n?/i, '').replace(/\n?```$/i, '').trim();
      return cleaned;
    }
  } catch (err) {
    console.error(`  ✗ AI content generation failed:`, (err as Error).message?.slice(0, 200));
  }
  return `<p>${excerpt}</p>`;
}

// ── Enhance existing short articles ─────────────────────────────────
async function enhanceShortArticles() {
  console.log("\n── Enhancing short articles with AI ──");
  const articles = db.prepare(`
    SELECT a.id, a.title, a.excerpt, a.content, c.slug as category
    FROM articles a JOIN categories c ON a.category_id = c.id
    WHERE a.status = 'published'
    ORDER BY a.created_at DESC LIMIT 100
  `).all() as { id: number; title: string; excerpt: string; content: string; category: string }[];

  const shortArticles = articles.filter(a => stripHtml(a.content || '').length < 200).slice(0, 10);

  if (shortArticles.length === 0) {
    console.log("No short articles to enhance.");
    return;
  }

  const update = db.prepare("UPDATE articles SET content = ?, updated_at = datetime('now') WHERE id = ?");
  let enhanced = 0;

  for (const article of shortArticles) {
    const newContent = await generateArticleContent(article.title, article.excerpt, article.category);
    if (stripHtml(newContent).length > stripHtml(article.content || '').length) {
      update.run(newContent, article.id);
      enhanced++;
      console.log(`  ✓ Enhanced: ${article.title}`);
    }
    await sleep(1000);
  }
  console.log(`Enhanced ${enhanced}/${shortArticles.length} short articles.`);
}

// ── Author personas ─────────────────────────────────────────────────
const AUTHOR_PERSONAS = [
  { name: "Sarah Chen", email: "sarah.chen@technews.dev", bio: "Tech reporter covering startups and emerging technology", avatar: "https://images.unsplash.com/photo-1438761681033-6461ffad8d80?w=200&h=200&fit=crop" },
  { name: "Marcus Webb", email: "marcus.webb@technews.dev", bio: "AI correspondent exploring the frontiers of artificial intelligence", avatar: "https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?w=200&h=200&fit=crop" },
  { name: "Elena Rodriguez", email: "elena.rodriguez@technews.dev", bio: "Science editor with a focus on space, climate, and quantum research", avatar: "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=200&h=200&fit=crop" },
  { name: "James Park", email: "james.park@technews.dev", bio: "Deals & reviews editor helping you find the best tech at the best prices", avatar: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=200&h=200&fit=crop" },
  { name: "Alex Thompson", email: "alex.thompson@technews.dev", bio: "Senior editor covering the tech industry at large", avatar: "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=200&h=200&fit=crop" },
];

const CATEGORY_AUTHOR_MAP: Record<string, string> = {
  ai: "Marcus Webb",
  science: "Elena Rodriguez",
  reviews: "James Park",
  tech: "Alex Thompson",
  entertainment: "Sarah Chen",
  creators: "Sarah Chen",
};

function ensureAuthors() {
  const insert = db.prepare(
    "INSERT OR IGNORE INTO authors (name, email, password_hash, avatar, bio, role) VALUES (?, ?, 'not-a-login', ?, ?, 'editor')"
  );
  for (const a of AUTHOR_PERSONAS) {
    insert.run(a.name, a.email, a.avatar, a.bio);
  }
}

let authorIdCache: Record<string, number> = {};
function getAuthorId(category: string): number {
  const name = CATEGORY_AUTHOR_MAP[category] || AUTHOR_PERSONAS[Math.floor(Math.random() * AUTHOR_PERSONAS.length)].name;
  if (authorIdCache[name]) return authorIdCache[name];
  const row = db.prepare("SELECT id FROM authors WHERE name = ?").get(name) as { id: number } | undefined;
  if (row) { authorIdCache[name] = row.id; return row.id; }
  return 1; // fallback to Admin
}

// ── AI Rewrite (Gemini) ─────────────────────────────────────────────
async function rewriteWithClaude(title: string, originalContent: string, source: string): Promise<{ title: string; content: string; excerpt: string } | null> {
  const plainContent = stripHtml(originalContent).slice(0, 4000);
  if (plainContent.length < 100) return null;

  const apiKey = getGeminiApiKey();
  if (!apiKey) { console.error("  ✗ No Gemini API key for rewrite"); return null; }

  const prompt = `You are a tech news editor for TechNews, a modern tech publication. Rewrite this article in your own words with a fresh editorial voice. Keep all facts accurate but make it original content — not a paraphrase.

Original source: ${source}
Original title: ${title}

Original content:
${plainContent}

Respond in EXACTLY this JSON format, nothing else:
{
  "title": "Your rewritten headline (catchy, concise)",
  "excerpt": "1-2 sentence summary for the article card (max 200 chars)",
  "content": "Full rewritten article in HTML using <p>, <h2>, <h3> tags. 3-6 paragraphs. Original reporting voice."
}`;

  try {
    console.log(`  ✍️  Rewriting: ${title.slice(0, 60)}...`);
    
    const resp = await fetch(
      `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          contents: [{ parts: [{ text: prompt }] }],
          generationConfig: { temperature: 0.7, maxOutputTokens: 2048, responseMimeType: "application/json" }
        })
      }
    );

    if (!resp.ok) { console.error(`  ✗ Gemini rewrite API error: ${resp.status}`); return null; }
    const data = await resp.json() as any;
    const text = data?.candidates?.[0]?.content?.parts?.[0]?.text?.trim();
    if (!text) { console.error("  ✗ Empty Gemini rewrite response"); return null; }

    const jsonMatch = text.match(/\{[\s\S]*"title"[\s\S]*"content"[\s\S]*\}/);
    if (!jsonMatch) { console.error("  ✗ Rewrite didn't return valid JSON"); return null; }
    const parsed = JSON.parse(jsonMatch[0]);
    if (parsed.title && parsed.content && parsed.excerpt) {
      console.log(`  ✓ Rewritten: "${parsed.title.slice(0, 60)}"`);
      return parsed;
    }
    return null;
  } catch (err) {
    console.error(`  ✗ Rewrite failed:`, (err as Error).message?.slice(0, 200));
    return null;
  }
}

// ── Main ────────────────────────────────────────────────────────────
async function main() {
  ensureCategories();
  ensureAuthors();

  // Gather articles from all sources
  const allArticles: RawArticle[] = [];
  const feeds = await Promise.all([
    fetchRssFeed("https://techcrunch.com/feed/", "TechCrunch"),
    fetchRssFeed("https://www.theverge.com/rss/index.xml", "The Verge"),
    fetchRssFeed("https://feeds.arstechnica.com/arstechnica/index", "Ars Technica"),
  ]);
  for (const f of feeds) allArticles.push(...f);

  if (allArticles.length === 0) {
    console.log("No articles fetched from any source. Exiting.");
    return;
  }

  // Shuffle and pick up to 5
  const shuffled = allArticles.sort(() => Math.random() - 0.5);
  const slugExists = db.prepare("SELECT 1 FROM articles WHERE slug = ?");
  const getCategoryId = db.prepare("SELECT id FROM categories WHERE slug = ?");
  const insertArticle = db.prepare(`
    INSERT INTO articles (title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at, view_count, created_at, updated_at, source, source_url)
    VALUES (?, ?, ?, ?, ?, ?, ?, 'published', datetime('now'), 0, datetime('now'), datetime('now'), ?, ?)
  `);

  let imported = 0;
  for (const article of shuffled) {
    if (imported >= 10) break;
    if (!isNewsworthy(article.title, article.source)) continue;
    const slug = slugify(article.title);
    if (slugExists.get(slug)) continue;

    // Fetch full article content + og:image
    let content = "";
    let featuredImage: string | null = null;
    const html = await safeFetch(article.url);
    if (html) {
      featuredImage = extractOgImage(html);
      content = extractArticleContent(html);
    }
    if (!content || content.length < 100) {
      // Fallback: wrap excerpt in <p>
      content = `<p>${article.excerpt}</p>`;
    }

    let plainContent = stripHtml(content);
    const category = categorize(article.title, plainContent);
    const catRow = getCategoryId.get(category) as { id: number } | undefined;
    if (!catRow) continue;

    // Rewrite article with Claude for original content
    let finalTitle = article.title;
    let excerpt = "";
    const rewritten = await rewriteWithClaude(article.title, content, article.source);
    if (rewritten) {
      finalTitle = rewritten.title;
      content = rewritten.content;
      excerpt = rewritten.excerpt;
      plainContent = stripHtml(content);
    } else {
      // Fallback: use original with excerpt generation
      excerpt = generateExcerpt(article.title, content, article.excerpt) || extractExcerpt(article.excerpt, content);
    }

    // If content is too short even after rewrite, enhance with AI
    if (plainContent.length < 300) {
      const enhanced = await generateArticleContent(finalTitle, excerpt || article.excerpt, category);
      if (enhanced && stripHtml(enhanced).length > plainContent.length) {
        content = enhanced;
        plainContent = stripHtml(content);
      }
    }

    // Use og:image from source (hotlink, don't download) — AI fallback only
    const finalSlug = slugify(finalTitle);
    if (!featuredImage) {
      featuredImage = await generateArticleImage(finalTitle, category, finalSlug, excerpt);
      if (featuredImage) await sleep(2000);
    }

    if (!excerpt) {
      excerpt = generateExcerpt(finalTitle, content, article.excerpt) || extractExcerpt(article.excerpt, content);
    }
    const authorId = getAuthorId(category);

    try {
      insertArticle.run(finalTitle, finalSlug, excerpt, content, featuredImage, catRow.id, authorId, article.source, article.url);
    } catch (err) {
      console.error(`  ✗ Failed to insert "${finalTitle}":`, err);
      continue;
    }

    console.log(`Imported: ${finalTitle} → ${category}`);
    imported++;
  }

  if (imported === 0) {
    console.log("No new articles to import (all duplicates or errors).");
  } else {
    console.log(`\nDone! Imported ${imported} articles.`);
  }
}

// ── Cleanup bad existing data ───────────────────────────────────────
function cleanupBadArticles() {
  console.log("\n── Cleaning up bad articles ──");
  const junkPatterns = [
    "TechCrunch TechCrunch Desktop Logo",
    "Skip to content",
    "The Verge Skip to main content",
    "Skip to main content",
    "Ars Technica",
    "Advertisement",
  ];

  const allArticles = db.prepare("SELECT id, content, excerpt FROM articles").all() as {
    id: number; content: string; excerpt: string;
  }[];

  const update = db.prepare("UPDATE articles SET content = ?, updated_at = datetime('now') WHERE id = ?");
  let fixed = 0;

  for (const a of allArticles) {
    const content = (a.content || "").trim();
    const plainText = stripHtml(content);
    const isBad = junkPatterns.some((p) => content.startsWith(p) || plainText.startsWith(p)) ||
      // Full page dumps: very long with no HTML structure
      (content.length > 1000 && !content.includes("<p>") && !content.includes("<h")) ||
      // Contains navigation-like text at the start
      /^(<[^>]+>)?\s*(Text settings|Button|Menu|Navigation|AI News Tech)/i.test(plainText) ||
      // Content has too much junk relative to text
      (plainText.length < 100 && content.length > 500);

    if (isBad) {
      const fallback = a.excerpt ? `<p>${a.excerpt}</p>` : `<p>${content.slice(0, 300)}</p>`;
      update.run(fallback, a.id);
      fixed++;
    }
  }
  console.log(`Fixed ${fixed} articles with bad content.`);
}

async function backfillMissingImages() {
  console.log("\n── Backfilling missing article images ──");
  const articles = db.prepare(
    "SELECT a.id, a.title, a.slug, c.slug as category FROM articles a JOIN categories c ON a.category_id = c.id WHERE (a.featured_image IS NULL OR a.featured_image = '') LIMIT 10"
  ).all() as { id: number; title: string; slug: string; category: string }[];

  if (articles.length === 0) {
    console.log("No articles missing images.");
    return;
  }

  const update = db.prepare("UPDATE articles SET featured_image = ?, updated_at = datetime('now') WHERE id = ?");
  let generated = 0;

  for (const article of articles) {
    const imagePath = await generateArticleImage(article.title, article.category, article.slug);
    if (imagePath) {
      update.run(imagePath, article.id);
      generated++;
      console.log(`  ✓ Backfilled image for: ${article.title}`);
      await sleep(2000);
    }
  }
  console.log(`Backfilled ${generated}/${articles.length} article images.`);
}

// ── Delete junk articles ────────────────────────────────────────────
function deleteJunkArticles() {
  console.log("\n── Deleting junk articles ──");
  const result = db.prepare(`
    DELETE FROM articles WHERE
      title LIKE 'Show HN:%'
      OR title LIKE '%[pdf]%'
      OR title LIKE '%Abstract:%'
      OR title LIKE '%arXiv%'
      OR title LIKE 'State of Show HN%'
      OR title LIKE '%(2009)%'
      OR title LIKE '%(2008)%'
      OR title LIKE '%(2007)%'
      OR title LIKE 'Study:%'
  `).run();
  console.log(`Deleted ${result.changes} junk articles.`);
}

// ── Cleanup bad excerpts ────────────────────────────────────────────
async function cleanupExcerpts() {
  console.log("\n── Cleaning up bad excerpts ──");
  const badExcerpts = db.prepare(`
    SELECT a.id, a.title, a.excerpt, a.content, c.slug as category
    FROM articles a JOIN categories c ON a.category_id = c.id
    WHERE a.excerpt = a.title
      OR LENGTH(a.excerpt) < 50
      OR a.excerpt LIKE 'Abstract:%'
      OR a.excerpt LIKE 'Show HN:%'
      OR a.excerpt LIKE '&#%'
    LIMIT 20
  `).all() as { id: number; title: string; excerpt: string; content: string; category: string }[];

  if (badExcerpts.length === 0) {
    console.log("No bad excerpts to fix.");
    return;
  }

  const update = db.prepare("UPDATE articles SET excerpt = ?, updated_at = datetime('now') WHERE id = ?");
  let fixed = 0;

  for (const article of badExcerpts) {
    let newExcerpt = generateExcerpt(article.title, article.content, "");

    // If still empty, try Gemini
    if (!newExcerpt || newExcerpt.length < 30) {
      const apiKey = getGeminiApiKey();
      if (apiKey) {
        try {
          const response = await fetch(
            `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`,
            {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                contents: [{ parts: [{ text: `Write a single sentence summary (max 150 chars) for this tech news article titled: "${article.title}". Just the summary, no quotes.` }] }],
                generationConfig: { temperature: 0.3, maxOutputTokens: 100 }
              })
            }
          );
          const data = await response.json();
          const text = data.candidates?.[0]?.content?.parts?.[0]?.text?.trim();
          if (text && text.length > 20) newExcerpt = truncateAtSentence(text, 200);
          await sleep(500);
        } catch {}
      }
    }

    if (newExcerpt && newExcerpt.length > 20 && newExcerpt.toLowerCase() !== article.title.toLowerCase()) {
      update.run(newExcerpt, article.id);
      fixed++;
      console.log(`  ✓ Fixed excerpt for: ${article.title}`);
    }
  }
  console.log(`Fixed ${fixed}/${badExcerpts.length} bad excerpts.`);
}

// ── Reassign authors for existing articles ──────────────────────────
function reassignAuthors() {
  console.log("\n── Reassigning author variety ──");
  const articles = db.prepare(`
    SELECT a.id, a.title, c.slug as category FROM articles a
    JOIN categories c ON a.category_id = c.id
    WHERE a.author_id = 1
  `).all() as { id: number; title: string; category: string }[];

  if (articles.length === 0) return;

  const update = db.prepare("UPDATE articles SET author_id = ?, updated_at = datetime('now') WHERE id = ?");
  let reassigned = 0;
  for (const a of articles) {
    const authorId = getAuthorId(a.category);
    if (authorId !== 1) {
      update.run(authorId, a.id);
      reassigned++;
    }
  }
  console.log(`Reassigned ${reassigned} articles to varied authors.`);
}

main()
  .then(() => deleteJunkArticles())
  .then(() => cleanupBadArticles())
  .then(() => enhanceShortArticles())
  .then(() => { ensureAuthors(); reassignAuthors(); })
  .then(() => cleanupExcerpts())
  .then(() => backfillMissingImages())
  .catch(console.error);
