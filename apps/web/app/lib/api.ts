import type { Article } from "../data/articles";
import { parseApiDate, toIsoDate } from "./dates";
import { readingTimeLabel } from "./reading-time";
import { usableInlineImages } from "./inline-images";
import { parseTopicDetail, parseTopicList, parseTopicRefs, topicTag, TOPICS_TAG, type TopicDetail, type TopicSummary } from "./topics";

function getApiUrl() {
  const configured = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim();
  if (process.env.NODE_ENV === "production") {
    if (!configured || configured.includes("localhost") || configured.includes("127.0.0.1")) {
      return "https://technews.subtunnel.dev";
    }
  }
  return configured || "http://localhost:4001";
}

const API_URL = getApiUrl();

// The API base the reader's browser calls directly (the article view
// beacon). Only NEXT_PUBLIC_API_URL is meant to be public; API_URL may be a
// private address, so it is used only outside production. Production falls
// back to the public API host, never localhost.
export function getPublicApiUrl(env: Record<string, string | undefined> = process.env): string {
  const configured = env.NEXT_PUBLIC_API_URL?.trim();
  if (env.NODE_ENV === "production") {
    if (!configured || configured.includes("localhost") || configured.includes("127.0.0.1")) {
      return "https://technews.subtunnel.dev";
    }
    return configured.replace(/\/+$/, "");
  }
  return (configured || env.API_URL?.trim() || "http://localhost:4001").replace(/\/+$/, "");
}

// Cache policy. Pages are refreshed on demand by POST /api/revalidate when
// the API publishes or edits an article; these are only the fallbacks.
export const ARTICLE_REVALIDATE_SECONDS = 3600;
export const LIST_REVALIDATE_SECONDS = 300;
export const ARTICLES_TAG = "articles";
export function articleTag(slug: string) {
  return `article:${slug}`;
}

export interface ApiArticle {
  id: number;
  title: string;
  slug: string;
  excerpt: string;
  content: string;
  featured_image: string;
  category_id: number;
  author_id: number;
  status: string;
  published_at: string;
  view_count: number;
  created_at: string;
  updated_at: string;
  meta_title?: string | null;
  meta_description?: string | null;
  tldr?: string[];
  why_it_matters?: string;
  topics?: unknown;
  inlineImages?: {
    url: string;
    alt: string;
    afterParagraph: number;
    credit?: string;
    creditUrl?: string;
    license?: string;
    licenseUrl?: string;
  }[];
  category: { id: number; name: string; slug: string; description: string; color: string };
  author: { id: number; name: string; email: string; avatar: string; bio: string; role: string };
}

export interface ApiCategory {
  id: number;
  name: string;
  slug: string;
  description: string;
  color: string;
}

// Map API color to Tailwind class
function mapColor(color: string): string {
  const colorMap: Record<string, string> = {
    "#8B5CF6": "bg-accent-purple",
    "#8b5cf6": "bg-accent-purple",
    "#3B82F6": "bg-accent-blue",
    "#3b82f6": "bg-accent-blue",
    "#10B981": "bg-accent-green",
    "#10b981": "bg-accent-green",
    "#EC4899": "bg-accent-magenta",
    "#ec4899": "bg-accent-magenta",
    "#F97316": "bg-accent-orange",
    "#f97316": "bg-accent-orange",
    "#F59E0B": "bg-accent-yellow",
    "#f59e0b": "bg-accent-yellow",
    "#22C55E": "bg-accent-green",
    "#22c55e": "bg-accent-green",
    purple: "bg-accent-purple",
    blue: "bg-accent-blue",
    green: "bg-accent-green",
    magenta: "bg-accent-magenta",
    orange: "bg-accent-orange",
  };
  return colorMap[color] || "bg-accent-purple";
}

function timeAgo(dateStr: string): string {
  const parsed = parseApiDate(dateStr);
  if (!parsed) return "Just now";
  const diff = Date.now() - parsed.getTime();
  if (diff < 0) return "Just now";
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  return parsed.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
}

