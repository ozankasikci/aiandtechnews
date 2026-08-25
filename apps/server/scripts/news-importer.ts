import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import db, { initializeDatabase } from "../src/db";
import {
  APPROVED_FEEDS,
  EDITORIAL_AUTHOR,
  getAutomaticItemRejectionReason,
  getItemRejectionReason,
  normalizeSourceUrl,
  RewrittenArticle,
  slugify,
  sourceForUrl,
  stripHtml,
  validateRewrittenArticle,
} from "./news-policy";

interface FeedArticle {
  title: string;
  url: string;
  source: string;
  publishedAt: string | null;
}

interface FetchResult {
  body: string;
  finalUrl: string;
}

const CATEGORY_KEYWORDS: Record<string, string[]> = {
  ai: ["artificial intelligence", "machine learning", "llm", "openai", "chatgpt", "anthropic", "gemini", "generative ai", "neural network", "ai model"],
  science: ["science", "space", "nasa", "physics", "biology", "climate", "quantum", "telescope", "asteroid", "fusion", "genome"],
  entertainment: ["game", "gaming", "movie", "film", "streaming", "playstation", "xbox", "nintendo", "netflix", "spotify"],
  reviews: ["review", "hands-on", "benchmark", "comparison", "tested", "unboxing"],
  creators: ["creator", "youtube", "tiktok", "influencer", "podcast", "twitch", "patreon"],
  tech: [],
};

const CATEGORY_COLORS: Record<string, string> = {
  tech: "#3b82f6",
  reviews: "#f59e0b",
  science: "#10b981",
  entertainment: "#ec4899",
  ai: "#8b5cf6",
  creators: "#f97316",
};

const LOCK_PATH = process.env.NEWS_IMPORT_LOCK_PATH || path.join(os.tmpdir(), "technews-news-import.lock");
const LOCK_STALE_AFTER_MS = 2 * 60 * 60 * 1000;

function decodeHtmlEntities(value: string): string {
  return value
    .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, "$1")
    .replace(/&#x([0-9a-f]+);/gi, (_, hex: string) => String.fromCodePoint(Number.parseInt(hex, 16)))
    .replace(/&#(\d+);/g, (_, decimal: string) => String.fromCodePoint(Number.parseInt(decimal, 10)))
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&apos;/gi, "'")
    .replace(/\s+/g, " ")
    .trim();
}

function acquireImporterLock(): () => void {
  const token = `${process.pid}-${Date.now()}`;

  const tryAcquire = (canRemoveStale: boolean): void => {
    try {
      const descriptor = fs.openSync(LOCK_PATH, "wx");
      fs.writeFileSync(descriptor, JSON.stringify({ token, pid: process.pid, startedAt: new Date().toISOString() }));
      fs.closeSync(descriptor);
    } catch (error) {
      const nodeError = error as NodeJS.ErrnoException;
      if (nodeError.code !== "EEXIST") throw error;

      let startedAt = 0;
      try {
        const existing = JSON.parse(fs.readFileSync(LOCK_PATH, "utf8")) as { startedAt?: string };
        startedAt = existing.startedAt ? Date.parse(existing.startedAt) : 0;
      } catch {
        startedAt = 0;
      }

      const isStale = !startedAt || Date.now() - startedAt > LOCK_STALE_AFTER_MS;
      if (!canRemoveStale || !isStale) {
        throw new Error(`Another news importer run holds ${LOCK_PATH}`);
      }
      fs.unlinkSync(LOCK_PATH);
      tryAcquire(false);
    }
  };

  tryAcquire(true);
  return () => {
    try {
      const existing = JSON.parse(fs.readFileSync(LOCK_PATH, "utf8")) as { token?: string };
      if (existing.token === token) fs.unlinkSync(LOCK_PATH);
    } catch {
      // The lock was already removed or replaced. Do not remove an unknown lock.
    }
  };
}

