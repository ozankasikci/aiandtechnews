const WORDS_PER_MINUTE = 220;

// Article bodies are HTML, so tags have to come out before counting or
// markup-heavy posts overstate their reading time.
export function readingMinutes(html: string | null | undefined): number {
  if (!html) return 1;

  const text = html
    .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&[a-z]+;|&#\d+;/gi, "")
    .trim();

  if (!text) return 1;
  return Math.max(1, Math.round(text.split(/\s+/).length / WORDS_PER_MINUTE));
}
