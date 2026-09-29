import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { parseApiDate } from "../lib/dates";
import { archiveHref, getArchivePage, parseArchivePage } from "./archive-page";

type Props = { searchParams: Promise<{ page?: string | string[] }> };

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 300;

export async function generateMetadata({ searchParams }: Props): Promise<Metadata> {
  const { page: rawPage } = await searchParams;
  const page = parseArchivePage(rawPage);
  if (page === null) return { title: "Archive page not found", robots: { index: false } };

  const title = page === 1 ? "AI News Archive" : `AI News Archive, Page ${page}`;
  const description = page === 1
    ? "Browse every published AI and technology news story from AI and Tech News."
    : `Browse page ${page} of the AI and Tech News article archive, with earlier AI and technology stories.`;
  const canonicalUrl = `${BASE_URL}${archiveHref(page)}`;

  return {
    title,
    description,
    alternates: { canonical: canonicalUrl },
    openGraph: { title, description, url: canonicalUrl },
  };
}

export default async function ArchivePage({ searchParams }: Props) {
  const { page: rawPage } = await searchParams;
  const result = await getArchivePage(rawPage);
  if (result.state === "missing") notFound();
  if (result.state === "unavailable") throw new Error("Article archive backend unavailable");

  const { articles, page, total, totalPages } = result;

  return (
    <main className="max-w-[1000px] mx-auto px-4 lg:px-8 py-10">
      <div className="mb-8 pb-6 border-b border-border">
        <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">All stories</p>
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-3">AI news archive</h1>
        <p className="text-text-secondary text-lg">
          Browse the latest AI and technology news, then follow the pages back through our earlier coverage.
        </p>
        {total > 0 && (
          <p className="text-text-muted text-sm mt-4">
            {total.toLocaleString("en-US")} stories · Page {page} of {totalPages}
          </p>
        )}
      </div>

      {articles.length === 0 ? (
        <p className="text-text-muted py-8">No stories have been published yet.</p>
      ) : (
        <ol className="divide-y divide-border">
          {articles.map((article) => {
            const date = parseApiDate(article.published_at);

            return (
              <li key={article.id} className="py-5">
                <article>
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted mb-2">
                    <span className="font-bold uppercase tracking-wider text-accent-purple">
                      {article.category?.name || "News"}
                    </span>
                    {date && (
                      <time dateTime={date.toISOString()}>
                        {date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" })}
                      </time>
                    )}
                  </div>
                  <h2 className="text-xl md:text-2xl font-bold leading-snug tracking-tight">
                    <Link href={`/article/${article.slug}`} className="hover:text-accent-purple transition-colors">
                      {article.title}
                    </Link>
                  </h2>
                  {article.excerpt && <p className="text-text-secondary text-sm leading-relaxed mt-2">{article.excerpt}</p>}
                </article>
              </li>
            );
          })}
        </ol>
      )}

      {totalPages > 1 && (
        <nav aria-label="Article archive pages" className="flex flex-wrap items-center gap-3 mt-8 pt-6 border-t border-border text-sm font-bold">
          {page > 1 && (
            <>
              <Link href={archiveHref(1)} className="text-text-secondary hover:text-white transition-colors">First page</Link>
              <Link href={archiveHref(page - 1)} rel="prev" className="px-4 py-2 border border-border rounded-sm hover:border-accent-purple transition-colors">Previous</Link>
            </>
          )}
          <span className="text-text-muted font-normal">Page {page} of {totalPages}</span>
          {page < totalPages && (
            <Link href={archiveHref(page + 1)} rel="next" className="px-4 py-2 border border-border rounded-sm hover:border-accent-purple transition-colors">Next</Link>
          )}
        </nav>
      )}
    </main>
  );
}
