const WORDS_PER_MINUTE = 220;
const MIN_MINUTES = 1;

// Article bodies are stored as HTML, so tags and entities have to come out
// before counting or markup-heavy posts overstate their reading time.
export function countWords(html: string | null | undefined): number {
  if (!html) return 0;

  const text = html
    .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&[a-z]+;|&#\d+;/gi, "")
    .trim();

  if (!text) return 0;
  return text.split(/\s+/).length;
}

export function readingTimeMinutes(html: string | null | undefined): number {
  const words = countWords(html);
  if (words === 0) return MIN_MINUTES;
  return Math.max(MIN_MINUTES, Math.round(words / WORDS_PER_MINUTE));
}

export function readingTimeLabel(html: string | null | undefined): string {
  return `${readingTimeMinutes(html)} min read`;
}
