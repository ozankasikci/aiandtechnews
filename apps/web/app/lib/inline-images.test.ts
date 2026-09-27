import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { INLINE_IMAGE_CAPTION, inlineFigure, insertInlineImages, usableInlineImages } from "./inline-images";
import { splitAfterParagraph } from "./read-next";

const paragraphs = (n: number) =>
  Array.from({ length: n }, (_, i) => `<p>Paragraph ${i + 1}.</p>`).join("");
const image = { url: "https://img.test/features/x-inline.webp", alt: "Engineers roll in a rack.", afterParagraph: 5 };

test("the figure goes right after its paragraph, counted over the whole body", () => {
  const body = "<p>One.</p><h2>Heading</h2><p>Two.</p><p>Three.</p><p>Four.</p><p>Five.</p><h2>Next</h2><p>Six.</p><p>Seven.</p>";
  const html = insertInlineImages(body, [image]);
  assert.ok(html.startsWith("<p>One.</p><h2>Heading</h2><p>Two.</p><p>Three.</p><p>Four.</p><p>Five.</p><figure"));
  assert.ok(html.endsWith("</figure><h2>Next</h2><p>Six.</p><p>Seven.</p>"));
  assert.equal(html.match(/<figure/g)?.length, 1);
});

test("an image after paragraph 4 or later lands below the read-next split", () => {
  const html = insertInlineImages(paragraphs(8), [{ ...image, afterParagraph: 4 }]);
  const [start, rest] = splitAfterParagraph(html, 3);
  assert.doesNotMatch(start, /<figure/);
  assert.ok(rest.startsWith("<p>Paragraph 4.</p><figure"));
});

test("the body is unchanged without usable images", () => {
  const body = paragraphs(6);
  assert.equal(insertInlineImages(body, undefined), body);
  assert.equal(insertInlineImages(body, []), body);
  assert.equal(insertInlineImages(body, [{ ...image, afterParagraph: 7 }]), body, "past the last paragraph");
  assert.equal(insertInlineImages(body, [{ ...image, afterParagraph: 0 }]), body);
  assert.equal(insertInlineImages(body, [{ ...image, url: "javascript:alert(1)" }]), body);
});

test("several images go in body order", () => {
  const html = insertInlineImages(paragraphs(8), [
    { ...image, url: "https://img.test/b.webp", afterParagraph: 6 },
    { ...image, url: "https://img.test/a.webp", afterParagraph: 4 },
  ]);
  const a = html.indexOf("https://img.test/a.webp");
  const b = html.indexOf("https://img.test/b.webp");
  assert.ok(html.indexOf("Paragraph 4.") < a && a < html.indexOf("Paragraph 5.") && html.indexOf("Paragraph 6.") < b && b < html.indexOf("Paragraph 7."));
});

test("the figure is a lazy full-width 16:9 image with its alt and a grey credit", () => {
  const figure = inlineFigure({ ...image, alt: 'A "quoted" <scene> & more' });
  assert.match(figure, /<img src="https:\/\/img\.test\/features\/x-inline\.webp"/);
  assert.match(figure, /alt="A &quot;quoted&quot; &lt;scene&gt; &amp; more"/);
  assert.match(figure, /loading="lazy"/);
  for (const token of ["w-full", "aspect-video", "object-cover", "rounded-[4px]", "text-[13px]", "text-[#777]"]) {
    assert.ok(figure.includes(token), token);
  }
  assert.match(figure, new RegExp(`<figcaption[^>]*>${INLINE_IMAGE_CAPTION}</figcaption>`));
  assert.equal(INLINE_IMAGE_CAPTION, "Illustration: AI & Tech News");
});

test("only well-formed API images are kept", () => {
  assert.equal(usableInlineImages(undefined), undefined);
  assert.equal(usableInlineImages([]), undefined);
  assert.deepEqual(usableInlineImages([{ url: "https://img.test/a.webp", afterParagraph: 5 }, { url: "", alt: "x", afterParagraph: 4 }]), [
    { url: "https://img.test/a.webp", alt: "", afterParagraph: 5 },
  ]);
});

test("the article page inserts inline images before splitting for the read-next card", () => {
  const page = readFileSync(new URL("../article/[slug]/page.tsx", import.meta.url), "utf8");
  const api = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
  assert.match(page, /const body = insertInlineImages\(glossary\.html, article\.inlineImages\);/);
  assert.ok(page.indexOf("insertInlineImages(glossary.html") < page.indexOf("readNextSlot(body)"));
  assert.match(api, /inlineImages: usableInlineImages\(a\.inlineImages\),/);
});
