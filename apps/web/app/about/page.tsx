import type { Metadata } from "next";
import Link from "next/link";

const CONTACT_URL = "https://github.com/ozankasikci/aiandtechnews/issues/new";

export const metadata: Metadata = {
  title: "About",
  description: "How AI and Tech News selects stories, drafts articles with AI assistance, checks facts, and corrects errors.",
  alternates: { canonical: "/about" },
};

export default function AboutPage() {
  return (
    <div className="max-w-[800px] mx-auto px-4 lg:px-8 py-12">
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-6">About AI and Tech News</h1>

      <div className="border-b border-border pb-8 mb-10 space-y-4">
        <p className="text-text-secondary text-lg leading-relaxed">
          AI and Tech News publishes concise coverage of artificial intelligence, software, hardware,
          cybersecurity, robotics, internet platforms, and technology startups.
        </p>
        <p className="text-text-secondary text-lg leading-relaxed">
          Our goal is to explain what happened, preserve the important facts, and provide useful context
          without hype, clickbait, or invented details.
        </p>
      </div>

      <section id="editorial-standards" className="mb-12 scroll-mt-24">
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">
          How we report
        </h2>
        <div className="bg-bg-card rounded-sm p-6 space-y-4 text-text-secondary leading-relaxed">
          <p>
            Articles are published under the name <span className="font-semibold text-white">TechNews Editorial</span>.
            We find stories by monitoring company blogs and newsrooms, research labs, regulators,
            open-source releases, and other technology reporting.
          </p>
          <p>
            Our editorial process selects significant stories. We prefer original reports based on primary
            sources. Rewrites of other technology reporting are reserved for major stories. We skip stories
            with little significance, promotional items, and duplicates.
          </p>
          <p>
            Articles are drafted from the source material with the help of AI models. A second automated
            check compares every article against its source before publication. Headlines and facts must be
            supported by the source. We do not invent quotes or numbers. Stories that cannot support a
            complete and accurate article are skipped.
          </p>
          <p>
            Illustrations are original artwork, not copied from other sites. Real photos are used only with
            a licence that permits their use and are credited.
          </p>
        </div>
      </section>

      <section id="corrections" className="mb-12 scroll-mt-24">
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">
          Corrections
        </h2>
        <div className="bg-bg-card rounded-sm p-6 space-y-4 text-text-secondary leading-relaxed">
          <p>
            To report a factual error, include the article URL, the exact statement that needs review, and a
            reliable supporting source. We review reports, fix confirmed errors, and update the article.
          </p>
          <Link
            href={CONTACT_URL}
            target="_blank"
            rel="noreferrer"
            className="inline-flex text-accent-purple hover:text-white font-semibold transition-colors"
          >
            Report a correction
          </Link>
        </div>
      </section>

      <section id="contact" className="scroll-mt-24">
        <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">
          Contact
        </h2>
        <div className="bg-bg-card rounded-sm p-6 space-y-4 text-text-secondary leading-relaxed">
          <p>
            General editorial questions, source concerns, and site feedback can be submitted through the public
            project issue tracker.
          </p>
          <Link
            href={CONTACT_URL}
            target="_blank"
            rel="noreferrer"
            className="inline-flex text-accent-purple hover:text-white font-semibold transition-colors"
          >
            Contact AI and Tech News
          </Link>
        </div>
      </section>
    </div>
  );
}
