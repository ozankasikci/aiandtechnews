import assert from "node:assert/strict";
import test from "node:test";
import nextConfig from "../../next.config";

test("image optimization limits cache-key variants and keeps them cached", () => {
  assert.deepEqual(nextConfig.images?.deviceSizes, [640, 828, 1080, 1200, 1600, 2800]);
  assert.deepEqual(nextConfig.images?.imageSizes, [180, 300, 400]);
  assert.equal(nextConfig.images?.minimumCacheTTL, 2_592_000);
});

test("the article hero stays sharp at its maximum width on 2x displays", () => {
  const articleHeroMaxCssWidth = 1_400;
  const targetPixelDensity = 2;
  const largestCandidate = Math.max(...(nextConfig.images?.deviceSizes ?? []));

  assert.ok(largestCandidate >= articleHeroMaxCssWidth * targetPixelDensity);
});
