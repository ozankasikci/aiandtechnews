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
import {
  createApprovedAgedOutCandidate,
  extractSourceText,
  parseManualImporterArgs,
  resolveApprovedArticleRedirect,
  selectManualCandidate,
  verifyApprovedAgedOutPageMetadata,
} from "./news-importer";

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
  assert.equal(sourceForUrl("https://www.technologyreview.com/2026/08/28/example"), "MIT Technology Review");
  assert.equal(sourceForUrl("https://venturebeat.com/ai/example"), "VentureBeat");
  assert.equal(sourceForUrl("https://www.404media.co/example"), "404 Media");
  assert.equal(sourceForUrl("https://restofworld.org/2026/example"), "Rest of World");
  assert.equal(sourceForUrl("https://decrypt.co/378101/example"), "Decrypt");
  assert.equal(sourceForUrl("https://the-decoder.com/example"), "The Decoder");
  assert.equal(sourceForUrl("https://www.zdnet.com/article/example"), "ZDNET");
  assert.equal(sourceForUrl("https://www.infoq.com/news/example"), "InfoQ");
  assert.equal(sourceForUrl("https://spectrum.ieee.org/example"), "IEEE Spectrum");
  assert.equal(sourceForUrl("https://siliconangle.com/example"), "SiliconANGLE");
  assert.equal(sourceForUrl("https://aibusiness.com/example"), "AI Business");
  assert.equal(sourceForUrl("https://www.sciencedaily.com/releases/example.htm"), "ScienceDaily");
  assert.equal(sourceForUrl("https://news.ycombinator.com/item?id=1"), null);
});

test("includes the expanded approved RSS feed set", () => {
  assert.deepEqual(
    APPROVED_FEEDS.map((feed) => feed.source),
    [
      "TechCrunch",
      "The Verge",
      "Ars Technica",
      "WIRED",
      "Engadget",
      "BleepingComputer",
      "The Register",
      "MIT Technology Review",
      "VentureBeat",
      "404 Media",
      "Rest of World",
      "Decrypt",
      "The Decoder",
      "ZDNET",
      "InfoQ",
      "IEEE Spectrum",
      "SiliconANGLE",
      "AI Business",
      "ScienceDaily",
    ],
  );
});

test("allows an explicitly approved fresh item that aged out of its RSS window", () => {
  const candidate = createApprovedAgedOutCandidate(
    "https://the-decoder.com/simulated-students-that-make-realistic-mistakes-help-ai-tutors-learn-faster/",
    {
      title: "Simulated students that make realistic mistakes help AI tutors learn faster",
      publishedAt: "2026-09-20T09:50:32.000Z",
    },
    new Date("2026-09-21T18:30:00.000Z"),
  );
  assert.deepEqual(candidate, {
    title: "Simulated students that make realistic mistakes help AI tutors learn faster",
    url: "https://the-decoder.com/simulated-students-that-make-realistic-mistakes-help-ai-tutors-learn-faster",
    source: "The Decoder",
    publishedAt: "2026-09-20T09:50:32.000Z",
    verifyPageMetadata: true,
  });
});

test("explicit aged-out approval remains bounded by source, metadata, and freshness", () => {
  const now = new Date("2026-09-21T18:30:00.000Z");
  assert.throws(
    () => createApprovedAgedOutCandidate("https://example.com/ai-story", { title: "AI story", publishedAt: "2026-09-21T10:00:00Z" }, now),
    /approved RSS publisher/,
  );
  assert.throws(
    () => createApprovedAgedOutCandidate("https://the-decoder.com/ai-story", { title: "", publishedAt: "2026-09-21T10:00:00Z" }, now),
    /approved title/,
  );
  assert.throws(
    () => createApprovedAgedOutCandidate("https://the-decoder.com/ai-story", { title: "AI story", publishedAt: "invalid" }, now),
    /valid publication timestamp/,
  );
  assert.throws(
    () => createApprovedAgedOutCandidate("https://the-decoder.com/ai-story", { title: "AI story", publishedAt: "2026-09-14T18:29:59Z" }, now),
    /seven days/,
  );
  assert.throws(
    () => createApprovedAgedOutCandidate("https://the-decoder.com/ai-story", { title: "AI story", publishedAt: "2026-09-21T18:31:00Z" }, now),
    /future/,
  );
});

