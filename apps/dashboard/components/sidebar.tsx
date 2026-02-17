"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";
import { useAuth } from "@/contexts/auth-context";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  FileText,
  FolderOpen,
  Image,
  Settings,
  LogOut,
  Menu,
  X,
  Newspaper,
} from "lucide-react";

const navItems = [
  { href: "/articles", label: "Articles", icon: FileText },
  { href: "/categories", label: "Categories", icon: FolderOpen },
  { href: "/media", label: "Media", icon: Image },
  { href: "/settings", label: "Settings", icon: Settings },
];

export function Sidebar() {
  const pathname = usePathname();
  const [mobileOpen, setMobileOpen] = useState(false);
  const { user, logout } = useAuth();

  return (
    <>
      {/* Mobile hamburger button */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed top-4 left-4 z-50 lg:hidden rounded-md bg-[var(--bg-secondary)] border border-[var(--border-primary)] p-2 hover:bg-[var(--bg-hover)] transition-colors cursor-pointer"
      >
        <Menu className="h-5 w-5" />
      </button>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/60 lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          "fixed top-0 left-0 z-50 h-full w-[240px] bg-[var(--bg-secondary)] border-r border-[var(--border-primary)] flex flex-col transition-transform duration-200",
          "lg:translate-x-0",
          mobileOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0"
        )}
      >
        {/* Mobile close button */}
        <button
          onClick={() => setMobileOpen(false)}
          className="absolute top-4 right-4 lg:hidden text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer"
        >
          <X className="h-5 w-5" />
        </button>

        {/* Logo */}
        <div className="px-5 py-6 flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--accent-primary)]">
            <Newspaper className="h-4 w-4 text-white" />
          </div>
          <div>
            <span className="text-base font-bold text-[var(--text-primary)]">
              TechNews
            </span>
            <span className="ml-1.5 text-[10px] font-medium uppercase tracking-wider text-[var(--text-tertiary)] bg-[var(--bg-hover)] px-1.5 py-0.5 rounded">
              CMS
            </span>
          </div>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-3 py-2 space-y-1">
          {navItems.map((item) => {
            const isActive = pathname.startsWith(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={() => setMobileOpen(false)}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-[var(--bg-hover)] text-[var(--accent-primary)]"
                    : "text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]"
                )}
              >
                <item.icon
                  className={cn(
                    "h-4 w-4",
                    isActive
                      ? "text-[var(--accent-primary)]"
                      : "text-[var(--text-tertiary)]"
                  )}
                />
                {item.label}
              </Link>
            );
          })}
        </nav>

        {/* User section */}
        <div className="border-t border-[var(--border-primary)] px-3 py-4">
          <div className="flex items-center gap-3">
            <Avatar className="h-8 w-8">
              <AvatarFallback className="bg-[var(--accent-primary)] text-white text-xs font-semibold">
                {user?.name?.charAt(0)?.toUpperCase() || 'U'}
              </AvatarFallback>
            </Avatar>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-[var(--text-primary)] truncate">
                {user?.name || 'User'}
              </p>
              <p className="text-xs text-[var(--text-tertiary)] capitalize">
                {user?.role || 'User'}
              </p>
            </div>
            <button
              onClick={logout}
              className="rounded-md p-1.5 text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-colors"
              title="Logout"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}
