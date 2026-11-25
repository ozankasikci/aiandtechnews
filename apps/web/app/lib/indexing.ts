// Short articles stay readable on the site but are kept out of search
// indexes and sitemaps, so search engines (and ad reviewers) judge the site
// on its fuller pages.
export const MIN_INDEXED_WORDS = 250;

export function articleWordCount(html: string): number {
  const text = html.replace(/<[^>]+>/g, " ").replace(/&nbsp;/g, " ").replace(/&[a-z#0-9]+;/gi, "");
  return text.split(/\s+/).filter(Boolean).length;
}

export function isIndexable(article: { content?: string | null }): boolean {
  return articleWordCount(article.content ?? "") >= MIN_INDEXED_WORDS;
}
