"use client";

import Image from "next/image";
import Link from "next/link";
import { useEffect, useState } from "react";
import { AnalyticsLink } from "./AnalyticsLink";
import { unseenFirst } from "../lib/read-next";
import { readVisited, recordVisit } from "../lib/visited";

export interface ReadNextStory {
  slug: string;
  headline: string;
  image: string;
  tag: string;
  tagColor: string;
}

// The server renders the picks in relevance order. After mounting, stories the
// reader has already opened move to the back, so two closely related articles
// don't keep pointing at each other. "mid" shows the first pick after the third
// paragraph; "row" shows the next three (or the first three without a card).
export function ReadNext({
  placement,
  current,
  picks,
  withMidCard,
}: {
  placement: "mid" | "row";
  current: string;
  picks: ReadNextStory[];
  withMidCard: boolean;
}) {
  const [ordered, setOrdered] = useState(picks);
  useEffect(() => {
    setOrdered(unseenFirst(picks, readVisited()));
    recordVisit(current);
  }, [picks, current]);

  if (placement === "mid") {
    const story = ordered[0];
    if (!story) return null;
    return (
      <AnalyticsLink
        href={`/article/${story.slug}`}
        eventName="select_content"
        eventParameters={{ content_type: "related_article", item_id: story.slug, source_article: current, link_position: "mid_article" }}
        className="group grid grid-cols-[96px_1fr] sm:grid-cols-[120px_1fr] gap-4 items-center bg-bg-card border border-border rounded-sm p-3 mb-6 no-underline"
      >
        <span className="relative block h-[64px] sm:h-[76px] rounded-sm overflow-hidden">
          <Image src={story.image} alt="" fill className="object-cover" sizes="120px" />
        </span>
        <span>
          <span className="block text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-1">Read next</span>
          <span className="block text-base font-bold leading-snug text-white group-hover:text-accent-purple transition-colors">{story.headline}</span>
        </span>
      </AnalyticsLink>
    );
  }

  const row = ordered.slice(withMidCard ? 1 : 0, withMidCard ? 4 : 3);
  if (!row.length) return null;
  return (
    <section className="mt-12 pt-8 border-t border-border">
      <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6">Keep reading</h2>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {row.map((r) => (
          <div key={r.slug} className="group">
            <AnalyticsLink
              href={`/article/${r.slug}`}
              eventName="select_content"
              eventParameters={{ content_type: "related_article", item_id: r.slug, source_article: current, link_position: "image" }}
            >
              <div className="relative h-[160px] rounded-sm overflow-hidden mb-3">
                <Image src={r.image} alt="" fill className="object-cover group-hover:scale-105 transition-transform" sizes="300px" />
              </div>
            </AnalyticsLink>
            <Link href={`/${r.tag.toLowerCase()}`} className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-1 hover:opacity-80 transition-opacity ${r.tagColor}`}>
              {r.tag}
            </Link>
            <AnalyticsLink
              href={`/article/${r.slug}`}
              eventName="select_content"
              eventParameters={{ content_type: "related_article", item_id: r.slug, source_article: current, link_position: "headline" }}
            >
              <h3 className="text-sm font-bold leading-snug group-hover:text-accent-purple transition-colors">{r.headline}</h3>
            </AnalyticsLink>
          </div>
        ))}
      </div>
    </section>
  );
}
