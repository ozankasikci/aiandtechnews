"use client";

import { useState, useEffect, useRef } from "react";
import { StoryCard } from "./StoryCard";
import { mapArticle } from "../lib/api";
import type { Article } from "../data/articles";

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

  useEffect(() => {
    if (!hasMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !loading) {
          loadMore();
        }
      },
      { rootMargin: "200px" }
    );
    if (sentinelRef.current) observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [hasMore, loading, page]);

  const loadMore = async () => {
    setLoading(true);
    try {
      const nextPage = page + 1;
      const res = await fetch(`/api/search?page=${nextPage}&limit=12`);
      const data = await res.json();
      if (data.articles?.length) {
        const newArticles = data.articles.map(mapArticle);
        setArticles((prev) => [...prev, ...newArticles]);
        setPage(nextPage);
        setHasMore(articles.length + newArticles.length < data.total);
      } else {
        setHasMore(false);
      }
    } catch {
      setHasMore(false);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {articles.map((a) => (
        <StoryCard key={a.id} story={a} />
      ))}

      {/* Infinite scroll sentinel */}
      <div ref={sentinelRef} className="py-2" />

      {loading && (
        <div className="flex justify-center py-6">
          <div className="w-5 h-5 border-2 border-accent-purple border-t-transparent rounded-full animate-spin" />
        </div>
      )}

      {!hasMore && articles.length > 0 && (
        <p className="text-center text-text-muted text-xs py-6 border-t border-border mt-4">You've reached the end</p>
      )}
    </div>
  );
}
