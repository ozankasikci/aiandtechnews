import { writeFileSync } from "fs";
import { join, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const API_URL = process.env.NEXT_PUBLIC_API_URL?.trim() || "http://localhost:4001";

async function main() {
  try {
    const res = await fetch(`${API_URL}/api/articles?limit=500`);
    const data = await res.json();
    const slim = {
      total: data.total,
      articles: (data.articles || []).map((a) => ({
        id: a.id,
        slug: a.slug,
        title: a.title,
        excerpt: a.excerpt,
        featured_image: a.featured_image,
        published_at: a.published_at,
        category: {
          name: a.category?.name,
          slug: a.category?.slug,
          color: a.category?.color,
        },
        author: {
          name: a.author?.name,
          avatar: a.author?.avatar,
        },
      })),
    };
    const dest = join(__dirname, "../public/articles-data.json");
    writeFileSync(dest, JSON.stringify(slim));
    console.log(`✓ Generated articles-data.json (${slim.articles.length} articles)`);
  } catch (err) {
    console.warn("⚠ Could not fetch articles, using existing data:", err.message);
  }
}

main();
