import { NextRequest, NextResponse } from "next/server";
import articlesData from "../../../public/articles-data.json";

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

export async function GET(request: NextRequest) {
  const q = (request.nextUrl.searchParams.get("q") || "").toLowerCase().trim();
  const limit = parseInt(request.nextUrl.searchParams.get("limit") || "12");
  const page = parseInt(request.nextUrl.searchParams.get("page") || "1");

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

  return NextResponse.json({
    articles,
    total: q ? total : totalArticles,
    page,
    totalPages: Math.ceil((q ? total : totalArticles) / limit),
  });
}