async function fetchText(url: string, timeoutMs = 20_000): Promise<FetchResult | null> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(url, {
      redirect: "follow",
      signal: controller.signal,
      headers: {
        Accept: "text/html,application/xhtml+xml,application/rss+xml,application/atom+xml;q=0.9,*/*;q=0.8",
        "User-Agent": "TechNews-Editorial-Importer/2.0",
      },
    });
    if (!response.ok) {
      console.error(`Fetch failed with ${response.status}: ${url}`);
      return null;
    }
    return { body: await response.text(), finalUrl: response.url || url };
  } catch (error) {
    console.error(`Fetch failed: ${url}`, error instanceof Error ? error.message : error);
    return null;
  } finally {
    clearTimeout(timeout);
  }
}

function extractXmlValue(block: string, names: string[]): string {
  for (const name of names) {
    const cdata = block.match(new RegExp(`<${name}[^>]*><!\\[CDATA\\[([\\s\\S]*?)\\]\\]><\\/${name}>`, "i"));
    if (cdata?.[1]) return decodeHtmlEntities(cdata[1]);
    const regular = block.match(new RegExp(`<${name}[^>]*>([\\s\\S]*?)<\\/${name}>`, "i"));
    if (regular?.[1]) return decodeHtmlEntities(stripHtml(regular[1]));
  }
  return "";
}

function parseFeed(xml: string, source: string): FeedArticle[] {
  const blocks = xml.match(/<(?:item|entry)\b[\s\S]*?<\/(?:item|entry)>/gi) || [];
  const articles: FeedArticle[] = [];

  for (const block of blocks.slice(0, 30)) {
    const title = extractXmlValue(block, ["title"]);
    const rssLink = extractXmlValue(block, ["link"]);
    const atomLink = block.match(/<link\b[^>]*\bhref=["']([^"']+)["'][^>]*>/i)?.[1] || "";
    const urlValue = decodeHtmlEntities(rssLink || atomLink);
    const dateValue = extractXmlValue(block, ["pubDate", "published", "updated"]);
    const publicationDate = dateValue ? new Date(dateValue) : null;

    if (!title || !urlValue) continue;
    try {
      const normalizedUrl = normalizeSourceUrl(urlValue);
      articles.push({
        title,
        url: normalizedUrl,
        source,
        publishedAt: publicationDate && !Number.isNaN(publicationDate.getTime()) ? publicationDate.toISOString() : null,
      });
    } catch {
      console.log(`Skipping invalid RSS URL from ${source}: ${urlValue}`);
    }
  }
  return articles;
}

async function fetchApprovedFeedItems(automaticOnly: boolean): Promise<FeedArticle[]> {
  const feedResults = await Promise.all(APPROVED_FEEDS.map(async (feed) => {
    console.log(`Fetching RSS: ${feed.source}`);
    const result = await fetchText(feed.url);
    if (!result) return [];
    const items = parseFeed(result.body, feed.source);
    console.log(`Found ${items.length} RSS items from ${feed.source}`);
    return items;
  }));

  const seenUrls = new Set<string>();
  const seenSlugs = new Set<string>();
  const uniqueItems: FeedArticle[] = [];

  for (const item of feedResults.flat()) {
    const rejection = automaticOnly
      ? getAutomaticItemRejectionReason(item.title, item.url, item.source)
      : getItemRejectionReason(item.title, item.url, item.source);
    if (rejection) {
      console.log(`Rejected RSS item (${rejection}): ${item.title}`);
      continue;
    }
    const slug = slugify(item.title);
    if (seenUrls.has(item.url) || seenSlugs.has(slug)) continue;
    seenUrls.add(item.url);
    seenSlugs.add(slug);
    uniqueItems.push(item);
  }

  return uniqueItems.sort((left, right) => {
    const leftTime = left.publishedAt ? Date.parse(left.publishedAt) : 0;
    const rightTime = right.publishedAt ? Date.parse(right.publishedAt) : 0;
    return rightTime - leftTime;
  });
}

