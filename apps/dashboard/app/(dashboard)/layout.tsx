"use client";

import { useAuth } from "@/contexts/auth-context";
import { Sidebar } from "@/components/sidebar";
import { ToastContainer } from "@/components/toast";
import { Loader2 } from "lucide-react";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Loading...</p>
        </div>
      </div>
    );
  }

  if (!user) {
    // Auth context will redirect to login, but show loading in case
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto mb-4" />
          <p className="text-sm text-[var(--text-secondary)]">Redirecting to login...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen">
      <Sidebar />
      <main className="lg:pl-[240px]">
        <div className="mx-auto max-w-[1200px] px-4 py-8 pt-16 lg:px-8 lg:pt-8">
          {children}
        </div>
      </main>
      <ToastContainer />
    </div>
  );
}
