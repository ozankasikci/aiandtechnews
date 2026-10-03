import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";
import { getTopic, mapArticle } from "../../lib/api";
import { parseApiDate, toAbsoluteUrl } from "../../lib/dates";
import { firstSentence, groupByMonth, TOPIC_KIND_LABELS } from "../../lib/topics";

type Props = { params: Promise<{ slug: string }> };

const BASE_URL = "https://www.aiandtech.news";

// Same cache policy as article pages: cached for an hour, refreshed on
// demand by POST /api/revalidate (topics: ["slug"]), nothing prebuilt.
export const revalidate = 86400;
export const dynamicParams = true;
export function generateStaticParams(): { slug: string }[] {
  return [];
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug } = await params;
  const lookup = await getTopic(slug);
  if (lookup.state === "missing") return { title: "Topic Not Found", robots: { index: false } };
  if (lookup.state === "unavailable") {
    return { title: "Topic temporarily unavailable", description: "This topic is temporarily unavailable. Please try again shortly." };
  }
  const { topic } = lookup;
  const title = `${topic.name}: latest AI news and updates`;
  const description = firstSentence(topic.summary) || `Our latest coverage of ${topic.name}.`;
  const url = `${BASE_URL}/topics/${topic.slug}`;
  return {
    title,
    description,
    alternates: { canonical: url },
    openGraph: { title, description, url },
    twitter: { card: "summary", title, description },
  };
}

export default async function TopicPage({ params }: Props) {
  const { slug } = await params;
  const lookup = await getTopic(slug);
  if (lookup.state === "missing") notFound();
  if (lookup.state === "unavailable") throw new Error("Topic backend unavailable");

  const { topic } = lookup;
  const articles = lookup.articles.map(mapArticle);
  const months = groupByMonth(articles, (a) => a.publishedAt);
  const url = `${BASE_URL}/topics/${topic.slug}`;
  const paragraphs = topic.summary.split(/\n{2,}/).map((p) => p.trim()).filter(Boolean);

  const jsonLd = {
    "@context": "https://schema.org",
    "@type": "CollectionPage",
    name: `${topic.name}: latest AI news and updates`,
    description: firstSentence(topic.summary),
    url,
    inLanguage: "en",
    dateModified: topic.updatedAt,
    isPartOf: { "@type": "WebSite", name: "AI and Tech News", url: BASE_URL },
    mainEntity: {
      "@type": "ItemList",
      numberOfItems: articles.length,
      itemListElement: articles.map((a, i) => ({
        "@type": "ListItem",
        position: i + 1,
        url: `${BASE_URL}/article/${a.slug}`,
      })),
    },
  };

  const updated = parseApiDate(topic.updatedAt);

  return (
    <main className="max-w-[1000px] mx-auto px-4 lg:px-8 py-10">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />

      <nav aria-label="Breadcrumb" className="text-xs text-text-muted mb-4">
        <Link href="/topics" className="hover:text-white transition-colors">Topics</Link>
      </nav>

      <header className="mb-8 pb-6 border-b border-border">
        <p className="text-xs font-bold uppercase tracking-widest text-accent-purple mb-3">{TOPIC_KIND_LABELS[topic.kind]}</p>
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-5">{topic.name}</h1>
        <div className="space-y-4 max-w-3xl">
          {paragraphs.map((p, i) => (
            <p key={i} className="text-[#e5e5e5] text-base leading-relaxed">{p}</p>
          ))}
        </div>
        <p className="text-text-muted text-xs mt-5">
          {topic.articleCount} {topic.articleCount === 1 ? "story" : "stories"}
          {updated && (
            <>
              {" "}· Updated{" "}
              <time dateTime={updated.toISOString()}>
                {updated.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" })}
              </time>
            </>
          )}
        </p>
      </header>

      {topic.facts.length > 0 && (
        <section aria-labelledby="key-facts" className="mb-10">
          <h2 id="key-facts" className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4">Key facts</h2>
          <ul className="space-y-2 max-w-3xl">
            {topic.facts.map((fact, i) => (
              <li key={i} className="flex gap-3 text-[#e5e5e5] text-[15px] leading-relaxed">
                <span aria-hidden className="mt-2.5 h-1.5 w-1.5 shrink-0 rounded-full bg-accent-purple" />
                <span>{fact}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section aria-labelledby="timeline" className="mb-10">
        <h2 id="timeline" className="text-xs font-bold uppercase tracking-widest text-text-muted mb-2">Timeline</h2>
        {months.length === 0 ? (
          <p className="text-text-muted py-4">No stories yet.</p>
        ) : (
          months.map((month) => (
            <div key={month.key} className="mt-6">
              <h3 className="text-sm font-bold text-accent-purple mb-1">{month.label}</h3>
              <ol className="divide-y divide-border">
                {month.items.map((article) => {
                  const image = toAbsoluteUrl(article.image, BASE_URL);
                  return (
                    <li key={article.slug} className="py-4 flex gap-4">
                      {image && (
                        <Link href={`/article/${article.slug}`} tabIndex={-1} aria-hidden className="relative hidden sm:block w-28 h-[72px] shrink-0 overflow-hidden rounded-sm bg-bg-card">
                          <Image src={article.image} alt="" fill sizes="112px" className="object-cover" />
                        </Link>
                      )}
                      <article className="min-w-0">
                        <time dateTime={article.publishedAt} className="text-xs text-text-muted">{article.date}</time>
                        <h4 className="text-lg font-bold leading-snug tracking-tight mt-1">
                          <Link href={`/article/${article.slug}`} className="hover:text-accent-purple transition-colors">
                            {article.headline}
                          </Link>
                        </h4>
                        {article.excerpt && <p className="text-text-secondary text-sm leading-relaxed mt-1.5">{article.excerpt}</p>}
                      </article>
                    </li>
                  );
                })}
              </ol>
            </div>
          ))
        )}
      </section>

      {topic.related.length > 0 && (
        <section aria-labelledby="related-topics">
          <h2 id="related-topics" className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4">Related topics</h2>
          <ul className="flex flex-wrap gap-2">
            {topic.related.map((r) => (
              <li key={r.slug}>
                <Link href={`/topics/${r.slug}`} className="inline-block px-3 py-1.5 text-sm border border-border rounded-sm text-text-secondary hover:text-white hover:border-accent-purple transition-colors">
                  {r.name}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}
