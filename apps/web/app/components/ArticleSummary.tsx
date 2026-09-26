// The TL;DR points and "Why it matters" line the rewrite writes for each
// article, as a numbered brief. Older articles have neither, and then nothing
// is rendered.
export function ArticleSummary({ tldr, whyItMatters }: { tldr?: string[]; whyItMatters?: string }) {
  if (!tldr?.length && !whyItMatters) return null;

  return (
    <section aria-label="Summary" className="flex flex-col gap-4 bg-bg-card border border-border rounded-md px-5 py-4 md:px-6 md:py-5 mb-8">
      {tldr?.length ? (
        <>
          <div className="flex items-baseline justify-between gap-4">
            <h2 className="text-[11px] font-bold uppercase tracking-[0.14em] text-accent-purple">The short version</h2>
            <span className="text-xs text-text-muted">30-second read</span>
          </div>
          <ol className="flex flex-col gap-2.5">
            {tldr.map((point, i) => (
              <li key={point} className="grid grid-cols-[24px_1fr] md:grid-cols-[30px_1fr] items-baseline">
                <span className="text-base md:text-lg font-black text-accent-purple tabular-nums">{i + 1}</span>
                <span className="text-[15px] leading-relaxed text-[#e5e5e5]">{point}</span>
              </li>
            ))}
          </ol>
        </>
      ) : null}
      {tldr?.length && whyItMatters ? <div className="h-px bg-border" /> : null}
      {whyItMatters ? (
        <div className="grid md:grid-cols-[30px_1fr] items-start">
          <svg className="hidden md:block mt-0.5 text-accent-purple" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M5 12h14" />
            <path d="M13 6l6 6-6 6" />
          </svg>
          <div className="flex flex-col gap-1">
            <h3 className="text-[11px] font-bold uppercase tracking-[0.14em] text-accent-purple">Why it matters</h3>
            <p className="text-[15px] leading-relaxed font-medium text-white">{whyItMatters}</p>
          </div>
        </div>
      ) : null}
    </section>
  );
}
