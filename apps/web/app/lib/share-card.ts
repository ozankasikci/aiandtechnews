// The homepage share card: a 1200×630 image of the latest stories, served by
// app/og/today/route.tsx. The URL carries the UTC date so chat apps and social
// sites, which cache previews by URL, fetch a fresh card each day.

export const SHARE_CARD_PATH = "/og/today";
export const SHARE_CARD_SIZE = { width: 1200, height: 630 };

export function shareCardUrl(now: Date): string {
  return `${SHARE_CARD_PATH}?d=${now.toISOString().slice(0, 10)}`;
}

export function shareCardDateLabel(now: Date): string {
  return now.toLocaleDateString("en-US", { weekday: "long", month: "long", day: "numeric", timeZone: "UTC" });
}

// Headlines past the lead story, at most `limit`, for the card's short list.
export function shareCardMoreHeadlines(titles: string[], limit = 3): string[] {
  return titles.slice(1, 1 + limit).filter((title) => title.trim() !== "");
}
