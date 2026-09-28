import type { MetadataRoute } from "next";
import { getAllArticlesForSitemap, getNewsletterEditions, type ApiArticle } from "./lib/api";
import { parseApiDate } from "./lib/dates";
import { fallbackArticles } from "./lib/fallback";
import { CATEGORIES } from "./data/articles";
import { GLOSSARY } from "./data/glossary";

const BASE_URL = "https://www.aiandtech.news";

export function articleLastModified(article: Pick<ApiArticle, "published_at" | "updated_at">): Date | undefined {
  const published = parseApiDate(article.published_at);
  const updated = parseApiDate(article.updated_at);

  // An edit made after publication can change the page. Never advertise a
  // pre-publication edit or a future timestamp as a fresh update.
  if (updated && updated.getTime() <= Date.now() && (!published || updated > published)) return updated;
  return published || undefined;
}

function snapshotArticlePages(): MetadataRoute.Sitemap {
  return fallbackArticles().map((article) => ({
    url: `${BASE_URL}/article/${article.slug}`,
    lastModified: parseApiDate(article.publishedAt) || undefined,
  }));
}

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  // Only emit lastModified where the source supplies a meaningful date.
  const static_pages: MetadataRoute.Sitemap = [
    { url: BASE_URL },
    { url: `${BASE_URL}/archive` },
    { url: `${BASE_URL}/about` },
    { url: `${BASE_URL}/newsletter/archive` },
    { url: `${BASE_URL}/quiz` },
  ];

  let edition_pages: MetadataRoute.Sitemap = [];
  try {
    const editions = await getNewsletterEditions(100);
    edition_pages = (editions?.editions || []).map((edition) => ({
      url: `${BASE_URL}/newsletter/archive/${edition.edition}`,
      lastModified: parseApiDate(edition.createdAt) || undefined,
    }));
  } catch {
    // fall through with no edition pages
  }

  // Category pages
  const category_pages: MetadataRoute.Sitemap = Object.keys(CATEGORIES).map((slug) => ({
    url: `${BASE_URL}/${slug}`,
  }));

  // Article pages
  let article_pages: MetadataRoute.Sitemap = [];
  try {
    const articles = await getAllArticlesForSitemap();
    if (articles) {
      article_pages = articles.map((a) => ({
        url: `${BASE_URL}/article/${a.slug}`,
        lastModified: articleLastModified(a),
      }));
    } else article_pages = snapshotArticlePages();
  } catch {
    article_pages = snapshotArticlePages();
  }

  const glossary_pages: MetadataRoute.Sitemap = [
    { url: `${BASE_URL}/glossary` },
    ...GLOSSARY.map((entry) => ({
      url: `${BASE_URL}/glossary/${entry.slug}`,
    })),
  ];

  return [...static_pages, ...category_pages, ...glossary_pages, ...edition_pages, ...article_pages];
}
