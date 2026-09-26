"use client";

import Link from "next/link";
import { useState } from "react";

export interface TrendingItem {
  title: string;
  slug: string;
  time: string;
}

const TABS = [
  { key: "today", label: "Today" },
  { key: "week", label: "This week" },
] as const;

export function TrendingTabs({ heading, today, week }: { heading: string; today: TrendingItem[]; week: TrendingItem[] }) {
  const [tab, setTab] = useState<(typeof TABS)[number]["key"]>(today.length ? "today" : "week");
  const items = tab === "today" ? today : week;

  return (
    <>
      <div className="flex items-center justify-between mb-4 pb-2 border-b border-border">
        <h2 className="text-[10px] font-bold uppercase tracking-widest text-text-muted">{heading}</h2>
        <div role="tablist" aria-label={`${heading} period`} className="flex gap-3">
          {TABS.map(({ key, label }) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={tab === key}
              onClick={() => setTab(key)}
              className={`text-[10px] font-bold uppercase tracking-widest pb-0.5 border-b-2 transition-colors ${
                tab === key ? "text-white border-accent-purple" : "text-text-muted border-transparent hover:text-white"
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <ol className="space-y-4" role="tabpanel">
        {items.map((item, i) => (
          <li key={item.slug}>
            <Link href={`/article/${item.slug}`} className="flex gap-3 group cursor-pointer">
              <span className="text-2xl font-black text-text-muted/50 leading-none shrink-0 w-7 text-right">{i + 1}</span>
              <span>
                <span className="block text-sm font-semibold leading-snug group-hover:text-accent-purple transition-colors">{item.title}</span>
                <span className="text-text-muted text-xs">{item.time}</span>
              </span>
            </Link>
          </li>
        ))}
      </ol>
    </>
  );
}
