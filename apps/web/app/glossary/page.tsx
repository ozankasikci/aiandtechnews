import Link from "next/link";
import { GLOSSARY } from "../data/glossary";

const BASE_URL = "https://www.aiandtech.news";

export const metadata = {
  title: "AI Glossary: Terms Explained in Plain English",
  description: "Short, plain definitions of the AI and tech terms used in our news coverage, from AI agents to zero-days.",
  alternates: { canonical: `${BASE_URL}/glossary` },
};

export default function GlossaryIndex() {
  return (
    <div className="max-w-3xl mx-auto px-4 lg:px-8 py-8">
      <div className="mb-8 pb-6 border-b border-border">
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-2">Glossary</h1>
        <p className="text-text-secondary text-lg">The AI and tech terms in our stories, explained in a sentence or two.</p>
      </div>
      <dl className="divide-y divide-border">
        {GLOSSARY.map((entry) => (
          <div key={entry.slug} id={entry.slug} className="py-5">
            <dt>
              <Link href={`/glossary/${entry.slug}`} className="text-lg font-bold hover:text-accent-purple transition-colors">
                {entry.term}
              </Link>
            </dt>
            <dd className="text-[#e5e5e5] text-[15px] leading-relaxed mt-1">{entry.definition}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
