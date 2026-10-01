import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { DEFAULT_ARTICLE_IMAGE, isOwnImage, ownImageOr } from "./own-image";

test("only our own or licensed stock images are shown; source photos fall back to the default", () => {
  assert.equal(isOwnImage("https://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/features/x.webp"), true);
  assert.equal(isOwnImage("/images/generated/x.jpg"), true);
  assert.equal(isOwnImage("https://images.unsplash.com/photo-1?w=800"), true);
  for (const source of [
    "https://techcrunch.com/wp-content/uploads/2026/02/x.jpg",
    "https://platform.theverge.com/wp-content/uploads/x.jpg",
    "https://cdn.arstechnica.net/wp-content/uploads/x.png",
    "//techcrunch.com/x.jpg",
    "http://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/x.webp",
    "",
  ]) {
    assert.equal(isOwnImage(source), false, source);
    assert.equal(ownImageOr(source), DEFAULT_ARTICLE_IMAGE);
  }
});

test("every place that shows an article image goes through the own-image check", () => {
  const api = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
  const fallback = readFileSync(new URL("./fallback.ts", import.meta.url), "utf8");
  const search = readFileSync(new URL("../search/page.tsx", import.meta.url), "utf8");
  assert.match(api, /image: ownImageOr\(a\.featured_image\)/);
  assert.match(fallback, /image: ownImageOr\(entry\.featured_image\)/);
  assert.match(search, /ownImageOr\(a\.featured_image\)/);
});
