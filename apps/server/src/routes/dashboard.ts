import { Router, Request, Response } from "express";
import bcrypt from "bcryptjs";
import db from "../db";
import { generateToken, requireAuth } from "../auth";
import { upload } from "../upload";
import fs from "fs";
import path from "path";
import {
  EDITORIAL_AUTHOR,
  normalizeSourceUrl,
  sourceForUrl,
  validateRewrittenArticle,
} from "../news-policy";

const router: ReturnType<typeof Router> = Router();

// ─── Auth ────────────────────────────────────────────────────────────────────

router.post("/auth/login", (req: Request, res: Response) => {
  const { email, password } = req.body;
  if (!email || !password) {
    res.status(400).json({ error: "Email and password are required" });
    return;
  }

  const user = db
    .prepare("SELECT * FROM authors WHERE email = ?")
    .get(email) as Record<string, unknown> | undefined;

  if (!user || !bcrypt.compareSync(password, user.password_hash as string)) {
    res.status(401).json({ error: "Invalid email or password" });
    return;
  }

  const token = generateToken({
    id: user.id as number,
    email: user.email as string,
    role: user.role as string,
  });

  res.json({
    token,
    user: {
      id: user.id,
      name: user.name,
      email: user.email,
      role: user.role,
    },
  });
});

router.get("/auth/me", requireAuth, (req: Request, res: Response) => {
  // requireAuth middleware sets req.user
  res.json({ user: req.user });
});

router.post("/auth/logout", requireAuth, (_req: Request, res: Response) => {
  // For JWT tokens, logout is handled client-side by removing the token
  // But we can still acknowledge the logout
  res.json({ success: true });
});

// ─── All routes below require auth ───────────────────────────────────────────
router.use(requireAuth);

// ─── Dashboard Articles ──────────────────────────────────────────────────────

router.get("/dashboard/articles", (req: Request, res: Response) => {
  const page = Math.max(1, parseInt(req.query.page as string) || 1);
  const limit = Math.min(50, Math.max(1, parseInt(req.query.limit as string) || 20));
  const offset = (page - 1) * limit;
  const status = req.query.status as string | undefined;
  const category = req.query.category as string | undefined;
  const search = req.query.search as string | undefined;

  let where = "WHERE 1=1";
  const params: unknown[] = [];

  if (status && ["draft", "published", "scheduled"].includes(status)) {
    where += " AND a.status = ?";
    params.push(status);
  }

  if (category) {
    where += " AND c.slug = ?";
    params.push(category);
  }

  if (search) {
    where += " AND (a.title LIKE ? OR a.excerpt LIKE ?)";
    const term = `%${search}%`;
    params.push(term, term);
  }

  const countRow = db
    .prepare(
      `SELECT COUNT(*) as count FROM articles a
       JOIN categories c ON a.category_id = c.id
       ${where}`
    )
    .get(...params) as { count: number };

  const total = countRow.count;
  const totalPages = Math.ceil(total / limit);

  const articles = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       ${where}
       ORDER BY a.updated_at DESC
       LIMIT ? OFFSET ?`
    )
    .all(...params, limit, offset)
    .map((row) => formatArticleRow(row as Record<string, unknown>));

  res.json({ articles, total, page, totalPages });
});

router.get("/dashboard/articles/:id", (req: Request, res: Response) => {
  const row = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       WHERE a.id = ?`
    )
    .get(req.params.id) as Record<string, unknown> | undefined;

  if (!row) {
    res.status(404).json({ error: "Article not found" });
    return;
  }

  res.json({ article: formatArticleRow(row) });
});

function getEditorialAuthorId(): number | null {
  const author = db
    .prepare("SELECT id FROM authors WHERE name = ?")
    .get(EDITORIAL_AUTHOR.name) as { id: number } | undefined;
  return author?.id ?? null;
}

function validatePublishedArticle(input: {
  title: string;
  excerpt: string;
  content: string;
  source: string;
  sourceUrl: string;
}): string[] {
  const errors = validateRewrittenArticle({
    title: input.title,
    excerpt: input.excerpt,
    content: input.content,
  });
  const expectedSource = sourceForUrl(input.sourceUrl);
  if (!expectedSource) errors.push("source URL is not from an approved publication");
  if (expectedSource && expectedSource !== input.source) errors.push("source does not match source URL");
  return [...new Set(errors)];
}