function extractMetaContent(html: string, attribute: "property" | "name", value: string): string | null {
  const escapedValue = value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const first = html.match(new RegExp(`<meta[^>]*${attribute}=["']${escapedValue}["'][^>]*content=["']([^"']+)["'][^>]*>`, "i"));
  const second = html.match(new RegExp(`<meta[^>]*content=["']([^"']+)["'][^>]*${attribute}=["']${escapedValue}["'][^>]*>`, "i"));
  return decodeHtmlEntities(first?.[1] || second?.[1] || "") || null;
}

function extractCanonicalUrl(html: string, pageUrl: string): string {
  const first = html.match(/<link[^>]*rel=["'][^"']*canonical[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>/i);
  const second = html.match(/<link[^>]*href=["']([^"']+)["'][^>]*rel=["'][^"']*canonical[^"']*["'][^>]*>/i);
  const rawUrl = decodeHtmlEntities(first?.[1] || second?.[1] || pageUrl);
  try {
    const resolved = new URL(rawUrl, pageUrl).toString();
    return sourceForUrl(resolved) ? normalizeSourceUrl(resolved) : normalizeSourceUrl(pageUrl);
  } catch {
    return normalizeSourceUrl(pageUrl);
  }
}

function extractOgImage(html: string, pageUrl: string): string | null {
  const rawUrl = extractMetaContent(html, "property", "og:image");
  if (!rawUrl) return null;
  try {
    const resolved = new URL(rawUrl, pageUrl);
    return /^https?:$/.test(resolved.protocol) ? resolved.toString() : null;
  } catch {
    return null;
  }
}

function extractJsonLdArticleBody(html: string): string {
  const scripts = html.match(/<script[^>]*type=["']application\/ld\+json["'][^>]*>[\s\S]*?<\/script>/gi) || [];
  const search = (value: unknown): string | null => {
    if (!value || typeof value !== "object") return null;
    if (Array.isArray(value)) {
      for (const item of value) {
        const result = search(item);
        if (result) return result;
      }
      return null;
    }
    const record = value as Record<string, unknown>;
    if (typeof record.articleBody === "string" && record.articleBody.length > 500) return record.articleBody;
    for (const child of Object.values(record)) {
      const result = search(child);
      if (result) return result;
    }
    return null;
  };

  for (const script of scripts) {
    const json = script.replace(/^<script[^>]*>/i, "").replace(/<\/script>$/i, "").trim();
    try {
      const body = search(JSON.parse(json));
      if (body) return decodeHtmlEntities(stripHtml(body));
    } catch {
      // Continue to the next JSON-LD block.
    }
  }
  return "";
}

function extractSourceText(html: string): string {
  const jsonLdBody = extractJsonLdArticleBody(html);
  if (jsonLdBody.length >= 800) return jsonLdBody.slice(0, 14_000);

  const cleaned = html
    .replace(/<!--([\s\S]*?)-->/g, " ")
    .replace(/<(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)\b[^>]*>[\s\S]*?<\/(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)>/gi, " ");
  const articleMatch = cleaned.match(/<article\b[^>]*>([\s\S]*?)<\/article>/i);
  const mainMatch = cleaned.match(/<main\b[^>]*>([\s\S]*?)<\/main>/i);
  const scope = articleMatch?.[1] || mainMatch?.[1] || cleaned;
  const paragraphBlocks = scope.match(/<p\b[^>]*>[\s\S]*?<\/p>/gi) || [];
  const seen = new Set<string>();
  const paragraphs: string[] = [];

  for (const block of paragraphBlocks) {
    const text = decodeHtmlEntities(stripHtml(block));
    if (text.length < 60) continue;
    if (/^(?:advertisement|subscribe|sign up|read more|all rights reserved)\b/i.test(text)) continue;
    if (/cookie|newsletter preferences|privacy policy/i.test(text) && text.length < 250) continue;
    const key = text.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    paragraphs.push(text);
    if (paragraphs.join("\n\n").length >= 14_000) break;
  }

  return paragraphs.join("\n\n").slice(0, 14_000);
}

async function isUsableRemoteImage(imageUrl: string): Promise<boolean> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 10_000);
  try {
    const response = await fetch(imageUrl, {
      method: "HEAD",
      redirect: "follow",
      signal: controller.signal,
      headers: { "User-Agent": "TechNews-Editorial-Importer/2.0" },
    });
    return response.ok && (response.headers.get("content-type") || "").toLowerCase().startsWith("image/");
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
  }
}

function getGeminiApiKey(): string | null {
  if (process.env.GEMINI_API_KEY) return process.env.GEMINI_API_KEY;
  try {
    const configPath = path.join(os.homedir(), ".openclaw", "openclaw.json");
    const config = JSON.parse(fs.readFileSync(configPath, "utf8")) as {
      skills?: { entries?: Record<string, { apiKey?: string }> };
    };
    return config.skills?.entries?.["nano-banana-pro"]?.apiKey || null;
  } catch {
    return null;
  }
}

async function callGeminiText(prompt: string): Promise<string | null> {
  const apiKey = getGeminiApiKey();
  if (!apiKey) {
    console.error("GEMINI_API_KEY is required. The importer will not publish fallback text.");
    return null;
  }

  const model = process.env.GEMINI_TEXT_MODEL || "gemini-3.5-flash-lite";
  const response = await fetch(`https://generativelanguage.googleapis.com/v1beta/models/${model}:generateContent`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "x-goog-api-key": apiKey },
    body: JSON.stringify({
      contents: [{ parts: [{ text: prompt }] }],
      generationConfig: {
        maxOutputTokens: 4096,
        responseMimeType: "application/json",
        temperature: 0.4,
      },
    }),
  });
  if (!response.ok) {
    console.error(`Gemini rewrite failed with ${response.status}: ${(await response.text()).slice(0, 300)}`);
    return null;
  }
  const data = await response.json() as {
    candidates?: Array<{ content?: { parts?: Array<{ text?: string }> } }>;
  };
  return data.candidates?.[0]?.content?.parts?.map((part) => part.text || "").join("").trim() || null;
}

function parseRewrittenArticle(value: string): RewrittenArticle | null {
  const cleaned = value.replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/i, "").trim();
  const candidate = cleaned.match(/\{[\s\S]*\}/)?.[0] || cleaned;
  try {
    const parsed = JSON.parse(candidate) as Partial<RewrittenArticle>;
    if (typeof parsed.title !== "string" || typeof parsed.excerpt !== "string" || typeof parsed.content !== "string") return null;
    return { title: parsed.title.trim(), excerpt: parsed.excerpt.trim(), content: parsed.content.trim() };
  } catch {
    return null;
  }
}

async function rewriteArticle(
  item: FeedArticle,
  sourceText: string,
  minWords = 150,
): Promise<RewrittenArticle | null> {
  const basePrompt = `You are TechNews Editorial. Rewrite the source reporting below as an original news article.

Requirements:
- Preserve every fact, name, number, date, and quotation accurately.
- Never invent quotations, statistics, motives, consequences, or unsupported details.
- Do not copy the source wording. Use a conversational but factual voice.
- Use short, punchy sentences and no filler.
- Never use an em dash.
- Never use the phrases "In a move that" or "It remains to be seen".
- Never use "groundbreaking", "revolutionary", or "game-changing".
- Write a short, direct, factual, non-clickbait headline, maximum 120 characters.
- Write one plain-sentence excerpt, maximum 180 characters, with no HTML.
- Write ${minWords} to 300 words in 5 to 8 paragraphs.
- Open with a clear lede explaining what happened.
- Include relevant context and background.
- Include supported industry implications or analysis without presenting speculation as fact.
- End with the next known step. Do not invent a next step.
- Use only <p> and optional <h2> tags, with no tag attributes.
- Do not include the headline in the body.
- Do not add a source or sources footer. Attribution is stored separately.

Publication: ${item.source}
Original headline: ${item.title}
Canonical source URL: ${item.url}

Source reporting:
${sourceText}

Return only JSON with this exact shape:
{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>"}`;

  let correction = "";
  for (let attempt = 1; attempt <= 2; attempt += 1) {
    const raw = await callGeminiText(`${basePrompt}${correction}`);
    if (!raw) return null;
    const article = parseRewrittenArticle(raw);
    if (!article) {
      correction = "\n\nYour previous response was not valid JSON. Return only the required JSON object.";
      continue;
    }
    const validationErrors = validateRewrittenArticle(article, { minWords });
    if (validationErrors.length === 0) return article;
    console.log(`Rewrite validation failed on attempt ${attempt}: ${validationErrors.join("; ")}`);
    correction = `\n\nYour previous draft failed these checks: ${validationErrors.join("; ")}. Rewrite it again from the same source reporting and satisfy every requirement.`;
  }
  return null;
}

async function generateFallbackImage(article: RewrittenArticle, slug: string): Promise<string | null> {
  const apiKey = getGeminiApiKey();
  if (!apiKey) return null;
  const model = process.env.GEMINI_IMAGE_MODEL || "gemini-3.1-flash-image-preview";
  const prompt = `Create a factual editorial image for this technology news article. Headline: ${article.title}. Summary: ${article.excerpt}. Match the specific subject and neutral news tone. Use a 16:9 composition. Do not add text, captions, logos, watermarks, or dramatic effects.`;

  try {
    const response = await fetch(`https://generativelanguage.googleapis.com/v1beta/models/${model}:generateContent`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "x-goog-api-key": apiKey },
      body: JSON.stringify({
        contents: [{ parts: [{ text: prompt }] }],
        generationConfig: {
          responseFormat: { image: { aspectRatio: "16:9", imageSize: "2K" } },
        },
      }),
    });
    if (!response.ok) {
      console.error(`Gemini image generation failed with ${response.status}: ${(await response.text()).slice(0, 300)}`);
      return null;
    }
    const data = await response.json() as {
      candidates?: Array<{ content?: { parts?: Array<{ inlineData?: { data?: string; mimeType?: string }; inline_data?: { data?: string; mime_type?: string } }> } }>;
    };
    const parts = data.candidates?.[0]?.content?.parts || [];
    for (const part of parts) {
      const imageData = part.inlineData?.data || part.inline_data?.data;
      const mimeType = part.inlineData?.mimeType || part.inline_data?.mime_type;
      if (!imageData) continue;
      const extension = mimeType === "image/jpeg" ? "jpg" : mimeType === "image/webp" ? "webp" : "png";
      const outputDirectory = path.resolve(__dirname, "../../web/public/images/generated");
      fs.mkdirSync(outputDirectory, { recursive: true });
      const filename = `${slug}.${extension}`;
      fs.writeFileSync(path.join(outputDirectory, filename), Buffer.from(imageData, "base64"));
      return `/images/generated/${filename}`;
    }
  } catch (error) {
    console.error("Gemini image generation failed:", error instanceof Error ? error.message : error);
  }
  return null;
}

