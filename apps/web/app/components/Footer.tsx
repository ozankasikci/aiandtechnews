import Link from "next/link";
import { NAV_ITEMS } from "../data/articles";
import { FooterNewsletterForm } from "./Newsletter";

const ABOUT_LINKS = [
  { label: "About", href: "/about" },
  { label: "Editorial Standards", href: "/about#editorial-standards" },
  { label: "Corrections", href: "/about#corrections" },
  { label: "Contact", href: "/about#contact" },
];

export function Footer() {
  return (
    <footer className="border-t border-border mt-12 py-10 px-4">
      <div className="max-w-[1400px] mx-auto">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-8 mb-8">
          <div>
            <Link href="/" className="font-black text-white text-sm uppercase tracking-tight">AI &amp; Tech News</Link>
            <p className="text-text-muted text-xs mt-3 leading-relaxed">
              Breaking AI and technology news, daily. Covering artificial intelligence, startups, big tech, and developer tools.
            </p>
          </div>
          <div>
            <h3 className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-3">Sections</h3>
            <nav className="flex flex-col gap-2 text-xs text-text-secondary">
              {NAV_ITEMS.map((item) => (
                <Link key={item} href={`/${item.toLowerCase()}`} className="hover:text-white transition-colors">{item}</Link>
              ))}
            </nav>
          </div>
          <div>
            <h3 className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-3">Company</h3>
            <nav className="flex flex-col gap-2 text-xs text-text-secondary">
              {ABOUT_LINKS.map((l) => (
                <Link key={l.label} href={l.href} className="hover:text-white transition-colors">{l.label}</Link>
              ))}
              <a href="/rss.xml" className="hover:text-white transition-colors">RSS Feed</a>
            </nav>
          </div>
          <div>
            <h3 className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-3">Newsletter</h3>
            <p className="text-text-secondary text-xs mb-3">The best AI &amp; tech stories in one concise daily digest.</p>
            <FooterNewsletterForm />
            <Link href="/newsletter/archive" className="inline-block text-text-muted hover:text-white text-xs mt-3 transition-colors">
              Browse past editions
            </Link>
          </div>
        </div>
        <div className="pt-6 border-t border-border text-text-muted text-xs">
          © 2026 AI and Tech News. All rights reserved.
        </div>
      </div>
    </footer>
  );
}
