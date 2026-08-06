import { getArticlesUpTo } from "../lib/api";
import { parseApiDate } from "../lib/dates";
import { buildNewsSitemap, RECENT_NEWS_WINDOW_MS } from "./news-sitemap";

export const revalidate = 300;

// Google News sitemaps allow up to 1,000 URLs; articles older than the
// 48-hour window are filtered out by buildNewsSitemap.
const MAX_NEWS_ARTICLES = 1_000;

export async function GET() {
  const cutoff = Date.now() - RECENT_NEWS_WINDOW_MS;
  const articles = await getArticlesUpTo(MAX_NEWS_ARTICLES, (article) => {
    const published = parseApiDate(article.published_at);
    return published !== null && published.getTime() < cutoff;
  });

  if (!articles) {
    return new Response(buildNewsSitemap([]), {
      status: 503,
      headers: {
        "Content-Type": "application/xml; charset=utf-8",
        "Cache-Control": "no-store",
        "Retry-After": "300",
      },
    });
  }

  return new Response(buildNewsSitemap(articles), {
    headers: {
      "Content-Type": "application/xml; charset=utf-8",
      "Cache-Control": "public, max-age=0, s-maxage=300, stale-while-revalidate=300",
    },
  });
}
