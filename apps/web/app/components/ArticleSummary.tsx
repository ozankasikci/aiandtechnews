// The TL;DR bullets and "Why it matters" line the rewrite writes for each
// article. Older articles have neither, and then nothing is rendered.
export function ArticleSummary({ tldr, whyItMatters }: { tldr?: string[]; whyItMatters?: string }) {
  if (!tldr?.length && !whyItMatters) return null;

  return (
    <aside aria-label="Summary" className="bg-bg-card border border-border border-l-4 border-l-accent-purple rounded-sm p-5 mb-8">
      {tldr?.length ? (
        <>
          <p className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-3">TL;DR</p>
          <ul className="list-disc pl-5 space-y-2 text-[#e5e5e5] text-[15px] leading-relaxed">
            {tldr.map((point) => (
              <li key={point}>{point}</li>
            ))}
          </ul>
        </>
      ) : null}
      {whyItMatters ? (
        <p className={`text-[#e5e5e5] text-[15px] leading-relaxed${tldr?.length ? " mt-4" : ""}`}>
          <strong className="text-accent-purple">Why it matters:</strong> {whyItMatters}
        </p>
      ) : null}
    </aside>
  );
}
