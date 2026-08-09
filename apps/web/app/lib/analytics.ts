export type AnalyticsParameter = string | number | boolean | undefined;
export type AnalyticsParameters = Record<string, AnalyticsParameter>;

export function sanitizeSearchTerm(value: string): string {
  const normalized = value.trim().replace(/\s+/g, " ").slice(0, 100);
  if (!normalized) return "";
  if (/@|https?:\/\/|www\./i.test(normalized)) return "[redacted]";
  return normalized;
}

export function sanitizePagePath(pathname: string, searchParams: URLSearchParams): string {
  const params = new URLSearchParams();

  for (const [key, value] of searchParams.entries()) {
    const sanitized = sanitizeSearchTerm(value);
    if (sanitized) params.set(key, sanitized);
  }

  const query = params.toString();
  return query ? `${pathname}?${query}` : pathname;
}

export function trackEvent(name: string, parameters: AnalyticsParameters = {}): void {
  if (typeof window === "undefined" || typeof window.gtag !== "function") return;

  const definedParameters = Object.fromEntries(
    Object.entries(parameters).filter(([, value]) => value !== undefined),
  );

  window.gtag("event", name, definedParameters);
}