router.post("/dashboard/articles", (req: Request, res: Response) => {
  const {
    title,
    slug,
    excerpt,
    content,
    featured_image,
    category_id,
    status,
    published_at,
    meta_title,
    meta_description,
    source,
    source_url,
  } = req.body;

  if (!title || !slug || !category_id) {
    res.status(400).json({ error: "Title, slug, and category_id are required" });
    return;
  }

  const articleStatus = status || "draft";
  let normalizedSourceUrl: string | null = null;
  if (source_url) {
    try {
      normalizedSourceUrl = normalizeSourceUrl(source_url);
    } catch {
      res.status(400).json({ error: "Source URL is invalid" });
      return;
    }
  }

  if (articleStatus === "published") {
    if (!source || !normalizedSourceUrl) {
      res.status(400).json({ error: "Published articles require source and source_url" });
      return;
    }
    const validationErrors = validatePublishedArticle({
      title,
      excerpt: excerpt || "",
      content: content || "",
      source,
      sourceUrl: normalizedSourceUrl,
    });
    if (validationErrors.length) {
      res.status(400).json({ error: "Article failed publishing policy", details: validationErrors });
      return;
    }
  }

  const category = db.prepare("SELECT slug FROM categories WHERE id = ?").get(category_id) as { slug: string } | undefined;
  if (!category) {
    res.status(400).json({ error: "Category not found" });
    return;
  }
  if (category.slug === "deals") {
    res.status(400).json({ error: "Deals articles are not allowed" });
    return;
  }

  const duplicate = db
    .prepare("SELECT id FROM articles WHERE slug = ? OR (? IS NOT NULL AND source_url = ?) LIMIT 1")
    .get(slug, normalizedSourceUrl, normalizedSourceUrl) as { id: number } | undefined;
  if (duplicate) {
    res.status(409).json({ error: "Article slug or source_url already exists" });
    return;
  }

  const editorialAuthorId = getEditorialAuthorId();
  if (!editorialAuthorId) {
    res.status(500).json({ error: "TechNews Editorial author is missing" });
    return;
  }

  const publishedAt = articleStatus === "published" ? (published_at || new Date().toISOString()) : published_at || null;

  const result = db
    .prepare(
      `INSERT INTO articles (
        title, slug, excerpt, content, featured_image, category_id, author_id, status,
        published_at, meta_title, meta_description, source, source_url
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
    )
    .run(
      title,
      slug,
      excerpt || "",
      content || "",
      featured_image || null,
      category_id,
      editorialAuthorId,
      articleStatus,
      publishedAt,
      meta_title || null,
      meta_description || null,
      source || null,
      normalizedSourceUrl
    );

  const article = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       WHERE a.id = ?`
    )
    .get(result.lastInsertRowid) as Record<string, unknown>;

  res.status(201).json({ article: formatArticleRow(article) });
});

