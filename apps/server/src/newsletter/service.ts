import type Database from "better-sqlite3";
import {
  confirmationEmail,
  createResendSender,
  digestEmail,
  type DigestArticle,
  type EmailSender,
  NewsletterConfigurationError,
  welcomeEmail,
} from "./email";
import { createNewsletterToken, verifyNewsletterToken } from "./tokens";

type DatabaseConnection = InstanceType<typeof Database>;

interface SubscriberRow {
  id: number;
  email: string;
  status: "pending" | "active" | "unsubscribed";
  confirmation_sent_at: string | null;
}

interface ArticleRow {
  title: string;
  slug: string;
  excerpt: string;
  category_name: string;
  published_at: string | null;
}

export interface NewsletterEnvironment {
  NEWSLETTER_SITE_URL?: string;
  NEWSLETTER_TOKEN_SECRET?: string;
  RESEND_API_KEY?: string;
  NEWSLETTER_FROM?: string;
  NEWSLETTER_REPLY_TO?: string;
}

export interface SubscriptionResult {
  state: "confirmation_sent" | "already_active" | "throttled";
}

export interface ConfirmationResult {
  state: "confirmed" | "already_confirmed" | "invalid";
  welcomeSent: boolean;
}

export interface DigestResult {
  edition: string;
  articles: number;
  sent: number;
  skipped: number;
  failed: number;
}

function normalizeEmail(email: string): string | null {
  const value = email.trim().toLowerCase();
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value) && value.length <= 254 ? value : null;
}

function formatIstanbulDate(date: Date): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Europe/Istanbul",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const get = (type: Intl.DateTimeFormatPartTypes) => parts.find((part) => part.type === type)?.value || "";
  return `${get("year")}-${get("month")}-${get("day")}`;
}

function safeSiteUrl(value: string | undefined): string {
  const configured = value?.trim() || "https://aiandtech.news";
  const parsed = new URL(configured);
  if (parsed.protocol !== "https:" && parsed.hostname !== "localhost") {
    throw new NewsletterConfigurationError("NEWSLETTER_SITE_URL must use HTTPS");
  }
  return parsed.origin;
}

function requireTokenSecret(environment: NewsletterEnvironment): string {
  const secret = environment.NEWSLETTER_TOKEN_SECRET?.trim();
  if (!secret || secret.length < 32) {
    throw new NewsletterConfigurationError("NEWSLETTER_TOKEN_SECRET must contain at least 32 characters");
  }
  return secret;
}

function toPublicArticle(row: ArticleRow): DigestArticle {
  return {
    title: row.title,
    slug: row.slug,
    excerpt: row.excerpt,
    category: row.category_name,
  };
}

function parseArticleTimestamp(value: string | null): number {
  if (!value) return Number.NaN;
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value)
    ? `${value.replace(" ", "T")}Z`
    : value;
  return Date.parse(normalized);
}

export class NewsletterService {
  private readonly siteUrl: string;

  constructor(
    private readonly database: DatabaseConnection,
    private readonly environment: NewsletterEnvironment = process.env as NewsletterEnvironment,
    private readonly sendEmail: EmailSender = createResendSender(process.env),
  ) {
    this.siteUrl = safeSiteUrl(environment.NEWSLETTER_SITE_URL);
  }

  async requestSubscription(rawEmail: string, placement = "unknown", now = new Date()): Promise<SubscriptionResult> {
    const email = normalizeEmail(rawEmail);
    if (!email) throw new TypeError("Valid email required");

    const secret = requireTokenSecret(this.environment);
    const existing = this.database
      .prepare("SELECT id, email, status, confirmation_sent_at FROM subscribers WHERE lower(email) = ?")
      .get(email) as SubscriberRow | undefined;

    if (existing?.status === "active") return { state: "already_active" };

    const lastSent = existing?.confirmation_sent_at ? Date.parse(existing.confirmation_sent_at) : Number.NaN;
    if (Number.isFinite(lastSent) && now.getTime() - lastSent < 10 * 60 * 1000) {
      return { state: "throttled" };
    }

    const subscriber = existing || (this.database
      .prepare(
        `INSERT INTO subscribers (email, status, source_placement, created_at, updated_at)
         VALUES (?, 'pending', ?, ?, ?)
         RETURNING id, email, status, confirmation_sent_at`,
      )
      .get(email, placement.slice(0, 80), now.toISOString(), now.toISOString()) as SubscriberRow);

    if (existing) {
      this.database
        .prepare(
          `UPDATE subscribers
           SET status = 'pending', source_placement = ?, unsubscribed_at = NULL, updated_at = ?
           WHERE id = ?`,
        )
        .run(placement.slice(0, 80), now.toISOString(), subscriber.id);
    }

    const expiresAt = new Date(now.getTime() + 48 * 60 * 60 * 1000);
    const token = createNewsletterToken(subscriber.id, "confirm", secret, expiresAt);
    const confirmUrl = `${this.siteUrl}/api/newsletter/confirm?token=${encodeURIComponent(token)}`;
    await this.sendEmail(confirmationEmail(email, confirmUrl), `newsletter-confirm-${subscriber.id}-${Math.floor(now.getTime() / 600000)}`);

    this.database
      .prepare("UPDATE subscribers SET confirmation_sent_at = ?, updated_at = ? WHERE id = ?")
      .run(now.toISOString(), now.toISOString(), subscriber.id);
    return { state: "confirmation_sent" };
  }

