// Only images we host (or a licensed stock library) may appear on the site.
// Older articles still point at the news source's own photo; those are never
// shown, since the site never shows or copies its sources.

export const DEFAULT_ARTICLE_IMAGE = "/images/default-article.jpg";

const OWN_IMAGE_HOSTS = new Set([
  "aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com",
  "www.aiandtech.news",
  "aiandtech.news",
  // Unsplash photos are free to use under the Unsplash licence.
  "images.unsplash.com",
]);

export function isOwnImage(url: string | null | undefined): boolean {
  if (!url) return false;
  if (url.startsWith("/") && !url.startsWith("//")) return true;
  try {
    const parsed = new URL(url);
    return parsed.protocol === "https:" && OWN_IMAGE_HOSTS.has(parsed.hostname);
  } catch {
    return false;
  }
}

// The image to show for an article: its own image, or the site default.
export function ownImageOr(url: string | null | undefined, fallback = DEFAULT_ARTICLE_IMAGE): string {
  return isOwnImage(url) ? (url as string) : fallback;
}
