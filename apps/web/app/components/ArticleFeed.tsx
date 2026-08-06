"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { ArticleImage } from "./ArticleImage";
import type { Article } from "../data/articles";
import { toIsoDate } from "../lib/dates";

function colorFromHex(color: string): string {
  const map: Record<string, string> = {
    "#8b5cf6": "bg-accent-purple", "#8B5CF6": "bg-accent-purple",
    "#3b82f6": "bg-accent-blue", "#3B82F6": "bg-accent-blue",
    "#10b981": "bg-accent-green", "#10B981": "bg-accent-green",
    "#ec4899": "bg-accent-magenta", "#EC4899": "bg-accent-magenta",
    "#f97316": "bg-accent-orange", "#F97316": "bg-accent-orange",
    "#f59e0b": "bg-accent-yellow", "#F59E0B": "bg-accent-yellow",
    "#22c55e": "bg-accent-green",
  };
  return map[color] || "bg-accent-purple";
}

function tagHex(tagColor: string): string {
  return tagColor
    .replace("bg-accent-purple", "#a855f7")
    .replace("bg-accent-blue", "#6366f1")
    .replace("bg-accent-green", "#22c55e")
    .replace("bg-accent-magenta", "#d946ef")
    .replace("bg-accent-orange", "#f97316")
    .replace("bg-accent-yellow", "#f59e0b");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function staticToArticle(a: any): Article {
  const tagColor = colorFromHex(a.category?.color || "");
  const published = a.published_at ? new Date(a.published_at) : new Date();
  const diffMs = Date.now() - published.getTime();
  const diffH = Math.floor(diffMs / 3600000);
  const time = diffH < 1 ? "Just now" : diffH < 24 ? `${diffH}h ago` : `${Math.floor(diffH / 24)}d ago`;

  return {
    id: a.id,
    slug: a.slug,
    headline: a.title,
    excerpt: a.excerpt || "",
    image: a.featured_image || "",
    tag: a.category?.name || "News",
    tagColor,
    date: published.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }),
    publishedAt: toIsoDate(a.published_at),
    updatedAt: toIsoDate(a.updated_at || a.published_at),
    readTime: "2 min read",
    time,
    body: "",
    author: a.author?.name || "TechNews Editorial",
    avatar: a.author?.avatar || "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=40&h=40&fit=crop",
    source: a.source || undefined,
    sourceUrl: a.source_url || undefined,
  };
}

interface Props {
  initialArticles: Article[];
  initialTotal: number;
}

export function ArticleFeed({ initialArticles, initialTotal }: Props) {
  const [articles, setArticles] = useState<Article[]>(initialArticles);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(initialArticles.length < initialTotal);
  const [page, setPage] = useState(1);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const loadingRef = useRef(false);

  useEffect(() => {
    if (!hasMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !loadingRef.current) {
          loadMore();
        }
      },
      { rootMargin: "300px" }
    );
    if (sentinelRef.current) observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [hasMore, page]);

  const loadMore = async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true);
    try {
      const nextPage = page + 1;
      const res = await fetch(`/api/search?page=${nextPage}&limit=12`);
      const data = await res.json();
      if (data.articles?.length) {
        const newArticles = data.articles.map(staticToArticle);
        setArticles((prev) => {
          const existingIds = new Set(prev.map((a) => a.id));
          const unique = newArticles.filter((a: Article) => !existingIds.has(a.id));
          return [...prev, ...unique];
        });
        setPage(nextPage);
        setHasMore(data.page < data.totalPages);
      } else {
        setHasMore(false);
      }
    } catch {
      setHasMore(false);
    } finally {
      setLoading(false);
      loadingRef.current = false;
    }
  };

  return (
    <div>
      {articles.map((a) => (
        <Link key={a.id} href={`/article/${a.slug}`}>
          <article className="flex gap-4 py-5 border-b border-border group" style={{ "--tag-color": tagHex(a.tagColor) } as React.CSSProperties}>
            <div className="flex-1 min-w-0">
              <span className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-2 ${a.tagColor}`}>
                {a.tag}
              </span>
              <h3 className="text-lg font-bold leading-snug mb-1.5 transition-colors story-title">
                {a.headline}
              </h3>
              <p className="text-text-secondary text-sm leading-relaxed mb-2 line-clamp-2">{a.excerpt}</p>
              <span className="text-text-muted text-xs">{a.time}</span>
            </div>
            <div className="w-[140px] h-[90px] md:w-[180px] md:h-[110px] relative rounded-sm overflow-hidden shrink-0">
              <ArticleImage src={a.image} alt="" fill className="object-cover" sizes="180px" />
            </div>
          </article>
        </Link>
      ))}

      <div ref={sentinelRef} className="py-2" />

      {loading && (
        <div className="flex justify-center py-6">
          <div className="w-5 h-5 border-2 border-accent-purple border-t-transparent rounded-full animate-spin" />
        </div>
      )}

      {!hasMore && articles.length > initialArticles.length && (
        <p className="text-center text-text-muted text-xs py-6 border-t border-border mt-4">You've reached the end</p>
      )}
    </div>
  );
}
