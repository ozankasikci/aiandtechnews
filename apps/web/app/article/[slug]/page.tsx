import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";
import { getArticleBySlug, ALL_ARTICLES } from "../../data/articles";
import { MostPopularSidebar } from "../../components/Sidebar";
import { ShareButtons } from "../../components/ShareButtons";
import { getArticle as fetchArticle, getArticles, mapArticle } from "../../lib/api";

type Props = { params: Promise<{ slug: string }> };

export const revalidate = 60;

export async function generateMetadata({ params }: Props) {
  const { slug } = await params;
  const apiData = await fetchArticle(slug);
  const article = apiData?.article ? mapArticle(apiData.article) : getArticleBySlug(slug);
  if (!article) return { title: "Article Not Found" };
  return {
    title: article.headline,
    description: article.excerpt || article.headline,
    openGraph: {
      title: article.headline,
      description: article.excerpt || article.headline,
      images: article.image ? [{ url: article.image }] : [],
      type: "article",
    },
  };
}

export async function generateStaticParams() {
  const data = await getArticles({ limit: 100 });
  if (!data?.articles?.length) return [];
  return data.articles.map((a) => ({ slug: a.slug }));
}

export const dynamicParams = true;

export default async function ArticlePage({ params }: Props) {
  const { slug } = await params;

  // Try API first, fall back to static
  const apiData = await fetchArticle(slug);
  const article = apiData?.article ? mapArticle(apiData.article) : getArticleBySlug(slug);
  if (!article) notFound();

  const body = article.body || "<p>Article content unavailable.</p>";

  // Get related articles
  const relatedData = await getArticles({ category: article.tag.toLowerCase(), limit: 4 });
  const related = relatedData?.articles?.length
    ? relatedData.articles.map(mapArticle).filter((a) => a.slug !== slug).slice(0, 3)
    : ALL_ARTICLES.filter((a) => a.tag === article.tag && a.slug !== slug).slice(0, 3);

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "NewsArticle",
    headline: article.headline,
    description: article.excerpt,
    image: article.image ? [`https://www.aiandtech.news${article.image}`] : [],
    datePublished: article.date,
    dateModified: article.date,
    author: [{ "@type": "Person", name: article.author }],
    publisher: {
      "@type": "Organization",
      name: "AI and Tech News",
      logo: { "@type": "ImageObject", url: "https://www.aiandtech.news/favicon-32.png" },
    },
    mainEntityOfPage: { "@type": "WebPage", "@id": `https://www.aiandtech.news/article/${article.slug}` },
  };

  return (
    <div className="max-w-[1400px] mx-auto">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />
      <div className="relative w-full h-[300px] md:h-[450px] lg:h-[500px]">
        <Image src={article.image} alt={article.headline} fill className="object-cover" sizes="100vw" priority />
        <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/40 to-transparent" />
        {article.imageSource && (
          <span className="absolute bottom-3 right-4 text-xs text-white/50 z-10">Image: {article.imageSource}</span>
        )}
      </div>

      <div className="px-4 lg:px-8 -mt-24 relative z-10">
        <div className="flex flex-col lg:flex-row gap-8">
          <article className="flex-1 min-w-0 max-w-3xl">
            <Link href={`/${article.tag.toLowerCase()}`} className={`inline-block px-3 py-1 text-xs font-bold uppercase tracking-wider text-black rounded-sm mb-4 hover:opacity-80 transition-opacity ${article.tagColor}`}>
              {article.tag}
            </Link>
            <h1 className="text-3xl md:text-4xl lg:text-5xl font-black leading-[1.1] tracking-tight mb-6">
              {article.headline}
            </h1>

            <div className="flex items-center gap-3 mb-6 pb-6 border-b border-border">
              <img
                src={article.avatar}
                alt={article.author}
                className="w-8 h-8 rounded-full object-cover shrink-0"
              />
              <div className="flex flex-col gap-0.5">
                <span className="text-white text-sm font-semibold leading-tight">{article.author}</span>
                <span className="text-text-muted text-xs">{article.date} · {article.readTime}</span>
              </div>
            </div>

            <ShareButtons title={article.headline} />

            <div
              className="prose prose-invert max-w-none
                [&_p]:text-[#e5e5e5] [&_p]:text-base [&_p]:leading-relaxed [&_p]:mb-5
                [&_h2]:text-xl [&_h2]:font-bold [&_h2]:mt-8 [&_h2]:mb-4 [&_h2]:text-white
                [&_blockquote]:border-l-4 [&_blockquote]:border-accent-purple [&_blockquote]:pl-4 [&_blockquote]:italic [&_blockquote]:text-text-secondary [&_blockquote]:my-6"
              dangerouslySetInnerHTML={{ __html: body }}
            />

            {related.length > 0 && (
              <section className="mt-12 pt-8 border-t border-border">
                <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6">Related Stories</h2>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                  {related.map((r) => (
                    <div key={r.id} className="group">
                      <Link href={`/article/${r.slug}`}>
                        <div className="relative h-[160px] rounded-sm overflow-hidden mb-3">
                          <Image src={r.image} alt="" fill className="object-cover group-hover:scale-105 transition-transform" sizes="300px" />
                        </div>
                      </Link>
                      <Link href={`/${r.tag.toLowerCase()}`} className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-1 hover:opacity-80 transition-opacity ${r.tagColor}`}>
                        {r.tag}
                      </Link>
                      <Link href={`/article/${r.slug}`}>
                        <h3 className="text-sm font-bold leading-snug group-hover:text-accent-purple transition-colors">
                          {r.headline}
                        </h3>
                      </Link>
                    </div>
                  ))}
                </div>
              </section>
            )}
          </article>

          <div className="lg:mt-32">
            <MostPopularSidebar />
          </div>
        </div>
      </div>
    </div>
  );
}
