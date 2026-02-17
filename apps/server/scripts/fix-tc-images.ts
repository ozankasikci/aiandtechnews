import db, { initializeDatabase } from "../src/db";
initializeDatabase();

const CATEGORY_IMAGES: Record<string, string[]> = {
  ai: [
    "https://images.unsplash.com/photo-1677442136019-21780ecad995?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1620712943543-bcc4688e7485?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1655720828018-edd2daec9349?w=1200&h=630&fit=crop",
  ],
  tech: [
    "https://images.unsplash.com/photo-1518770660439-4636190af475?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1488590528505-98d2b5aba04b?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1550751827-4bd374c3f58b?w=1200&h=630&fit=crop",
  ],
  science: [
    "https://images.unsplash.com/photo-1451187580459-43490279c0fa?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1446776811953-b23d57bd21aa?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1507413245164-6160d8298b31?w=1200&h=630&fit=crop",
  ],
  programming: [
    "https://images.unsplash.com/photo-1555066931-4365d14bab8c?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1629654297299-c8506221ca97?w=1200&h=630&fit=crop",
  ],
  startups: [
    "https://images.unsplash.com/photo-1559136555-9303baea8ebd?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1553877522-43269d4ea984?w=1200&h=630&fit=crop",
  ],
  deals: [
    "https://images.unsplash.com/photo-1607082348824-0a96f2a4b9da?w=1200&h=630&fit=crop",
    "https://images.unsplash.com/photo-1472851294608-062f824d29cc?w=1200&h=630&fit=crop",
  ],
  entertainment: [
    "https://images.unsplash.com/photo-1598899134739-24c46f58b8c0?w=1200&h=630&fit=crop",
  ],
  reviews: [
    "https://images.unsplash.com/photo-1519389950473-47ba0277781c?w=1200&h=630&fit=crop",
  ],
  creators: [
    "https://images.unsplash.com/photo-1611162617213-7d7a39e9b1d7?w=1200&h=630&fit=crop",
  ],
};

// Replace tc-backlight and tc-logo images with proper ones
const articles = db.prepare(`
  SELECT a.id, a.title, a.featured_image, c.slug as cat_slug
  FROM articles a JOIN categories c ON a.category_id = c.id
  WHERE a.featured_image LIKE '%tc-backlig%' OR a.featured_image LIKE '%tc-logo%'
`).all() as Array<{ id: number; title: string; featured_image: string; cat_slug: string }>;

const update = db.prepare("UPDATE articles SET featured_image = ? WHERE id = ?");
for (const a of articles) {
  const imgs = CATEGORY_IMAGES[a.cat_slug] || CATEGORY_IMAGES.tech;
  const img = imgs[a.id % imgs.length];
  update.run(img, a.id);
  console.log(`Fixed: ${a.title.slice(0, 50)} → ${a.cat_slug} image`);
}

console.log(`Fixed ${articles.length} articles`);
