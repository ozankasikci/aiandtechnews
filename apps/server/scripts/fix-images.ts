import db from "../src/db";

const images: Record<string, string> = {
  "rise-of-ai-agents-reshaping-software-development": "https://images.unsplash.com/photo-1677442136019-21780ecad995?w=1200&h=630&fit=crop",
  "typescript-6-introduces-pattern-matching-effect-system": "https://images.unsplash.com/photo-1555066931-4365d14bab8c?w=1200&h=630&fit=crop",
  "startup-raises-50m-open-source-alternative-aws-lambda": "https://images.unsplash.com/photo-1559136555-9303baea8ebd?w=1200&h=630&fit=crop",
  "understanding-transformer-architectures-deep-dive-modern-llms": "https://images.unsplash.com/photo-1620712943543-bcc4688e7485?w=1200&h=630&fit=crop",
  "complete-guide-building-cli-tools-rust-2026": "https://images.unsplash.com/photo-1629654297299-c8506221ca97?w=1200&h=630&fit=crop",
};

const stmt = db.prepare("UPDATE articles SET featured_image = ? WHERE slug = ? AND featured_image IS NULL");
for (const [slug, img] of Object.entries(images)) {
  const r = stmt.run(img, slug);
  if (r.changes) console.log(`Updated: ${slug}`);
}
console.log("Done");