  async confirmSubscription(token: string, now = new Date()): Promise<ConfirmationResult> {
    const secret = requireTokenSecret(this.environment);
    const subscriberId = verifyNewsletterToken(token, "confirm", secret, now);
    if (!subscriberId) return { state: "invalid", welcomeSent: false };

    const subscriber = this.database
      .prepare("SELECT id, email, status, confirmation_sent_at FROM subscribers WHERE id = ?")
      .get(subscriberId) as SubscriberRow | undefined;
    if (!subscriber || subscriber.status === "unsubscribed") return { state: "invalid", welcomeSent: false };
    if (subscriber.status === "active") return { state: "already_confirmed", welcomeSent: false };

    this.database
      .prepare(
        `UPDATE subscribers
         SET status = 'active', confirmed_at = ?, unsubscribed_at = NULL, updated_at = ?
         WHERE id = ?`,
      )
      .run(now.toISOString(), now.toISOString(), subscriber.id);

    const unsubscribeToken = createNewsletterToken(subscriber.id, "unsubscribe", secret);
    const unsubscribeUrl = `${this.siteUrl}/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeToken)}`;
    let welcomeSent = false;
    try {
      await this.sendEmail(welcomeEmail(subscriber.email, this.siteUrl, unsubscribeUrl), `newsletter-welcome-${subscriber.id}`);
      welcomeSent = true;
    } catch (error) {
      console.error("Failed to send newsletter welcome email", error);
    }

    return { state: "confirmed", welcomeSent };
  }

  unsubscribe(token: string, now = new Date()): "unsubscribed" | "already_unsubscribed" | "invalid" {
    const secret = requireTokenSecret(this.environment);
    const subscriberId = verifyNewsletterToken(token, "unsubscribe", secret, now);
    if (!subscriberId) return "invalid";

    const subscriber = this.database
      .prepare("SELECT id, email, status, confirmation_sent_at FROM subscribers WHERE id = ?")
      .get(subscriberId) as SubscriberRow | undefined;
    if (!subscriber) return "invalid";
    if (subscriber.status === "unsubscribed") return "already_unsubscribed";

    this.database
      .prepare(
        `UPDATE subscribers
         SET status = 'unsubscribed', unsubscribed_at = ?, updated_at = ?
         WHERE id = ?`,
      )
      .run(now.toISOString(), now.toISOString(), subscriber.id);
    return "unsubscribed";
  }

  async sendDailyDigest(now = new Date()): Promise<DigestResult> {
    const secret = requireTokenSecret(this.environment);
    const edition = formatIstanbulDate(now);
    const cutoff = now.getTime() - 30 * 60 * 60 * 1000;
    const articleRows = this.database
      .prepare(
        `SELECT a.title, a.slug, a.excerpt, a.published_at, c.name AS category_name
         FROM articles a
         JOIN categories c ON c.id = a.category_id
         WHERE a.status = 'published'
         ORDER BY a.published_at DESC
         LIMIT 20`,
      )
      .all() as ArticleRow[];
    const articles = articleRows
      .filter((article) => {
        const publishedAt = parseArticleTimestamp(article.published_at);
        return Number.isFinite(publishedAt) && publishedAt <= now.getTime() && publishedAt >= cutoff;
      })
      .slice(0, 5)
      .map(toPublicArticle);

    const result: DigestResult = { edition, articles: articles.length, sent: 0, skipped: 0, failed: 0 };
    if (articles.length === 0) return result;

    const subscribers = this.database
      .prepare("SELECT id, email, status, confirmation_sent_at FROM subscribers WHERE status = 'active' ORDER BY id")
      .all() as SubscriberRow[];

    for (const [index, subscriber] of subscribers.entries()) {
      const delivery = this.database
        .prepare("SELECT status FROM newsletter_deliveries WHERE subscriber_id = ? AND edition_key = ?")
        .get(subscriber.id, edition) as { status: string } | undefined;
      if (delivery?.status === "sent") {
        result.skipped += 1;
        continue;
      }

      this.database
        .prepare(
          `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, created_at)
           VALUES (?, ?, 'sending', ?)
           ON CONFLICT(subscriber_id, edition_key)
           DO UPDATE SET status = 'sending', error = NULL`,
        )
        .run(subscriber.id, edition, now.toISOString());

      const unsubscribeToken = createNewsletterToken(subscriber.id, "unsubscribe", secret);
      const unsubscribeUrl = `${this.siteUrl}/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeToken)}`;
      try {
        const deliveryResult = await this.sendEmail(
          digestEmail(subscriber.email, articles, this.siteUrl, unsubscribeUrl),
          `newsletter-digest-${edition}-${subscriber.id}`,
        );
        this.database
          .prepare(
            `UPDATE newsletter_deliveries
             SET status = 'sent', provider_message_id = ?, sent_at = ?, error = NULL
             WHERE subscriber_id = ? AND edition_key = ?`,
          )
          .run(deliveryResult.id, now.toISOString(), subscriber.id, edition);
        result.sent += 1;
      } catch (error) {
        const message = error instanceof Error ? error.message.slice(0, 500) : "Unknown delivery error";
        this.database
          .prepare(
            `UPDATE newsletter_deliveries
             SET status = 'failed', error = ?
             WHERE subscriber_id = ? AND edition_key = ?`,
          )
          .run(message, subscriber.id, edition);
        result.failed += 1;
      }

      if (index < subscribers.length - 1) {
        await new Promise((resolve) => setTimeout(resolve, 550));
      }
    }

    return result;
  }
}
