"use client";

import { useEffect, useRef } from "react";
import { ADSENSE_CLIENT } from "../lib/adsense";

// One responsive AdSense unit. The block (label included) stays hidden until
// AdSense marks it filled, so an empty or unapproved slot leaves no gap (see
// .ad-slot in globals.css).
export function AdSlot({ slot }: { slot: string }) {
  const requested = useRef(false);

  useEffect(() => {
    if (requested.current) return;
    requested.current = true;
    try {
      const w = window as unknown as { adsbygoogle?: unknown[] };
      (w.adsbygoogle = w.adsbygoogle || []).push({});
    } catch {
      // An ad blocker or a failed script load: show nothing.
    }
  }, []);

  return (
    <div className="ad-slot" aria-label="Advertisement">
      <p className="text-[10px] uppercase tracking-widest text-text-muted mb-2">Advertisement</p>
      <ins
        className="adsbygoogle"
        style={{ display: "block" }}
        data-ad-client={ADSENSE_CLIENT}
        data-ad-slot={slot}
        data-ad-format="auto"
        data-full-width-responsive="true"
      />
    </div>
  );
}
