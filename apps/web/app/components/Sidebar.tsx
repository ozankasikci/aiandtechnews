import { getTrendingArticles, mapArticle, type ListCache } from "../lib/api";
import { TrendingTabs, type TrendingItem } from "./TrendingTabs";
import { QuizCard } from "./QuizCard";

async function trendingItems(request: ReturnType<typeof getTrendingArticles>): Promise<TrendingItem[]> {
  const data = await request;
  return (data?.articles ?? []).map(mapArticle).map((a) => ({ title: a.headline, slug: a.slug, time: a.time }));
}

export async function TrendingSidebar({ cache = "list" }: { cache?: ListCache } = {}) {
  const [today, week] = await Promise.all([
    trendingItems(getTrendingArticles(5, "24h", cache)),
    trendingItems(getTrendingArticles(5, "7d", cache)),
  ]);
  if (!today.length && !week.length) return null;

  return (
    <aside className="w-full lg:w-[300px] xl:w-[380px] shrink-0">
      <TrendingTabs heading="Trending" today={today} week={week} />
      <QuizCard cache={cache} />
    </aside>
  );
}
