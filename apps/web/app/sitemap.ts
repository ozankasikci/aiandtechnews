import { MetadataRoute } from "next";
import { getArticlesUpTo, getNewsletterEditions } from "./lib/api";
import { parseApiDate } from "./lib/dates";
import { CATEGORIES } from "./data/articles";

const BASE_URL = "https://www.aiandtech.news";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  // Static pages
  const static_pages: MetadataRoute.Sitemap = [
    { url: BASE_URL, lastModified: new Date(), changeFrequency: "hourly", priority: 1 },
    { url: `${BASE_URL}/about`, lastModified: new Date(), changeFrequency: "monthly", priority: 0.3 },
    { url: `${BASE_URL}/newsletter/archive`, lastModified: new Date(), changeFrequency: "daily", priority: 0.5 },
  ];

  let edition_pages: MetadataRoute.Sitemap = [];
  try {
    const editions = await getNewsletterEditions(100);
    edition_pages = (editions?.editions || []).map((edition) => ({
      url: `${BASE_URL}/newsletter/archive/${edition.edition}`,
      lastModified: parseApiDate(edition.createdAt) || new Date(),
      changeFrequency: "yearly" as const,
      priority: 0.4,
    }));
  } catch {
    // fall through with no edition pages
  }

  // Category pages
  const category_pages: MetadataRoute.Sitemap = Object.keys(CATEGORIES).map((slug) => ({
    url: `${BASE_URL}/${slug}`,
    lastModified: new Date(),
    changeFrequency: "hourly" as const,
    priority: 0.8,
  }));

  // Article pages
  let article_pages: MetadataRoute.Sitemap = [];
  try {
    const articles = await getArticlesUpTo(500);
    if (articles) {
      article_pages = articles.map((a) => ({
        url: `${BASE_URL}/article/${a.slug}`,
        lastModified: parseApiDate(a.published_at) || new Date(),
        changeFrequency: "weekly" as const,
        priority: 0.7,
      }));
    }
  } catch {
    // fall through with empty articles
  }

  return [...static_pages, ...category_pages, ...edition_pages, ...article_pages];
}
