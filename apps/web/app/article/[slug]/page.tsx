import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";
import { getArticleBySlug, ALL_ARTICLES } from "../../data/articles";
import { MostPopularSidebar } from "../../components/Sidebar";
import { getArticle as fetchArticle, getArticles, mapArticle } from "../../lib/api";

type Props = { params: Promise<{ slug: string }> };

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

export const dynamicParams = false;

function ShareButtons() {
  return (
    <div className="flex items-center gap-3 py-4 border-b border-border mb-8">
      <span className="text-text-muted text-xs font-bold uppercase tracking-wider mr-2">Share</span>
      <a href="#" className="w-8 h-8 rounded-full bg-bg-card flex items-center justify-center text-text-secondary hover:text-white hover:bg-accent-purple transition-colors">
        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z"/></svg>
      </a>
      <a href="#" className="w-8 h-8 rounded-full bg-bg-card flex items-center justify-center text-text-secondary hover:text-white hover:bg-accent-blue transition-colors">
        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M24 12.073c0-6.627-5.373-12-12-12s-12 5.373-12 12c0 5.99 4.388 10.954 10.125 11.854v-8.385H7.078v-3.47h3.047V9.43c0-3.007 1.792-4.669 4.533-4.669 1.312 0 2.686.235 2.686.235v2.953H15.83c-1.491 0-1.956.925-1.956 1.874v2.25h3.328l-.532 3.47h-2.796v8.385C19.612 23.027 24 18.062 24 12.073z"/></svg>
      </a>
      <a href="#" className="w-8 h-8 rounded-full bg-bg-card flex items-center justify-center text-text-secondary hover:text-white hover:bg-accent-blue transition-colors">
        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433c-1.144 0-2.063-.926-2.063-2.065 0-1.138.92-2.063 2.063-2.063 1.14 0 2.064.925 2.064 2.063 0 1.139-.925 2.065-2.064 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z"/></svg>
      </a>
    </div>
  );
}

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

  return (
    <div className="max-w-[1400px] mx-auto">
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
            <span className={`inline-block px-3 py-1 text-xs font-bold uppercase tracking-wider text-black rounded-sm mb-4 ${article.tagColor}`}>
              {article.tag}
            </span>
            <h1 className="text-3xl md:text-4xl lg:text-5xl font-black leading-[1.1] tracking-tight mb-6">
              {article.headline}
            </h1>

            <div className="flex items-center gap-3 mb-6 pb-6 border-b border-border">
              <span className="text-text-muted text-sm">{article.date} · {article.readTime}</span>
            </div>

            <ShareButtons />

            <div
              className="prose prose-invert max-w-none
                [&_p]:text-text-secondary [&_p]:text-base [&_p]:leading-relaxed [&_p]:mb-5
                [&_h2]:text-xl [&_h2]:font-bold [&_h2]:mt-8 [&_h2]:mb-4 [&_h2]:text-white
                [&_blockquote]:border-l-4 [&_blockquote]:border-accent-purple [&_blockquote]:pl-4 [&_blockquote]:italic [&_blockquote]:text-text-secondary [&_blockquote]:my-6"
              dangerouslySetInnerHTML={{ __html: body }}
            />

            {related.length > 0 && (
              <section className="mt-12 pt-8 border-t border-border">
                <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6">Related Stories</h2>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                  {related.map((r) => (
                    <Link key={r.id} href={`/article/${r.slug}`} className="group">
                      <div className="relative h-[160px] rounded-sm overflow-hidden mb-3">
                        <Image src={r.image} alt="" fill className="object-cover group-hover:scale-105 transition-transform" sizes="300px" />
                      </div>
                      <span className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-1 ${r.tagColor}`}>
                        {r.tag}
                      </span>
                      <h3 className="text-sm font-bold leading-snug group-hover:text-accent-purple transition-colors">
                        {r.headline}
                      </h3>
                    </Link>
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
