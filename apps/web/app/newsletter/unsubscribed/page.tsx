import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Newsletter unsubscribe",
  robots: { index: false, follow: false },
};

const states = {
  unsubscribed: {
    title: "You're unsubscribed",
    message: "You will no longer receive the AI & Tech News daily digest.",
  },
  already_unsubscribed: {
    title: "You're already unsubscribed",
    message: "This address is already removed from the daily digest.",
  },
  invalid: {
    title: "This unsubscribe link is not valid",
    message: "Use the unsubscribe link in your most recent newsletter, or contact us for help.",
  },
  unavailable: {
    title: "Unsubscribe is temporarily unavailable",
    message: "Please try the link again in a few minutes.",
  },
} as const;

export default async function NewsletterUnsubscribedPage({
  searchParams,
}: {
  searchParams: Promise<{ state?: string }>;
}) {
  const { state } = await searchParams;
  const key = state && state in states ? (state as keyof typeof states) : "invalid";
  const content = states[key];

  return (
    <main className="min-h-[65vh] flex items-center justify-center px-4 py-16">
      <div className="w-full max-w-xl bg-bg-card border border-border rounded-sm p-8 text-center">
        <p className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-3">Newsletter</p>
        <h1 className="text-3xl font-black mb-3">{content.title}</h1>
        <p className="text-text-secondary leading-relaxed mb-7">{content.message}</p>
        <Link href="/" className="inline-block border border-border hover:border-accent-purple transition-colors text-white font-bold px-6 py-3 rounded-sm">
          Return to AI & Tech News
        </Link>
      </div>
    </main>
  );
}
