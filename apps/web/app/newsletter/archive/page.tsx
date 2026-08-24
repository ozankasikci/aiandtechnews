import type { Metadata } from "next";
import Link from "next/link";
import { NewsletterBanner } from "../../components/Newsletter";
import { getNewsletterEditions } from "../../lib/api";
import { formatEditionDate } from "../edition-date";

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 300;

export const metadata: Metadata = {
  title: "Newsletter Archive",
  description: "Every edition of the AI and Tech News daily digest.",
  alternates: { canonical: `${BASE_URL}/newsletter/archive` },
  openGraph: {
    title: "Newsletter Archive",
    description: "Every edition of the AI and Tech News daily digest.",
    url: `${BASE_URL}/newsletter/archive`,
  },
};

export default async function NewsletterArchivePage() {
  const data = await getNewsletterEditions();
  const editions = data?.editions || [];

  return (
    <div className="max-w-[820px] mx-auto px-4 lg:px-8 py-10">
      <div className="mb-8 pb-6 border-b border-border">
        <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">Newsletter</p>
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-3">Archive</h1>
        <p className="text-text-secondary text-lg">
          Every edition of the daily digest. One email each morning with the AI and technology stories worth knowing.
        </p>
      </div>

      <NewsletterBanner placement="archive_index" />

      {editions.length === 0 ? (
        <p className="text-text-muted py-8">No editions have been published yet.</p>
      ) : (
        <ul className="mt-4">
          {editions.map((edition) => (
            <li key={edition.edition} className="border-b border-border">
              <Link href={`/newsletter/archive/${edition.edition}`} className="block py-5 group">
                <span className="text-text-muted text-xs uppercase tracking-wider">
                  {formatEditionDate(edition.edition)}
                </span>
                <h2 className="text-lg font-bold leading-snug mt-1 group-hover:text-accent-purple transition-colors">
                  {edition.subject}
                </h2>
                <span className="text-text-muted text-xs">
                  {edition.articles.length} {edition.articles.length === 1 ? "story" : "stories"}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
