import Link from "next/link";
import { notFound } from "next/navigation";
import { GLOSSARY } from "../../data/glossary";
import { getArticlesUpTo, mapArticle } from "../../lib/api";
import { glossaryTerm, mentionsTerm } from "../../lib/glossary";

type Props = { params: Promise<{ slug: string }> };

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 3600;

export function generateStaticParams() {
  return GLOSSARY.map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: Props) {
  const entry = glossaryTerm((await params).slug);
  if (!entry) return { title: "Not Found" };
  return {
    title: `What is ${entry.term}? Meaning and latest news`,
    description: entry.definition,
    alternates: { canonical: `${BASE_URL}/glossary/${entry.slug}` },
  };
}

export default async function GlossaryTermPage({ params }: Props) {
  const entry = glossaryTerm((await params).slug);
  if (!entry) notFound();

  // Stories that mention the term, newest first, from the latest 200.
  const recent = (await getArticlesUpTo(200)) ?? [];
  const stories = recent
    .filter((a) => mentionsTerm(entry, `${a.title} ${a.content.replace(/<[^>]+>/g, " ")}`))
    .slice(0, 12)
    .map(mapArticle);

  return (
    <div className="max-w-3xl mx-auto px-4 lg:px-8 py-8">
      <p className="text-sm text-text-muted mb-3">
        <Link href="/glossary" className="text-accent-purple hover:underline">Glossary</Link> / {entry.term}
      </p>
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-4">{entry.term}</h1>
      <p className="text-[#e5e5e5] text-lg leading-relaxed mb-3">{entry.definition}</p>
      {entry.aliases.length > 0 && <p className="text-text-muted text-sm">Also written as: {entry.aliases.join(", ")}</p>}

      <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mt-10 mb-2 pb-2 border-b border-border">In the news</h2>
      {stories.length ? (
        <ul>
          {stories.map((story) => (
            <li key={story.slug} className="py-4 border-b border-border">
              <Link href={`/article/${story.slug}`} className="group block">
                <span className="block font-bold leading-snug group-hover:text-accent-purple transition-colors">{story.headline}</span>
                <span className="text-text-muted text-xs">{story.time}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-text-secondary py-4">No recent stories mention this term.</p>
      )}
    </div>
  );
}