function categorize(title: string, content: string): string {
  const headline = title.toLowerCase();
  const sample = stripHtml(content).toLowerCase().slice(0, 1_000);
  for (const [category, keywords] of Object.entries(CATEGORY_KEYWORDS)) {
    if (category !== "tech" && keywords.some((keyword) => headline.includes(keyword))) return category;
  }
  for (const [category, keywords] of Object.entries(CATEGORY_KEYWORDS)) {
    if (category !== "tech" && keywords.filter((keyword) => sample.includes(keyword)).length >= 2) return category;
  }
  return "tech";
}

function ensureCategories(): void {
  const insert = db.prepare("INSERT OR IGNORE INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)");
  for (const slug of Object.keys(CATEGORY_COLORS)) {
    insert.run(slug.charAt(0).toUpperCase() + slug.slice(1), slug, `${slug} news`, CATEGORY_COLORS[slug]);
  }
}

function ensureEditorialAuthor(): number {
  const byName = db.prepare("SELECT id FROM authors WHERE name = ?").get(EDITORIAL_AUTHOR.name) as { id: number } | undefined;
  if (byName) return byName.id;

  const byEmail = db.prepare("SELECT id FROM authors WHERE email = ?").get(EDITORIAL_AUTHOR.email) as { id: number } | undefined;
  if (byEmail) {
    db.prepare("UPDATE authors SET name = ?, bio = ? WHERE id = ?").run(EDITORIAL_AUTHOR.name, EDITORIAL_AUTHOR.bio, byEmail.id);
    return byEmail.id;
  }

  const result = db.prepare(
    "INSERT INTO authors (name, email, password_hash, avatar, bio, role) VALUES (?, ?, 'not-a-login', NULL, ?, 'editor')",
  ).run(EDITORIAL_AUTHOR.name, EDITORIAL_AUTHOR.email, EDITORIAL_AUTHOR.bio);
  return Number(result.lastInsertRowid);
}

