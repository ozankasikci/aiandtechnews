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
      published_at TEXT,
      status TEXT NOT NULL,
      category_id INTEGER NOT NULL
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

test("subscription confirmation, digest, idempotency, and unsubscribe form a complete loop", async () => {
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

  const requested = await service.requestSubscription(" Reader@Example.com ", "inline", now);
  assert.equal(requested.state, "confirmation_sent");
  assert.equal(sent.length, 1);
  assert.equal(sent[0].email.to, "reader@example.com");

  const throttled = await service.requestSubscription("reader@example.com", "header", new Date(now.getTime() + 60_000));
  assert.equal(throttled.state, "throttled");
  assert.equal(sent.length, 1);

  const confirmMatch = sent[0].email.text.match(/token=([^\s]+)/);
  assert.ok(confirmMatch);
  const confirmed = await service.confirmSubscription(decodeURIComponent(confirmMatch[1]), new Date(now.getTime() + 120_000));
  assert.deepEqual(confirmed, { state: "confirmed", welcomeSent: true });
  assert.equal(sent.length, 2);
  assert.equal(database.prepare("SELECT status FROM subscribers WHERE id = 1").pluck().get(), "active");

  const confirmedAgain = await service.confirmSubscription(decodeURIComponent(confirmMatch[1]), new Date(now.getTime() + 180_000));
  assert.deepEqual(confirmedAgain, { state: "already_confirmed", welcomeSent: false });
  assert.equal(sent.length, 2);

  database.prepare("INSERT INTO categories (id, name) VALUES (1, 'AI')").run();
  database
    .prepare(
      `INSERT INTO articles (id, title, slug, excerpt, published_at, status, category_id)
       VALUES (1, ?, ?, ?, ?, 'published', 1)`,
    )
    .run("A useful AI update", "useful-ai-update", "What changed and why it matters.", "2026-08-09 07:59:00");

  const firstDigest = await service.sendDailyDigest(new Date(now.getTime() + 240_000));
  assert.deepEqual(firstDigest, {
    edition: "2026-08-09",
    articles: 1,
    sent: 1,
    skipped: 0,
    failed: 0,
  });
  assert.equal(sent.length, 3);
  assert.match(sent[2].email.headers?.["List-Unsubscribe"] || "", /newsletter\/unsubscribe/);
  assert.equal(sent[2].email.html.includes("—"), false);

  const duplicateDigest = await service.sendDailyDigest(new Date(now.getTime() + 300_000));
  assert.equal(duplicateDigest.sent, 0);
  assert.equal(duplicateDigest.skipped, 1);
  assert.equal(sent.length, 3);

  const unsubscribeMatch = sent[2].email.text.match(/unsubscribe\?token=([^\s]+)/);
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
