"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import Image from "next/image";
import { sanitizeSearchTerm, trackEvent } from "../lib/analytics";

type Result = {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  featured_image: string;
  published_at: string;
  category: { name: string; slug: string; color: string };
  author: { name: string; avatar: string };
};

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

function timeAgo(dateStr: string): string {
  const diffH = Math.floor((Date.now() - new Date(dateStr).getTime()) / 3600000);
  if (diffH < 1) return "Just now";
  if (diffH < 24) return `${diffH}h ago`;
  return `${Math.floor(diffH / 24)}d ago`;
}

export default function SearchPage() {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Result[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [total, setTotal] = useState(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastTrackedQueryRef = useRef("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const q = params.get("q");
    if (q) {
      setQuery(q);
      doSearch(q);
    }
  }, []);

  const doSearch = async (q: string) => {
    if (!q.trim()) {
      setResults([]);
      setSearched(false);
      setTotal(0);
      return;
    }
    setLoading(true);
    setSearched(true);
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(q)}&limit=20`);
      const data = await res.json();
      setResults(data.articles || []);
      setTotal(data.total || 0);
      const searchTerm = sanitizeSearchTerm(q);
      if (searchTerm && searchTerm !== lastTrackedQueryRef.current) {
        lastTrackedQueryRef.current = searchTerm;
        trackEvent("search", { search_term: searchTerm, result_count: data.total || 0 });
      }
    } catch {
      setResults([]);
    } finally {
      setLoading(false);
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setQuery(val);
    const url = val ? `/search?q=${encodeURIComponent(val)}` : "/search";
    window.history.replaceState({}, "", url);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => doSearch(val), 300);
  };

  return (
    <div className="max-w-[900px] mx-auto px-4 lg:px-8 py-8">
      <div className="mb-10">
        <h1 className="text-3xl font-black tracking-tight mb-6">Search</h1>
        <div className="relative">
          <svg className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          {loading && (
            <div className="absolute right-4 top-1/2 -translate-y-1/2 w-4 h-4 border-2 border-accent-purple border-t-transparent rounded-full animate-spin" />
          )}
          <input
            type="text"
            value={query}
            onChange={handleChange}
            placeholder="Search TechNews..."
            autoFocus
            className="w-full bg-bg-card border border-border rounded-sm py-4 pl-12 pr-4 text-lg text-white placeholder:text-text-muted focus:outline-none focus:border-accent-purple transition-colors"
          />
        </div>
      </div>

      <div>
        {searched && (
          <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4 pb-2 border-b border-border">
            {loading ? "Searching..." : total > 0 ? `${total} result${total !== 1 ? "s" : ""} for "${query}"` : `No results for "${query}"`}
          </h2>
        )}

        {!searched && !loading && (
          <p className="text-text-muted text-sm mt-2">Start typing to search articles...</p>
        )}

        {results.map((a) => {
          const tagColor = colorFromHex(a.category?.color || "");
          return (
            <Link
              key={a.id}
              href={`/article/${a.slug}`}
              onClick={() => trackEvent("select_content", {
                content_type: "search_result",
                item_id: a.slug,
              })}
            >
              <article className="flex gap-4 py-5 border-b border-border group">
                <div className="flex-1 min-w-0">
                  <span className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-2 ${tagColor}`}>
                    {a.category?.name}
                  </span>
                  <h3 className="text-lg font-bold leading-snug mb-1.5 group-hover:text-accent-purple transition-colors">
                    {a.title}
                  </h3>
                  <p className="text-text-secondary text-sm leading-relaxed mb-2 line-clamp-2">{a.excerpt}</p>
                  <span className="text-text-muted text-xs">{timeAgo(a.published_at)}</span>
                </div>
                {a.featured_image && (
                  <div className="w-[140px] h-[90px] md:w-[180px] md:h-[110px] relative rounded-sm overflow-hidden shrink-0">
                    <Image src={a.featured_image} alt="" fill className="object-cover" sizes="180px" />
                  </div>
                )}
              </article>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
