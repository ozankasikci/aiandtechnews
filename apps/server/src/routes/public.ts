import { Router, Request, Response } from "express";
import db from "../db";

const router: ReturnType<typeof Router> = Router();

// GET /api/articles — list published articles
router.get("/articles", (req: Request, res: Response) => {
  const page = Math.max(1, parseInt(req.query.page as string) || 1);
  const limit = Math.min(50, Math.max(1, parseInt(req.query.limit as string) || 12));
  const offset = (page - 1) * limit;
  const category = req.query.category as string | undefined;
  const search = req.query.search as string | undefined;

  let where = "WHERE a.status = 'published'";
  const params: unknown[] = [];

  if (category) {
    where += " AND c.slug = ?";
    params.push(category);
  }

  if (search) {
    where += " AND (a.title LIKE ? OR a.excerpt LIKE ? OR a.content LIKE ?)";
    const term = `%${search}%`;
    params.push(term, term, term);
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
       ORDER BY a.published_at DESC
       LIMIT ? OFFSET ?`
    )
    .all(...params, limit, offset)
    .map((row) => formatArticleRow(row as Record<string, unknown>));

  res.json({ articles, total, page, totalPages });
});

// GET /api/articles/trending — most viewed articles
router.get("/articles/trending", (req: Request, res: Response) => {
  const limit = Math.min(20, Math.max(1, parseInt(req.query.limit as string) || 5));

  const articles = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       WHERE a.status = 'published'
       ORDER BY a.view_count DESC
       LIMIT ?`
    )
    .all(limit)
    .map((row) => formatArticleRow(row as Record<string, unknown>));

  res.json({ articles });
});

// GET /api/articles/:slug — single article by slug
router.get("/articles/:slug", (req: Request, res: Response) => {
  const row = db
    .prepare(
      `SELECT a.*, c.name as category_name, c.slug as category_slug, c.color as category_color, c.description as category_description,
              au.name as author_name, au.email as author_email, au.avatar as author_avatar, au.bio as author_bio, au.role as author_role
       FROM articles a
       JOIN categories c ON a.category_id = c.id
       JOIN authors au ON a.author_id = au.id
       WHERE a.slug = ? AND a.status = 'published'`
    )
    .get(req.params.slug) as Record<string, unknown> | undefined;

  if (!row) {
    res.status(404).json({ error: "Article not found" });
    return;
  }

  // Increment view count
  db.prepare("UPDATE articles SET view_count = view_count + 1 WHERE id = ?").run(
    row.id
  );

  res.json({ article: formatArticleRow(row) });
});

// GET /api/categories — list all categories
router.get("/categories", (_req: Request, res: Response) => {
  const categories = db.prepare("SELECT * FROM categories ORDER BY name").all();
  res.json({ categories });
});

// GET /api/authors — list all authors
router.get("/authors", (_req: Request, res: Response) => {
  const authors = db.prepare("SELECT id, name, email, avatar, bio, role FROM authors ORDER BY name").all();
  res.json({ authors });
});

// GET /api/articles/id/:id — single article by ID
router.get("/articles/id/:id", (req: Request, res: Response) => {
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

// POST /api/subscribe
router.post("/subscribe", (req: Request, res: Response) => {
  const { email } = req.body;
  if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    res.status(400).json({ error: "Valid email required" });
    return;
  }
  try {
    db.prepare("INSERT INTO subscribers (email) VALUES (?)").run(email.toLowerCase().trim());
    res.json({ success: true, message: "You're subscribed!" });
  } catch (err: unknown) {
    if (err instanceof Error && err.message?.includes("UNIQUE")) {
      res.json({ success: true, message: "Already subscribed!" });
    } else {
      res.status(500).json({ error: "Something went wrong" });
    }
  }
});

export default router;
