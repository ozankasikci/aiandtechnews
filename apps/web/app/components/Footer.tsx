import Link from "next/link";

export function Footer() {
  return (
    <footer className="border-t border-border mt-12 py-8 px-4">
      <div className="max-w-[1400px] mx-auto flex flex-col md:flex-row items-center justify-between gap-4 text-text-muted text-xs">
        <Link href="/" className="font-black text-white text-sm uppercase tracking-tight">TechNews</Link>
        <nav className="flex gap-4">
          {[
            { label: "About", href: "/about" },
            { label: "Ethics Statement", href: "#" },
            { label: "Contact", href: "/about" },
            { label: "Tip Us", href: "#" },
            { label: "Community Guidelines", href: "#" },
            { label: "Privacy Policy", href: "#" },
          ].map((l) => (
            <Link key={l.label} href={l.href} className="hover:text-white transition-colors">{l.label}</Link>
          ))}
        </nav>
        <span>© 2026 TechNews. All rights reserved.</span>
      </div>
    </footer>
  );
}
