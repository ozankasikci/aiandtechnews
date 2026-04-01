"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import Image from "next/image";
import { mapArticle } from "../lib/api";

type Result = {
  id: number;
  slug: string;
  headline: string;
  excerpt: string;
  image: string;
  tag: string;
  tagColor: string;
  time: string;
};

export default function SearchPage() {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Result[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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
      return;
    }
    setLoading(true);
    setSearched(true);
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(q)}&limit=20`);
      const data = await res.json();
      const articles = data?.articles?.length ? data.articles.map(mapArticle) : [];
      setResults(articles);
    } catch {
      setResults([]);
    } finally {
      setLoading(false);
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setQuery(val);
    // Update URL without reload
    const url = val ? `/search?q=${encodeURIComponent(val)}` : "/search";
    window.history.replaceState({}, "", url);
    // Debounce 300ms
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
            {loading ? "Searching..." : results.length > 0 ? `${results.length} result${results.length !== 1 ? "s" : ""} for "${query}"` : `No results for "${query}"`}
          </h2>
        )}

        {!searched && !loading && (
          <p className="text-text-muted text-sm mt-2">Start typing to search articles...</p>
        )}

        {results.map((a) => (
          <Link key={a.id} href={`/article/${a.slug}`}>
            <article className="flex gap-4 py-5 border-b border-border group" style={{ "--tag-color": a.tagColor.replace("bg-accent-purple","#a855f7").replace("bg-accent-blue","#6366f1").replace("bg-accent-green","#22c55e").replace("bg-accent-magenta","#d946ef").replace("bg-accent-orange","#f97316").replace("bg-accent-yellow","#f59e0b") } as React.CSSProperties}>
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
              {a.image && (
                <div className="w-[140px] h-[90px] md:w-[180px] md:h-[110px] relative rounded-sm overflow-hidden shrink-0">
                  <Image src={a.image} alt="" fill className="object-cover" sizes="180px" />
                </div>
              )}
            </article>
          </Link>
        ))}
      </div>
    </div>
  );
}
