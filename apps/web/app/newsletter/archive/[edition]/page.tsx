import Link from "next/link";
import { notFound } from "next/navigation";
import { NewsletterBanner } from "../../../components/Newsletter";
import { getNewsletterEdition } from "../../../lib/api";
import { formatEditionDate } from "../../edition-date";

type Props = { params: Promise<{ edition: string }> };

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 300;

export async function generateMetadata({ params }: Props) {
  const { edition: editionKey } = await params;
  const data = await getNewsletterEdition(editionKey);
  if (!data?.edition) return { title: "Edition Not Found" };

  const { edition } = data;
  const canonicalUrl = `${BASE_URL}/newsletter/archive/${edition.edition}`;
  const description = `The AI and Tech News digest for ${formatEditionDate(edition.edition)}: ${edition.articles
    .map((article) => article.title)
    .slice(0, 3)
    .join(", ")}`;

  return {
    title: `${edition.subject} (${formatEditionDate(edition.edition)})`,
    description,
    alternates: { canonical: canonicalUrl },
    openGraph: { title: edition.subject, description, url: canonicalUrl, type: "article" },
  };
}

export default async function NewsletterEditionPage({ params }: Props) {
  const { edition: editionKey } = await params;
  const data = await getNewsletterEdition(editionKey);
  if (!data?.edition) notFound();

  const { edition } = data;
  const totalMinutes = edition.articles.reduce((sum, article) => sum + article.readingMinutes, 0);

  return (
    <div className="max-w-[820px] mx-auto px-4 lg:px-8 py-10">
      <Link href="/newsletter/archive" className="text-text-muted hover:text-white text-xs transition-colors">
        ← All editions
      </Link>

      <div className="mt-6 mb-8 pb-6 border-b border-border">
        <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">
          {formatEditionDate(edition.edition)}
        </p>
        <h1 className="text-3xl md:text-4xl font-black tracking-tight mb-3">{edition.subject}</h1>
        <p className="text-text-secondary">
          {edition.articles.length} {edition.articles.length === 1 ? "story" : "stories"}, about {totalMinutes}{" "}
          {totalMinutes === 1 ? "minute" : "minutes"} of reading.
        </p>
      </div>

      {edition.articles.map((article) => (
        <article key={article.slug} className="py-5 border-b border-border">
          <span className="text-[10px] font-bold uppercase tracking-widest text-text-muted">{article.category}</span>
          <h2 className="text-xl font-bold leading-snug mt-2 mb-1">
            <Link href={`/article/${article.slug}`} className="hover:text-accent-purple transition-colors">
              {article.title}
            </Link>
            <span className="text-text-muted text-sm font-normal whitespace-nowrap">
              {" "}
              ({article.readingMinutes} minute read)
            </span>
          </h2>
          <p className="text-text-secondary leading-relaxed">{article.excerpt}</p>
        </article>
      ))}

      <NewsletterBanner placement="archive_edition" />
    </div>
  );
}