function isStored(sourceUrl: string, slug: string): boolean {
  const duplicate = db.prepare("SELECT id FROM articles WHERE source_url = ? OR slug = ? LIMIT 1").get(sourceUrl, slug);
  return Boolean(duplicate);
}

async function importFeedItem(item: FeedArticle, automaticOnly: boolean, minWords = 150): Promise<boolean> {
  const initialRejection = automaticOnly
    ? getAutomaticItemRejectionReason(item.title, item.url, item.source)
    : getItemRejectionReason(item.title, item.url, item.source);
  if (initialRejection) {
    console.log(`Rejected (${initialRejection}): ${item.title}`);
    return false;
  }

  const initialSlug = slugify(item.title);
  if (!initialSlug || isStored(item.url, initialSlug)) {
    console.log(`Duplicate source URL or source headline slug: ${item.title}`);
    return false;
  }

  console.log(`Fetching article: ${item.url}`);
  const fetched = await fetchText(item.url);
  if (!fetched) return false;
  const canonicalUrl = extractCanonicalUrl(fetched.body, fetched.finalUrl);
  const canonicalSource = sourceForUrl(canonicalUrl);
  if (canonicalSource !== item.source) {
    console.log(`Rejected canonical URL outside ${item.source}: ${canonicalUrl}`);
    return false;
  }
  const canonicalRejection = automaticOnly
    ? getAutomaticItemRejectionReason(item.title, canonicalUrl, item.source)
    : getItemRejectionReason(item.title, canonicalUrl, item.source);
  if (canonicalRejection) {
    console.log(`Rejected canonical item (${canonicalRejection}): ${item.title}`);
    return false;
  }
  if (isStored(canonicalUrl, initialSlug)) {
    console.log(`Duplicate canonical source URL or source headline slug: ${item.title}`);
    return false;
  }

  const sourceText = extractSourceText(fetched.body);
  if (sourceText.length < 800) {
    console.log(`Skipped because the source text is too short for an accurate rewrite: ${item.title}`);
    return false;
  }

  const rewritten = await rewriteArticle({ ...item, url: canonicalUrl }, sourceText, minWords);
  if (!rewritten) {
    console.log(`Skipped because a compliant rewrite could not be produced: ${item.title}`);
    return false;
  }

  const finalSlug = slugify(rewritten.title);
  if (!finalSlug || isStored(canonicalUrl, finalSlug)) {
    console.log(`Duplicate canonical source URL or final generated slug: ${rewritten.title}`);
    return false;
  }

  const ogImage = extractOgImage(fetched.body, canonicalUrl);
  let featuredImage: string | null = null;
  if (ogImage && await isUsableRemoteImage(ogImage)) {
    featuredImage = ogImage;
    console.log("Using source og:image by hotlink.");
  } else {
    console.log("No usable source og:image. Generating a Gemini fallback image.");
    featuredImage = await generateFallbackImage(rewritten, finalSlug);
  }
  if (!featuredImage) {
    console.log(`Skipped because no compliant featured image is available: ${rewritten.title}`);
    return false;
  }

  const validationErrors = validateRewrittenArticle(rewritten, { minWords });
  if (validationErrors.length) {
    console.log(`Final validation failed: ${validationErrors.join("; ")}`);
    return false;
  }

  const categorySlug = categorize(rewritten.title, rewritten.content);
  const category = db.prepare("SELECT id FROM categories WHERE slug = ?").get(categorySlug) as { id: number } | undefined;
  if (!category) throw new Error(`Missing category: ${categorySlug}`);
  const authorId = ensureEditorialAuthor();

  const insert = db.transaction(() => {
    if (isStored(canonicalUrl, finalSlug)) throw new Error("Duplicate detected immediately before insertion");
    db.prepare(`
      INSERT INTO articles (
        title, slug, excerpt, content, featured_image, category_id, author_id,
        status, published_at, view_count, created_at, updated_at, source, source_url
      ) VALUES (?, ?, ?, ?, ?, ?, ?, 'published', datetime('now'), 0, datetime('now'), datetime('now'), ?, ?)
    `).run(
      rewritten.title,
      finalSlug,
      rewritten.excerpt,
      rewritten.content,
      featuredImage,
      category.id,
      authorId,
      item.source,
      canonicalUrl,
    );
  });
  insert();

  const stored = db.prepare(`
    SELECT a.title, a.slug, a.status, a.source, a.source_url, au.name AS author
    FROM articles a
    JOIN authors au ON au.id = a.author_id
    WHERE a.source_url = ? AND a.slug = ?
  `).get(canonicalUrl, finalSlug) as {
    title: string;
    slug: string;
    status: string;
    source: string;
    source_url: string;
    author: string;
  } | undefined;

  if (!stored || stored.status !== "published" || stored.author !== EDITORIAL_AUTHOR.name || stored.source !== item.source) {
    throw new Error("Post-insert readback did not match the publishing contract");
  }
  console.log(`Published: ${stored.title} (${stored.slug})`);
  return true;
}

