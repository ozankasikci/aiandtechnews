import { parseApiDate } from "../lib/dates";

const BASE_URL = "https://www.aiandtech.news";

export interface RssArticle {
  slug: string;
  title: string;
  excerpt: string;
  published_at: string;
  category?: { name: string };
  author?: { name: string };
}

function escapeXml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

export function buildRssFeed(articles: RssArticle[], now = new Date()): string {
  const items = articles
    .map((article) => ({ article, publishedDate: parseApiDate(article.published_at) }))
    .filter(({ publishedDate }) => publishedDate !== null)
    .sort((a, b) => b.publishedDate!.getTime() - a.publishedDate!.getTime())
    .map(({ article, publishedDate }) => {
      const articleUrl = new URL(`/article/${encodeURIComponent(article.slug)}`, BASE_URL).toString();
      return [
        "    <item>",
        `      <title>${escapeXml(article.title)}</title>`,
        `      <link>${escapeXml(articleUrl)}</link>`,
        `      <guid isPermaLink="true">${escapeXml(articleUrl)}</guid>`,
        `      <pubDate>${publishedDate!.toUTCString()}</pubDate>`,
        ...(article.category?.name ? [`      <category>${escapeXml(article.category.name)}</category>`] : []),
        ...(article.excerpt ? [`      <description>${escapeXml(article.excerpt)}</description>`] : []),
        "    </item>",
      ].join("\n");
    });

  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">',
    "  <channel>",
    "    <title>AI and Tech News</title>",
    `    <link>${BASE_URL}</link>`,
    "    <description>Breaking AI and technology news, daily. Covering artificial intelligence, startups, big tech, and developer tools.</description>",
    "    <language>en</language>",
    `    <lastBuildDate>${now.toUTCString()}</lastBuildDate>`,
    `    <atom:link href="${BASE_URL}/rss.xml" rel="self" type="application/rss+xml"/>`,
    ...items,
    "  </channel>",
    "</rss>",
    "",
  ].join("\n");
}