test("aged-out approval metadata must match authoritative page metadata", () => {
  const item = createApprovedAgedOutCandidate(
    "https://the-decoder.com/ai-story",
    { title: "AI tutors learn from simulated students", publishedAt: "2026-09-20T09:50:32Z" },
    new Date("2026-09-21T18:30:00Z"),
  );
  const html = `
    <meta property="og:title" content="AI tutors learn from simulated students">
    <meta property="article:published_time" content="2026-09-20T09:50:32+00:00">
  `;
  assert.equal(verifyApprovedAgedOutPageMetadata(html, item, new Date("2026-09-21T18:30:00Z")), null);
  assert.match(verifyApprovedAgedOutPageMetadata(html.replace("AI tutors", "Robots"), item, new Date("2026-09-21T18:30:00Z")) || "", /headline/i);
  assert.match(verifyApprovedAgedOutPageMetadata(html.replace("2026-09-20T09:50:32+00:00", "2026-09-10T09:50:32+00:00"), item, new Date("2026-09-21T18:30:00Z")) || "", /timestamp/i);
  assert.match(verifyApprovedAgedOutPageMetadata("<html></html>", item, new Date("2026-09-21T18:30:00Z")) || "", /metadata/i);
});

test("manual importer CLI fails closed on malformed or split approval arguments", () => {
  assert.deepEqual(parseManualImporterArgs(["https://the-decoder.com/ai-story"]), {
    articleUrl: "https://the-decoder.com/ai-story",
    approval: undefined,
  });
  assert.deepEqual(parseManualImporterArgs([
    "https://the-decoder.com/ai-story",
    "--approved-aged-out",
    "2026-09-20T09:50:32Z",
    "AI tutors learn from simulated students",
  ]), {
    articleUrl: "https://the-decoder.com/ai-story",
    approval: {
      publishedAt: "2026-09-20T09:50:32Z",
      title: "AI tutors learn from simulated students",
    },
  });
  assert.throws(() => parseManualImporterArgs([]), /Usage/);
  assert.throws(() => parseManualImporterArgs(["https://the-decoder.com/ai-story", "extra"]), /Usage/);
  assert.throws(() => parseManualImporterArgs([
    "https://the-decoder.com/ai-story",
    "--approved-aged-out",
    "2026-09-20T09:50:32Z",
    "AI",
    "tutors",
  ]), /Usage/);
});

test("approved article redirects fail closed before requesting an unapproved destination", () => {
  assert.equal(
    resolveApprovedArticleRedirect(
      "https://the-decoder.com/ai-story",
      "/canonical-ai-story",
      "The Decoder",
    ),
    "https://the-decoder.com/canonical-ai-story",
  );
  assert.throws(
    () => resolveApprovedArticleRedirect(
      "https://the-decoder.com/ai-story",
      "http://127.0.0.1:8000/private",
      "The Decoder",
    ),
    /outside The Decoder/,
  );
  assert.throws(
    () => resolveApprovedArticleRedirect(
      "https://the-decoder.com/ai-story",
      "https://example.com/spoofed-article",
      "The Decoder",
    ),
    /outside The Decoder/,
  );
});

test("aged-out approval assertions take precedence even if the URL remains in the feed", () => {
  const url = "https://the-decoder.com/ai-story";
  const feedItems = [{ title: "AI feed title", url, source: "The Decoder", publishedAt: "2026-09-20T09:00:00Z" }];
  const selected = selectManualCandidate(
    url,
    feedItems,
    { title: "AI explicitly approved title", publishedAt: "2026-09-20T10:00:00Z" },
    new Date("2026-09-21T18:30:00Z"),
  );
  assert.equal(selected?.title, "AI explicitly approved title");
  assert.equal(selected?.verifyPageMetadata, true);
});

test("extracts the richest article scope when a short article card precedes the real story", () => {
  const storyParagraph = "OpenAI users reported a change in model behavior after the latest release, and developers shared detailed examples of outputs that no longer matched their earlier results. The company has not confirmed a deliberate capability reduction, so the evidence remains based on user reports and comparative testing.";
  const html = `<html><body><article><p>Short card.</p></article><main>${Array.from({ length: 5 }, (_, index) => `<p>${storyParagraph} Reported example ${index + 1} describes a distinct test performed by a different user.</p>`).join("")}</main></body></html>`;

  const extracted = extractSourceText(html);

  assert.ok(extracted.length >= 800);
  assert.match(extracted, /developers shared detailed examples/);
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
  assert.equal(getItemRejectionReason("OpenAI updates ChatGPT", "https://www.theverge.com/ai-artificial-intelligence/openai-chatgpt", "The Verge", current), null);
});

test("rejects the Samsung preorder promotion from its headline and canonical URL", () => {
  const title = "Last Chance For Samsung Galaxy Z Fold 8 And Flip 8 Preorder Bonuses";
  const url = "https://www.theverge.com/gadgets/976103/samsung-galaxy-z-fold-flip-8-preorder-airpods-pro-3-deal-sale";

  assert.match(getItemRejectionReason(title, url, "The Verge") || "", /promotional/i);
});

test("allows corporate agreements that use deal as a transaction term", () => {
  const title = "Anthropic continues compute-gobbling streak in $45B deal with Nscale";
  const url = "https://techcrunch.com/2026/08/26/anthropic-continues-compute-gobbling-streak-in-45-billion-deal-with-nscale/";

  assert.equal(getItemRejectionReason(title, url, "TechCrunch"), null);
});

