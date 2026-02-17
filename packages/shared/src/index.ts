// Article

export interface Article {
  id: number;
  title: string;
  slug: string;
  excerpt: string;
  content: string;
  featured_image: string | null;
  category_id: number;
  author_id: number;
  status: "draft" | "published" | "scheduled";
  published_at: string | null;
  meta_title: string | null;
  meta_description: string | null;
  created_at: string;
  updated_at: string;
  author?: Author;
  category?: Category;
}

export interface CreateArticleInput {
  title: string;
  slug: string;
  excerpt: string;
  content: string;
  featured_image?: string | null;
  category_id: number;
  status: "draft" | "published" | "scheduled";
  published_at?: string | null;
  meta_title?: string | null;
  meta_description?: string | null;
}

export interface UpdateArticleInput extends Partial<CreateArticleInput> {}

// Category

export interface Category {
  id: number;
  name: string;
  slug: string;
  description: string;
  color: string;
}

export interface CreateCategoryInput {
  name: string;
  slug: string;
  description: string;
  color: string;
}

export interface UpdateCategoryInput extends Partial<CreateCategoryInput> {}

// Author

export interface Author {
  id: number;
  name: string;
  email: string;
  avatar: string | null;
  bio: string | null;
  role: "admin" | "editor";
}

export interface AuthorWithPassword extends Author {
  password_hash: string;
}

// Media

export interface Media {
  id: number;
  filename: string;
  url: string;
  mime_type: string;
  size: number;
  uploaded_at: string;
}

// Settings

export interface Settings {
  site_name: string;
  site_description: string;
  social_twitter: string | null;
  social_linkedin: string | null;
  social_github: string | null;
  newsletter_enabled: boolean;
  newsletter_provider: "none" | "mailchimp" | "convertkit" | "custom";
  newsletter_webhook_url: string | null;
}

// API responses

export interface PaginatedResponse<T> {
  articles: T[];
  total: number;
  page: number;
  totalPages: number;
}

export interface LoginResponse {
  token: string;
  user: Omit<Author, "avatar" | "bio">;
}
