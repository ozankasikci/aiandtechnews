"use client";

import { useEffect } from "react";
import { trackEvent } from "../lib/analytics";

interface ArticleReadTrackerProps {
  bodyId: string;
  slug: string;
  category: string;
}

export function ArticleReadTracker({ bodyId, slug, category }: ArticleReadTrackerProps) {
  useEffect(() => {
    let tracked = false;

    const checkProgress = () => {
      if (tracked) return;
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
    checkProgress();

    return () => {
      window.removeEventListener("scroll", checkProgress);
      window.removeEventListener("resize", checkProgress);
    };
  }, [bodyId, category, slug]);

  return null;
}
