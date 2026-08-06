"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { trackEvent, type AnalyticsParameters } from "../lib/analytics";

interface AnalyticsLinkProps {
  href: string;
  eventName: string;
  eventParameters?: AnalyticsParameters;
  children: ReactNode;
  className?: string;
  target?: "_blank" | "_self";
  rel?: string;
}

export function AnalyticsLink({
  href,
  eventName,
  eventParameters,
  children,
  className,
  target,
  rel,
}: AnalyticsLinkProps) {
  return (
    <Link
      href={href}
      className={className}
      target={target}
      rel={rel}
      onClick={() => trackEvent(eventName, eventParameters)}
    >
      {children}
    </Link>
  );
}
