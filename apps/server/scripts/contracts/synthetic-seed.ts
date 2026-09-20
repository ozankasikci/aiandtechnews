import { writeFileSync } from "node:fs";
import path from "node:path";
import type { DatabaseConnection } from "../../src/db";

/** Password for both synthetic authors. Safe for tests only. */
export const CONTRACT_TEST_PASSWORD = "contract-test-password";
/** bcrypt hash of CONTRACT_TEST_PASSWORD with the documented fixed test salt. */
export const CONTRACT_TEST_PASSWORD_HASH = "$2a$04$abcdefghijklmnopqrstuu.GAp6e4m7hQ19qGCjMlGaXpbrUyvyq6";

export function seedSyntheticContractData(db: DatabaseConnection, uploadDir: string): void {
  const mediaFilename = "synthetic-contract-image.png";
  writeFileSync(path.join(uploadDir, mediaFilename), "synthetic contract media\n", { flag: "wx" });

  db.transaction(() => {
    const categories = [
      [101, "Synthetic AI", "synthetic-ai", "Synthetic artificial intelligence fixtures", "#111111"],
      [102, "Synthetic Code", "synthetic-code", "Synthetic programming fixtures", "#222222"],
      [103, "Synthetic Startups", "synthetic-startups", "Synthetic startup fixtures", "#333333"],
      [104, "Synthetic Unused", "synthetic-unused", "Intentionally unused contract category", "#444444"],
    ] as const;
    const insertCategory = db.prepare("INSERT INTO categories (id, name, slug, description, color) VALUES (?, ?, ?, ?, ?)");
    for (const category of categories) insertCategory.run(...category);

    const insertAuthor = db.prepare(`INSERT INTO authors
      (id, name, email, password_hash, avatar, bio, role) VALUES (?, ?, ?, ?, ?, ?, ?)`);
    insertAuthor.run(201, "TechNews Editorial", "editorial@example.invalid", CONTRACT_TEST_PASSWORD_HASH,
      "/uploads/synthetic-contract-image.png", "Synthetic editorial contract fixture.", "admin");
    insertAuthor.run(202, "Synthetic Reporter", "reporter@example.invalid", CONTRACT_TEST_PASSWORD_HASH,
      null, null, "editor");

    const insertArticle = db.prepare(`INSERT INTO articles
      (id, title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at,
       meta_title, meta_description, source, source_url, view_count, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`);
    insertArticle.run(301, "Synthetic Published Newer", "synthetic-published-newer", "Newer synthetic excerpt",
      "<p>Entirely synthetic contract article content.</p>", "/uploads/synthetic-contract-image.png", 101, 201,
      "published", "2026-09-19T12:00:00.000Z", "Synthetic meta title", "Synthetic meta description",
      "Synthetic Wire", "https://news.example.invalid/newer", 42, "2026-09-19T10:00:00.000Z", "2026-09-19T12:00:00.000Z");
    insertArticle.run(302, "Synthetic Published Null Options", "synthetic-published-null-options", "Null option excerpt",
      "Synthetic plain text.", null, 102, 202, "published", "2026-09-18 09:00:00",
      null, null, null, null, 0, "2026-09-18 08:00:00", "2026-09-18 09:00:00");
    insertArticle.run(303, "Synthetic Draft", "synthetic-draft", "Draft excerpt", "<p>Synthetic draft.</p>",
      null, 103, 201, "draft", null, null, null, "Synthetic Wire", "https://news.example.invalid/draft", 3,
      "2026-09-17T00:00:00.000Z", "2026-09-17T00:00:00.000Z");
    insertArticle.run(304, "Synthetic Scheduled", "synthetic-scheduled", "Scheduled excerpt", "<p>Synthetic scheduled.</p>",
      null, 101, 202, "scheduled", "2030-01-01T00:00:00.000Z", null, null, "Synthetic Wire",
      "https://news.example.invalid/scheduled", 1, "2026-09-16T00:00:00.000Z", "2026-09-16T00:00:00.000Z");

    db.prepare("INSERT INTO media (id, filename, url, mime_type, size, uploaded_at) VALUES (?, ?, ?, ?, ?, ?)")
      .run(401, mediaFilename, `/uploads/${mediaFilename}`, "image/png", 25, "2026-09-19T00:00:00.000Z");

    const settings = [
      ["site_name", "Synthetic TechNews"],
      ["site_description", "Synthetic contract fixture"],
      ["social_twitter", "https://social.example.invalid/synthetic"],
      ["social_linkedin", ""],
      ["social_github", "https://code.example.invalid/synthetic"],
      ["newsletter_enabled", "true"],
      ["newsletter_provider", "recorder"],
      ["newsletter_webhook_url", "https://hooks.example.invalid/newsletter"],
    ] as const;
    const insertSetting = db.prepare("INSERT INTO settings (key, value) VALUES (?, ?)");
    for (const setting of settings) insertSetting.run(...setting);

    const insertSubscriber = db.prepare(`INSERT INTO subscribers
      (id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`);
    insertSubscriber.run(501, "pending@example.invalid", "pending", "contract", "2026-09-18T00:00:00.000Z", null, null,
      "2026-09-18T00:00:00.000Z", "2026-09-18T00:00:00.000Z");
    insertSubscriber.run(502, "active@example.invalid", "active", "contract", null, "2026-09-18T01:00:00.000Z", null,
      "2026-09-18T00:00:00.000Z", "2026-09-18T01:00:00.000Z");
    insertSubscriber.run(503, "unsubscribed@example.invalid", "unsubscribed", "contract", null,
      "2026-09-17T01:00:00.000Z", "2026-09-18T02:00:00.000Z", "2026-09-17T00:00:00.000Z", "2026-09-18T02:00:00.000Z");

    const validArticles = JSON.stringify([{
      title: "Synthetic Published Newer", slug: "synthetic-published-newer", excerpt: "Newer synthetic excerpt",
      category: "Synthetic AI", readingMinutes: 1,
    }]);
    db.prepare("INSERT INTO newsletter_editions (id, edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?, ?)")
      .run(601, "2026-09-19", "Synthetic daily digest", validArticles, "2026-09-19T13:00:00.000Z");
    db.prepare("INSERT INTO newsletter_editions (id, edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?, ?)")
      .run(602, "2026-09-18", "Synthetic malformed digest", "{malformed", "2026-09-18T13:00:00.000Z");
  })();
}
