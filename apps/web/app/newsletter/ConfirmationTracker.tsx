"use client";

import { useEffect } from "react";
import { trackEvent } from "../lib/analytics";

export function ConfirmationTracker() {
  useEffect(() => {
    trackEvent("generate_lead", { method: "newsletter", status: "confirmed" });
  }, []);

  return null;
}
