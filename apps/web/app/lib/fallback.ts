import snapshot from "../../public/articles-data.json";
import type { Article } from "../data/articles";
import { parseApiDate, toIsoDate } from "./dates";

// A build-time snapshot of published articles, used when the API is
// unreachable. It carries no article bodies, so it can back listings
// (homepage, categories, related stories) but never an article page.
interface SnapshotArticle {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  featured_image: string;
  published_at: string;
  category?: { name: string; slug: string; color: string };
  author?: { name: string; avatar: string };
}

const HEX_TO_CLASS: Record<string, string> = {
  "#8b5cf6": "bg-accent-purple",
  "#3b82f6": "bg-accent-blue",
  "#10b981": "bg-accent-green",
  "#22c55e": "bg-accent-green",
  "#ec4899": "bg-accent-magenta",
  "#f97316": "bg-accent-orange",
  "#f59e0b": "bg-accent-yellow",
};

const DEFAULT_AVATAR = "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=40&h=40&fit=crop&crop=face";

export function classFromHex(color: string | undefined): string {
  return HEX_TO_CLASS[(color || "").toLowerCase()] || "bg-accent-purple";
}

function relativeTime(published: Date, now: Date): string {
  const diffMs = now.getTime() - published.getTime();
  if (diffMs < 0) return "Just now";
  const hours = Math.floor(diffMs / 3_600_000);
  if (hours < 1) return "Just now";
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function snapshotToArticle(entry: SnapshotArticle, now = new Date()): Article {
  const published = parseApiDate(entry.published_at) || now;

  return {
    id: entry.id,
    slug: entry.slug,
    tag: entry.category?.name || "News",
    tagColor: classFromHex(entry.category?.color),
    headline: entry.title,
    excerpt: entry.excerpt || "",
    author: entry.author?.name || "TechNews Editorial",
    avatar: entry.author?.avatar || DEFAULT_AVATAR,
    time: relativeTime(published, now),
    date: published.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }),
    publishedAt: toIsoDate(entry.published_at),
    updatedAt: toIsoDate(entry.published_at),
    readTime: "1 min read",
    image: entry.featured_image || "",
  };
}

const SNAPSHOT: SnapshotArticle[] = (snapshot as { articles?: SnapshotArticle[] }).articles || [];

export function fallbackArticles(limit?: number, now = new Date()): Article[] {
  const mapped = SNAPSHOT.map((entry) => snapshotToArticle(entry, now));
  return typeof limit === "number" ? mapped.slice(0, limit) : mapped;
}

export function fallbackArticlesByCategory(categoryName: string, limit?: number, now = new Date()): Article[] {
  const wanted = categoryName.toLowerCase();
  const matches = SNAPSHOT.filter(
    (entry) =>
      entry.category?.slug?.toLowerCase() === wanted || entry.category?.name?.toLowerCase() === wanted,
  ).map((entry) => snapshotToArticle(entry, now));
  return typeof limit === "number" ? matches.slice(0, limit) : matches;
}
