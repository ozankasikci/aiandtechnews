import { NextRequest, NextResponse } from "next/server";
import { readFileSync } from "fs";
import { join } from "path";

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

function loadArticles(): { articles: ArticleRecord[]; total: number } {
  try {
    // Try API first (works at build time / local)
    const data = JSON.parse(
      readFileSync(join(process.cwd(), "public", "articles-data.json"), "utf-8")
    );
    return data;
  } catch {
    return { articles: [], total: 0 };
  }
}

export async function GET(request: NextRequest) {
  const q = (request.nextUrl.searchParams.get("q") || "").toLowerCase().trim();
  const limit = parseInt(request.nextUrl.searchParams.get("limit") || "12");
  const page = parseInt(request.nextUrl.searchParams.get("page") || "1");

  const { articles: all } = loadArticles();

  // Filter
  const filtered = q
    ? all.filter(
        (a) =>
          a.title.toLowerCase().includes(q) ||
          a.excerpt.toLowerCase().includes(q) ||
          a.category.name.toLowerCase().includes(q)
      )
    : all;

  const total = filtered.length;
  const offset = (page - 1) * limit;
  const articles = filtered.slice(offset, offset + limit);

  return NextResponse.json({ articles, total, page, totalPages: Math.ceil(total / limit) });
}
