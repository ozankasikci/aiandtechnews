import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const layoutSource = readFileSync(new URL("../layout.tsx", import.meta.url), "utf8");

test("site metadata permits large image previews for search and Discover", () => {
  assert.match(layoutSource, /["']max-image-preview["']:\s*["']large["']/);
  assert.match(layoutSource, /["']max-snippet["']:\s*-1/);
  assert.match(layoutSource, /["']max-video-preview["']:\s*-1/);
});
