"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { trackEvent } from "../lib/analytics";

export interface GlossaryPopoverTerm {
  slug: string;
  term: string;
  definition: string;
}

const WIDTH = 320;

// Glossary terms in the article body are ordinary links to their glossary
// page. With JavaScript, a click opens a short definition next to the word
// instead; the page stays one tap away.
export function GlossaryPopover({ terms }: { terms: GlossaryPopoverTerm[] }) {
  const [open, setOpen] = useState<{ term: GlossaryPopoverTerm; top: number; left: number } | null>(null);
  const card = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const bySlug = new Map(terms.map((t) => [t.slug, t]));
    const onClick = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (card.current?.contains(target)) return;
      const link = target?.closest<HTMLAnchorElement>("a[data-glossary]");
      const term = link && bySlug.get(link.dataset.glossary ?? "");
      if (!link || !term) return setOpen(null);
      event.preventDefault();
      const rect = link.getBoundingClientRect();
      const width = Math.min(WIDTH, window.innerWidth - 32);
      setOpen({
        term,
        top: rect.bottom + window.scrollY + 8,
        left: Math.max(16, Math.min(rect.left + window.scrollX, window.scrollX + window.innerWidth - width - 16)),
      });
      trackEvent("glossary_open", { item_id: term.slug });
    };
    const onKey = (event: KeyboardEvent) => event.key === "Escape" && setOpen(null);
    document.addEventListener("click", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("click", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [terms]);

  // Rendered at the end of <body> so the page coordinates above place it
  // right under the word, whatever positioned container the article sits in.
  if (!open) return null;
  return createPortal(
    <div
      ref={card}
      role="dialog"
      aria-label={open.term.term}
      className="absolute z-40 bg-bg-card border border-border rounded-sm shadow-2xl p-4"
      style={{ top: open.top, left: open.left, width: `min(${WIDTH}px, calc(100vw - 32px))` }}
    >
      <p className="text-white font-bold text-[15px] mb-1">{open.term.term}</p>
      <p className="text-[#e5e5e5] text-sm leading-relaxed">{open.term.definition}</p>
      <Link href={`/glossary/${open.term.slug}`} className="inline-block mt-3 text-sm font-semibold text-accent-purple hover:underline">
        Stories about {open.term.term} →
      </Link>
    </div>,
    document.body,
  );
}
