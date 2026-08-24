import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Search",
  description: "Search AI and technology news articles.",
  alternates: { canonical: "https://www.aiandtech.news/search" },
  robots: { index: false, follow: true },
};

export default function SearchLayout({ children }: { children: React.ReactNode }) {
  return children;
}
