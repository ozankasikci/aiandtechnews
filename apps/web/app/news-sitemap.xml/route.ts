import { getArticles } from "../lib/api";
import { buildNewsSitemap } from "./news-sitemap";

export const revalidate = 300;

export async function GET() {
  const data = await getArticles({ limit: 100 });

  if (!data) {
    return new Response(buildNewsSitemap([]), {
      status: 503,
      headers: {
        "Content-Type": "application/xml; charset=utf-8",
        "Cache-Control": "no-store",
        "Retry-After": "300",
      },
    });
  }

  return new Response(buildNewsSitemap(data.articles), {
    headers: {
      "Content-Type": "application/xml; charset=utf-8",
      "Cache-Control": "public, max-age=0, s-maxage=300, stale-while-revalidate=300",
    },
  });
}
