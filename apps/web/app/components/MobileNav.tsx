"use client";

import { useState } from "react";
import Link from "next/link";
import { NAV_ITEMS } from "../data/articles";

export function MobileNav() {
  const [open, setOpen] = useState(false);

  return (
    <div className="md:hidden">
      <button
        onClick={() => setOpen(!open)}
        aria-label={open ? "Close menu" : "Open menu"}
        aria-expanded={open}
        className="flex items-center justify-center w-9 h-9 -ml-2 text-text-secondary hover:text-white transition-colors"
      >
        {open ? (
          <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
          </svg>
        ) : (
          <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        )}
      </button>

      {open && (
        <nav className="absolute left-0 right-0 top-14 bg-bg border-b border-border shadow-2xl">
          <div className="max-w-[1400px] mx-auto px-4 py-2 flex flex-col">
            {NAV_ITEMS.map((item) => (
              <Link
                key={item}
                href={`/${item.toLowerCase()}`}
                onClick={() => setOpen(false)}
                className="py-3 border-b border-border last:border-b-0 text-text-secondary hover:text-white font-medium transition-colors"
              >
                {item}
              </Link>
            ))}
          </div>
        </nav>
      )}
    </div>
  );
}
