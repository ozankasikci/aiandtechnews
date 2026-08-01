import { NextRequest, NextResponse } from "next/server";
import articlesData from "../../../public/articles-data.json";

function getApiBase() {
  const configured = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim();
  if (process.env.NODE_ENV === "production") {
    if (!configured || configured.includes("localhost") || configured.includes("127.0.0.1")) {
      return "https://technews.subtunnel.dev";
    }
  }
  return configured || "http://localhost:4001";
}

const API_BASE = getApiBase();

export const dynamic = "force-dynamic";

interface ArticleRecord {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  featured_image: string;
  published_at: string;
  category: { name: string; slug: string; color: string };
  author: { name: string; avatar: string };
}

const allArticles: ArticleRecord[] = (articlesData as { articles: ArticleRecord[] }).articles || [];
const totalArticles: number = (articlesData as { total: number }).total || allArticles.length;

function fallbackSearch(q: string, limit: number, page: number) {
  const filtered = q
    ? allArticles.filter(
        (a) =>
          a.title.toLowerCase().includes(q) ||
          a.excerpt.toLowerCase().includes(q) ||
          a.category?.name?.toLowerCase().includes(q)
      )
    : allArticles;

  const total = filtered.length;
  const offset = (page - 1) * limit;
  const articles = filtered.slice(offset, offset + limit);

  return {
    articles,
    total: q ? total : totalArticles,
    page,
    totalPages: Math.ceil((q ? total : totalArticles) / limit),
    source: "static-fallback",
  };
}

export async function GET(request: NextRequest) {
  const q = (request.nextUrl.searchParams.get("q") || "").toLowerCase().trim();
  const limit = parseInt(request.nextUrl.searchParams.get("limit") || "12");
  const page = parseInt(request.nextUrl.searchParams.get("page") || "1");

  const params = new URLSearchParams({ limit: String(limit), page: String(page) });
  if (q) params.set("search", q);

  try {
    const res = await fetch(`${API_BASE}/api/articles?${params}`, { cache: "no-store" });
    if (res.ok) {
      const data = await res.json();
      return NextResponse.json({ ...data, source: "api" });
    }
  } catch {
    // Fall through to the static snapshot so search/infinite scroll degrade gracefully.
  }

  return NextResponse.json(fallbackSearch(q, limit, page));
}
