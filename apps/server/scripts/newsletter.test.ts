import assert from "node:assert/strict";
import test from "node:test";
import Database from "better-sqlite3";
import { NewsletterService } from "../src/newsletter/service";
import type { EmailSender, NewsletterEmail } from "../src/newsletter/email";
import { createNewsletterToken, verifyNewsletterToken } from "../src/newsletter/tokens";

const TOKEN_SECRET = "test-newsletter-token-secret-with-32-characters";

function createTestDatabase() {
  const database = new Database(":memory:");
  database.pragma("foreign_keys = ON");
  database.exec(`
    CREATE TABLE subscribers (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      email TEXT NOT NULL UNIQUE,
      status TEXT NOT NULL DEFAULT 'pending',
      source_placement TEXT,
      confirmation_sent_at TEXT,
      confirmed_at TEXT,
      unsubscribed_at TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );
    CREATE TABLE newsletter_deliveries (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      subscriber_id INTEGER NOT NULL,
      edition_key TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'sending',
      provider_message_id TEXT,
      error TEXT,
      created_at TEXT NOT NULL,
      sent_at TEXT,
      UNIQUE(subscriber_id, edition_key),
      FOREIGN KEY (subscriber_id) REFERENCES subscribers(id) ON DELETE CASCADE
    );
    CREATE TABLE categories (
      id INTEGER PRIMARY KEY,
      name TEXT NOT NULL
    );
    CREATE TABLE articles (
      id INTEGER PRIMARY KEY,
      title TEXT NOT NULL,
      slug TEXT NOT NULL,
      excerpt TEXT NOT NULL,
      content TEXT,
      published_at TEXT,
      status TEXT NOT NULL,
      category_id INTEGER NOT NULL
    );
    CREATE TABLE newsletter_editions (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      edition_key TEXT NOT NULL UNIQUE,
      subject TEXT NOT NULL,
      articles TEXT NOT NULL,
      created_at TEXT NOT NULL
    );
  `);
  return database;
}

test("newsletter tokens enforce purpose, signature, and expiry", () => {
  const now = new Date("2026-08-09T08:00:00.000Z");
  const token = createNewsletterToken(42, "confirm", TOKEN_SECRET, new Date(now.getTime() + 60_000));

  assert.equal(verifyNewsletterToken(token, "confirm", TOKEN_SECRET, now), 42);
  assert.equal(verifyNewsletterToken(token, "unsubscribe", TOKEN_SECRET, now), null);
  assert.equal(verifyNewsletterToken(`${token}x`, "confirm", TOKEN_SECRET, now), null);
  assert.equal(verifyNewsletterToken(token, "confirm", TOKEN_SECRET, new Date(now.getTime() + 61_000)), null);
});

