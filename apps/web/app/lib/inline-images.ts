import type { InlineImage } from "../data/articles";

// Illustrations the API places inside an article's body. Each goes after
// paragraph `afterParagraph`, counting the whole body's paragraphs from 1, so
// an image after paragraph 5 ends up below the read-next card that
// splitAfterParagraph(body, 3) puts after paragraph 3.

export const INLINE_IMAGE_CAPTION = "Illustration: AI & Tech News";

function escapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function isUsable(image: InlineImage): boolean {
  return (
    typeof image?.url === "string" &&
    /^https?:\/\//i.test(image.url) &&
    Number.isInteger(image.afterParagraph) &&
    image.afterParagraph >= 1
  );
}

// Keeps the well-formed images from the API, in body order.
export function usableInlineImages(images: unknown): InlineImage[] | undefined {
  if (!Array.isArray(images)) return undefined;
  const usable = images
    .filter((image): image is InlineImage => isUsable(image))
    .map(({ url, alt, afterParagraph }) => ({ url, alt: typeof alt === "string" ? alt : "", afterParagraph }))
    .sort((a, b) => a.afterParagraph - b.afterParagraph);
  return usable.length ? usable : undefined;
}

// A full-width 16:9 figure with a small grey credit line.
export function inlineFigure(image: InlineImage): string {
  return (
    `<figure class="my-8">` +
    `<img src="${escapeAttribute(image.url)}" alt="${escapeAttribute(image.alt)}" loading="lazy" decoding="async" width="1600" height="900" class="block w-full aspect-video object-cover rounded-[4px] bg-bg-card">` +
    `<figcaption class="mt-2 text-[13px] leading-snug text-[#777]">${INLINE_IMAGE_CAPTION}</figcaption>` +
    `</figure>`
  );
}

// Inserts each image's figure right after its paragraph. Images placed past
// the last paragraph are left out.
export function insertInlineImages(html: string, images: InlineImage[] | undefined): string {
  const usable = usableInlineImages(images);
  if (!usable) return html;
  const ends: number[] = [];
  for (let at = html.indexOf("</p>"); at >= 0; at = html.indexOf("</p>", at + 4)) ends.push(at + 4);
  let out = "";
  let cursor = 0;
  for (const image of usable) {
    const end = ends[image.afterParagraph - 1];
    if (end === undefined) continue;
    out += html.slice(cursor, end) + inlineFigure(image);
    cursor = end;
  }
  return out + html.slice(cursor);
}