export function mapArticle(a: ApiArticle): Article {
  const publishedDate = parseApiDate(a.published_at || a.created_at);

  return {
    id: a.id,
    slug: a.slug,
    tag: a.category?.name || "News",
    tagColor: mapColor(a.category?.color || ""),
    headline: a.title,
    excerpt: a.excerpt,
    author: a.author?.name || "Staff",
    avatar: a.author?.avatar || "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=40&h=40&fit=crop&crop=face",
    time: timeAgo(a.published_at || a.created_at),
    date: publishedDate?.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }) || "Date unavailable",
    publishedAt: toIsoDate(a.published_at || a.created_at),
    updatedAt: toIsoDate(a.updated_at || a.published_at || a.created_at),
    readTime: readingTimeLabel(a.content),
    image: a.featured_image || "https://images.unsplash.com/photo-1518770660439-4636190af475?w=800&h=500&fit=crop",
    tldr: a.tldr?.length ? a.tldr : undefined,
    whyItMatters: a.why_it_matters || undefined,
    inlineImages: usableInlineImages(a.inlineImages),
    topics: parseTopicRefs(a.topics),
    metaTitle: a.meta_title || undefined,
    metaDescription: a.meta_description || undefined,
    body: a.content,
  };
}

async function apiFetch<T>(path: string, tags?: string[]): Promise<T | null> {
  try {
    const res = await fetch(`${API_URL}${path}`, {
      next: tags ? { revalidate: LIST_REVALIDATE_SECONDS, tags } : { revalidate: LIST_REVALIDATE_SECONDS },
    });
    if (!res.ok) return null;
    return res.json();
  } catch {
    return null;
  }
}

export async function getArticles(opts?: { page?: number; limit?: number; category?: string; search?: string }) {
  const params = new URLSearchParams();
  if (opts?.page) params.set("page", String(opts.page));
  if (opts?.limit) params.set("limit", String(opts.limit));
  if (opts?.category) params.set("category", opts.category);
  if (opts?.search) params.set("search", opts.search);
  const qs = params.toString();
  return apiFetch<{ articles: ApiArticle[]; total: number; page: number; totalPages: number }>(`/api/articles${qs ? `?${qs}` : ""}`, [ARTICLES_TAG]);
}

// The public API caps each page at 50 articles, so callers that need more
// (sitemaps) must paginate. Results arrive newest first; `stopWhen` lets
// callers stop paginating once articles are older than they need. Returns
// null when the first request fails so callers can distinguish "API down"
// from "no articles".
export async function getArticlesUpTo(
  maxCount: number,
  stopWhen?: (article: ApiArticle) => boolean,
): Promise<ApiArticle[] | null> {
  const pageSize = 50;
  const articles: ApiArticle[] = [];
  let page = 1;

  while (articles.length < maxCount) {
    const data = await getArticles({ page, limit: pageSize });
    if (!data) return page === 1 ? null : articles;
    if (!data.articles.length) break;
    articles.push(...data.articles);
    if (stopWhen && data.articles.some(stopWhen)) break;
    if (page >= (data.totalPages || 1)) break;
    page++;
  }

  return articles.slice(0, maxCount);
}

// A sitemap must not silently omit older articles when the public API has
// more than one page. Keep room for non-article URLs under the 50,000-URL
// sitemap limit; a larger archive will need partitioned sitemaps.
const MAX_ARTICLES_IN_SITEMAP = 49_000;

export async function getAllArticlesForSitemap(
  getPage: typeof getArticles = getArticles,
): Promise<ApiArticle[] | null> {
  const pageSize = 50;
  const articles: ApiArticle[] = [];
  const seenSlugs = new Set<string>();
  let expectedTotal: number | null = null;
  let expectedPages: number | null = null;

  for (let page = 1; expectedPages === null || page <= expectedPages; page++) {
    let data: Awaited<ReturnType<typeof getArticles>>;
    try {
      data = await getPage({ page, limit: pageSize });
    } catch {
      return null;
    }

    if (
      !data ||
      !Array.isArray(data.articles) ||
      !Number.isSafeInteger(data.total) ||
      !Number.isSafeInteger(data.page) ||
      !Number.isSafeInteger(data.totalPages) ||
      data.total < 0 ||
      data.total > MAX_ARTICLES_IN_SITEMAP ||
      data.page !== page ||
      data.totalPages !== Math.ceil(data.total / pageSize) ||
      (expectedTotal !== null && data.total !== expectedTotal) ||
      (expectedPages !== null && data.totalPages !== expectedPages)
    ) {
      return null;
    }

    expectedTotal = data.total;
    expectedPages = data.totalPages;

    const expectedCount = Math.min(pageSize, expectedTotal - articles.length);
    if (data.articles.length !== expectedCount) return null;

    for (const article of data.articles) {
      if (!article || typeof article.slug !== "string" || !article.slug || seenSlugs.has(article.slug)) {
        return null;
      }
      seenSlugs.add(article.slug);
      articles.push(article);
    }
  }

  return articles.length === expectedTotal ? articles : null;
}

// "24h" and "7d" rank by reads in that window, topped up with the newest
// stories; no window ranks by all-time views.
export type TrendingWindow = "24h" | "7d";

