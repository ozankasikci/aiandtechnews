import Link from "next/link";
import { NAV_ITEMS, NAV_COLORS } from "../data/articles";
import { SubscribeButton } from "./Newsletter";

export function TopBanner() {
  return (
    <div className="w-full bg-[#111] border-b border-border py-2 px-4 text-center">
      <span className="text-text-secondary text-xs tracking-wide">Reliable iOS Metrics — Sponsored</span>
    </div>
  );
}

export function Header() {
  return (
    <header className="sticky top-0 z-50 bg-bg border-b border-border">
      <div className="max-w-[1400px] mx-auto flex items-center h-14 px-4 gap-6">
        <Link href="/" className="flex items-center mr-4 shrink-0">
          <span className="text-xl md:text-2xl font-black tracking-tighter text-white uppercase" style={{ fontFamily: "var(--font-sans)" }}>
            AI &amp; Tech News
          </span>
        </Link>
        <nav className="hidden md:flex items-center gap-5 text-sm text-text-secondary">
          {NAV_ITEMS.map((item) => (
            <Link
              key={item}
              href={`/${item.toLowerCase()}`}
              className="hover:text-white transition-colors font-medium text-text-secondary"
            >
              {item}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-4">
          <SubscribeButton />
          <Link href="/search" className="text-text-secondary hover:text-white">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          </Link>
        </div>
      </div>
    </header>
  );
}
