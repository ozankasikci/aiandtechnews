"use client";

import { useEffect } from "react";
import { trackEvent } from "../lib/analytics";

export function ConfirmationTracker() {
  useEffect(() => {
    const key = "aiandtech-newsletter-confirmation-tracked";
    if (window.sessionStorage.getItem(key)) return;
    window.sessionStorage.setItem(key, "true");
    trackEvent("generate_lead", { method: "newsletter", status: "confirmed" });
  }, []);

  return null;
}
