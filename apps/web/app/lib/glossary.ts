import { GLOSSARY, type GlossaryTerm } from "../data/glossary";

// Every term and alias, longest first so "coding agents" wins over a shorter
// name at the same position. Acronyms (API, LLMs) only match in capitals, so
// "api" inside ordinary words or lowercase text is left alone.
const NAMES = GLOSSARY.flatMap((entry) => [entry.term, ...entry.aliases].map((name) => ({ name, entry })))
  .sort((a, b) => b.name.length - a.name.length);
const BY_LOWER = new Map(NAMES.map((n) => [n.name.toLowerCase(), n]));
const PATTERN = new RegExp(`\\b(${NAMES.map((n) => n.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})\\b`, "gi");

const isAcronym = (name: string) => /^[A-Z0-9]+s?$/.test(name) && /[A-Z]{2}/.test(name);

function matchedEntry(text: string): GlossaryTerm | undefined {
  const named = BY_LOWER.get(text.toLowerCase());
  if (!named || (isAcronym(named.name) && text !== named.name)) return undefined;
  return named.entry;
}

export function glossaryTerm(slug: string): GlossaryTerm | undefined {
  return GLOSSARY.find((entry) => entry.slug === slug);
}

// Whether plain text mentions the term or one of its aliases.
export function mentionsTerm(entry: GlossaryTerm, text: string): boolean {
  for (const match of text.matchAll(PATTERN)) {
    if (matchedEntry(match[0])?.slug === entry.slug) return true;
  }
  return false;
}

// Links the first mention of each glossary term in the article's paragraphs to
// its glossary page, keeping the article's own wording. Headings and existing
// links are left alone. Returns the linked HTML and the slugs it used, in order.
export function linkGlossaryTerms(html: string): { html: string; slugs: string[] } {
  const used: string[] = [];
  let inParagraph = false;
  let linkDepth = 0;
  const parts = html.split(/(<[^>]+>)/);
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i];
    if (part.startsWith("<")) {
      if (/^<p[\s>]/i.test(part)) inParagraph = true;
      else if (/^<\/p>/i.test(part)) inParagraph = false;
      else if (/^<a[\s>]/i.test(part)) linkDepth++;
      else if (/^<\/a>/i.test(part)) linkDepth = Math.max(0, linkDepth - 1);
      continue;
    }
    if (!inParagraph || linkDepth > 0 || !part) continue;
    parts[i] = part.replace(PATTERN, (text) => {
      const entry = matchedEntry(text);
      if (!entry || used.includes(entry.slug)) return text;
      used.push(entry.slug);
      return `<a href="/glossary/${entry.slug}" class="glossary-term" data-glossary="${entry.slug}">${text}</a>`;
    });
  }
  return { html: parts.join(""), slugs: used };
}
