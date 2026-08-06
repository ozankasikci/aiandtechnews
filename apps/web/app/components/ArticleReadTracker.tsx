"use client";

import { useEffect } from "react";
import { trackEvent } from "../lib/analytics";

interface ArticleReadTrackerProps {
  bodyId: string;
  slug: string;
  category: string;
}

// An article whose body fits in the viewport meets the 75% position on mount,
// so require a minimum dwell time before counting it as read.
const MIN_DWELL_MS = 10_000;

export function ArticleReadTracker({ bodyId, slug, category }: ArticleReadTrackerProps) {
  useEffect(() => {
    let tracked = false;
    const start = Date.now();

    const checkProgress = () => {
      if (tracked) return;
      if (Date.now() - start < MIN_DWELL_MS) return;
      const body = document.getElementById(bodyId);
      if (!body) return;

      const bodyTop = body.getBoundingClientRect().top + window.scrollY;
      const targetPosition = bodyTop + body.offsetHeight * 0.75;
      if (window.scrollY + window.innerHeight < targetPosition) return;

      tracked = true;
      trackEvent("article_read", {
        content_type: "article",
        item_id: slug,
        category,
        percent_scrolled: 75,
      });
      window.removeEventListener("scroll", checkProgress);
      window.removeEventListener("resize", checkProgress);
    };

    window.addEventListener("scroll", checkProgress, { passive: true });
    window.addEventListener("resize", checkProgress);
    // Covers short articles that need no scrolling: check again once the
    // dwell threshold has passed.
    const dwellTimer = window.setTimeout(checkProgress, MIN_DWELL_MS);

    return () => {
      window.clearTimeout(dwellTimer);
      window.removeEventListener("scroll", checkProgress);
      window.removeEventListener("resize", checkProgress);
    };
  }, [bodyId, category, slug]);

  return null;
}