router.put("/dashboard/articles/:id", (req: Request, res: Response) => {
  const existing = db
    .prepare("SELECT * FROM articles WHERE id = ?")
    .get(req.params.id) as Record<string, unknown> | undefined;

  if (!existing) {
    res.status(404).json({ error: "Article not found" });
    return;
  }

  const {
    title,
    slug,
    excerpt,
    content,
    featured_image,
    category_id,
    status,
    published_at,
    meta_title,
    meta_description,
    source,
    source_url,
  } = req.body;

  let normalizedSourceUrl = existing.source_url as string | null;
  if (source_url !== undefined) {
    if (source_url === null || source_url === "") {
      normalizedSourceUrl = null;
    } else {
      try {
        normalizedSourceUrl = normalizeSourceUrl(source_url);
      } catch {
        res.status(400).json({ error: "Source URL is invalid" });
        return;
      }
    }
  }

  const nextTitle = (title ?? existing.title) as string;
  const nextSlug = (slug ?? existing.slug) as string;
  const nextExcerpt = (excerpt ?? existing.excerpt ?? "") as string;
  const nextContent = (content ?? existing.content ?? "") as string;
  const nextSource = (source ?? existing.source) as string | null;
  const nextStatus = (status ?? existing.status) as string;
  const nextCategoryId = (category_id ?? existing.category_id) as number;

  const category = db.prepare("SELECT slug FROM categories WHERE id = ?").get(nextCategoryId) as { slug: string } | undefined;
  if (!category) {
    res.status(400).json({ error: "Category not found" });
    return;
  }
  if (category.slug === "deals") {
    res.status(400).json({ error: "Deals articles are not allowed" });
    return;
  }

  if (nextStatus === "published") {
    if (!nextSource || !normalizedSourceUrl) {
      res.status(400).json({ error: "Published articles require source and source_url" });
      return;
    }
    const validationErrors = validatePublishedArticle({
      title: nextTitle,
      excerpt: nextExcerpt,
      content: nextContent,
      source: nextSource,
      sourceUrl: normalizedSourceUrl,
    });
    if (validationErrors.length) {
      res.status(400).json({ error: "Article failed publishing policy", details: validationErrors });
      return;
    }
  }

  const duplicate = db
    .prepare("SELECT id FROM articles WHERE id != ? AND (slug = ? OR (? IS NOT NULL AND source_url = ?)) LIMIT 1")
    .get(req.params.id, nextSlug, normalizedSourceUrl, normalizedSourceUrl) as { id: number } | undefined;
  if (duplicate) {
    res.status(409).json({ error: "Article slug or source_url already exists" });
    return;
  }

  const publishedAt =
    status === "published" && !published_at
      ? new Date().toISOString()
      : published_at !== undefined
      ? published_at
      : undefined;

  const fields: string[] = [];
  const values: unknown[] = [];

  if (title !== undefined) { fields.push("title = ?"); values.push(title); }
  if (slug !== undefined) { fields.push("slug = ?"); values.push(slug); }
  if (excerpt !== undefined) { fields.push("excerpt = ?"); values.push(excerpt); }
  if (content !== undefined) { fields.push("content = ?"); values.push(content); }
  if (featured_image !== undefined) { fields.push("featured_image = ?"); values.push(featured_image); }
  if (category_id !== undefined) { fields.push("category_id = ?"); values.push(category_id); }
  if (status !== undefined) { fields.push("status = ?"); values.push(status); }
  if (publishedAt !== undefined) { fields.push("published_at = ?"); values.push(publishedAt); }
  if (meta_title !== undefined) { fields.push("meta_title = ?"); values.push(meta_title); }
  if (meta_description !== undefined) { fields.push("meta_description = ?"); values.push(meta_description); }
  if (source !== undefined) { fields.push("source = ?"); values.push(source || null); }
  if (source_url !== undefined) { fields.push("source_url = ?"); values.push(normalizedSourceUrl); }

  if (nextStatus === "published") {
    const editorialAuthorId = getEditorialAuthorId();
    if (!editorialAuthorId) {
      res.status(500).json({ error: "TechNews Editorial author is missing" });
      return;
    }
    fields.push("author_id = ?");
    values.push(editorialAuthorId);
  }

  if (fields.length === 0) {
    res.status(400).json({ error: "No fields to update" });
    return;
  }

  fields.push("updated_at = datetime('now')");

  db.prepare(`UPDATE articles SET ${fields.join(", ")} WHERE id = ?`).run(
    ...values,
    req.params.id
  );

  const article = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       WHERE a.id = ?`
    )
    .get(req.params.id) as Record<string, unknown>;

  res.json({ article: formatArticleRow(article) });
});

router.delete("/dashboard/articles/:id", (req: Request, res: Response) => {
  const result = db
    .prepare("DELETE FROM articles WHERE id = ?")
    .run(req.params.id);

  if (result.changes === 0) {
    res.status(404).json({ error: "Article not found" });
    return;
  }

  res.json({ success: true });
});

// ─── Dashboard Categories ────────────────────────────────────────────────────

router.get("/dashboard/categories", (_req: Request, res: Response) => {
  const categories = db
    .prepare(
      `SELECT c.*, (SELECT COUNT(*) FROM articles WHERE category_id = c.id) as article_count
       FROM categories c ORDER BY c.name`
    )
    .all();
  res.json({ categories });
});

router.post("/dashboard/categories", (req: Request, res: Response) => {
  const { name, slug, description, color } = req.body;
  if (!name || !slug) {
    res.status(400).json({ error: "Name and slug are required" });
    return;
  }

  const result = db
    .prepare(
      "INSERT INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)"
    )
    .run(name, slug, description || "", color || "#6366f1");

  const category = db
    .prepare("SELECT * FROM categories WHERE id = ?")
    .get(result.lastInsertRowid);

  res.status(201).json({ category });
});

router.put("/dashboard/categories/:id", (req: Request, res: Response) => {
  const existing = db
    .prepare("SELECT id FROM categories WHERE id = ?")
    .get(req.params.id) as { id: number } | undefined;

  if (!existing) {
    res.status(404).json({ error: "Category not found" });
    return;
  }

  const { name, slug, description, color } = req.body;

  const fields: string[] = [];
  const values: unknown[] = [];

  if (name !== undefined) { fields.push("name = ?"); values.push(name); }
  if (slug !== undefined) { fields.push("slug = ?"); values.push(slug); }
  if (description !== undefined) { fields.push("description = ?"); values.push(description); }
  if (color !== undefined) { fields.push("color = ?"); values.push(color); }

  if (fields.length === 0) {
    res.status(400).json({ error: "No fields to update" });
    return;
  }

  db.prepare(`UPDATE categories SET ${fields.join(", ")} WHERE id = ?`).run(
    ...values,
    req.params.id
  );

  const category = db
    .prepare("SELECT * FROM categories WHERE id = ?")
    .get(req.params.id);

  res.json({ category });
});

router.delete("/dashboard/categories/:id", (req: Request, res: Response) => {
  // Check if category has articles
  const articleCount = db
    .prepare("SELECT COUNT(*) as count FROM articles WHERE category_id = ?")
    .get(req.params.id) as { count: number };

  if (articleCount.count > 0) {
    res.status(400).json({
      error: "Cannot delete category with existing articles. Reassign articles first.",
    });
    return;
  }

  const result = db
    .prepare("DELETE FROM categories WHERE id = ?")
    .run(req.params.id);

  if (result.changes === 0) {
    res.status(404).json({ error: "Category not found" });
    return;
  }

  res.json({ success: true });
});

// ─── Dashboard Media ─────────────────────────────────────────────────────────

router.get("/dashboard/media", (_req: Request, res: Response) => {
  const media = db
    .prepare("SELECT * FROM media ORDER BY uploaded_at DESC")
    .all();
  res.json({ media });
});

router.post(
  "/dashboard/media/upload",
  upload.single("file"),
  (req: Request, res: Response) => {
    if (!req.file) {
      res.status(400).json({ error: "No file uploaded" });
      return;
    }

    const url = `/uploads/${req.file.filename}`;

    const result = db
      .prepare(
        "INSERT INTO media (filename, url, mime_type, size) VALUES (?, ?, ?, ?)"
      )
      .run(req.file.originalname, url, req.file.mimetype, req.file.size);

    const media = db
      .prepare("SELECT * FROM media WHERE id = ?")
      .get(result.lastInsertRowid);

    res.status(201).json({ media });
  }
);

router.delete("/dashboard/media/:id", (req: Request, res: Response) => {
  const media = db
    .prepare("SELECT * FROM media WHERE id = ?")
    .get(req.params.id) as { id: number; url: string; filename: string } | undefined;

  if (!media) {
    res.status(404).json({ error: "Media not found" });
    return;
  }

  // Delete file from disk
  const filePath = path.join(__dirname, "..", media.url);
  if (fs.existsSync(filePath)) {
    fs.unlinkSync(filePath);
  }

  db.prepare("DELETE FROM media WHERE id = ?").run(req.params.id);

  res.json({ success: true });
});

// ─── Dashboard Settings ──────────────────────────────────────────────────────

router.get("/dashboard/settings", (_req: Request, res: Response) => {
  const rows = db.prepare("SELECT key, value FROM settings").all() as {
    key: string;
    value: string;
  }[];

  const settings: Record<string, unknown> = {};
  for (const row of rows) {
    if (row.key === "newsletter_enabled") {
      settings[row.key] = row.value === "true";
    } else {
      settings[row.key] = row.value;
    }
  }

  res.json({ settings });
});

router.put("/dashboard/settings", (req: Request, res: Response) => {
  const upsert = db.prepare(
    "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
  );

  const validKeys = [
    "site_name",
    "site_description",
    "social_twitter",
    "social_linkedin",
    "social_github",
    "newsletter_enabled",
    "newsletter_provider",
    "newsletter_webhook_url",
  ];

  const transaction = db.transaction(() => {
    for (const key of validKeys) {
      if (req.body[key] !== undefined) {
        const value =
          typeof req.body[key] === "boolean"
            ? String(req.body[key])
            : req.body[key];
        upsert.run(key, value);
      }
    }
  });

  transaction();

  // Return updated settings
  const rows = db.prepare("SELECT key, value FROM settings").all() as {
    key: string;
    value: string;
  }[];

  const settings: Record<string, unknown> = {};
  for (const row of rows) {
    if (row.key === "newsletter_enabled") {
      settings[row.key] = row.value === "true";
    } else {
      settings[row.key] = row.value;
    }
  }

  res.json({ settings });
});

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatArticleRow(row: Record<string, unknown>) {
  return {
    id: row.id,
    title: row.title,
    slug: row.slug,
    excerpt: row.excerpt,
    content: row.content,
    featured_image: row.featured_image,
    category_id: row.category_id,
    author_id: row.author_id,
    status: row.status,
    published_at: row.published_at,
    meta_title: row.meta_title,
    meta_description: row.meta_description,
    source: row.source,
    source_url: row.source_url,
    view_count: row.view_count,
    created_at: row.created_at,
    updated_at: row.updated_at,
    category: {
      id: row.category_id,
      name: row.category_name,
      slug: row.category_slug,
      description: row.category_description,
      color: row.category_color,
    },
    author: {
      id: row.author_id,
      name: row.author_name,
      email: row.author_email,
      avatar: row.author_avatar,
      bio: row.author_bio,
      role: row.author_role,
    },
  };
}

export default router;
