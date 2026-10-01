import { parseApiDate } from "./dates";

// Topic hubs: one page per company, product, person or theme, collecting our
// coverage under a maintained summary. Pure helpers only (parsing, grouping,
// in-body linking); fetching lives in api.ts.

export const TOPICS_TAG = "topics";
export function topicTag(slug: string) {
  return `topic:${slug}`;
}

export const TOPIC_KINDS = ["company", "product", "person", "theme"] as const;
export type TopicKind = (typeof TOPIC_KINDS)[number];

export const TOPIC_KIND_LABELS: Record<TopicKind, string> = {
  company: "Company",
  product: "Product",
  person: "Person",
  theme: "Theme",
};
export const TOPIC_KIND_PLURALS: Record<TopicKind, string> = {
  company: "Companies",
  product: "Products",
  person: "People",
  theme: "Themes",
};

export interface TopicRef {
  slug: string;
  name: string;
}

export interface TopicSummary extends TopicRef {
  kind: TopicKind;
  articleCount: number;
  updatedAt?: string;
}

export interface TopicDetail extends TopicSummary {
  summary: string;
  facts: string[];
  related: TopicRef[];
}

export const MAX_ARTICLE_TOPICS = 4;
export const MAX_BODY_TOPIC_LINKS = 3;

const TOPIC_SLUG = /^[a-z0-9][a-z0-9-]{0,199}$/;

export function isTopicSlug(value: unknown): value is string {
  return typeof value === "string" && TOPIC_SLUG.test(value);
}

function isKind(value: unknown): value is TopicKind {
  return typeof value === "string" && (TOPIC_KINDS as readonly string[]).includes(value);
}

function cleanText(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed ? trimmed : null;
}

export function parseTopicRef(raw: unknown): TopicRef | null {
  if (!raw || typeof raw !== "object") return null;
  const { slug, name } = raw as { slug?: unknown; name?: unknown };
  const cleanName = cleanText(name);
  if (!isTopicSlug(slug) || !cleanName) return null;
  return { slug, name: cleanName };
}

// Topic refs on an article: malformed entries and duplicates are dropped and
// the list is capped, so a bad API payload never breaks the page.
export function parseTopicRefs(raw: unknown, max = MAX_ARTICLE_TOPICS): TopicRef[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  const out: TopicRef[] = [];
  for (const item of raw) {
    const ref = parseTopicRef(item);
    if (!ref || seen.has(ref.slug)) continue;
    seen.add(ref.slug);
    out.push(ref);
    if (out.length >= max) break;
  }
  return out;
}

export function parseTopicSummary(raw: unknown): TopicSummary | null {
  const ref = parseTopicRef(raw);
  if (!ref) return null;
  const { kind, articleCount, updatedAt } = raw as { kind?: unknown; articleCount?: unknown; updatedAt?: unknown };
  if (!isKind(kind)) return null;
  const count = typeof articleCount === "number" && Number.isFinite(articleCount) && articleCount >= 0 ? Math.floor(articleCount) : 0;
  return { ...ref, kind, articleCount: count, updatedAt: typeof updatedAt === "string" ? updatedAt : undefined };
}

export function parseTopicList(raw: unknown): TopicSummary[] | null {
  if (!raw || typeof raw !== "object") return null;
  const topics = (raw as { topics?: unknown }).topics;
  if (!Array.isArray(topics)) return null;
  return topics.map(parseTopicSummary).filter((t): t is TopicSummary => t !== null);
}

export function parseTopicDetail(raw: unknown): TopicDetail | null {
  const base = parseTopicSummary(raw);
  if (!base) return null;
  const { summary, facts, related } = raw as { summary?: unknown; facts?: unknown; related?: unknown };
  const summaryText = cleanText(summary);
  if (!summaryText) return null;
  return {
    ...base,
    summary: summaryText,
    facts: Array.isArray(facts) ? facts.map(cleanText).filter((f): f is string => f !== null) : [],
    related: parseTopicRefs(related, 12).filter((r) => r.slug !== base.slug),
  };
}

export function groupTopicsByKind(topics: TopicSummary[]): { kind: TopicKind; label: string; topics: TopicSummary[] }[] {
  return TOPIC_KINDS.map((kind) => ({
    kind,
    label: TOPIC_KIND_PLURALS[kind],
    topics: topics
      .filter((t) => t.kind === kind)
      .sort((a, b) => b.articleCount - a.articleCount || a.name.localeCompare(b.name)),
  })).filter((g) => g.topics.length > 0);
}

