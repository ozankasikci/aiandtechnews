import Link from "next/link";

export function Footer() {
  return (
    <footer className="border-t border-border mt-12 py-8 px-4">
      <div className="max-w-[1400px] mx-auto flex flex-col md:flex-row items-center justify-between gap-4 text-text-muted text-xs">
        <Link href="/" className="font-black text-white text-sm uppercase tracking-tight">AI &amp; Tech News</Link>
        <nav className="flex gap-4">
          {[
            { label: "About", href: "/about" },
            { label: "Editorial Standards", href: "/about#editorial-standards" },
            { label: "Corrections", href: "/about#corrections" },
            { label: "Contact", href: "/about#contact" },
          ].map((l) => (
            <Link key={l.label} href={l.href} className="hover:text-white transition-colors">{l.label}</Link>
          ))}
        </nav>
        <span>© 2026 AI and Tech News. All rights reserved.</span>
      </div>
    </footer>
  );
}
