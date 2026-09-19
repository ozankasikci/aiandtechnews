import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  images: {
    remotePatterns: [
      { protocol: "https", hostname: "**" },
    ],
    deviceSizes: [640, 828, 1080, 1200, 1600, 2800],
    imageSizes: [180, 300, 400],
    minimumCacheTTL: 2_592_000,
  },
};

export default nextConfig;
