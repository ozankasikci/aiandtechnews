import Link from "next/link";
import type { Article } from "../data/articles";

export function ArticleAnalysis({ analysis }: { analysis?: Article["analysis"] }) {
  if (!analysis) return null;
  return (
    <section aria-label="What this means" className="my-8 border border-border bg-bg-card rounded-sm p-5 md:p-6">
      <h2 className="text-xs font-bold uppercase tracking-widest text-accent-purple mb-4">What this means</h2>
      <p className="text-base leading-relaxed text-[#e5e5e5]">{analysis.text}</p>
      {analysis.links.length > 0 && (
        <nav aria-label="Related coverage" className="mt-5 pt-4 border-t border-border">
          <h3 className="text-xs font-bold uppercase tracking-wider text-text-muted mb-3">Related coverage</h3>
          <ul className="space-y-2">
            {analysis.links.map(({ slug, title }) => (
              <li key={slug}><Link href={`/article/${slug}`} className="text-sm font-semibold text-text-secondary hover:text-white transition-colors">{title}</Link></li>
            ))}
          </ul>
        </nav>
      )}
    </section>
  );
}
