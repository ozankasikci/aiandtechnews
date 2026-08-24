"use client";

import Link from "next/link";

export default function Error({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div className="max-w-[1400px] mx-auto px-4 lg:px-8 py-24 text-center">
      <p className="text-[10px] font-bold uppercase tracking-widest text-accent-orange mb-3">Error</p>
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-4">Something went wrong</h1>
      <p className="text-text-secondary text-lg mb-8">
        An unexpected error occurred while loading this page. It&apos;s not you, it&apos;s us.
      </p>
      <div className="flex flex-wrap items-center justify-center gap-3">
        <button
          onClick={reset}
          className="bg-accent-purple hover:brightness-110 transition-all text-white text-sm font-bold px-5 py-2.5 rounded-sm"
        >
          Try again
        </button>
        <Link
          href="/"
          className="border border-border hover:border-text-muted transition-colors text-text-secondary hover:text-white text-sm font-bold px-5 py-2.5 rounded-sm"
        >
          Back to homepage
        </Link>
      </div>
    </div>
  );
}
