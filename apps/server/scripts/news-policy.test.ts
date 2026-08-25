import assert from "node:assert/strict";
import test from "node:test";
import {
  APPROVED_FEEDS,
  getAutomaticItemRejectionReason,
  getItemRejectionReason,
  normalizeSourceUrl,
  slugify,
  sourceForUrl,
  validateRewrittenArticle,
} from "./news-policy";

const validParagraph = "The company shared a detailed product update with customers and developers today. The release changes how teams use the service while keeping its existing tools available. Executives described the update without announcing new pricing or making unsupported performance claims. Customers can review the published documentation before deciding whether the changes fit their work.";

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
  assert.equal(sourceForUrl("https://www.wired.com/story/example"), "WIRED");
  assert.equal(sourceForUrl("https://www.engadget.com/example"), "Engadget");
  assert.equal(sourceForUrl("https://www.bleepingcomputer.com/news/security/example"), "BleepingComputer");
  assert.equal(sourceForUrl("https://www.theregister.com/2026/08/16/example"), "The Register");
  assert.equal(sourceForUrl("https://news.ycombinator.com/item?id=1"), null);
});

test("includes the expanded approved RSS feed set", () => {
  assert.deepEqual(
    APPROVED_FEEDS.map((feed) => feed.source),
    ["TechCrunch", "The Verge", "Ars Technica", "WIRED", "Engadget", "BleepingComputer", "The Register"],
  );
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

test("rejects the Samsung preorder promotion from its headline and canonical URL", () => {
  const title = "Last Chance For Samsung Galaxy Z Fold 8 And Flip 8 Preorder Bonuses";
  const url = "https://www.theverge.com/gadgets/976103/samsung-galaxy-z-fold-flip-8-preorder-airpods-pro-3-deal-sale";

  assert.match(getItemRejectionReason(title, url, "The Verge") || "", /promotional/i);
});

test("automatic imports reject non-tech entertainment while manual policy still allows it", () => {
  const title = "Spider-Man: Brand New Day Smashes Box Office Records With $1 Billion Worldwide Opening";
  const url = "https://www.theverge.com/entertainment/975297/spider-man-brand-new-day-marvel-sony-xmen-doomsday";

  assert.equal(getItemRejectionReason(title, url, "The Verge"), null);
  assert.match(getAutomaticItemRejectionReason(title, url, "The Verge") || "", /entertainment|not clearly technology-related/i);
});

test("automatic imports accept clearly technology-related stories", () => {
  assert.equal(
    getAutomaticItemRejectionReason(
      "Apple releases a macOS security update",
      "https://www.theverge.com/tech/975300/apple-macos-security-update",
      "The Verge",
    ),
    null,
  );
  assert.equal(
    getAutomaticItemRejectionReason(
      "OpenAI launches a new model for developers",
      "https://techcrunch.com/2026/08/05/openai-launches-a-new-model-for-developers/",
      "TechCrunch",
    ),
    null,
  );
});

test("automatic imports fail closed when a general-feed story has no technology signal", () => {
  assert.match(
    getAutomaticItemRejectionReason(
      "Summer travel destinations attracting record crowds",
      "https://techcrunch.com/2026/08/05/summer-travel-destinations/",
      "TechCrunch",
    ) || "",
    /not clearly technology-related/i,
  );
});

test("automatic imports reject weak stories that escaped through generic URL or title signals", () => {
  assert.match(
    getAutomaticItemRejectionReason(
      "Ted Lasso Returns for Season Four Alongside New Tech and App Releases",
      "https://www.theverge.com/tech/977084/ted-lasso-bose-tony-installer",
      "The Verge",
    ) || "",
    /review, guide, or roundup|entertainment/i,
  );
  assert.match(
    getAutomaticItemRejectionReason(
      "NASA Perseverance Rover Nears Mars Distance Record",
      "https://arstechnica.com/space/2026/08/the-first-self-driving-vehicle-on-mars-has-proven-to-be-a-smashing-success",
      "Ars Technica",
    ) || "",
    /not clearly technology-related/i,
  );
});

test("automatic imports reject entertainment even when a generic technology word is present", () => {
  assert.match(
    getAutomaticItemRejectionReason(
      "Spider-Man Season Four Arrives With a New Mobile App",
      "https://www.theverge.com/tech/975297/spider-man-season-four-mobile-app",
      "The Verge",
    ) || "",
    /entertainment/i,
  );
});

test("news policy rejects reviews and recurring mixed-topic roundups", () => {
  assert.match(
    getItemRejectionReason(
      "Review: The $450 Chuwi UniBook Laptop Falls Short",
      "https://www.theverge.com/tech/977031/chuwi-unibook-laptop-intel-wildcat-lake-review",
      "The Verge",
    ) || "",
    /rather than news/i,
  );
  assert.match(
    getItemRejectionReason(
      "New apps and hardware to try this weekend",
      "https://www.theverge.com/tech/977084/apps-hardware-installer",
      "The Verge",
    ) || "",
    /rather than news/i,
  );
});

test("creates deterministic slugs", () => {
  assert.equal(slugify("OpenAI's New API: What Changed?"), "openai-s-new-api-what-changed");
});

test("accepts an article that follows the writing contract", () => {
  assert.deepEqual(validateRewrittenArticle(validArticle()), []);
});

test("enforces the 150 to 300 word range", () => {
  const article = validArticle();
  const paragraph = (count: number) => Array.from({ length: count }, (_, index) => `word${index}`).join(" ");

  article.content = `<p>${paragraph(29)}</p>` + Array.from({ length: 4 }, () => `<p>${paragraph(30)}</p>`).join("");
  assert.match(validateRewrittenArticle(article).join("\n"), /150 to 300 words/i);

  article.content = Array.from({ length: 5 }, () => `<p>${paragraph(30)}</p>`).join("");
  assert.doesNotMatch(validateRewrittenArticle(article).join("\n"), /150 to 300 words/i);

  article.content = Array.from({ length: 5 }, () => `<p>${paragraph(60)}</p>`).join("");
  assert.doesNotMatch(validateRewrittenArticle(article).join("\n"), /150 to 300 words/i);

  article.content = `<p>${paragraph(61)}</p>` + Array.from({ length: 4 }, () => `<p>${paragraph(60)}</p>`).join("");
  assert.match(validateRewrittenArticle(article).join("\n"), /150 to 300 words/i);
});

test("rejects a promotional headline produced during rewriting", () => {
  const article = validArticle();
  article.title = "Last Chance For Samsung Galaxy Z Fold 8 And Flip 8 Preorder Bonuses";

  assert.match(validateRewrittenArticle(article).join("\n"), /promotional/i);
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
  assert.match(errors, /150 to 300 words/i);
});