test("signup activates immediately, and digest, idempotency, and unsubscribe form a complete loop", async () => {
  const database = createTestDatabase();
  const sent: { email: NewsletterEmail; idempotencyKey: string }[] = [];
  const sendEmail: EmailSender = async (email, idempotencyKey) => {
    sent.push({ email, idempotencyKey });
    return { id: `message-${sent.length}` };
  };
  const service = new NewsletterService(
    database,
    {
      NEWSLETTER_SITE_URL: "https://aiandtech.news",
      NEWSLETTER_TOKEN_SECRET: TOKEN_SECRET,
    },
    sendEmail,
  );
  const now = new Date("2026-08-09T08:00:00.000Z");

  // Signup activates on the spot and sends nothing.
  const requested = await service.requestSubscription(" Reader@Example.com ", "inline", now);
  assert.equal(requested.state, "subscribed");
  assert.equal(sent.length, 0, "signup must not send any email");
  assert.equal(database.prepare("SELECT email FROM subscribers WHERE id = 1").pluck().get(), "reader@example.com");
  assert.equal(database.prepare("SELECT status FROM subscribers WHERE id = 1").pluck().get(), "active");
  assert.ok(database.prepare("SELECT confirmed_at FROM subscribers WHERE id = 1").pluck().get());

  // Resubmitting is idempotent: no duplicate row, no second activation.
  const again = await service.requestSubscription("reader@example.com", "header", new Date(now.getTime() + 60_000));
  assert.equal(again.state, "already_active");
  assert.equal(sent.length, 0);
  assert.equal(database.prepare("SELECT COUNT(*) FROM subscribers").pluck().get(), 1);

  database.prepare("INSERT INTO categories (id, name) VALUES (1, 'AI')").run();
  database
    .prepare(
      `INSERT INTO articles (id, title, slug, excerpt, content, published_at, status, category_id)
       VALUES (1, ?, ?, ?, ?, ?, 'published', 1)`,
    )
    .run(
      "A useful AI update",
      "useful-ai-update",
      "What changed and why it matters.",
      `<p>${Array.from({ length: 440 }, () => "word").join(" ")}</p>`,
      "2026-08-09 07:59:00",
    );

  const firstDigest = await service.sendDailyDigest(new Date(now.getTime() + 240_000));
  assert.deepEqual(firstDigest, {
    edition: "2026-08-09",
    articles: 1,
    sent: 1,
    skipped: 0,
    failed: 0,
  });
  assert.equal(sent.length, 1, "the digest is now the only email the flow sends");
  assert.match(sent[0].email.headers?.["List-Unsubscribe"] || "", /newsletter\/unsubscribe/);
  assert.equal(sent[0].email.html.includes("—"), false);

  // The digest carries a per-story read time and leads with the top story.
  assert.equal(sent[0].email.subject, "A useful AI update");
  assert.match(sent[0].email.html, /\(2 minute read\)/);
  assert.match(sent[0].email.text, /A useful AI update \(2 minute read\)/);
  assert.match(sent[0].email.html, />AI</);

  const archived = service.getEdition("2026-08-09");
  assert.ok(archived);
  assert.equal(archived.subject, "A useful AI update");
  assert.deepEqual(archived.articles, [
    {
      title: "A useful AI update",
      slug: "useful-ai-update",
      excerpt: "What changed and why it matters.",
      category: "AI",
      readingMinutes: 2,
    },
  ]);
  assert.deepEqual(service.listEditions().map((edition) => edition.edition), ["2026-08-09"]);

  const duplicateDigest = await service.sendDailyDigest(new Date(now.getTime() + 300_000));
  assert.equal(duplicateDigest.sent, 0);
  assert.equal(duplicateDigest.skipped, 1);
  assert.equal(sent.length, 1, "a skipped edition sends nothing further");

  const unsubscribeMatch = sent[0].email.text.match(/unsubscribe\?token=([^\s]+)/);
  assert.ok(unsubscribeMatch);
  assert.equal(service.unsubscribe(decodeURIComponent(unsubscribeMatch[1]), new Date(now.getTime() + 360_000)), "unsubscribed");
  assert.equal(service.unsubscribe(decodeURIComponent(unsubscribeMatch[1]), new Date(now.getTime() + 420_000)), "already_unsubscribed");
  assert.equal(database.prepare("SELECT status FROM subscribers WHERE id = 1").pluck().get(), "unsubscribed");

  database.close();
});

test("invalid addresses are rejected before they are stored", async () => {
  const database = createTestDatabase();
  const service = new NewsletterService(
    database,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TOKEN_SECRET },
    async () => ({ id: "unused" }),
  );

  await assert.rejects(() => service.requestSubscription("not-an-email"), /Valid email required/);
  assert.equal(database.prepare("SELECT COUNT(*) FROM subscribers").pluck().get(), 0);
  database.close();
});

test("signup works with no email provider configured and revives stale rows", async () => {
  const database = createTestDatabase();
  const sendEmail: EmailSender = async () => {
    throw new Error("the signup path must never reach the email provider");
  };
  // No NEWSLETTER_TOKEN_SECRET: signup issues no tokens, so it must not need one.
  const service = new NewsletterService(database, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, sendEmail);
  const now = new Date("2026-08-09T08:00:00.000Z");

  // A row left pending by the old double opt-in flow.
  database
    .prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, ?, 'pending', ?, ?)")
    .run("stale@example.com", now.toISOString(), now.toISOString());

  const revived = await service.requestSubscription("Stale@Example.com", "inline", now);
  assert.equal(revived.state, "subscribed");
  assert.equal(database.prepare("SELECT status FROM subscribers WHERE id = 1").pluck().get(), "active");
  assert.equal(database.prepare("SELECT COUNT(*) FROM subscribers").pluck().get(), 1);

  // Someone who previously unsubscribed can subscribe again.
  database
    .prepare("INSERT INTO subscribers (id, email, status, unsubscribed_at, created_at, updated_at) VALUES (2, ?, 'unsubscribed', ?, ?, ?)")
    .run("returning@example.com", now.toISOString(), now.toISOString(), now.toISOString());

  const returned = await service.requestSubscription("returning@example.com", "header", now);
  assert.equal(returned.state, "subscribed");
  assert.equal(database.prepare("SELECT status FROM subscribers WHERE id = 2").pluck().get(), "active");
  assert.equal(database.prepare("SELECT unsubscribed_at FROM subscribers WHERE id = 2").pluck().get(), null);

  database.close();
});