async function runWithLock(operation: () => Promise<number>): Promise<number> {
  const releaseLock = acquireImporterLock();
  try {
    initializeDatabase();
    ensureCategories();
    return await operation();
  } finally {
    releaseLock();
  }
}

export async function runDailyImporter(): Promise<number> {
  if (process.env.IMPORT_ONLY !== "1") {
    throw new Error("The daily importer requires IMPORT_ONLY=1 so it cannot trigger maintenance work");
  }
  if (process.env.MAX_IMPORTS && Number(process.env.MAX_IMPORTS) > 1) {
    console.log("MAX_IMPORTS was greater than one. The publishing policy clamps every run to one article.");
  }

  return runWithLock(async () => {
    const candidates = await fetchApprovedFeedItems(true);
    for (const candidate of candidates) {
      if (await importFeedItem(candidate, true)) return 1;
    }
    console.log("No compliant new article was available. Published 0 articles.");
    return 0;
  });
}

export async function runManualImporter(articleUrl: string): Promise<number> {
  return runWithLock(async () => {
    let normalizedRequestedUrl: string;
    try {
      normalizedRequestedUrl = normalizeSourceUrl(articleUrl);
    } catch {
      throw new Error("Manual import URL is invalid");
    }
    if (!sourceForUrl(normalizedRequestedUrl)) {
      throw new Error("Manual imports must use an approved RSS publisher");
    }

    const feedItems = await fetchApprovedFeedItems(false);
    const candidate = feedItems.find((item) => item.url === normalizedRequestedUrl);
    if (!candidate) {
      throw new Error("Manual imports must originate from an item currently present in an approved RSS feed");
    }
    return await importFeedItem(candidate, false) ? 1 : 0;
  });
}