export interface ApiQuiz {
  number: number;
  day: string;
  questions: { question: string; options: string[]; answer: number; article: { slug: string; title: string } }[];
}

// The latest published daily quiz, or null when there is none yet.
export async function getTodayQuiz() {
  const data = await apiFetch<{ quiz: ApiQuiz }>("/api/quiz/today");
  return data?.quiz ?? null;
}

export async function getTrendingArticles(limit = 5, window?: TrendingWindow) {
  const windowParam = window ? `&window=${window}` : "";
  return apiFetch<{ articles: ApiArticle[] }>(`/api/articles/trending?limit=${limit}${windowParam}`, [ARTICLES_TAG]);
}

export type ArticleLookupResult =
  | { state: "found"; article: ApiArticle }
  | { state: "missing" }
  | { state: "unavailable" };

export async function getArticleLookup(
  slug: string,
  fetchImplementation: typeof fetch = fetch,
): Promise<ArticleLookupResult> {
  try {
    // Only 200 responses enter Next's data cache, so a 404 for a slug that
    // is published a moment later is not remembered here.
    const articleResponse = await fetchImplementation(`${API_URL}/api/articles/${encodeURIComponent(slug)}`, {
      next: { revalidate: ARTICLE_REVALIDATE_SECONDS, tags: [articleTag(slug)] },
    });

    if (articleResponse.ok) {
      const data = await articleResponse.json() as { article?: ApiArticle };
      return data.article ? { state: "found", article: data.article } : { state: "unavailable" };
    }

    if (articleResponse.status !== 404) return { state: "unavailable" };

    const healthResponse = await fetchImplementation(`${API_URL}/api/health`, {
      next: { revalidate: 30 },
    });
    return healthResponse.ok ? { state: "missing" } : { state: "unavailable" };
  } catch {
    return { state: "unavailable" };
  }
}

export async function getArticle(slug: string) {
  const result = await getArticleLookup(slug);
  return result.state === "found" ? { article: result.article } : null;
}

export async function getCategories() {
  return apiFetch<{ categories: ApiCategory[] }>("/api/categories");
}

export interface NewsletterEditionArticle {
  title: string;
  slug: string;
  excerpt: string;
  category: string;
  readingMinutes: number;
}

export interface NewsletterEdition {
  edition: string;
  subject: string;
  articles: NewsletterEditionArticle[];
  createdAt: string;
}

export async function getNewsletterEditions(limit = 30) {
  return apiFetch<{ editions: NewsletterEdition[] }>(`/api/newsletter/editions?limit=${limit}`);
}

export async function getNewsletterEdition(edition: string) {
  return apiFetch<{ edition: NewsletterEdition }>(`/api/newsletter/editions/${encodeURIComponent(edition)}`);
}

// All live topics, most covered first. "unavailable" covers an API without
// the topics endpoint yet, so callers can render nothing instead of failing.
export async function getTopics(fetchImplementation: typeof fetch = fetch): Promise<TopicSummary[] | null> {
  try {
    const res = await fetchImplementation(`${API_URL}/api/topics`, {
      next: { revalidate: LIST_REVALIDATE_SECONDS, tags: [TOPICS_TAG] },
    });
    if (!res.ok) return null;
    return parseTopicList(await res.json());
  } catch {
    return null;
  }
}

export type TopicLookupResult =
  | { state: "found"; topic: TopicDetail; articles: ApiArticle[] }
  | { state: "missing" }
  | { state: "unavailable" };

export async function getTopic(
  slug: string,
  fetchImplementation: typeof fetch = fetch,
): Promise<TopicLookupResult> {
  try {
    const res = await fetchImplementation(`${API_URL}/api/topics/${encodeURIComponent(slug)}`, {
      next: { revalidate: LIST_REVALIDATE_SECONDS, tags: [TOPICS_TAG, topicTag(slug)] },
    });
    if (res.ok) {
      const data = (await res.json()) as { topic?: unknown; articles?: unknown };
      const topic = parseTopicDetail(data.topic);
      if (!topic || !Array.isArray(data.articles)) return { state: "unavailable" };
      const articles = (data.articles as ApiArticle[]).filter((a) => a && typeof a.slug === "string" && a.slug);
      return { state: "found", topic, articles };
    }
    if (res.status !== 404) return { state: "unavailable" };
    const health = await fetchImplementation(`${API_URL}/api/health`, { next: { revalidate: 30 } });
    return health.ok ? { state: "missing" } : { state: "unavailable" };
  } catch {
    return { state: "unavailable" };
  }
}