// The first sentence of a summary, for meta descriptions. Falls back to a
// trimmed prefix when there is no sentence end.
export function firstSentence(text: string, maxLength = 200): string {
  const flat = text.replace(/\s+/g, " ").trim();
  const match = flat.match(/^.*?[.!?](?=\s|$)/);
  const sentence = match ? match[0] : flat;
  if (sentence.length <= maxLength) return sentence;
  return `${sentence.slice(0, maxLength - 1).replace(/\s+\S*$/, "")}…`;
}

export interface MonthGroup<T> {
  key: string; // "2026-09", or "unknown"
  label: string; // "September 2026"
  items: T[];
}

// Groups articles (given newest first) by the month they were published, in
// UTC, keeping the input order within and between months.
export function groupByMonth<T>(items: T[], getDate: (item: T) => string | null | undefined): MonthGroup<T>[] {
  const groups: MonthGroup<T>[] = [];
  for (const item of items) {
    const date = parseApiDate(getDate(item));
    const key = date ? `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, "0")}` : "unknown";
    const label = date
      ? date.toLocaleDateString("en-US", { month: "long", year: "numeric", timeZone: "UTC" })
      : "Earlier";
    const last = groups[groups.length - 1];
    if (last && last.key === key) last.items.push(item);
    else {
      const existing = groups.find((g) => g.key === key);
      if (existing) existing.items.push(item);
      else groups.push({ key, label, items: [item] });
    }
  }
  return groups;
}

const escapeRegex = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const escapeHtml = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

// Links the first plain-text mention of each topic name in the article's
// paragraphs to its hub, at most `max` links. Text inside existing links
// (glossary terms included), headings and any other element outside <p> is
// left alone, and the article's own wording is kept. Run it after the
// glossary pass so glossary links win. Names that are all capitals (GPT, AI)
// match only in that case; others match ignoring case. Returns the HTML and
// the slugs linked, in order.
export function linkTopicMentions(
  html: string,
  topics: TopicRef[],
  max = MAX_BODY_TOPIC_LINKS,
): { html: string; slugs: string[] } {
  const candidates = parseTopicRefs(topics, 50);
  if (!candidates.length || max <= 0) return { html, slugs: [] };

  const names = candidates
    .map((topic) => ({ topic, name: topic.name }))
    .sort((a, b) => b.name.length - a.name.length);
  const byLower = new Map(names.map((n) => [n.name.toLowerCase(), n]));
  const pattern = new RegExp(`(?<![\\w-])(${names.map((n) => escapeRegex(n.name)).join("|")})(?![\\w-])`, "gi");
  const exactOnly = (name: string) => name === name.toUpperCase() && name !== name.toLowerCase();

  const used: string[] = [];
  let inParagraph = false;
  let linkDepth = 0;
  let headingDepth = 0;
  const parts = html.split(/(<[^>]+>)/);
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i];
    if (part.startsWith("<")) {
      if (/^<p[\s>]/i.test(part)) inParagraph = true;
      else if (/^<\/p>/i.test(part)) inParagraph = false;
      else if (/^<a[\s>]/i.test(part)) linkDepth++;
      else if (/^<\/a>/i.test(part)) linkDepth = Math.max(0, linkDepth - 1);
      else if (/^<h[1-6][\s>]/i.test(part)) headingDepth++;
      else if (/^<\/h[1-6]>/i.test(part)) headingDepth = Math.max(0, headingDepth - 1);
      continue;
    }
    if (!inParagraph || linkDepth > 0 || headingDepth > 0 || !part || used.length >= max) continue;
    parts[i] = part.replace(pattern, (text) => {
      const hit = byLower.get(text.toLowerCase());
      if (!hit || used.length >= max || used.includes(hit.topic.slug)) return text;
      if (exactOnly(hit.name) && text !== hit.name) return text;
      used.push(hit.topic.slug);
      return `<a href="/topics/${encodeURIComponent(hit.topic.slug)}" class="topic-link" data-topic="${escapeHtml(hit.topic.slug)}">${text}</a>`;
    });
  }
  return { html: parts.join(""), slugs: used };
}
