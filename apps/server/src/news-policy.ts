export const APPROVED_FEEDS = [
  { source: "TechCrunch", url: "https://techcrunch.com/feed/" },
  { source: "The Verge", url: "https://www.theverge.com/rss/index.xml" },
  { source: "Ars Technica", url: "https://feeds.arstechnica.com/arstechnica/index" },
] as const;

export const EDITORIAL_AUTHOR = {
  name: "TechNews Editorial",
  email: "editorial@technews.dev",
  bio: "The TechNews editorial team covers artificial intelligence and technology.",
} as const;

export const MAX_ARTICLES_PER_RUN = 1;

export interface RewrittenArticle {
  title: string;
  excerpt: string;
  content: string;
}

const SOURCE_HOSTS: Record<string, string[]> = {
  TechCrunch: ["techcrunch.com"],
  "The Verge": ["theverge.com"],
  "Ars Technica": ["arstechnica.com"],
};

const REJECTED_TITLE_PATTERNS: Array<[RegExp, string]> = [
  [/^show hn:/i, "Show HN item"],
  [/^state of show hn/i, "Show HN item"],
  [/\[(?:pdf|video)\]/i, "PDF or video item"],
  [/\b(?:abstract|arxiv)\s*:/i, "abstract or arXiv-style item"],
  [/\barxiv\b/i, "arXiv-style item"],
  [/\b(?:deals?|coupon|discount|sale|buying guide)\b/i, "deal or promotional item"],
  [/\b(?:lowest|best) price\b/i, "deal or promotional item"],
  [/\bprice (?:drop|cut)\b/i, "deal or promotional item"],
  [/\b(?:save \$?\d+|\d+% off|percent off)\b/i, "deal or promotional item"],
  [/\b(?:prime day|black friday|cyber monday)\b/i, "deal or promotional item"],
];

const FORBIDDEN_COPY = [
  { pattern: /\u2014/, label: "em dash" },
  { pattern: /\bin a move that\b/i, label: "In a move that" },
  { pattern: /\bit remains to be seen\b/i, label: "It remains to be seen" },
  { pattern: /\bgroundbreaking\b/i, label: "groundbreaking" },
  { pattern: /\brevolutionary\b/i, label: "revolutionary" },
  { pattern: /\bgame-changing\b/i, label: "game-changing" },
];

export function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 120);
}

export function stripHtml(html: string): string {
  return html
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/\s+/g, " ")
    .trim();
}

export function wordCount(html: string): number {
  const text = stripHtml(html);
  return text ? text.split(/\s+/).length : 0;
}

export function sourceForUrl(value: string): string | null {
  try {
    const url = new URL(value);
    if (!/^https?:$/.test(url.protocol)) return null;
    const host = url.hostname.toLowerCase().replace(/^www\./, "");

    for (const [source, domains] of Object.entries(SOURCE_HOSTS)) {
      if (domains.some((domain) => host === domain || host.endsWith(`.${domain}`))) {
        return source;
      }
    }
  } catch {
    return null;
  }
  return null;
}

export function normalizeSourceUrl(value: string): string {
  const url = new URL(value);
  url.hash = "";
  for (const key of [...url.searchParams.keys()]) {
    if (/^(?:utm_.+|gclid|fbclid|mc_cid|mc_eid)$/i.test(key)) {
      url.searchParams.delete(key);
    }
  }
  if (url.pathname.length > 1) url.pathname = url.pathname.replace(/\/+$/, "");
  return url.toString();
}

export function getItemRejectionReason(
  title: string,
  sourceUrl: string,
  expectedSource?: string,
  now = new Date(),
): string | null {
  const source = sourceForUrl(sourceUrl);
  if (!source) return "source is not on the approved publication list";
  if (expectedSource && source !== expectedSource) return "source URL does not match its RSS publication";

  for (const [pattern, reason] of REJECTED_TITLE_PATTERNS) {
    if (pattern.test(title.trim())) return reason;
  }

  let url: URL;
  try {
    url = new URL(sourceUrl);
  } catch {
    return "invalid source URL";
  }

  if (/\.(?:pdf|mp4|mov|webm)(?:$|[?#])/i.test(url.pathname)) return "PDF or video item";
  if (/\/(?:videos?|deals?)(?:\/|$)/i.test(url.pathname)) return "video, deal, or promotional item";

  const oldTitleYear = title.match(/\(((?:19|20)\d{2})\)\s*$/)?.[1];
  const oldUrlYear = url.pathname.match(/\/((?:19|20)\d{2})\//)?.[1];
  const cutoffYear = now.getUTCFullYear() - 1;
  if (oldTitleYear && Number(oldTitleYear) < cutoffYear) return "obviously old repost";
  if (oldUrlYear && Number(oldUrlYear) < cutoffYear) return "obviously old repost";

  return null;
}

function sentenceCount(text: string): number {
  return (text.match(/[.!?](?:["')\]]*)?(?=\s+[A-Z0-9]|$)/g) || []).length;
}

export function validateRewrittenArticle(article: RewrittenArticle): string[] {
  const errors: string[] = [];
  const title = typeof article.title === "string" ? article.title.trim() : "";
  const excerpt = typeof article.excerpt === "string" ? article.excerpt.trim() : "";
  const content = typeof article.content === "string" ? article.content.trim() : "";
  const allCopy = `${title}\n${excerpt}\n${content}`;

  if (!title) errors.push("headline is missing");
  if (title.length > 120) errors.push("headline exceeds 120 characters");
  if (/!{1,}|\?{2,}/.test(title)) errors.push("headline appears clickbait-like");

  if (!excerpt) errors.push("excerpt is missing");
  if (excerpt.length > 180) errors.push("excerpt exceeds 180 characters");
  if (/<[^>]+>/.test(excerpt)) errors.push("excerpt contains HTML");
  if (/\r|\n/.test(excerpt)) errors.push("excerpt is not one plain line");
  if (excerpt && sentenceCount(excerpt) !== 1) errors.push("excerpt must be exactly one sentence");

  for (const forbidden of FORBIDDEN_COPY) {
    if (forbidden.pattern.test(allCopy)) errors.push(`copy contains prohibited ${forbidden.label}`);
  }

  const tags = [...content.matchAll(/<\/?([a-z][a-z0-9]*)\b[^>]*>/gi)].map((match) => match[1].toLowerCase());
  if (tags.some((tag) => tag !== "p" && tag !== "h2")) {
    errors.push("article HTML contains tags other than p or h2");
  }
  if ([...content.matchAll(/<\/?(?:p|h2)\b[^>]*>/gi)].some((match) => !/^<\/?(?:p|h2)>$/i.test(match[0]))) {
    errors.push("article HTML tags contain attributes");
  }

  const blockPattern = /<(p|h2)>[\s\S]*?<\/\1>/gi;
  if (content.replace(blockPattern, "").trim()) errors.push("article HTML contains text outside p or h2 blocks");

  const paragraphs = [...content.matchAll(/<p>([\s\S]*?)<\/p>/gi)];
  if (paragraphs.length < 5 || paragraphs.length > 8) errors.push("article must contain 5 to 8 paragraphs");
  if (paragraphs.some((paragraph) => !stripHtml(paragraph[1]))) errors.push("article contains an empty paragraph");

  const words = wordCount(content);
  if (words < 400 || words > 600) errors.push(`article must contain 400 to 600 words, found ${words}`);

  const lastParagraph = paragraphs.at(-1);
  if (lastParagraph && /^(?:source|sources)\s*:/i.test(stripHtml(lastParagraph[1]))) {
    errors.push("article contains a source footer");
  }

  return [...new Set(errors)];
}

