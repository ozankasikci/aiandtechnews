import { notFound } from "next/navigation";
import { CATEGORIES, getArticlesByCategory } from "../data/articles";
import { StoryCard } from "../components/StoryCard";
import { MostPopularSidebar } from "../components/Sidebar";
import { getArticles, mapArticle } from "../lib/api";

type Props = { params: Promise<{ category: string }> };

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 60;

export function generateStaticParams() {
  return Object.keys(CATEGORIES).map((category) => ({ category }));
}

export async function generateMetadata({ params }: Props) {
  const { category } = await params;
  const cat = CATEGORIES[category];
  if (!cat) return { title: "Not Found" };
  const canonicalUrl = `${BASE_URL}/${category}`;

  return {
    title: `${cat.title} News`,
    description: cat.description,
    alternates: { canonical: canonicalUrl },
    openGraph: {
      title: `${cat.title} News`,
      description: cat.description,
      url: canonicalUrl,
    },
  };
}

export default async function CategoryPage({ params }: Props) {
  const { category } = await params;
  const cat = CATEGORIES[category];
  if (!cat) notFound();

  // Try API first
  const data = await getArticles({ category, limit: 20 });
  const articles = data?.articles?.length
    ? data.articles.map(mapArticle)
    : getArticlesByCategory(cat.title); // fallback to static

  return (
    <div className="max-w-[1400px] mx-auto px-4 lg:px-8 py-8">
      <div className="mb-8 pb-6 border-b border-border">
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-2">{cat.title}</h1>
        <p className="text-text-secondary text-lg">{cat.description}</p>
      </div>

      <div className="flex flex-col lg:flex-row gap-8">
        <div className="flex-1 min-w-0">
          {articles.map((a) => (
            <StoryCard key={a.id} story={a} />
          ))}
          {articles.length === 0 && (
            <p className="text-text-muted py-8">No articles in this category yet.</p>
          )}
        </div>
        <MostPopularSidebar />
      </div>
    </div>
  );
}
