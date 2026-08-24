import { getArticles } from "../lib/api";
import { buildRssFeed } from "./rss";

export const revalidate = 300;

const FEED_SIZE = 50;

export async function GET() {
  const data = await getArticles({ limit: FEED_SIZE });

  if (!data) {
    return new Response(buildRssFeed([]), {
      status: 503,
      headers: {
        "Content-Type": "application/rss+xml; charset=utf-8",
        "Cache-Control": "no-store",
        "Retry-After": "300",
      },
    });
  }

  return new Response(buildRssFeed(data.articles), {
    headers: {
      "Content-Type": "application/rss+xml; charset=utf-8",
      "Cache-Control": "public, max-age=0, s-maxage=300, stale-while-revalidate=300",
    },
  });
}
