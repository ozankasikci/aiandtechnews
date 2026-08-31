import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";
import { fallbackArticlesByCategory } from "../../lib/fallback";
import { MostPopularSidebar } from "../../components/Sidebar";
import { ShareButtons } from "../../components/ShareButtons";
import { AnalyticsLink } from "../../components/AnalyticsLink";
import { ArticleReadTracker } from "../../components/ArticleReadTracker";
import { NewsletterBanner } from "../../components/Newsletter";
import { getArticleLookup, getArticles, mapArticle } from "../../lib/api";
import { toAbsoluteUrl } from "../../lib/dates";

type Props = { params: Promise<{ slug: string }> };

const BASE_URL = "https://www.aiandtech.news";

export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: Props) {
  const { slug } = await params;
  const lookup = await getArticleLookup(slug);
  if (lookup.state === "missing") return { title: "Article Not Found" };
  if (lookup.state === "unavailable") {
    return {
      title: "Article temporarily unavailable",
      description: "This article is temporarily unavailable. Please try again shortly.",
    };
  }
  const article = mapArticle(lookup.article);
  const canonicalUrl = `${BASE_URL}/article/${article.slug}`;
  const imageUrl = toAbsoluteUrl(article.image, BASE_URL);
  const metaTitle = article.metaTitle || article.headline;
  const metaDescription = article.metaDescription || article.excerpt || article.headline;

  return {
    title: metaTitle,
    description: metaDescription,
    alternates: { canonical: canonicalUrl },
    openGraph: {
      title: metaTitle,
      description: metaDescription,
      url: canonicalUrl,
      images: imageUrl ? [{ url: imageUrl }] : [],
      type: "article",
      publishedTime: article.publishedAt,
      modifiedTime: article.updatedAt,
      authors: [`${BASE_URL}/about`],
    },
    twitter: {
      card: "summary_large_image",
      title: metaTitle,
      description: metaDescription,
      images: imageUrl ? [imageUrl] : [],
    },
  };
}

export default async function ArticlePage({ params }: Props) {
  const { slug } = await params;

  // The offline snapshot carries no article bodies. Distinguish a real missing
  // slug from an API outage so temporary failures do not look like removals.
  const lookup = await getArticleLookup(slug);
  if (lookup.state === "missing") notFound();
  if (lookup.state === "unavailable") throw new Error("Article backend unavailable");
  const article = mapArticle(lookup.article);

  const body = article.body || "<p>Article content unavailable.</p>";
  const articleUrl = `${BASE_URL}/article/${article.slug}`;
  const imageUrl = toAbsoluteUrl(article.image, BASE_URL);

  // Get related articles
  const relatedData = await getArticles({ category: article.tag.toLowerCase(), limit: 4 });
  const related = relatedData?.articles?.length
    ? relatedData.articles.map(mapArticle).filter((a) => a.slug !== slug).slice(0, 3)
    : fallbackArticlesByCategory(article.tag).filter((a) => a.slug !== slug).slice(0, 3);

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "NewsArticle",
    headline: article.headline,
    description: article.excerpt,
    image: imageUrl ? [imageUrl] : undefined,
    datePublished: article.publishedAt,
    dateModified: article.updatedAt || article.publishedAt,
    articleSection: article.tag,
    inLanguage: "en",
    isAccessibleForFree: true,
    author: [{ "@type": "Organization", name: "TechNews Editorial", url: `${BASE_URL}/about` }],
    publisher: {
      "@type": "Organization",
      name: "AI and Tech News",
      url: BASE_URL,
      logo: { "@type": "ImageObject", url: `${BASE_URL}/icon-512.png`, width: 512, height: 512 },
    },
    mainEntityOfPage: { "@type": "WebPage", "@id": articleUrl },
  };

  return (
    <div className="max-w-[1400px] mx-auto">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />
      <div className="relative w-full h-[300px] md:h-[450px] lg:h-[500px]">
        <Image src={article.image} alt={article.headline} fill className="object-cover" sizes="100vw" priority />
        <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/40 to-transparent" />
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
              id="article-body"
              className="prose prose-invert max-w-none
                [&_p]:text-[#e5e5e5] [&_p]:text-base [&_p]:leading-relaxed [&_p]:mb-5
                [&_h2]:text-xl [&_h2]:font-bold [&_h2]:mt-8 [&_h2]:mb-4 [&_h2]:text-white
                [&_blockquote]:border-l-4 [&_blockquote]:border-accent-purple [&_blockquote]:pl-4 [&_blockquote]:italic [&_blockquote]:text-text-secondary [&_blockquote]:my-6"
              dangerouslySetInnerHTML={{ __html: body }}
            />
            <ArticleReadTracker bodyId="article-body" slug={article.slug} category={article.tag} />

            <NewsletterBanner placement="article_footer" />

            {related.length > 0 && (
              <section className="mt-12 pt-8 border-t border-border">
                <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6">Related Stories</h2>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                  {related.map((r) => (
                    <div key={r.id} className="group">
                      <AnalyticsLink
                        href={`/article/${r.slug}`}
                        eventName="select_content"
                        eventParameters={{
                          content_type: "related_article",
                          item_id: r.slug,
                          source_article: article.slug,
                          link_position: "image",
                        }}
                      >
                        <div className="relative h-[160px] rounded-sm overflow-hidden mb-3">
                          <Image src={r.image} alt="" fill className="object-cover group-hover:scale-105 transition-transform" sizes="300px" />
                        </div>
                      </AnalyticsLink>
                      <Link href={`/${r.tag.toLowerCase()}`} className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-1 hover:opacity-80 transition-opacity ${r.tagColor}`}>
                        {r.tag}
                      </Link>
                      <AnalyticsLink
                        href={`/article/${r.slug}`}
                        eventName="select_content"
                        eventParameters={{
                          content_type: "related_article",
                          item_id: r.slug,
                          source_article: article.slug,
                          link_position: "headline",
                        }}
                      >
                        <h3 className="text-sm font-bold leading-snug group-hover:text-accent-purple transition-colors">
                          {r.headline}
                        </h3>
                      </AnalyticsLink>
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
