"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, Suspense } from "react";
import { sanitizePagePath } from "../lib/analytics";

function AnalyticsInner() {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  useEffect(() => {
    if (typeof window.gtag === "function") {
      // Query values are sanitized so search terms containing emails or URLs
      // never reach analytics via page_location.
      const pagePath = sanitizePagePath(pathname, searchParams);
      window.gtag("event", "page_view", {
        page_location: `${window.location.origin}${pagePath}`,
        page_title: document.title,
      });
    }
  }, [pathname, searchParams]);

  return null;
}

export function Analytics() {
  return (
    <Suspense fallback={null}>
      <AnalyticsInner />
    </Suspense>
  );
}
