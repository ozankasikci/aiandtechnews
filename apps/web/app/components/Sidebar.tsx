import { getTrendingArticles, mapArticle } from "../lib/api";
import { TrendingTabs, type TrendingItem } from "./TrendingTabs";

async function trendingItems(request: ReturnType<typeof getTrendingArticles>): Promise<TrendingItem[]> {
  const data = await request;
  return (data?.articles ?? []).map(mapArticle).map((a) => ({ title: a.headline, slug: a.slug, time: a.time }));
}

export async function TrendingSidebar() {
  const [today, week] = await Promise.all([
    trendingItems(getTrendingArticles(5, "24h")),
    trendingItems(getTrendingArticles(5, "7d")),
  ]);
  if (!today.length && !week.length) return null;

  return (
    <aside className="w-full lg:w-[300px] shrink-0">
      <TrendingTabs heading="Trending" today={today} week={week} />
    </aside>
  );
}
