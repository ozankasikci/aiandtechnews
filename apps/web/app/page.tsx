import Image from "next/image";
import Link from "next/link";
import { MostPopularSidebar } from "./components/Sidebar";
import { StoryCard } from "./components/StoryCard";
import { ALL_ARTICLES } from "./data/articles";
import { getArticles, mapArticle } from "./lib/api";
import { TAG_COLORS } from "./data/articles";

function tagHex(tagColor: string): string {
  return tagColor
    .replace("bg-accent-purple", "#a855f7")
    .replace("bg-accent-blue", "#6366f1")
    .replace("bg-accent-green", "#22c55e")
    .replace("bg-accent-magenta", "#d946ef")
    .replace("bg-accent-orange", "#f97316")
    .replace("bg-accent-yellow", "#f59e0b");
}

const STICKER_COLORS = ["bg-accent-green", "bg-accent-magenta", "bg-accent-blue", "bg-accent-purple"];

function RotatedLogo() {
  return (
    <div className="hidden lg:flex items-start justify-center w-16 shrink-0 pt-8 sticky top-20">
      <div className="-rotate-90 whitespace-nowrap origin-center mt-24">
        <span className="text-5xl font-black tracking-[-0.06em] text-white uppercase" style={{ fontFamily: "var(--font-sans)" }}>TECHNEWS</span>
      </div>
    </div>
  );
}

export default async function Home() {
  const data = await getArticles({ limit: 12 });
  const articles = data?.articles?.length
    ? data.articles.map(mapArticle)
    : ALL_ARTICLES.slice(0, 12);

  const hero = articles[0];
  const feed = articles.slice(1, 7);
  const stickerArticles = articles.slice(7, 9);

  if (!hero) {
    return (
      <div className="max-w-[1400px] mx-auto px-4 py-16 text-center">
        <h1 className="text-3xl font-black mb-4">No articles yet</h1>
        <p className="text-text-secondary">Check back soon for the latest tech news.</p>
      </div>
    );
  }

  return (
    <div className="max-w-[1400px] mx-auto flex">
      <RotatedLogo />
      <main className="flex-1 min-w-0 px-4 lg:px-8 py-8">
        {/* Hero */}
        <section className="mb-8">
          <Link href={`/article/${hero.slug}`} className="flex flex-col md:flex-row gap-6 items-stretch group" style={{ "--tag-color": tagHex(hero.tagColor) } as React.CSSProperties}>
            <div className="flex-1 flex flex-col justify-center">
              <h1 className="text-3xl md:text-4xl lg:text-5xl font-black leading-[1.1] mb-4 tracking-tight transition-colors story-title">{hero.headline}</h1>
              <p className="text-text-secondary text-base leading-relaxed mb-3 max-w-lg">{hero.excerpt}</p>
              <span className={`inline-block w-fit px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black rounded-sm mb-3 ${hero.tagColor}`}>{hero.tag}</span>
              <div className="flex items-center gap-2 text-text-muted text-sm">
                <span>{hero.time}</span>
              </div>
            </div>
            <div className="flex-1 relative min-h-[300px] md:min-h-[400px] rounded-sm overflow-hidden">
              <div className="absolute inset-0 bg-gradient-to-br from-black/30 to-black/10 z-10 mix-blend-multiply" />
              <Image src={hero.image} alt={hero.headline} fill className="object-cover" sizes="(max-width: 768px) 100vw, 50vw" />
            </div>
          </Link>
        </section>

        <div className="flex flex-col lg:flex-row gap-8">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-[10px] font-bold uppercase tracking-widest text-text-muted">Latest</span>
              <div className="flex-1 h-px bg-border" />
            </div>
            {feed.slice(0, 3).map((s) => <StoryCard key={s.id} story={s} />)}

            {feed.length > 3 && (
              <>
                <div className="flex items-center gap-3 py-6">
                  <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted whitespace-nowrap">More Stories</h2>
                  <div className="flex-1 h-px bg-border" />
                </div>
                {feed.slice(3).map((s) => <StoryCard key={s.id} story={s} />)}
              </>
            )}
          </div>

          <div className="lg:w-[300px] shrink-0">
            <MostPopularSidebar />
            <div className="mt-8">
              {stickerArticles.map((s, i) => (
                <Link key={s.id} href={`/article/${s.slug}`} className="block relative rounded-sm overflow-hidden mb-6 group cursor-pointer">
                  <div className="relative h-[220px]">
                    <Image src={s.image} alt="" fill className="object-cover" sizes="400px" />
                  </div>
                  <div className={`${STICKER_COLORS[i % STICKER_COLORS.length]} p-4`}>
                    <span className="text-[10px] font-bold uppercase tracking-wider text-black/70 block mb-1">{s.tag}</span>
                    <h3 className="text-lg font-black text-black leading-snug">{s.headline}</h3>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
