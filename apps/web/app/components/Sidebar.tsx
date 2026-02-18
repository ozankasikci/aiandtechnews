import Link from "next/link";
import { getTrendingArticles, mapArticle } from "../lib/api";

export async function MostPopularSidebar() {
  const data = await getTrendingArticles(5);
  const items = data?.articles?.length
    ? data.articles.map(mapArticle).map((a) => ({ title: a.headline, slug: a.slug }))
    : [];

  if (!items.length) return null;

  return (
    <aside className="w-full lg:w-[300px] shrink-0">
      <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4 pb-2 border-b border-border">
        Most Popular
      </h2>
      <ol className="space-y-4">
        {items.map((item, i) => (
          <li key={i}>
            <Link href={item.slug === "#" ? "#" : `/article/${item.slug}`} className="flex gap-3 group cursor-pointer">
              <span className="text-2xl font-black text-text-muted/50 leading-none shrink-0 w-7 text-right">{i + 1}</span>
              <span className="text-sm font-semibold leading-snug group-hover:text-accent-purple transition-colors">
                {item.title}
              </span>
            </Link>
          </li>
        ))}
      </ol>
    </aside>
  );
}
