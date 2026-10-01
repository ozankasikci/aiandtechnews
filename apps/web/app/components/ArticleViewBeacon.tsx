"use client";

import { useEffect } from "react";
import { sendArticleView } from "../lib/view-beacon";

// Reports one view per page load to the API (which also ignores bots and
// repeat views), since the cached article page no longer reaches the API
// once per reader.
const sent = new Set<string>();

export function ArticleViewBeacon({ apiBase, slug }: { apiBase: string; slug: string }) {
  useEffect(() => {
    if (sent.has(slug)) return;
    sent.add(slug);
    sendArticleView(apiBase, slug, {
      webdriver: navigator.webdriver,
      userAgent: navigator.userAgent,
      sendBeacon: typeof navigator.sendBeacon === "function" ? navigator.sendBeacon.bind(navigator) : undefined,
      fetch: typeof window.fetch === "function" ? window.fetch.bind(window) : undefined,
    });
  }, [apiBase, slug]);
  return null;
}
