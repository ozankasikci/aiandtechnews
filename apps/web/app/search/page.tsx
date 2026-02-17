import { ALL_ARTICLES } from "../data/articles";
import { StoryCard } from "../components/StoryCard";
import { getArticles, mapArticle } from "../lib/api";

type Props = { searchParams: Promise<{ q?: string }> };

export default async function SearchPage({ searchParams }: Props) {
  const { q } = await searchParams;

  let articles;
  if (q) {
    const data = await getArticles({ search: q, limit: 20 });
    articles = data?.articles?.length
      ? data.articles.map(mapArticle)
      : ALL_ARTICLES.filter((a) =>
          a.headline.toLowerCase().includes(q.toLowerCase()) ||
          a.excerpt.toLowerCase().includes(q.toLowerCase())
        );
  } else {
    const data = await getArticles({ limit: 10 });
    articles = data?.articles?.length ? data.articles.map(mapArticle) : ALL_ARTICLES.slice(0, 10);
  }

  return (
    <div className="max-w-[900px] mx-auto px-4 lg:px-8 py-8">
      <div className="mb-10">
        <h1 className="text-3xl font-black tracking-tight mb-6">Search</h1>
        <form action="/search" method="GET">
          <div className="relative">
            <svg className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              name="q"
              defaultValue={q || ""}
              placeholder="Search TechNews..."
              className="w-full bg-bg-card border border-border rounded-sm py-4 pl-12 pr-4 text-lg text-white placeholder:text-text-muted focus:outline-none focus:border-accent-purple transition-colors"
            />
          </div>
        </form>
      </div>

      <div>
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4 pb-2 border-b border-border">
          {q ? `Results for "${q}"` : "Recent Articles"}
        </h2>
        {articles.length === 0 ? (
          <p className="text-text-muted py-8">No articles found.</p>
        ) : (
          articles.map((a) => <StoryCard key={a.id} story={a} />)
        )}
      </div>
    </div>
  );
}
