// The rewrite's "Why it matters" line and TL;DR points, insight first: the
// takeaway as a pull quote, then the facts behind it. Older articles have
// neither, and then nothing is rendered.
export function ArticleSummary({ tldr, whyItMatters }: { tldr?: string[]; whyItMatters?: string }) {
  if (!tldr?.length && !whyItMatters) return null;

  return (
    <section aria-label="Summary" className="flex flex-col gap-4 pt-1 pb-6 mb-8 border-b border-border">
      {whyItMatters ? (
        <div className="grid grid-cols-[32px_1fr] md:grid-cols-[44px_1fr] items-start">
          <svg className="text-accent-purple w-[22px] md:w-[28px] h-auto mt-1" viewBox="0 0 28 22" fill="currentColor" aria-hidden="true">
            <path d="M0 22V13C0 5.8 3.6 1.4 10.8 0l1.4 3.2C8.4 4.4 6.6 7 6.4 10.6H12V22H0Zm16 0V13c0-7.2 3.6-11.6 10.8-13l1.4 3.2c-3.8 1.2-5.6 3.8-5.8 7.4H28V22H16Z" />
          </svg>
          <div className="flex flex-col gap-1.5">
            <h2 className="text-xs font-bold uppercase tracking-[0.14em] text-accent-purple">Why it matters</h2>
            <p className="text-[17px] md:text-[19px] leading-snug font-bold tracking-[-0.01em] text-white">{whyItMatters}</p>
          </div>
        </div>
      ) : null}
      {tldr?.length ? (
        <div className="grid grid-cols-[32px_1fr] md:grid-cols-[44px_1fr]">
          <span />
          <div className="flex flex-col gap-2">
            <h3 className="text-xs font-bold uppercase tracking-[0.14em] text-text-muted">The facts</h3>
            <ul className="list-disc pl-[18px] flex flex-col gap-1 marker:text-text-muted">
              {tldr.map((point) => (
                <li key={point} className="text-sm leading-relaxed text-[#aaaaaa]">
                  {point}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ) : null}
    </section>
  );
}
