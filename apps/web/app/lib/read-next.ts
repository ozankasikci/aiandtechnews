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

// Where the mid-article read-next card goes: after paragraph n (0: nowhere).
// It sits just before the last section (above the last subheading, or two
// paragraphs from the end without one), so the story's opening reads
// uninterrupted and readers still meet it before they finish. It never goes
// above paragraph 3, never after a section's first paragraph (which would cut
// the heading off from its text), keeps at least two paragraphs between itself
// and an illustration, and needs two paragraphs after it, like
// splitAfterParagraph requires. When the ideal spot breaks a rule it takes the
// nearest one that doesn't.
export function readNextSlot(html: string): number {
  const blocks = html.match(/<p>|<h2>|<figure/g) ?? [];
  const paragraphs: { prev: string; next: string }[] = [];
  const figuresAfter: number[] = [];
  blocks.forEach((block, i) => {
    if (block === "<p>") paragraphs.push({ prev: blocks[i - 1] ?? "", next: blocks[i + 1] ?? "" });
    if (block === "<figure") figuresAfter.push(paragraphs.length);
  });
  const last = paragraphs.length - 2;
  let target = last;
  for (let n = last; n >= 1; n--) {
    if (paragraphs[n - 1]?.next === "<h2>") {
      target = n;
      break;
    }
  }
  let best = 0;
  let bestScore = Infinity;
  for (let n = 3; n <= last; n++) {
    const { prev, next } = paragraphs[n - 1];
    if (prev === "<h2>" || figuresAfter.some((f) => Math.abs(f - n) < 2)) continue;
    const score = Math.abs(n - target) + (next === "<h2>" ? 0 : 0.5);
    if (score < bestScore) {
      best = n;
      bestScore = score;
    }
  }
  return best;
}
