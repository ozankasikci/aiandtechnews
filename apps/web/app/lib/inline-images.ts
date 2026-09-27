import type { InlineImage } from "../data/articles";

// Images (illustrations or credited photos) the API places inside an article's body. Each goes after
// paragraph `afterParagraph`, counting the whole body's paragraphs from 1, so
// an image after paragraph 5 ends up below the read-next card that
// splitAfterParagraph(body, 3) puts after paragraph 3.

// The caption of a generated illustration; a real photo shows its credit.
export const INLINE_IMAGE_CAPTION = "Illustration: AI & Tech News";

function escapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function escapeText(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

const isHttpUrl = (value: unknown): value is string => typeof value === "string" && /^https?:\/\//i.test(value);
const nonEmpty = (value: unknown): value is string => typeof value === "string" && value.trim() !== "";

function isUsable(image: InlineImage): boolean {
  return (
    isHttpUrl(image?.url) &&
    Number.isInteger(image.afterParagraph) &&
    image.afterParagraph >= 1
  );
}

// Keeps the well-formed images from the API, in body order, with a credit
// only when it has text (and links only to http(s) URLs).
export function usableInlineImages(images: unknown): InlineImage[] | undefined {
  if (!Array.isArray(images)) return undefined;
  const usable = images
    .filter((image): image is InlineImage => isUsable(image))
    .map(({ url, alt, afterParagraph, credit, creditUrl, license, licenseUrl }) => {
      const image: InlineImage = { url, alt: typeof alt === "string" ? alt : "", afterParagraph };
      if (nonEmpty(credit)) {
        image.credit = credit.trim();
        if (isHttpUrl(creditUrl)) image.creditUrl = creditUrl;
        if (nonEmpty(license)) image.license = license.trim();
        if (nonEmpty(license) && isHttpUrl(licenseUrl)) image.licenseUrl = licenseUrl;
      }
      return image;
    })
    .sort((a, b) => a.afterParagraph - b.afterParagraph);
  return usable.length ? usable : undefined;
}

function creditLink(text: string, href: string | undefined): string {
  if (!href) return escapeText(text);
  return `<a href="${escapeAttribute(href)}" target="_blank" rel="nofollow noopener" class="underline underline-offset-2 hover:text-[#444]">${escapeText(text)}</a>`;
}

// The caption: a photo's credit, linked to its page, with a Commons licence
// linked to the licence ("Photo: Jane Doe / CC BY-SA 4.0, via Wikimedia
// Commons"); otherwise the illustration credit.
export function inlineCaption(image: InlineImage): string {
  const { credit, creditUrl, license, licenseUrl } = image;
  if (!nonEmpty(credit)) return INLINE_IMAGE_CAPTION;
  const at = license && licenseUrl ? credit.lastIndexOf(license) : -1;
  if (license && licenseUrl && at > 0) {
    const head = credit.slice(0, at);
    const tail = credit.slice(at + license.length);
    const separator = head.endsWith(" / ") ? " / " : "";
    return creditLink(head.slice(0, head.length - separator.length), creditUrl) + separator + creditLink(license, licenseUrl) + escapeText(tail);
  }
  return creditLink(credit, creditUrl);
}

// A full-width 16:9 figure with a small grey credit line.
export function inlineFigure(image: InlineImage): string {
  return (
    `<figure class="my-8">` +
    `<img src="${escapeAttribute(image.url)}" alt="${escapeAttribute(image.alt)}" loading="lazy" decoding="async" width="1600" height="900" class="block w-full aspect-video object-cover rounded-[4px] bg-bg-card">` +
    `<figcaption class="mt-2 text-[13px] leading-snug text-[#777]">${inlineCaption(image)}</figcaption>` +
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
