import type { NextConfig } from "next";

const nextConfig: NextConfig = {
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
