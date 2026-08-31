const DEFAULT_SITE_ORIGIN = "https://www.aiandtech.news";
const DEFAULT_API_ORIGIN = "https://technews.subtunnel.dev";
const DEFAULT_TIMEOUT_MS = 15_000;

function normalizedOrigin(value) {
  return new URL(value).origin;
}

async function fetchRequired(fetchImplementation, url, label, timeoutMs) {
  const response = await fetchImplementation(url, {
    headers: { "User-Agent": "aiandtechnews-production-smoke/1.0" },
    redirect: "follow",
    signal: AbortSignal.timeout(timeoutMs),
  });

  if (!response.ok) {
    const detail = (await response.text()).trim().slice(0, 200);
    throw new Error(`${label} returned HTTP ${response.status}${detail ? `: ${detail}` : ""}`);
  }

  return response;
}

export function extractFirstArticlePath(homepageHtml) {
  const match = homepageHtml.match(/href=["'](\/article\/[^"'?#]+)(?:[?#][^"']*)?["']/i);
  if (!match) throw new Error("Homepage does not link to an article");
  return match[1];
}

export function countSitemapArticleUrls(sitemapXml) {
  return [...sitemapXml.matchAll(/<loc>https:\/\/www\.aiandtech\.news\/article\/[^<]+<\/loc>/gi)].length;
}

export async function runProductionSmoke(options = {}) {
  const fetchImplementation = options.fetchImplementation || fetch;
  const siteOrigin = normalizedOrigin(options.siteOrigin || process.env.TECHNEWS_SITE_ORIGIN || DEFAULT_SITE_ORIGIN);
  const apiOrigin = normalizedOrigin(options.apiOrigin || process.env.TECHNEWS_API_ORIGIN || DEFAULT_API_ORIGIN);
  const timeoutMs = options.timeoutMs || DEFAULT_TIMEOUT_MS;
  const checks = [];

  const healthResponse = await fetchRequired(
    fetchImplementation,
    `${apiOrigin}/api/health`,
    "Backend health check",
    timeoutMs,
  );
  const health = await healthResponse.json();
  if (health?.status !== "ok") throw new Error("Backend health response did not report status ok");
  checks.push("backend health");

  const articlesResponse = await fetchRequired(
    fetchImplementation,
    `${apiOrigin}/api/articles?limit=1`,
    "Backend article listing",
    timeoutMs,
  );
  const articles = await articlesResponse.json();
  if (!Array.isArray(articles?.articles) || articles.articles.length === 0) {
    throw new Error("Backend article listing returned no published articles");
  }
  checks.push("backend articles");

  const homepageResponse = await fetchRequired(fetchImplementation, siteOrigin, "Homepage", timeoutMs);
  const articlePath = extractFirstArticlePath(await homepageResponse.text());
  checks.push("homepage article link");

  const articleResponse = await fetchRequired(
    fetchImplementation,
    new URL(articlePath, siteOrigin).toString(),
    "Homepage article page",
    timeoutMs,
  );
  const articleHtml = await articleResponse.text();
  if (!/<article(?:\s|>)/i.test(articleHtml)) throw new Error("Homepage article page is missing article content");
  checks.push("article page");

  const sitemapResponse = await fetchRequired(
    fetchImplementation,
    `${siteOrigin}/sitemap.xml`,
    "Sitemap",
    timeoutMs,
  );
  const articleUrlCount = countSitemapArticleUrls(await sitemapResponse.text());
  if (articleUrlCount === 0) throw new Error("Sitemap contains no article URLs");
  checks.push(`sitemap articles (${articleUrlCount})`);

  const newsletterResponse = await fetchRequired(
    fetchImplementation,
    `${apiOrigin}/api/newsletter/editions?limit=1`,
    "Newsletter archive API",
    timeoutMs,
  );
  const newsletter = await newsletterResponse.json();
  if (!Array.isArray(newsletter?.editions)) throw new Error("Newsletter archive API returned an invalid payload");
  checks.push("newsletter archive API");

  return { checks, articlePath, articleUrlCount };
}

if (import.meta.url === new URL(process.argv[1], "file:").href) {
  try {
    const result = await runProductionSmoke();
    console.log(`Production smoke check passed: ${result.checks.join(", ")}`);
  } catch (error) {
    console.error(`Production smoke check failed: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  }
}
