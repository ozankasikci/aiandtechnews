import { parseApiDate } from "../lib/dates";

const BASE_URL = "https://www.aiandtech.news";
const RECENT_NEWS_WINDOW_MS = 2 * 24 * 60 * 60 * 1000;

export interface NewsSitemapArticle {
  slug: string;
  title: string;
  published_at: string;
}

function escapeXml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

export function buildNewsSitemap(articles: NewsSitemapArticle[], now = new Date()): string {
  const cutoff = now.getTime() - RECENT_NEWS_WINDOW_MS;
  const recentArticles = articles
    .map((article) => ({ article, publishedDate: parseApiDate(article.published_at) }))
    .filter(({ publishedDate }) => {
      if (!publishedDate) return false;
      const timestamp = publishedDate.getTime();
      return timestamp >= cutoff && timestamp <= now.getTime();
    })
    .sort((a, b) => b.publishedDate!.getTime() - a.publishedDate!.getTime())
    .slice(0, 1_000);

  const entries = recentArticles.map(({ article, publishedDate }) => {
    const articleUrl = new URL(`/article/${encodeURIComponent(article.slug)}`, BASE_URL).toString();

    return [
      "  <url>",
      `    <loc>${escapeXml(articleUrl)}</loc>`,
      "    <news:news>",
      "      <news:publication>",
      "        <news:name>AI and Tech News</news:name>",
      "        <news:language>en</news:language>",
      "      </news:publication>",
      `      <news:publication_date>${publishedDate!.toISOString()}</news:publication_date>`,
      `      <news:title>${escapeXml(article.title)}</news:title>`,
      "    </news:news>",
      "  </url>",
    ].join("\n");
  });

  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">',
    ...entries,
    "</urlset>",
    "",
  ].join("\n");
}
