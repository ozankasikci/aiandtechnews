export function parseApiDate(value: string | null | undefined): Date | null {
  if (!value) return null;

  const trimmed = value.trim();
  const withTimeSeparator = trimmed.includes("T") ? trimmed : trimmed.replace(" ", "T");
  const hasTimezone = /(?:Z|[+-]\d{2}:?\d{2})$/i.test(withTimeSeparator);
  const parsed = new Date(hasTimezone ? withTimeSeparator : `${withTimeSeparator}Z`);

  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

export function toIsoDate(value: string | null | undefined): string | undefined {
  return parseApiDate(value)?.toISOString();
}

export function toAbsoluteUrl(value: string | null | undefined, baseUrl: string): string | undefined {
  if (!value) return undefined;

  try {
    return new URL(value, baseUrl).toString();
  } catch {
    return undefined;
  }
}
