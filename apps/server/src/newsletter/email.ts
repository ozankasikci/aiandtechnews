export interface NewsletterEmail {
  to: string;
  subject: string;
  html: string;
  text: string;
  headers?: Record<string, string>;
  tags?: { name: string; value: string }[];
}

export interface EmailDelivery {
  id: string;
}

export type EmailSender = (email: NewsletterEmail, idempotencyKey: string) => Promise<EmailDelivery>;

export class NewsletterConfigurationError extends Error {}

export function createResendSender(environment: NodeJS.ProcessEnv = process.env): EmailSender {
  return async (email, idempotencyKey) => {
    const apiKey = environment.RESEND_API_KEY?.trim();
    const from = environment.NEWSLETTER_FROM?.trim();
    if (!apiKey || !from) {
      throw new NewsletterConfigurationError("Newsletter delivery is not configured");
    }

    const response = await fetch("https://api.resend.com/emails", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify({
        from,
        to: [email.to],
        subject: email.subject,
        html: email.html,
        text: email.text,
        ...(environment.NEWSLETTER_REPLY_TO?.trim()
          ? { reply_to: environment.NEWSLETTER_REPLY_TO.trim() }
          : {}),
        ...(email.headers ? { headers: email.headers } : {}),
        ...(email.tags ? { tags: email.tags } : {}),
      }),
    });

    const body = (await response.json().catch(() => ({}))) as { id?: string; message?: string; error?: string };
    if (!response.ok || !body.id) {
      throw new Error(body.message || body.error || `Email provider returned ${response.status}`);
    }
    return { id: body.id };
  };
}

export function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function emailLayout(content: string, footer: string): string {
  return `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b10;color:#f5f5f7;font-family:Arial,sans-serif">
    <div style="max-width:620px;margin:0 auto;padding:36px 20px">
      <div style="font-size:12px;font-weight:800;letter-spacing:.16em;text-transform:uppercase;color:#9b87f5;margin-bottom:24px">AI &amp; Tech News</div>
      <div style="background:#15151d;border:1px solid #292936;border-radius:8px;padding:28px">${content}</div>
      <div style="color:#8f8f9c;font-size:12px;line-height:1.6;padding:20px 4px">${footer}</div>
    </div>
  </body>
</html>`;
}

function button(label: string, url: string): string {
  return `<a href="${escapeHtml(url)}" style="display:inline-block;background:#7c5cff;color:#fff;text-decoration:none;font-weight:700;border-radius:5px;padding:13px 20px">${escapeHtml(label)}</a>`;
}

export function confirmationEmail(to: string, confirmUrl: string): NewsletterEmail {
  const content = `
    <h1 style="margin:0 0 14px;font-size:26px;line-height:1.2">Confirm your subscription</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 22px">Confirm that you want the most useful AI and technology stories delivered to your inbox.</p>
    ${button("Confirm subscription", confirmUrl)}
    <p style="color:#8f8f9c;font-size:12px;line-height:1.5;margin:22px 0 0">This link expires in 48 hours. If you did not request this email, you can ignore it.</p>`;
  return {
    to,
    subject: "Confirm your AI & Tech News subscription",
    html: emailLayout(content, "You received this confirmation because someone entered this address at aiandtech.news."),
    text: `Confirm your AI & Tech News subscription:\n${confirmUrl}\n\nThis link expires in 48 hours. If you did not request this email, ignore it.`,
    tags: [{ name: "email_type", value: "confirmation" }],
  };
}

export function welcomeEmail(to: string, siteUrl: string, unsubscribeUrl: string): NewsletterEmail {
  const content = `
    <h1 style="margin:0 0 14px;font-size:26px;line-height:1.2">Welcome to AI &amp; Tech News</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 22px">Your subscription is confirmed. We will send you a concise daily digest of the AI and technology stories worth knowing.</p>
    ${button("Read the latest stories", siteUrl)}`;
  return {
    to,
    subject: "Welcome to AI & Tech News",
    html: emailLayout(content, `You can <a href="${escapeHtml(unsubscribeUrl)}" style="color:#b7a7ff">unsubscribe at any time</a>.`),
    text: `Welcome to AI & Tech News. Your subscription is confirmed.\n\nRead the latest stories: ${siteUrl}\n\nUnsubscribe: ${unsubscribeUrl}`,
    headers: {
      "List-Unsubscribe": `<${unsubscribeUrl}>`,
      "List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
    },
    tags: [{ name: "email_type", value: "welcome" }],
  };
}

export interface DigestArticle {
  title: string;
  slug: string;
  excerpt: string;
  category: string;
}

export function digestEmail(
  to: string,
  articles: DigestArticle[],
  siteUrl: string,
  unsubscribeUrl: string,
): NewsletterEmail {
  const articleHtml = articles
    .map((article) => {
      const url = `${siteUrl}/article/${encodeURIComponent(article.slug)}`;
      return `<div style="padding:18px 0;border-top:1px solid #292936">
        <div style="color:#9b87f5;font-size:11px;font-weight:800;letter-spacing:.12em;text-transform:uppercase;margin-bottom:7px">${escapeHtml(article.category)}</div>
        <a href="${escapeHtml(url)}" style="color:#f5f5f7;text-decoration:none;font-size:20px;line-height:1.3;font-weight:800">${escapeHtml(article.title)}</a>
        <p style="color:#b8b8c2;line-height:1.55;margin:8px 0 0">${escapeHtml(article.excerpt)}</p>
      </div>`;
    })
    .join("");
  const textArticles = articles
    .map((article) => `${article.title}\n${article.excerpt}\n${siteUrl}/article/${encodeURIComponent(article.slug)}`)
    .join("\n\n");

  const content = `
    <h1 style="margin:0 0 8px;font-size:26px;line-height:1.2">Today in AI and technology</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 14px">The stories worth knowing, selected from today&apos;s coverage.</p>
    ${articleHtml}
    <div style="padding-top:22px">${button("See all stories", siteUrl)}</div>`;
  return {
    to,
    subject: `${articles.length} AI and tech stories worth knowing today`,
    html: emailLayout(content, `You subscribed at aiandtech.news. <a href="${escapeHtml(unsubscribeUrl)}" style="color:#b7a7ff">Unsubscribe</a>.`),
    text: `Today in AI and technology\n\n${textArticles}\n\nSee all stories: ${siteUrl}\n\nUnsubscribe: ${unsubscribeUrl}`,
    headers: {
      "List-Unsubscribe": `<${unsubscribeUrl}>`,
      "List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
    },
    tags: [{ name: "email_type", value: "daily_digest" }],
  };
}
