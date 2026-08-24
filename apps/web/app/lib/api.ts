import type { Article } from "../data/articles";
import { parseApiDate, toIsoDate } from "./dates";

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
  source?: string;
  source_url?: string;
  meta_title?: string | null;
  meta_description?: string | null;
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
    readTime: `${Math.max(2, Math.ceil((a.content?.length || 0) / 1000))} min read`,
    image: a.featured_image || "https://images.unsplash.com/photo-1518770660439-4636190af475?w=800&h=500&fit=crop",
    source: a.source || undefined,
    sourceUrl: a.source_url || undefined,
    metaTitle: a.meta_title || undefined,
    metaDescription: a.meta_description || undefined,
    body: a.content,
  };
}

async function apiFetch<T>(path: string): Promise<T | null> {
  try {
    const res = await fetch(`${API_URL}${path}`, { next: { revalidate: 60 } });
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
  return apiFetch<{ articles: ApiArticle[]; total: number; page: number; totalPages: number }>(`/api/articles${qs ? `?${qs}` : ""}`);
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

export async function getTrendingArticles(limit = 5) {
  return apiFetch<{ articles: ApiArticle[] }>(`/api/articles/trending?limit=${limit}`);
}

export async function getArticle(slug: string) {
  return apiFetch<{ article: ApiArticle }>(`/api/articles/${slug}`);
}

export async function getCategories() {
  return apiFetch<{ categories: ApiCategory[] }>("/api/categories");
}
