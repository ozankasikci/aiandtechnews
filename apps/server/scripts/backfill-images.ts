/**
 * Backfill missing featured_image by fetching og:image from original article URLs.
 * For seed articles without URLs, assign relevant Unsplash images.
 */
import db, { initializeDatabase } from "../src/db";

initializeDatabase();

async function safeFetch(url: string): Promise<string | null> {
  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10000);
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

function extractOgImage(html: string): string | null {
  const match = html.match(/<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']/i)
    || html.match(/<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']/i);
  return match?.[1] || null;
}

// Search for article image using web search-like approach: try common tech news image sources
const FALLBACK_IMAGES: Record<string, string> = {
  ai: "https://images.unsplash.com/photo-1677442136019-21780ecad995?w=1200&h=630&fit=crop",
  tech: "https://images.unsplash.com/photo-1518770660439-4636190af475?w=1200&h=630&fit=crop",
  science: "https://images.unsplash.com/photo-1451187580459-43490279c0fa?w=1200&h=630&fit=crop",
  programming: "https://images.unsplash.com/photo-1555066931-4365d14bab8c?w=1200&h=630&fit=crop",
  startups: "https://images.unsplash.com/photo-1559136555-9303baea8ebd?w=1200&h=630&fit=crop",
  entertainment: "https://images.unsplash.com/photo-1598899134739-24c46f58b8c0?w=1200&h=630&fit=crop",
  reviews: "https://images.unsplash.com/photo-1519389950473-47ba0277781c?w=1200&h=630&fit=crop",
  creators: "https://images.unsplash.com/photo-1611162617213-7d7a39e9b1d7?w=1200&h=630&fit=crop",
  deals: "https://images.unsplash.com/photo-1607082348824-0a96f2a4b9da?w=1200&h=630&fit=crop",
};

async function main() {
  // First: try to find articles that were scraped from known sources and backfill og:image
  const articles = db.prepare(`
    SELECT a.id, a.title, a.slug, a.content, c.slug as cat_slug
    FROM articles a
    JOIN categories c ON a.category_id = c.id
    WHERE a.featured_image IS NULL
  `).all() as Array<{ id: number; title: string; slug: string; content: string; cat_slug: string }>;

  console.log(`Found ${articles.length} articles without images`);

  const update = db.prepare("UPDATE articles SET featured_image = ? WHERE id = ?");

  for (const a of articles) {
    // Try to find a URL in the content (some scraped articles might reference their source)
    const urlMatch = a.content.match(/https?:\/\/[^\s"'<>]+/);
    let image: string | null = null;

    if (urlMatch) {
      const html = await safeFetch(urlMatch[0]);
      if (html) image = extractOgImage(html);
    }

    // Try searching by title on common sources
    if (!image) {
      // Try TechCrunch
      const searchUrl = `https://techcrunch.com/?s=${encodeURIComponent(a.title.slice(0, 50))}`;
      const html = await safeFetch(searchUrl);
      if (html) image = extractOgImage(html);
    }

    // Fallback to category-based Unsplash
    if (!image) {
      image = FALLBACK_IMAGES[a.cat_slug] || FALLBACK_IMAGES.tech;
    }

    if (image) {
      update.run(image, a.id);
      console.log(`✅ ${a.title.slice(0, 50)}... → ${image.slice(0, 60)}`);
    }
  }

  // Also clean up CDATA and HTML entities in titles
  const dirtyTitles = db.prepare(`
    SELECT id, title, excerpt FROM articles WHERE title LIKE '%CDATA%' OR title LIKE '%&#%' OR excerpt LIKE '%&#%'
  `).all() as Array<{ id: number; title: string; excerpt: string }>;

  const updateTitle = db.prepare("UPDATE articles SET title = ?, excerpt = ? WHERE id = ?");
  for (const a of dirtyTitles) {
    const clean = (s: string) => s
      .replace(/<!\[CDATA\[(.*?)\]\]>/g, "$1")
      .replace(/&#8217;/g, "'").replace(/&#8216;/g, "'")
      .replace(/&#8220;/g, '"').replace(/&#8221;/g, '"')
      .replace(/&#8211;/g, "–").replace(/&#8212;/g, "—")
      .replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#39;/g, "'");
    updateTitle.run(clean(a.title), clean(a.excerpt), a.id);
    console.log(`🧹 Cleaned: ${clean(a.title).slice(0, 50)}`);
  }

  console.log("\nDone!");
}

main().catch(console.error);