test("all publishing paths reject non-AI stories, including otherwise valid technology news", () => {
  const entertainmentTitle = "Spider-Man: Brand New Day Smashes Box Office Records With $1 Billion Worldwide Opening";
  const entertainmentUrl = "https://www.theverge.com/entertainment/975297/spider-man-brand-new-day-marvel-sony-xmen-doomsday";
  const techTitle = "Apple releases a macOS security update";
  const techUrl = "https://www.theverge.com/tech/975300/apple-macos-security-update";

  assert.match(getItemRejectionReason(entertainmentTitle, entertainmentUrl, "The Verge") || "", /not clearly AI-related/i);
  assert.match(getAutomaticItemRejectionReason(entertainmentTitle, entertainmentUrl, "The Verge") || "", /not clearly AI-related/i);
  assert.match(getItemRejectionReason(techTitle, techUrl, "The Verge") || "", /not clearly AI-related/i);
  assert.match(getAutomaticItemRejectionReason(techTitle, techUrl, "The Verge") || "", /not clearly AI-related/i);
});

test("all publishing paths accept clearly AI-related stories", () => {
  const title = "OpenAI launches a new model for developers";
  const url = "https://techcrunch.com/2026/08/05/openai-launches-a-new-model-for-developers/";

  assert.equal(getItemRejectionReason(title, url, "TechCrunch"), null);
  assert.equal(getAutomaticItemRejectionReason(title, url, "TechCrunch"), null);
  assert.equal(
    getItemRejectionReason(
      "New safeguards arrive for frontier models",
      "https://www.technologyreview.com/ai/2026/09/11/frontier-model-safeguards/",
      "MIT Technology Review",
    ),
    null,
  );
});

test("all publishing paths fail closed when a story has no AI signal", () => {
  const title = "Summer travel destinations attracting record crowds";
  const url = "https://techcrunch.com/2026/08/05/summer-travel-destinations/";

  assert.match(getItemRejectionReason(title, url, "TechCrunch") || "", /not clearly AI-related/i);
  assert.match(getAutomaticItemRejectionReason(title, url, "TechCrunch") || "", /not clearly AI-related/i);
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
    /not clearly AI-related/i,
  );
});

test("automatic imports reject entertainment even when a generic technology word is present", () => {
  assert.match(
    getAutomaticItemRejectionReason(
      "Spider-Man Season Four Arrives With a New Mobile App",
      "https://www.theverge.com/tech/975297/spider-man-season-four-mobile-app",
      "The Verge",
    ) || "",
    /not clearly AI-related/i,
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

test("enforces the 150 to 800 word range", () => {
  const article = validArticle();
  const paragraph = (count: number) => Array.from({ length: count }, (_, index) => `word${index}`).join(" ");

  // 149 words: one under the floor.
  article.content = `<p>${paragraph(29)}</p>` + Array.from({ length: 4 }, () => `<p>${paragraph(30)}</p>`).join("");
  assert.match(validateRewrittenArticle(article).join("\n"), /150 to 800 words/i);

  // 150 words: exactly on the floor.
  article.content = Array.from({ length: 5 }, () => `<p>${paragraph(30)}</p>`).join("");
  assert.doesNotMatch(validateRewrittenArticle(article).join("\n"), /150 to 800 words/i);

  // 800 words across 10 paragraphs: exactly on the raised ceiling.
  article.content = Array.from({ length: 10 }, () => `<p>${paragraph(80)}</p>`).join("");
  assert.doesNotMatch(validateRewrittenArticle(article).join("\n"), /150 to 800 words/i);

  // 801 words: one over the ceiling.
  article.content = `<p>${paragraph(81)}</p>` + Array.from({ length: 9 }, () => `<p>${paragraph(80)}</p>`).join("");
  assert.match(validateRewrittenArticle(article).join("\n"), /150 to 800 words/i);

  // The old 300-word ceiling must no longer reject a mid-length article.
  article.content = Array.from({ length: 8 }, () => `<p>${paragraph(60)}</p>`).join("");
  assert.deepEqual(validateRewrittenArticle(article), []);
});

test("enforces the 5 to 12 paragraph range", () => {
  const article = validArticle();
  const paragraph = (count: number) => Array.from({ length: count }, (_, index) => `word${index}`).join(" ");

  // 12 paragraphs is allowed; 13 is not.
  article.content = Array.from({ length: 12 }, () => `<p>${paragraph(40)}</p>`).join("");
  assert.doesNotMatch(validateRewrittenArticle(article).join("\n"), /paragraphs/i);

  article.content = Array.from({ length: 13 }, () => `<p>${paragraph(40)}</p>`).join("");
  assert.match(validateRewrittenArticle(article).join("\n"), /5 to 12 paragraphs/i);
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
  assert.match(errors, /5 to 12 paragraphs/i);
  assert.match(errors, /150 to 800 words/i);
});
