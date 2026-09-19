import assert from "node:assert/strict";
import test from "node:test";
import nextConfig from "../../next.config";

test("image optimization limits cache-key variants and keeps them cached", () => {
  assert.deepEqual(nextConfig.images?.deviceSizes, [640, 828, 1080, 1200, 1600]);
  assert.deepEqual(nextConfig.images?.imageSizes, [180, 300, 400]);
  assert.equal(nextConfig.images?.minimumCacheTTL, 2_592_000);
});
