import type { Metadata } from "next";
import Link from "next/link";
import { ConfirmationTracker } from "../ConfirmationTracker";

export const metadata: Metadata = {
  title: "Newsletter confirmation",
  robots: { index: false, follow: false },
};

const states = {
  confirmed: {
    title: "You're subscribed",
    message: "Your email is confirmed. The next AI & Tech News digest will arrive in your inbox.",
  },
  already_confirmed: {
    title: "You're already subscribed",
    message: "This email address is already confirmed. There is nothing else you need to do.",
  },
  invalid: {
    title: "This confirmation link is not valid",
    message: "The link may have expired. Enter your email on the site to request a new confirmation.",
  },
  unavailable: {
    title: "Confirmation is temporarily unavailable",
    message: "Please try the link again in a few minutes.",
  },
} as const;

export default async function NewsletterConfirmedPage({
  searchParams,
}: {
  searchParams: Promise<{ state?: string }>;
}) {
  const { state } = await searchParams;
  const key = state && state in states ? (state as keyof typeof states) : "invalid";
  const content = states[key];

  return (
    <main className="min-h-[65vh] flex items-center justify-center px-4 py-16">
      {key === "confirmed" && <ConfirmationTracker />}
      <div className="w-full max-w-xl bg-bg-card border border-border rounded-sm p-8 text-center">
        <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">Newsletter</p>
        <h1 className="text-3xl font-black mb-3">{content.title}</h1>
        <p className="text-text-secondary leading-relaxed mb-7">{content.message}</p>
        <Link href="/" className="inline-block bg-accent-purple hover:brightness-110 transition-all text-white font-bold px-6 py-3 rounded-sm">
          Read the latest stories
        </Link>
      </div>
    </main>
  );
}
