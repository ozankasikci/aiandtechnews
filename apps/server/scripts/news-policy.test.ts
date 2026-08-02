import assert from "node:assert/strict";
import test from "node:test";
import {
  getItemRejectionReason,
  normalizeSourceUrl,
  slugify,
  sourceForUrl,
  validateRewrittenArticle,
} from "./news-policy";

const validParagraph = "The company shared a detailed product update with customers and developers today. The release changes how teams use the service while keeping its existing tools available. Executives described the update without announcing new pricing or making unsupported performance claims. Customers can review the published documentation before deciding whether the changes fit their work. The company said a broader rollout will follow after the first group completes testing. That schedule gives teams time to compare the update with their current systems and requirements.";

function validArticle() {
  return {
    title: "OpenAI Updates Its Developer Platform",
    excerpt: "OpenAI has updated its developer platform with new controls for teams.",
    content: Array.from({ length: 5 }, () => `<p>${validParagraph}</p>`).join(""),
  };
}

test("recognizes only approved publication domains", () => {
  assert.equal(sourceForUrl("https://techcrunch.com/example"), "TechCrunch");
  assert.equal(sourceForUrl("https://www.theverge.com/tech/example"), "The Verge");
  assert.equal(sourceForUrl("https://arstechnica.com/ai/example"), "Ars Technica");
  assert.equal(sourceForUrl("https://news.ycombinator.com/item?id=1"), null);
});

test("normalizes common tracking parameters without changing the article path", () => {
  assert.equal(
    normalizeSourceUrl("https://techcrunch.com/story/?utm_source=rss&ref=home#section"),
    "https://techcrunch.com/story?ref=home",
  );
});

test("rejects low-quality, promotional, and old items", () => {
  const current = new Date("2026-08-02T00:00:00Z");
  assert.match(getItemRejectionReason("Show HN: My app", "https://techcrunch.com/app", "TechCrunch", current) || "", /Show HN/);
  assert.match(getItemRejectionReason("The best laptop deals", "https://www.theverge.com/deals/laptops", "The Verge", current) || "", /deal/i);
  assert.match(getItemRejectionReason("Research paper [PDF]", "https://arstechnica.com/science/paper", "Ars Technica", current) || "", /PDF/);
  assert.match(getItemRejectionReason("A classic operating system (2009)", "https://arstechnica.com/tech/os", "Ars Technica", current) || "", /old repost/);
  assert.equal(getItemRejectionReason("Apple updates macOS", "https://www.theverge.com/tech/apple-macos", "The Verge", current), null);
});

test("creates deterministic slugs", () => {
  assert.equal(slugify("OpenAI's New API: What Changed?"), "openai-s-new-api-what-changed");
});

test("accepts an article that follows the writing contract", () => {
  assert.deepEqual(validateRewrittenArticle(validArticle()), []);
});

test("rejects prohibited language, source footers, and invalid fields", () => {
  const article = validArticle();
  article.title = "A Groundbreaking Update!";
  article.excerpt = "First sentence. Second sentence.";
  article.content = article.content.replace(/<p>[^]*<\/p>$/, "<p>Source: Example</p>");
  const errors = validateRewrittenArticle(article).join("\n");
  assert.match(errors, /clickbait/i);
  assert.match(errors, /exactly one sentence/i);
  assert.match(errors, /groundbreaking/i);
  assert.match(errors, /source footer/i);
});

test("rejects invalid article HTML and length", () => {
  const article = validArticle();
  article.content = "<p>Too short.</p><h3>Unsupported</h3>";
  const errors = validateRewrittenArticle(article).join("\n");
  assert.match(errors, /other than p or h2/i);
  assert.match(errors, /5 to 8 paragraphs/i);
  assert.match(errors, /400 to 600 words/i);
});
