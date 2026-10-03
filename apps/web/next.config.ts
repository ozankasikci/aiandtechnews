import path from "node:path";
import type { NextConfig } from "next";

// The Mac mini fallback builds with NEXT_STANDALONE=1 to get a self-contained
// server.js it can run from the internal disk. Vercel builds are unchanged.
const standalone = process.env.NEXT_STANDALONE === "1";

const nextConfig: NextConfig = {
  ...(standalone
    ? { output: "standalone" as const, outputFileTracingRoot: path.join(__dirname) }
    : {}),
  images: {
    // Serve images as-is: featured images are already compressed WebP on S3,
    // and Vercel's optimizer has a monthly quota that, once used up, answers
    // 402 and leaves new images broken.
    unoptimized: true,
    remotePatterns: [
      { protocol: "https", hostname: "**" },
    ],
    deviceSizes: [640, 828, 1080, 1200, 1600, 2800],
    imageSizes: [180, 300, 400],
    minimumCacheTTL: 2_592_000,
  },
};

export default nextConfig;
