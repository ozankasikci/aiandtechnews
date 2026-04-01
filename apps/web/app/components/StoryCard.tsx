import Image from "next/image";
import Link from "next/link";
import type { Article } from "../data/articles";

export function StoryCard({ story }: { story: Article }) {
  // Map bg-accent-* to hex color for inline hover
  const tagHex = story.tagColor
    .replace("bg-accent-purple", "#a855f7")
    .replace("bg-accent-blue", "#6366f1")
    .replace("bg-accent-green", "#22c55e")
    .replace("bg-accent-magenta", "#d946ef")
    .replace("bg-accent-orange", "#f97316")
    .replace("bg-accent-yellow", "#f59e0b");

  return (
    <Link href={`/article/${story.slug}`}>
      <article className="flex gap-4 py-5 border-b border-border group" style={{ "--tag-color": tagHex } as React.CSSProperties}>
        <div className="flex-1 min-w-0">
          <h3 className="text-lg font-bold leading-snug mb-1.5 transition-colors story-title">
            {story.headline}
          </h3>
          <span className={`inline-block px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-2 ${story.tagColor}`}>
            {story.tag}
          </span>
          <p className="text-text-secondary text-sm leading-relaxed mb-2 line-clamp-2">{story.excerpt}</p>
          <div className="flex items-center gap-2 text-text-muted text-xs">
            <span>{story.time}</span>
          </div>
        </div>
        <div className="w-[140px] h-[90px] md:w-[180px] md:h-[110px] relative rounded-sm overflow-hidden shrink-0">
          <Image src={story.image} alt="" fill className="object-cover" sizes="180px" />
        </div>
      </article>
    </Link>
  );
}
