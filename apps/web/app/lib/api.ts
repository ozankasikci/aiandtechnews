import type { Article } from "../data/articles";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4000";

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
    "#3B82F6": "bg-accent-blue",
    "#10B981": "bg-accent-green",
    "#EC4899": "bg-accent-magenta",
    purple: "bg-accent-purple",
    blue: "bg-accent-blue",
    green: "bg-accent-green",
    magenta: "bg-accent-magenta",
  };
  return colorMap[color] || "bg-accent-purple";
}

function timeAgo(dateStr: string): string {
  // DB stores UTC without Z suffix — append Z to parse correctly
  const utcStr = dateStr.includes("Z") || dateStr.includes("+") ? dateStr : dateStr + "Z";
  const diff = Date.now() - new Date(utcStr).getTime();
  if (diff < 0) return "Just now";
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(utcStr).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

export function mapArticle(a: ApiArticle): Article {
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
    date: new Date(a.published_at || a.created_at).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }),
    readTime: `${Math.max(2, Math.ceil((a.content?.length || 0) / 1000))} min read`,
    image: a.featured_image || "https://images.unsplash.com/photo-1518770660439-4636190af475?w=800&h=500&fit=crop",
    imageSource: (a as any).source || undefined,
    sourceUrl: (a as any).source_url || undefined,
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

export async function getTrendingArticles(limit = 5) {
  return apiFetch<{ articles: ApiArticle[] }>(`/api/articles/trending?limit=${limit}`);
}

export async function getArticle(slug: string) {
  return apiFetch<{ article: ApiArticle }>(`/api/articles/${slug}`);
}

export async function getCategories() {
  return apiFetch<{ categories: ApiCategory[] }>("/api/categories");
}
