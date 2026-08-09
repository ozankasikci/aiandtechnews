import { MetadataRoute } from "next";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: "*", allow: "/" },
    sitemap: [
      "https://www.aiandtech.news/sitemap.xml",
      "https://www.aiandtech.news/news-sitemap.xml",
    ],
  };
}
