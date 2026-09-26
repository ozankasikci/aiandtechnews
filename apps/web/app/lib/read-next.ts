// Picks the stories offered after an article: a card halfway through the body
// and a "Keep reading" row at the end. Without topic tags, relatedness is the
// number of meaningful words two headlines share.

type Story = { slug: string; headline: string; tag: string; publishedAt?: string };

const STOPWORDS = new Set([
  "about", "after", "again", "against", "amid", "and", "are", "can", "could", "for", "from", "has", "have",
  "how", "into", "its", "more", "new", "now", "over", "says", "than", "that", "the", "their", "this", "top",
  "was", "were", "what", "when", "why", "will", "with", "without",
]);

const MAX_AGE_MS = 7 * 24 * 3_600_000;

// Lowercase headline words of three or more characters, minus common words.
export function titleTerms(headline: string): string[] {
  return (headline.toLowerCase().match(/[a-z0-9][a-z0-9.-]*[a-z0-9]|[a-z0-9]/g) ?? []).filter(
    (word) => word.length >= 3 && !STOPWORDS.has(word),
  );
}

function publishedTime(story: Story): number {
  const time = story.publishedAt ? Date.parse(story.publishedAt) : NaN;
  return Number.isNaN(time) ? 0 : time;
}

// Up to `count` stories from the last week: those sharing the most headline
// terms first, then the newest in the same section, then the newest overall.
export function pickReadNext<T extends Story>(current: Story, candidates: T[], { now = new Date(), count = 4 } = {}): T[] {
  const terms = new Set(titleTerms(current.headline));
  const recent = candidates
    .filter((story) => story.slug !== current.slug && now.getTime() - publishedTime(story) <= MAX_AGE_MS)
    .sort((a, b) => publishedTime(b) - publishedTime(a));
  const shared = (story: T) => new Set(titleTerms(story.headline).filter((term) => terms.has(term))).size;

  const matching = recent
    .map((story) => ({ story, score: shared(story) }))
    .filter(({ score }) => score > 0)
    .sort((a, b) => b.score - a.score || publishedTime(b.story) - publishedTime(a.story))
    .map(({ story }) => story);
  const sameSection = recent.filter((story) => story.tag === current.tag);

  const picks: T[] = [];
  for (const story of [...matching, ...sameSection, ...recent]) {
    if (picks.length === count) break;
    if (!picks.includes(story)) picks.push(story);
  }
  return picks;
}

// Splits article HTML after its nth paragraph, but only when at least two
// more paragraphs follow; otherwise the whole body comes back with "".
export function splitAfterParagraph(html: string, n: number): [string, string] {
  let end = 0;
  for (let i = 0; i < n; i++) {
    const close = html.indexOf("</p>", end);
    if (close < 0) return [html, ""];
    end = close + "</p>".length;
  }
  const rest = html.slice(end);
  if ((rest.match(/<\/p>/g) ?? []).length < 2) return [html, ""];
  return [html.slice(0, end), rest];
}

// Picks the reader has not opened yet, in order, then the ones they have. With
// everything already read the original order comes back unchanged.
export function unseenFirst<T extends { slug: string }>(picks: T[], visited: string[]): T[] {
  const seen = new Set(visited);
  return [...picks.filter((p) => !seen.has(p.slug)), ...picks.filter((p) => seen.has(p.slug))];
}
