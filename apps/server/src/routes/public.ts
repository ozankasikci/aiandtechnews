import { Router, Request, Response } from "express";
import { timingSafeEqual } from "crypto";
import db from "../db";
import { NewsletterConfigurationError } from "../newsletter/email";
import { NewsletterService } from "../newsletter/service";

const router: ReturnType<typeof Router> = Router();
const newsletter = new NewsletterService(db);
const signupAttempts: number[] = [];

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

function secretsMatch(supplied: string | undefined, expected: string): boolean {
  if (!supplied) return false;
  const suppliedBuffer = Buffer.from(supplied);
  const expectedBuffer = Buffer.from(expected);
  return suppliedBuffer.length === expectedBuffer.length && timingSafeEqual(suppliedBuffer, expectedBuffer);
}

// POST /api/subscribe
router.post("/subscribe", async (req: Request, res: Response) => {
  const now = Date.now();
  while (signupAttempts.length && signupAttempts[0] < now - 60_000) signupAttempts.shift();
  if (signupAttempts.length >= 30) {
    res.status(429).json({ error: "Too many signup attempts. Please try again shortly." });
    return;
  }
  signupAttempts.push(now);

  const email = typeof req.body?.email === "string" ? req.body.email : "";
  const placement = typeof req.body?.placement === "string" ? req.body.placement : "unknown";
  try {
    const result = await newsletter.requestSubscription(email, placement);
    const message = result.state === "already_active"
      ? "You're already subscribed."
      : "Check your inbox to confirm your subscription.";
    res.json({ success: true, state: result.state, message });
  } catch (error) {
    if (error instanceof TypeError) {
      res.status(400).json({ error: error.message });
      return;
    }
    if (error instanceof NewsletterConfigurationError) {
      res.status(503).json({ error: "Newsletter signup is temporarily unavailable" });
      return;
    }
    console.error("Newsletter signup failed", error);
    res.status(502).json({ error: "We could not send the confirmation email. Please try again." });
  }
});

// GET /api/newsletter/confirm?token=...
router.get("/newsletter/confirm", async (req: Request, res: Response) => {
  const token = typeof req.query.token === "string" ? req.query.token : "";
  try {
    const result = await newsletter.confirmSubscription(token);
    res.json(result);
  } catch (error) {
    if (error instanceof NewsletterConfigurationError) {
      res.status(503).json({ state: "unavailable", error: "Newsletter confirmation is temporarily unavailable" });
      return;
    }
    console.error("Newsletter confirmation failed", error);
    res.status(500).json({ state: "invalid", error: "Confirmation failed" });
  }
});

async function unsubscribe(req: Request, res: Response) {
  const token = typeof req.query.token === "string"
    ? req.query.token
    : typeof req.body?.token === "string"
      ? req.body.token
      : "";
  try {
    const state = newsletter.unsubscribe(token);
    res.json({ state });
  } catch (error) {
    if (error instanceof NewsletterConfigurationError) {
      res.status(503).json({ state: "unavailable", error: "Unsubscribe is temporarily unavailable" });
      return;
    }
    console.error("Newsletter unsubscribe failed", error);
    res.status(500).json({ state: "invalid", error: "Unsubscribe failed" });
  }
}

// GET and POST /api/newsletter/unsubscribe?token=...
router.get("/newsletter/unsubscribe", unsubscribe);
router.post("/newsletter/unsubscribe", unsubscribe);

router.get("/newsletter/editions", (req: Request, res: Response) => {
  const limit = parseInt(String(req.query.limit || "30"), 10);
  res.json({ editions: newsletter.listEditions(Number.isFinite(limit) ? limit : 30) });
});

router.get("/newsletter/editions/:edition", (req: Request, res: Response) => {
  const edition = newsletter.getEdition(String(req.params.edition));
  if (!edition) {
    res.status(404).json({ error: "Edition not found" });
    return;
  }
  res.json({ edition });
});

async function sendDigest(req: Request, res: Response) {
  const expectedSecret = (process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || "").trim();
  const suppliedSecret = req.header("authorization")?.replace(/^Bearer\s+/i, "");
  if (!expectedSecret || !secretsMatch(suppliedSecret, expectedSecret)) {
    res.status(401).json({ error: "Unauthorized" });
    return;
  }

  try {
    const result = await newsletter.sendDailyDigest();
    res.json({ success: result.failed === 0, ...result });
  } catch (error) {
    if (error instanceof NewsletterConfigurationError) {
      res.status(503).json({ error: "Newsletter delivery is not configured" });
      return;
    }
    console.error("Newsletter digest failed", error);
    res.status(500).json({ error: "Newsletter digest failed" });
  }
}

// Vercel Cron invokes GET. POST is available for a secured manual retry.
router.get("/newsletter/digest", sendDigest);
router.post("/newsletter/digest", sendDigest);

export default router;
