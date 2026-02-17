import Database from "better-sqlite3";
import bcrypt from "bcryptjs";
import path from "path";

const DB_PATH = path.join(__dirname, "..", "data", "technews.db");

// Ensure data directory exists
import fs from "fs";
fs.mkdirSync(path.dirname(DB_PATH), { recursive: true });

const db: InstanceType<typeof Database> = new Database(DB_PATH);

// Enable WAL mode for better concurrent read performance
db.pragma("journal_mode = WAL");
db.pragma("foreign_keys = ON");

export function initializeDatabase() {
  db.exec(`
    CREATE TABLE IF NOT EXISTS categories (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      description TEXT NOT NULL DEFAULT '',
      color TEXT NOT NULL DEFAULT '#6366f1'
    );

    CREATE TABLE IF NOT EXISTS authors (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      email TEXT NOT NULL UNIQUE,
      password_hash TEXT NOT NULL,
      avatar TEXT,
      bio TEXT,
      role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))
    );

    CREATE TABLE IF NOT EXISTS articles (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      title TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      excerpt TEXT NOT NULL DEFAULT '',
      content TEXT NOT NULL DEFAULT '',
      featured_image TEXT,
      category_id INTEGER NOT NULL,
      author_id INTEGER NOT NULL,
      status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft', 'published', 'scheduled')),
      published_at TEXT,
      meta_title TEXT,
      meta_description TEXT,
      view_count INTEGER NOT NULL DEFAULT 0,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT NOT NULL DEFAULT (datetime('now')),
      FOREIGN KEY (category_id) REFERENCES categories(id),
      FOREIGN KEY (author_id) REFERENCES authors(id)
    );

    CREATE TABLE IF NOT EXISTS media (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      filename TEXT NOT NULL,
      url TEXT NOT NULL,
      mime_type TEXT NOT NULL,
      size INTEGER NOT NULL,
      uploaded_at TEXT NOT NULL DEFAULT (datetime('now'))
    );

    CREATE TABLE IF NOT EXISTS settings (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );
  `);

  seed();
}

function seed() {
  const categoryCount = db
    .prepare("SELECT COUNT(*) as count FROM categories")
    .get() as { count: number };

  // Only seed categories and admin users, never seed sample articles
  if (categoryCount.count > 0) return;

  // Seed categories
  const insertCategory = db.prepare(
    "INSERT INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)"
  );

  insertCategory.run("AI", "ai", "Artificial intelligence, machine learning, and deep learning news", "#8b5cf6");
  insertCategory.run("Programming", "programming", "Programming languages, frameworks, and developer tools", "#3b82f6");
  insertCategory.run("Startups", "startups", "Startup funding, launches, and entrepreneurship", "#10b981");

  // Seed admin users (passwords: technews2026)
  const passwordHash = bcrypt.hashSync("technews2026", 10);
  db.prepare(
    "INSERT INTO authors (name, email, password_hash, avatar, bio, role) VALUES (?, ?, ?, ?, ?, ?)"
  ).run(
    "Admin",
    "admin@technews.com",
    passwordHash,
    null,
    "TechNews administrator and editor-in-chief.",
    "admin"
  );
  
  db.prepare(
    "INSERT INTO authors (name, email, password_hash, avatar, bio, role) VALUES (?, ?, ?, ?, ?, ?)"
  ).run(
    "Ozan",
    "ozan@technews.com",
    passwordHash,
    null,
    "TechNews co-founder and technical editor.",
    "admin"
  );

  // NO sample articles — real articles come from the scraper

  // Seed default settings
  const insertSetting = db.prepare(
    "INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)"
  );
  insertSetting.run("site_name", "TechNews");
  insertSetting.run("site_description", "AI & Tech News, Daily.");
  insertSetting.run("social_twitter", "");
  insertSetting.run("social_linkedin", "");
  insertSetting.run("social_github", "");
  insertSetting.run("newsletter_enabled", "false");
  insertSetting.run("newsletter_provider", "none");
  insertSetting.run("newsletter_webhook_url", "");
}

export default db;
