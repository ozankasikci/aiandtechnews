import Link from "next/link";
import { NAV_ITEMS } from "./data/articles";

export default function NotFound() {
  return (
    <div className="max-w-[1400px] mx-auto px-4 lg:px-8 py-24 text-center">
      <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">404</p>
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-4">Page not found</h1>
      <p className="text-text-secondary text-lg mb-8">
        The page you&apos;re looking for doesn&apos;t exist or has been moved.
      </p>
      <div className="flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/"
          className="bg-accent-purple hover:brightness-110 transition-all text-white text-sm font-bold px-5 py-2.5 rounded-sm"
        >
          Back to homepage
        </Link>
        <Link
          href="/search"
          className="border border-border hover:border-text-muted transition-colors text-text-secondary hover:text-white text-sm font-bold px-5 py-2.5 rounded-sm"
        >
          Search articles
        </Link>
      </div>
      <nav className="mt-12 flex flex-wrap items-center justify-center gap-4 text-sm text-text-muted">
        {NAV_ITEMS.map((item) => (
          <Link key={item} href={`/${item.toLowerCase()}`} className="hover:text-white transition-colors">
            {item}
          </Link>
        ))}
      </nav>
    </div>
  );
}
