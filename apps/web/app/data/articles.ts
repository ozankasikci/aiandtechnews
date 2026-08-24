export type Article = {
  id: number;
  slug: string;
  tag: string;
  tagColor: string;
  headline: string;
  excerpt: string;
  author: string;
  avatar: string;
  time: string;
  date: string;
  publishedAt?: string;
  updatedAt?: string;
  readTime: string;
  image: string;
  source?: string;
  sourceUrl?: string;
  metaTitle?: string;
  metaDescription?: string;
  body?: string;
};

export const CATEGORIES: Record<string, { title: string; description: string; color: string }> = {
  tech: { title: "Tech", description: "The latest tech news about hardware, apps, and more", color: "bg-accent-purple" },
  reviews: { title: "Reviews", description: "In-depth reviews of the latest gadgets and software", color: "bg-accent-blue" },
  science: { title: "Science", description: "Discoveries, space exploration, and the natural world", color: "bg-accent-green" },
  entertainment: { title: "Entertainment", description: "Movies, TV, gaming, and streaming culture", color: "bg-accent-magenta" },
  ai: { title: "AI", description: "Artificial intelligence, machine learning, and the future of computing", color: "bg-accent-purple" },
  gaming: { title: "Gaming", description: "Video games, consoles, esports, and gaming industry news", color: "bg-accent-orange" },
  creators: { title: "Creators", description: "YouTube, TikTok, podcasts, and the creator economy", color: "bg-accent-green" },
};

export const NAV_ITEMS = ["Tech", "AI", "Gaming", "Reviews", "Science", "Entertainment", "Creators"];

export const NAV_COLORS: Record<string, string> = {
  AI: "#8b5cf6",
  Gaming: "#f97316",
  Tech: "#3b82f6",
  Reviews: "#f59e0b",
  Science: "#10b981",
  Entertainment: "#ec4899",
  Creators: "#f97316",
};

export const TAG_COLORS: Record<string, string> = {
  AI: "#8b5cf6",
  Gaming: "#f97316",
  Tech: "#3b82f6",
  Reviews: "#f59e0b",
  Science: "#10b981",
  Entertainment: "#ec4899",
  Creators: "#f97316",
  News: "#9ca3af",
};

export const ALL_ARTICLES: Article[] = [];

export function getArticlesByCategory(category: string): Article[] {
  return ALL_ARTICLES.filter((a) => a.tag.toLowerCase() === category.toLowerCase());
}

export function getArticleBySlug(slug: string): Article | undefined {
  return ALL_ARTICLES.find((a) => a.slug === slug);
}
