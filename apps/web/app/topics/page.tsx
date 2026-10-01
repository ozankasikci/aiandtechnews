import type { Metadata } from "next";
import Link from "next/link";
import { getTopics } from "../lib/api";
import { groupTopicsByKind } from "../lib/topics";

const BASE_URL = "https://www.aiandtech.news";

export const revalidate = 3600;

export const metadata: Metadata = {
  title: "AI Topics: Companies, Products and Themes",
  description: "Every company, product, person and theme we cover in AI and tech, each with a running summary and a timeline of our stories.",
  alternates: { canonical: `${BASE_URL}/topics` },
  openGraph: {
    title: "AI Topics: Companies, Products and Themes",
    description: "Every company, product, person and theme we cover in AI and tech, each with a running summary and a timeline of our stories.",
    url: `${BASE_URL}/topics`,
  },
};

export default async function TopicsIndexPage() {
  // A missing topics endpoint renders an empty index instead of failing.
  const topics = (await getTopics()) ?? [];
  const groups = groupTopicsByKind(topics);

  return (
    <main className="max-w-[1000px] mx-auto px-4 lg:px-8 py-10">
      <div className="mb-8 pb-6 border-b border-border">
        <p className="text-xs font-bold uppercase tracking-widest text-accent-purple mb-3">Topics</p>
        <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-3">Topics we follow</h1>
        <p className="text-text-secondary text-lg">
          Companies, products, people and themes in AI and tech, each with a running summary and a timeline of our coverage.
        </p>
      </div>

      {groups.length === 0 ? (
        <p className="text-text-muted py-8">Topic pages are on their way. In the meantime, browse the <Link href="/archive" className="text-accent-purple hover:underline">article archive</Link>.</p>
      ) : (
        <div className="space-y-10">
          {groups.map((group) => (
            <section key={group.kind} aria-labelledby={`kind-${group.kind}`}>
              <h2 id={`kind-${group.kind}`} className="text-xs font-bold uppercase tracking-widest text-text-muted mb-4">
                {group.label}
              </h2>
              <ul className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 divide-y sm:divide-y-0 divide-border">
                {group.topics.map((topic) => (
                  <li key={topic.slug} className="sm:border-b sm:border-border">
                    <Link href={`/topics/${topic.slug}`} className="flex items-baseline justify-between gap-3 py-3 group">
                      <span className="font-bold group-hover:text-accent-purple transition-colors">{topic.name}</span>
                      <span className="text-text-muted text-xs shrink-0">
                        {topic.articleCount} {topic.articleCount === 1 ? "story" : "stories"}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </main>
  );
}
